//go:build linux

// Package sysinfo 只依赖标准库 + /proc，零子进程开销。
// 这是 StarBT 低内存设计的关键：不 spawn ps/top/df。
package sysinfo

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// ---------- 内存 ----------

type MemInfo struct {
	TotalKB     uint64
	AvailKB     uint64
	SwapTotalKB uint64
	SwapFreeKB  uint64
}

func ReadMemInfo() (MemInfo, error) {
	var m MemInfo
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return m, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		v, _ := strconv.ParseUint(fields[1], 10, 64)
		switch strings.TrimSuffix(fields[0], ":") {
		case "MemTotal":
			m.TotalKB = v
		case "MemAvailable":
			m.AvailKB = v
		case "SwapTotal":
			m.SwapTotalKB = v
		case "SwapFree":
			m.SwapFreeKB = v
		}
	}
	return m, sc.Err()
}

// ---------- 负载 / 运行时长 ----------

type Load struct{ One, Five, Fifteen float64 }

func ReadLoad() (Load, error) {
	var l Load
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return l, err
	}
	f := strings.Fields(string(b))
	if len(f) < 3 {
		return l, fmt.Errorf("loadavg 格式异常")
	}
	l.One, _ = strconv.ParseFloat(f[0], 64)
	l.Five, _ = strconv.ParseFloat(f[1], 64)
	l.Fifteen, _ = strconv.ParseFloat(f[2], 64)
	return l, nil
}

func Uptime() (time.Duration, error) {
	b, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0, err
	}
	f := strings.Fields(string(b))
	if len(f) < 1 {
		return 0, fmt.Errorf("uptime 格式异常")
	}
	sec, _ := strconv.ParseFloat(f[0], 64)
	return time.Duration(sec * float64(time.Second)), nil
}

// ---------- 系统信息 ----------

type OSInfo struct {
	ID      string // debian / ubuntu / centos ...
	Version string // VERSION_ID
	Pretty  string // PRETTY_NAME
}

func ReadOSInfo() (OSInfo, error) {
	var o OSInfo
	f, err := os.Open("/etc/os-release")
	if err != nil {
		return o, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		v = strings.Trim(v, `"'`)
		switch k {
		case "ID":
			o.ID = v
		case "VERSION_ID":
			o.Version = v
		case "PRETTY_NAME":
			o.Pretty = v
		}
	}
	return o, sc.Err()
}

func utsStr(a []int8) string {
	b := make([]byte, 0, len(a))
	for _, c := range a {
		if c == 0 {
			break
		}
		b = append(b, byte(c))
	}
	return string(b)
}

func Kernel() (string, error) {
	var u syscall.Utsname
	if err := syscall.Uname(&u); err != nil {
		return "", err
	}
	return utsStr(u.Release[:]), nil
}

// ---------- 磁盘 ----------

type DiskInfo struct {
	TotalBytes uint64
	FreeBytes  uint64
}

func ReadDisk(path string) (DiskInfo, error) {
	var d DiskInfo
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return d, err
	}
	d.TotalBytes = st.Blocks * uint64(st.Bsize)
	d.FreeBytes = st.Bavail * uint64(st.Bsize)
	return d, nil
}

// ---------- CPU（双采样，算真实使用率） ----------

type cpuSample struct{ total, idle uint64 }

func readCPU() (cpuSample, error) {
	var s cpuSample
	b, err := os.ReadFile("/proc/stat")
	if err != nil {
		return s, err
	}
	line, _, _ := strings.Cut(string(b), "\n")
	if !strings.HasPrefix(line, "cpu ") {
		return s, fmt.Errorf("/proc/stat 无 cpu 行")
	}
	fields := strings.Fields(line)[1:]
	var total uint64
	for i, f := range fields {
		v, _ := strconv.ParseUint(f, 10, 64)
		total += v
		if i == 3 || i == 4 { // idle + iowait
			s.idle += v
		}
	}
	s.total = total
	return s, nil
}

// CPUUsage 两次采样间隔 d，返回这段时间的真实使用率(%)。
func CPUUsage(d time.Duration) (float64, error) {
	s1, err := readCPU()
	if err != nil {
		return 0, err
	}
	time.Sleep(d)
	s2, err := readCPU()
	if err != nil {
		return 0, err
	}
	dt := s2.total - s1.total
	if dt == 0 {
		return 0, nil
	}
	return float64(dt-(s2.idle-s1.idle)) / float64(dt) * 100, nil
}

// ---------- 网络（双采样速率） ----------

type netSample struct{ rx, tx uint64 }

func readNet() (netSample, error) {
	var s netSample
	f, err := os.Open("/proc/net/dev")
	if err != nil {
		return s, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		if lineNo <= 2 {
			continue
		}
		_, rest, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) < 9 {
			continue
		}
		rx, _ := strconv.ParseUint(fields[0], 10, 64)
		tx, _ := strconv.ParseUint(fields[8], 10, 64)
		s.rx += rx
		s.tx += tx
	}
	return s, sc.Err()
}

// NetRate 返回 d 时间内的平均速率（字节/秒）。
func NetRate(d time.Duration) (rxPerSec, txPerSec float64, err error) {
	s1, err := readNet()
	if err != nil {
		return 0, 0, err
	}
	time.Sleep(d)
	s2, err := readNet()
	if err != nil {
		return 0, 0, err
	}
	sec := d.Seconds()
	return float64(s2.rx-s1.rx) / sec, float64(s2.tx-s1.tx) / sec, nil
}

// ---------- 进程表 ----------

type Proc struct {
	PID   int
	Comm  string
	RSSKB uint64
}

