// sbt —— StarBT 轻量服务器管理面板（服务端 CLI + TUI 菜单）
// 部署形态参照服务端 Shadowsocks/gost：单二进制 + systemd 常驻 + SSH 上手敲命令。
package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"text/tabwriter"
	"time"

	"starbt/internal/export"
	"starbt/internal/sbtconf"
	"starbt/internal/ssserver"
	"starbt/internal/sysinfo"
)

const version = "v0.2.7"

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		menu()
		return
	}
	switch args[0] {
	case "version", "--version", "-v":
		fmt.Println("sbt", version)
	case "status":
		cmdStatus()
	case "proc":
		n := 15
		if len(args) >= 3 && args[1] == "-n" {
			if v, err := strconv.Atoi(args[2]); err == nil {
				n = v
			}
		}
		cmdProc(n)
	case "port":
		cmdPort()
	case "svc":
		if len(args) < 2 {
			cmdSvc([]string{"list"})
		} else {
			cmdSvc(args[1:])
		}
	case "fetch":
		if len(args) < 2 {
			fmt.Println("用法: sbt fetch <url>")
			os.Exit(2)
		}
		cmdFetch(args[1])
	case "daemon":
		cmdDaemon()
	case "serve":
		cmdServe()
	case "ss":
		cmdSS(args[1:])
	case "export":
		cmdExport(args[1:])
	case "menu":
		menu()
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Printf("未知命令: %s\n\n", args[0])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Printf(`sbt %s —— StarBT 轻量服务器管理面板

用法:
  sbt              进入交互管理菜单
  sbt status       服务器状态一览
  sbt proc [-n N]  按内存排序的进程 TOP N（默认 15）
  sbt port         监听端口及归属进程
  sbt svc list|start|stop|restart [服务名]
  sbt fetch <url>  服务器代抓网页转终端阅读
  sbt daemon       常驻守护进程（systemd 调用）
  sbt serve        前台运行 Shadowsocks 服务端
  sbt ss            查看客户端连接信息（地址/端口/密码/加密/导入链接）
  sbt ss passwd [新密码]   修改密码（不带参数 = 随机生成）
  sbt ss port <端口>       修改端口
  sbt ss log        实时连接日志      sbt ss restart  重启服务端
  sbt export clash  生成 Clash Verge 配置文件（.yaml）
  sbt export all    生成通用订阅（Clash / 订阅 / 分享链接 / sing-box）
  sbt version      版本号
`, version)
}

// ---------- status ----------

func cmdStatus() {
	if err := statusOnce(); err != nil {
		fmt.Println("读取服务器状态失败:", err)
		fmt.Println("（sbt 目前仅支持 Linux 服务端）")
		os.Exit(1)
	}
}

func statusOnce() error {
	osinfo, err := sysinfo.ReadOSInfo()
	if err != nil {
		return err
	}
	kernel, err := sysinfo.Kernel()
	if err != nil {
		return err
	}
	mem, err := sysinfo.ReadMemInfo()
	if err != nil {
		return err
	}
	load, err := sysinfo.ReadLoad()
	if err != nil {
		return err
	}
	up, err := sysinfo.Uptime()
	if err != nil {
		return err
	}
	disk, err := sysinfo.ReadDisk("/")
	if err != nil {
		return err
	}
	cpu, err := sysinfo.CPUUsage(400 * time.Millisecond)
	if err != nil {
		return err
	}
	rx, tx, _ := sysinfo.NetRate(600 * time.Millisecond)

	usedMB := (mem.TotalKB - mem.AvailKB) / 1024
	totalMB := mem.TotalKB / 1024
	pct := 0.0
	if mem.TotalKB > 0 {
		pct = float64(usedMB) / float64(totalMB) * 100
	}
	fmt.Printf("系统    %s (%s)\n", osinfo.Pretty, kernel)
	fmt.Printf("运行    %s\n", fmtDuration(up))
	fmt.Printf("负载    %.2f  %.2f  %.2f    CPU %.1f%%\n", load.One, load.Five, load.Fifteen, cpu)
	fmt.Printf("内存    %dMB / %dMB (%.1f%%)    Swap %dMB/%dMB\n",
		usedMB, totalMB, pct, (mem.SwapTotalKB-mem.SwapFreeKB)/1024, mem.SwapTotalKB/1024)
	fmt.Printf("磁盘 /  %.1fG 可用 / 共 %.1fG\n",
		float64(disk.FreeBytes)/1e9, float64(disk.TotalBytes)/1e9)
	fmt.Printf("网络    ↓ %.1f KB/s    ↑ %.1f KB/s\n", rx/1024, tx/1024)
	return nil
}

func fmtDuration(d time.Duration) string {
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60
	if days > 0 {
		return fmt.Sprintf("%d天%d小时%d分", days, hours, mins)
	}
	return fmt.Sprintf("%d小时%d分", hours, mins)
}

// ---------- proc ----------

func cmdProc(n int) {
	procs, err := sysinfo.Procs()
	if err != nil {
		fmt.Println("读取进程失败:", err)
		os.Exit(1)
	}
	w := tabwriter.NewWriter(os.Stdout, 4, 4, 2, ' ', 0)
	fmt.Fprintln(w, "PID\t内存(MB)\t进程")
	count := 0
	for _, p := range procs {
		if count >= n {
			break
		}
		fmt.Fprintf(w, "%d\t%.1f\t%s\n", p.PID, float64(p.RSSKB)/1024, p.Comm)
		count++
	}
	w.Flush()
}

// ---------- port ----------

func cmdPort() {
	ports, err := sysinfo.Ports()
	if err != nil {
		fmt.Println("读取端口失败:", err)
		os.Exit(1)
	}
	w := tabwriter.NewWriter(os.Stdout, 4, 4, 2, ' ', 0)
	fmt.Fprintln(w, "协议\t监听地址\t状态\tPID\t进程")
	for _, p := range ports {
		comm := p.Comm
		if comm == "" {
			comm = "-"
		}
		pid := ""
		if p.PID > 0 {
			pid = strconv.Itoa(p.PID)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", p.Proto, p.LocalAddr, p.State, pid, comm)
	}
	w.Flush()
}

// ---------- svc ----------

func cmdSvc(args []string) {
	sub := args[0]
	var cmd *exec.Cmd
	switch sub {
	case "list":
		cmd = exec.Command("systemctl", "list-units", "--type=service", "--state=running", "--no-pager", "--no-legend")
	case "start", "stop", "restart":
		if len(args) < 2 {
			fmt.Printf("用法: sbt svc %s <服务名>\n", sub)
			os.Exit(2)
		}
		cmd = exec.Command("systemctl", sub, args[1])
	default:
		fmt.Println("用法: sbt svc list|start|stop|restart [服务名]")
		os.Exit(2)
	}
	out, err := cmd.CombinedOutput()
	if err != nil && sub != "restart" {
		// restart 对未运行服务也可能返回非 0，照样打印输出
		if sub == "list" {
			fmt.Println("systemctl 不可用（M0 仅支持 systemd，后续版本补 init.d 兜底）:", err)
			os.Exit(1)
		}
	}
	if len(out) > 0 {
		fmt.Print(string(out))
	}
}

// ---------- fetch（服务端代抓网页，轻量上网模块 M2 的雏形） ----------

var (
	reTitle = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	reScript  = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	reStyle   = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	reNoScr   = regexp.MustCompile(`(?is)<noscript[^>]*>.*?</noscript>`)
	reTag     = regexp.MustCompile(`(?s)<[^>]+>`)
	reSpace   = regexp.MustCompile(`[ \t\r\f]+`)
)

func cmdFetch(rawURL string) {
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		rawURL = "https://" + rawURL
	}
	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		fmt.Println("URL 无效:", err)
		os.Exit(1)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; StarBT/0.1)")
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("抓取失败:", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		fmt.Println("读取失败:", err)
		os.Exit(1)
	}
	html := string(body)
	fmt.Printf("HTTP %d  %s\n\n", resp.StatusCode, resp.Header.Get("Content-Type"))
	if m := reTitle.FindStringSubmatch(html); len(m) > 1 {
		fmt.Println("标题:", strings.TrimSpace(stripTags(m[1])))
		fmt.Println()
	}
	text := stripTags(html)
	text = reSpace.ReplaceAllString(text, " ")
	text = strings.TrimSpace(text)
	runes := []rune(text)
	if len(runes) > 3000 {
		runes = runes[:3000]
	}
	fmt.Println(string(runes))
}