func procComm(pidDir string) (string, uint64, error) {
	b, err := os.ReadFile(pidDir + "/stat")
	if err != nil {
		return "", 0, err
	}
	data := string(b)
	idx := strings.LastIndex(data, ")")
	if idx < 0 {
		return "", 0, fmt.Errorf("stat 格式异常")
	}
	comm := commAfterPid(data[:idx])
	fields := strings.Fields(data[idx+1:]) // fields[0] = 第3列 state
	rss := uint64(0)
	if len(fields) > 21 { // 第24列 rss = fields[21]
		rss, _ = strconv.ParseUint(fields[21], 10, 64)
	}
	return comm, rss, nil
}

func commAfterPid(head string) string {
	// head 形如 " 123 (bash" → 取括号内
	if i := strings.Index(head, "("); i >= 0 {
		return head[i+1:]
	}
	return head
}

// Procs 返回全部进程（按 RSS 降序）。
func Procs() ([]Proc, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	var out []Proc
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		comm, rss, err := procComm("/proc/" + e.Name())
		if err != nil {
			continue
		}
		out = append(out, Proc{PID: pid, Comm: comm, RSSKB: rss})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RSSKB > out[j].RSSKB })
	return out, nil
}

// ---------- 监听端口 ----------

type Port struct {
	Proto     string
	LocalAddr string
	State     string
	PID       int
	Comm      string
}

func hexIPPort(s string) (string, int, error) {
	ipHex, portHex, ok := strings.Cut(s, ":")
	if !ok {
		return "", 0, fmt.Errorf("地址格式异常: %s", s)
	}
	port, err := strconv.ParseInt(portHex, 16, 32)
	if err != nil {
		return "", 0, err
	}
	raw := make([]byte, len(ipHex)/2)
	for i := 0; i < len(raw); i++ {
		v, err := strconv.ParseUint(ipHex[i*2:i*2+2], 16, 8)
		if err != nil {
			return "", 0, err
		}
		raw[i] = byte(v)
	}
	var ip net.IP
	if len(raw) == 4 {
		// IPv4 在 /proc 里是 4 字节小端序
		ip = net.IPv4(raw[3], raw[2], raw[1], raw[0])
	} else {
		ip6 := make(net.IP, len(raw))
		for i := 0; i < len(raw); i++ {
			ip6[i] = raw[len(raw)-1-i]
		}
		ip = ip6
	}
	return ip.String(), int(port), nil
}

func parseProcNet(path, proto string) ([]Port, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var out []Port
	sc := bufio.NewScanner(f)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		if lineNo <= 1 {
			continue
		}
		fields := strings.Fields(sc.Text())
		if len(fields) < 10 {
			continue
		}
		if fields[3] != "0A" { // 0A = LISTEN
			continue
		}
		addr, port, err := hexIPPort(fields[1])
		if err != nil {
			continue
		}
		out = append(out, Port{
			Proto:     proto,
			LocalAddr: fmt.Sprintf("%s:%d", addr, port),
			State:     "LISTEN",
		})
	}
	return out, sc.Err()
}

// socketInodes 返回 inode → (pid, comm)，首次命中优先。
func socketInodes() map[string]Proc {
	m := map[string]Proc{}
	entries, _ := os.ReadDir("/proc")
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		fds, err := os.ReadDir("/proc/" + e.Name() + "/fd")
		if err != nil {
			continue
		}
		comm := ""
		for _, fd := range fds {
			link, err := os.Readlink("/proc/" + e.Name() + "/fd/" + fd.Name())
			if err != nil || !strings.HasPrefix(link, "socket:[") {
				continue
			}
			inode := strings.TrimSuffix(strings.TrimPrefix(link, "socket:["), "]")
			if _, ok := m[inode]; !ok {
				if comm == "" {
					comm, _, _ = procComm("/proc/" + e.Name())
				}
				m[inode] = Proc{PID: pid, Comm: comm}
			}
		}
	}
	return m
}

// Ports 返回全部 TCP/UDP 监听端口及归属进程。
func Ports() ([]Port, error) {
	inodes := socketInodes()
	return portsWithInode(inodes)
}

func portsWithInode(inodes map[string]Proc) ([]Port, error) {
	var out []Port
	type specT struct{ path, proto string }
	for _, spec := range []specT{{"/proc/net/tcp", "tcp"}, {"/proc/net/tcp6", "tcp6"}, {"/proc/net/udp", "udp"}, {"/proc/net/udp6", "udp6"}} {
		f, err := os.Open(spec.path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		sc := bufio.NewScanner(f)
		lineNo := 0
		for sc.Scan() {
			lineNo++
			if lineNo <= 1 {
				continue
			}
			fields := strings.Fields(sc.Text())
			if len(fields) < 10 {
				continue
			}
			state := fields[3]
			if state != "0A" { // M0 只关心 LISTEN
				continue
			}
			addr, port, err := hexIPPort(fields[1])
			if err != nil {
				continue
			}
			p := Port{Proto: spec.proto, LocalAddr: fmt.Sprintf("%s:%d", addr, port), State: "LISTEN"}
			if pr, ok := inodes[fields[9]]; ok {
				p.PID = pr.PID
				p.Comm = pr.Comm
			}
			out = append(out, p)
		}
		f.Close()
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LocalAddr < out[j].LocalAddr })
	return out, nil
}

// ---------- 常驻心跳 ----------

func WriteHeartbeat(path string) error {
	if err := os.MkdirAll(dirOf(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(time.Now().Format(time.RFC3339)), 0o644)
}

func dirOf(p string) string {
	i := strings.LastIndexByte(p, '/')
	if i <= 0 {
		return "/"
	}
	return p[:i]
}