func stripTags(s string) string {
	s = reScript.ReplaceAllString(s, " ")
	s = reStyle.ReplaceAllString(s, " ")
	s = reNoScr.ReplaceAllString(s, " ")
	s = reTag.ReplaceAllString(s, " ")
	rep := strings.NewReplacer("&nbsp;", " ", "&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#39;", "'")
	return rep.Replace(s)
}

// ---------- Shadowsocks 服务端（让本地电脑用服务器 IP 访问网站） ----------

func confPath() string {
	if p := os.Getenv("SBT_CONFIG"); p != "" {
		return p
	}
	return "/etc/sbt/config.toml"
}

func cmdServe() {
	cfg, err := sbtconf.Load(confPath())
	if err != nil {
		fmt.Println("读取配置失败:", err)
		os.Exit(1)
	}
	if cfg.SSPassword == "" {
		fmt.Println("配置缺少 ss_password，先运行 sbt ss 检查")
		os.Exit(1)
	}
	if err := ssserver.Serve(":"+cfg.SSPort, cfg.SSPassword, cfg.SSMethod); err != nil {
		fmt.Println("服务端退出:", err)
		os.Exit(1)
	}
}

func startSSInBackground() {
	cfg, err := sbtconf.Load(confPath())
	if err != nil || cfg.SSPassword == "" {
		log.Println("[ss] 未配置 ss_password，跳过 Shadowsocks 服务端")
		return
	}
	go func() {
		if err := ssserver.Serve(":"+cfg.SSPort, cfg.SSPassword, cfg.SSMethod); err != nil {
			log.Println("[ss] 服务端退出:", err)
		}
	}()
}

// ---------- Shadowsocks 服务端管理（核心业务） ----------

var (
	stdin  = bufio.NewReader(os.Stdin)
	ipOnce sync.Once
	ipHost string
)

func ask(prompt string) string {
	fmt.Print(prompt)
	line, _ := stdin.ReadString('\n')
	return strings.TrimSpace(line)
}

func pause() { ask("\n  按回车返回菜单…") }

func loadSS() (*sbtconf.Config, error) {
	cfg, err := sbtconf.Load(confPath())
	if err != nil {
		return nil, fmt.Errorf("读取配置失败: %w", err)
	}
	if cfg.SSPassword == "" || cfg.SSPort == "" {
		return nil, errors.New("未配置 Shadowsocks 服务端（config.toml 缺 ss_password / ss_port）")
	}
	return cfg, nil
}

func publicIP() string {
	ipOnce.Do(func() {
		ipHost = "<本服务器公网IP或域名>"
		client := &http.Client{Timeout: 4 * time.Second}
		if resp, err := client.Get("https://api.ip.sb/ip"); err == nil {
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 64))
			resp.Body.Close()
			if s := strings.TrimSpace(string(b)); s != "" {
				ipHost = s
			}
		}
	})
	return ipHost
}

func serviceState() string {
	out, err := exec.Command("systemctl", "is-active", "sbt").Output()
	if err != nil {
		return "未知（systemctl 不可用或未注册为系统服务）"
	}
	switch s := strings.TrimSpace(string(out)); s {
	case "active":
		return "运行中（systemd active）"
	case "inactive":
		return "已停止"
	case "failed":
		return "异常退出（failed），请查看 sbt ss log"
	default:
		return s
	}
}

func showSSInfo() {
	cfg, err := loadSS()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	method := cfg.SSMethod
	if method == "" {
		method = "aes-256-gcm"
	}
	host := publicIP()
	link := export.Node{Name: "StarBT", Host: host, Port: cfg.SSPort, Password: cfg.SSPassword, Method: method}.ShareLink()
	bar := strings.Repeat("═", 58)
	sub := strings.Repeat("─", 58)
	fmt.Printf("\n%s\n  StarBT Shadowsocks 服务端 · 客户端连接信息\n%s\n", bar, bar)
	fmt.Printf("  服务器地址   %s\n  端口         %s\n  密码         %s\n  加密方式     %s\n", host, cfg.SSPort, cfg.SSPassword, method)
	fmt.Println(sub)
	fmt.Println("  一键导入链接（复制整行，在 Shadowsocks 客户端选「从剪贴板导入 URL」）")
	fmt.Println("  " + link)
	fmt.Println(sub)
	fmt.Printf("  服务状态     %s\n", serviceState())
	fmt.Printf("  端口监听     %s\n", portState(cfg.SSPort))
	fmt.Printf("  说明         填入以上四项后开启系统代理，本机上网即使用服务器 IP\n%s\n", bar)
}

func portState(port string) string {
	conn, err := net.DialTimeout("tcp", "127.0.0.1:"+port, time.Second)
	if err != nil {
		return "✗ " + port + " 未在监听，请执行 sbt ss log 排查"
	}
	conn.Close()
	return "✓ " + port + " 正在监听"
}

func restartSS() bool {
	if err := exec.Command("systemctl", "restart", "sbt").Run(); err != nil {
		fmt.Println("  ✗ 重启失败:", err, "（可手动执行 systemctl restart sbt）")
		return false
	}
	return true
}

func applySSChange(port string) {
	if !restartSS() {
		return
	}
	time.Sleep(800 * time.Millisecond)
	conn, err := net.DialTimeout("tcp", "127.0.0.1:"+port, 3*time.Second)
	if err != nil {
		fmt.Printf("  ✗ 重启后端口 %s 未监听，请执行 sbt ss log 排查\n", port)
		return
	}
	conn.Close()
	fmt.Printf("  ✓ 已生效，服务端正在监听 %s 端口\n", port)
	fmt.Println("  ✓ 本地客户端改成相同配置后即可连接")
}

func validPassword(p string) bool {
	if len(p) < 8 || len(p) > 64 {
		return false
	}
	for _, c := range p {
		if c < 0x21 || c > 0x7e || c == '"' || c == '\'' {
			return false
		}
	}
	return true
}

func changeSSPassword(newPass string) {
	if newPass == "" {
		b := make([]byte, 12)
		if _, err := rand.Read(b); err != nil {
			fmt.Println("  ✗ 生成随机密码失败:", err)
			return
		}
		newPass = hex.EncodeToString(b)
		fmt.Println("  已随机生成新密码:", newPass)
	}
	if !validPassword(newPass) {
		fmt.Println("  ✗ 密码需为 8~64 位可见字符，且不含空格和引号")
		return
	}
	cfg, err := loadSS()
	if err != nil {
		fmt.Println(err)
		return
	}
	if err := sbtconf.SetValue(confPath(), "ss_password", newPass); err != nil {
		fmt.Println("  ✗ 写入配置失败:", err)
		return
	}
	applySSChange(cfg.SSPort)
	fmt.Println("  → 用 sbt ss 查看新的连接信息和导入链接")
}

func changeSSPort(p string) {
	n, err := strconv.Atoi(p)
	if err != nil || n < 1 || n > 65535 {
		fmt.Println("  ✗ 端口需为 1~65535 的整数")
		return
	}
	cfg, err := loadSS()
	if err != nil {
		fmt.Println(err)
		return
	}
	if strconv.Itoa(n) == cfg.SSPort {
		fmt.Printf("  端口已是 %d，无需修改\n", n)
		return
	}
	ln, err := net.Listen("tcp", ":"+strconv.Itoa(n))
	if err != nil {
		fmt.Printf("  ✗ 端口 %d 已被占用，请换一个\n", n)
		return
	}
	ln.Close()
	if err := sbtconf.SetValue(confPath(), "ss_port", strconv.Itoa(n)); err != nil {
		fmt.Println("  ✗ 写入配置失败:", err)
		return
	}
	applySSChange(strconv.Itoa(n))
	fmt.Printf("  注意：云服务器（如阿里云）需在安全组放行 TCP %d 入站，否则外网连不上\n", n)
	fmt.Println("  → 用 sbt ss 查看新的连接信息和导入链接")
}

func showRecentLogs() {
	out, err := exec.Command("journalctl", "-u", "sbt", "-n", "200", "--no-pager").Output()
	if err != nil {
		fmt.Println("  读取日志失败:", err)
		return
	}
	var lines []string
	for _, l := range strings.Split(string(out), "\n") {
		if strings.Contains(l, "[ss]") {
			lines = append(lines, l)
		}
	}
	if len(lines) > 30 {
		lines = lines[len(lines)-30:]
	}
	fmt.Println("\n  最近 30 条代理连接（实时跟踪: sbt ss log）")
	if len(lines) == 0 {
		fmt.Println("  （暂无连接记录）")
	}
	for _, l := range lines {
		if i := strings.Index(l, "[ss]"); i >= 0 {
			fmt.Println("  " + strings.TrimSpace(l[i:]))
		}
	}
}

func serviceControl(action string) {
	args := []string{action, "sbt"}
	if action == "status" {
		args = []string{"status", "sbt", "--no-pager", "-n", "0"}
	}
	out, err := exec.Command("systemctl", args...).CombinedOutput()
	fmt.Print(string(out))
	if err != nil && action != "status" {
		fmt.Println("  ✗ 执行失败:", err)
	} else if action != "status" {
		fmt.Printf("  ✓ 服务已%s\n", map[string]string{"start": "启动", "stop": "停止", "restart": "重启"}[action])
	}
}

// ---------- 客户端配置导出（菜单 7 / 8） ----------

func currentNode() (export.Node, error) {
	cfg, err := loadSS()
	if err != nil {
		return export.Node{}, err
	}
	method := cfg.SSMethod
	if method == "" {
		method = "aes-256-gcm"
	}
	return export.Node{Name: "StarBT", Host: publicIP(), Port: cfg.SSPort, Password: cfg.SSPassword, Method: method}, nil
}

func exportDir() string { return filepath.Join(filepath.Dir(confPath()), "exports") }

// writeExport 写到配置目录下的 exports/（含密码，权限 600），返回路径。
func writeExport(filename, content string) (string, error) {
	if err := os.MkdirAll(exportDir(), 0o700); err != nil {
		return "", err
	}
	p := filepath.Join(exportDir(), filename)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		return "", err
	}
	return p, nil
}

func showFile(path, title, content string) {
	sub := strings.Repeat("─", 56)
	fmt.Printf("\n  %s\n  文件: %s\n  %s\n", title, path, sub)
	for _, l := range strings.Split(strings.TrimRight(content, "\n"), "\n") {
		fmt.Println("  | " + l)
	}
	fmt.Println("  " + sub)
}

var exportItems = []struct{ file, title, clients, how string }{
	{"starbt-clash.yaml", "Clash 系配置（YAML）", "Clash Verge / Rev、Clash Meta(mihomo)、Clash for Windows、ClashX、FlClash、Stash", "订阅页导入本地文件"},
	{"starbt-subscription.txt", "Base64 订阅内容", "Shadowrocket、v2rayN / v2rayNG、Nekoray / NekoBox、Hiddify 等「订阅」类客户端", "复制文件内容 → 从剪贴板导入"},
	{"starbt-share-link.txt", "单条分享链接（ss://）", "几乎所有 Shadowsocks 客户端（Windows / macOS / Android / iOS）", "复制链接 → 从剪贴板导入，或生成二维码扫码"},
	{"starbt-singbox.json", "sing-box 节点片段", "sing-box 及以其为内核的客户端", "合并进自己配置的 outbounds"},
}

func exportContents(n export.Node) map[string]string {
	return map[string]string{
		"starbt-clash.yaml":       n.ClashYAML(),
		"starbt-subscription.txt": n.Subscription(),
		"starbt-share-link.txt":   n.ShareLink() + "\n",
		"starbt-singbox.json":     n.SingBoxJSON(),
		"starbt-client-overview.txt": overviewText(n),
	}
}

func saveExports(n export.Node) map[string]string {
	contents := exportContents(n)
	for file, content := range contents {
		if _, err := writeExport(file, content); err != nil {
			fmt.Println("  ✗ 写入失败", file, err)
		}
	}
	return contents
}

func exportClash() {
	n, err := currentNode()
	if err != nil {
		fmt.Println("  ✗", err)
		return
	}
	contents := saveExports(n)
	p := filepath.Join(exportDir(), "starbt-clash.yaml")
	showFile(p, "Clash 系配置（YAML）", contents["starbt-clash.yaml"])
	fmt.Println("  导入 Clash Verge：订阅(Profiles) 页 → 把保存到本机的 starbt-clash.yaml 拖进窗口，或点「导入」选本地文件")
	fmt.Println("  其它 Clash 系客户端（Clash Meta / for Windows / ClashX / FlClash / Stash）导入同一个文件")
	fmt.Println("  取回本机（在本机终端执行）：")
	fmt.Printf("    scp root@%s:%s .\n", n.Host, p)
}

func exportAll() {
	n, err := currentNode()
	if err != nil {
		fmt.Println("  ✗", err)
		return
	}
	contents := saveExports(n)
	sub := strings.Repeat("─", 56)
	fmt.Printf("\n  通用订阅 —— 一次生成各类客户端要用的文件\n  %s\n", sub)
	for _, it := range exportItems {
		fmt.Printf("  %s\n    %-22s 适用: %s\n    %-22s 用法: %s\n", filepath.Join(exportDir(), it.file), it.title, it.clients, "", it.how)
	}
	fmt.Println("  " + sub)
	for _, it := range exportItems {
		showFile(filepath.Join(exportDir(), it.file), it.title, contents[it.file])
	}
	fmt.Printf("  取回本机（在本机终端执行，一次拿回整个目录）：\n    scp -r root@%s:%s .\n", n.Host, exportDir())
}

// overviewText 是一份「客户端 → 用哪个文件」速查表，随其它文件一并落盘。
func overviewText(n export.Node) string {
	return fmt.Sprintf(`StarBT 客户端配置速查（节点 %s）
==========================================

服务器地址   %s
端口         %s
密码         %s
加密方式     %s

一、Clash 系（Clash Verge / Rev、Clash Meta(mihomo)、Clash for Windows、ClashX、FlClash、Stash）
    → starbt-clash.yaml，在订阅页导入本地文件。

二、订阅类（Shadowrocket、v2rayN / v2rayNG、Nekoray / NekoBox、Hiddify 等）
    → starbt-subscription.txt 的内容（Base64 订阅），在客户端里「从剪贴板导入」。
      这类客户端平时填的是订阅网址；这里生成的是文件，所以走剪贴板。

三、所有 Shadowsocks 客户端（Windows / macOS / Android / iOS）
    → starbt-share-link.txt 里的 ss:// 链接：从剪贴板导入，或生成二维码扫码。
      也可以手动填最上面的四项。

四、sing-box 系（sing-box 及以其为内核的客户端）
    → 把 starbt-singbox.json 里的 outbound 合并进自己配置的 outbounds。

说明：服务端目前只转发 TCP，所以 Clash 配置里 udp 为 false。
      浏览网页、看视频不受影响；依赖 UDP 的 QUIC 会回落到 TCP。
`, n.Name, n.Host, n.Port, n.Password, n.Method)
}

func cmdExport(sub []string) {
	mode := "all"
	if len(sub) > 0 {
		mode = sub[0]
	}
	switch mode {
	case "clash":
		exportClash()
	case "all":
		exportAll()
	case "link":
		if n, err := currentNode(); err == nil {
			fmt.Println(n.ShareLink())
		} else {
			fmt.Println("  ✗", err)
		}
	default:
		fmt.Println("用法: sbt export [clash | all | link]")
	}
}

func cmdSS(sub []string) {
	if len(sub) == 0 {
		showSSInfo()
		return
	}
	switch sub[0] {
	case "passwd", "password", "pw":
		np := ""
		if len(sub) > 1 {
			np = sub[1]
		}
		changeSSPassword(np)
	case "port":
		if len(sub) < 2 {
			fmt.Println("用法: sbt ss port <新端口>")
			os.Exit(2)
		}
		changeSSPort(sub[1])
	case "log":
		cmd := exec.Command("journalctl", "-u", "sbt", "-f", "-n", "30", "--no-pager")
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		fmt.Println("  （Ctrl+C 结束跟踪）")
			_ = cmd.Run()
	case "restart", "start", "stop", "status":
		serviceControl(sub[0])
	default:
		fmt.Println("用法: sbt ss [passwd [新密码] | port <端口> | log | restart | start | stop | status]")
	}
}

// ---------- daemon（systemd 常驻入口：心跳 + 低内存自检） ----------

func cmdDaemon() {
	startSSInBackground()
	hb := "/var/lib/sbt/heartbeat"
	if err := sysinfo.WriteHeartbeat(hb); err != nil {
		fmt.Println("心跳写入失败:", err)
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	fmt.Printf("sbt daemon %s 已启动（心跳: %s）\n", version, hb)
	for {
		select {
		case <-stop:
			fmt.Println("sbt daemon 收到退出信号")
			return
		case <-ticker.C:
			if err := sysinfo.WriteHeartbeat(hb); err != nil {
				fmt.Println("心跳写入失败:", err)
			}
		}
	}
}

// ---------- 交互菜单（对标 bt / ssserver 的数字菜单） ----------

func menu() {
	for {
		bar := strings.Repeat("═", 58)
		endpt := ""
		if cfg, err := sbtconf.Load(confPath()); err == nil && cfg.SSPort != "" {
			endpt = publicIP() + ":" + cfg.SSPort
		}
		fmt.Printf("\n%s\n  StarBT Shadowsocks 服务端  %s", bar, version)
		if endpt != "" {
			fmt.Printf("    %s    [%s]", endpt, serviceState())
		}
		fmt.Printf("\n%s\n", bar)
		fmt.Println("  1. 连接信息     地址 / 端口 / 密码 / 加密方式 / 一键导入链接")
		fmt.Println("  2. 修改密码     自定义或随机生成，立即生效")
		fmt.Println("  3. 修改端口     自定义端口，立即生效")
		fmt.Println("  4. 服务管理     启动 / 停止 / 重启 / 状态")
		fmt.Println("  5. 连接日志     最近的代理连接记录")
		fmt.Println("  6. 服务器状态   CPU / 内存 / 磁盘 / 网络（附属）")
		fmt.Println("  7. Clash 配置   生成 .yaml，直接导入 Clash Verge")
		fmt.Println("  8. 通用订阅     各类客户端的配置 / 订阅 / 分享链接")
		fmt.Println("  0. 退出")
		fmt.Println(bar)
		switch ask("  请选择 [0-8]: ") {
		case "1":
			showSSInfo()
			pause()
		case "2":
			changeSSPassword(ask("  输入新密码（直接回车 = 随机生成）: "))
			pause()
		case "3":
			if p := ask("  输入新端口 (1-65535): "); p != "" {
				changeSSPort(p)
			}
			pause()
		case "4":
			serviceMenu()
		case "5":
			showRecentLogs()
			pause()
		case "6":
			cmdStatus()
			pause()
		case "7":
			exportClash()
			pause()
		case "8":
			exportAll()
			pause()
		case "0", "q", "Q":
			return
		}
	}
}

func serviceMenu() {
	for {
		fmt.Println("\n  服务管理 ─ 1.启动   2.停止   3.重启   4.状态   0.返回")
		switch ask("  请选择: ") {
		case "1":
			serviceControl("start")
			pause()
		case "2":
			serviceControl("stop")
			pause()
		case "3":
			if restartSS() {
				fmt.Println("  ✓ 服务已重启")
			}
			pause()
		case "4":
			serviceControl("status")
			pause()
		case "0", "":
			return
		}
	}
}
