//go:build linux
// +build linux

// Package ssserver —— StarBT 内置的 Shadowsocks 服务端（TCP，AEAD: aes-256-gcm）。
// 纯标准库实现，实现规范：
//   - 密钥派生 EVP_BytesToKey(MD5)（与 shadowsocks 官方一致）
//   - 会话子密钥 HKDF-SHA1，info = "ss-subkey"
//   - 分块: [len(2) + tag][payload + tag]，单块上限 0x3FFF，随机数 12 字节计数器
//   - 握手: 客户端先发 salt(32) → 首块 payload 携带目标地址 (ATYP + host + port)
//   - 回程: 服务端先发自己的 salt，再用同规则分块加密
package ssserver

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/md5" //nolint:gosec // Shadowsocks 协议规定用 MD5 派生密钥
	"crypto/rand"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"sync"
	"time"
)

const (
	keyLen     = 32 // aes-256-gcm
	saltLen    = 32
	tagLen     = 16
	maxPayload = 0x3FFF
	hkdfInfo   = "ss-subkey"
)

// ---------- 密钥派生 ----------

func evpBytesToKey(password []byte, klen int) []byte {
	var prev, out []byte
	for len(out) < klen {
		h := md5.New()
		h.Write(prev)
		h.Write(password)
		prev = h.Sum(nil)
		out = append(out, prev...)
	}
	return out[:klen]
}

func hkdfSHA1(ikm, salt []byte, info string, outLen int) []byte {
	if len(salt) == 0 {
		salt = make([]byte, sha1.Size)
	}
	extractor := hmac.New(sha1.New, salt)
	extractor.Write(ikm)
	prk := extractor.Sum(nil)

	var out, t []byte
	for counter := byte(1); len(out) < outLen; counter++ {
		expand := hmac.New(sha1.New, prk)
		expand.Write(t)
		expand.Write([]byte(info))
		expand.Write([]byte{counter})
		t = expand.Sum(nil)
		out = append(out, t...)
	}
	return out[:outLen]
}

// ---------- 12 字节计数器随机数 ----------

type nonceGen struct{ n [12]byte }

func (g *nonceGen) next() []byte {
	out := make([]byte, 12)
	copy(out, g.n[:])
	v := binary.LittleEndian.Uint32(g.n[:4])
	v++
	binary.LittleEndian.PutUint32(g.n[:4], v)
	return out
}

// ---------- AEAD 分块读 ----------

type aeadReader struct {
	r   io.Reader
	a   cipher.AEAD
	gen nonceGen
	buf []byte
}

func (cr *aeadReader) Read(p []byte) (int, error) {
	for len(cr.buf) == 0 {
		var hdr [2 + tagLen]byte
		if _, err := io.ReadFull(cr.r, hdr[:]); err != nil {
			return 0, err
		}
		plain, err := cr.a.Open(nil, cr.gen.next(), hdr[:], nil)
		if err != nil {
			return 0, fmt.Errorf("解密块长失败: %w", err)
		}
		n := int(binary.BigEndian.Uint16(plain))
		if n <= 0 || n > maxPayload {
			return 0, fmt.Errorf("非法块长 %d", n)
		}
		body := make([]byte, n+tagLen)
		if _, err := io.ReadFull(cr.r, body); err != nil {
			return 0, err
		}
		plain, err = cr.a.Open(nil, cr.gen.next(), body, nil)
		if err != nil {
			return 0, fmt.Errorf("解密数据块失败: %w", err)
		}
		cr.buf = plain
	}
	c := copy(p, cr.buf)
	cr.buf = cr.buf[c:]
	return c, nil
}

// ---------- AEAD 分块写 ----------

type aeadWriter struct {
	w   io.Writer
	a   cipher.AEAD
	gen nonceGen
}

func (cw *aeadWriter) Write(p []byte) (int, error) {
	total := 0
	for len(p) > 0 {
		n := len(p)
		if n > maxPayload {
			n = maxPayload
		}
		hdr := make([]byte, 2)
		binary.BigEndian.PutUint16(hdr, uint16(n))
		if _, err := cw.w.Write(cw.a.Seal(nil, cw.gen.next(), hdr, nil)); err != nil {
			return total, err
		}
		if _, err := cw.w.Write(cw.a.Seal(nil, cw.gen.next(), p[:n], nil)); err != nil {
			return total, err
		}
		total += n
		p = p[n:]
	}
	return total, nil
}

// ---------- 目标地址解析（ATYP + addr + port） ----------

func parseTarget(chunk []byte) (string, []byte, error) {
	if len(chunk) < 5 {
		return "", nil, fmt.Errorf("首块太短")
	}
	atyp := chunk[0]
	var host string
	var pos int
	switch atyp {
	case 0x01: // IPv4
		if len(chunk) < 1 + 4 + 2 {
			return "", nil, fmt.Errorf("IPv4 地址不完整")
		}
		host = net.IP(chunk[1:5]).String()
		pos = 5
	case 0x03: // 域名
		l := int(chunk[1])
		if len(chunk) < 2 + l + 2 {
			return "", nil, fmt.Errorf("域名地址不完整")
		}
		host = string(chunk[2 : 2+l])
		pos = 2 + l
	case 0x04: // IPv6
		if len(chunk) < 1 + 16 + 2 {
			return "", nil, fmt.Errorf("IPv6 地址不完整")
		}
		host = net.IP(chunk[1:17]).String()
		pos = 17
	default:
		return "", nil, fmt.Errorf("不支持的 ATYP: %#x", atyp)
	}
	port := binary.BigEndian.Uint16(chunk[pos : pos+2])
	return net.JoinHostPort(host, strconv.Itoa(int(port))), chunk[pos+2:], nil
}

// ---------- 服务端 ----------

// Serve 启动 Shadowsocks TCP 服务端，阻塞。
func Serve(listenAddr, password, method string) error {
	if method != "aes-256-gcm" {
		log.Printf("[ss] 不支持加密 %s，强制使用 aes-256-gcm", method)
	}
	master := evpBytesToKey([]byte(password), keyLen)
	ln, err := listenWithRetry(listenAddr, 20, 2*time.Second)
	if err != nil {
		return err
	}
	log.Printf("[ss] Shadowsocks 服务端就绪 %s (aes-256-gcm)", listenAddr)
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		go handle(conn, master)
	}
}

// listenWithRetry 端口暂时被占（旧进程未退、开机网络未就绪）时自动重试，而不是直接放弃。
func listenWithRetry(addr string, attempts int, delay time.Duration) (net.Listener, error) {
	var ln net.Listener
	var err error
	for i := 1; i <= attempts; i++ {
		ln, err = net.Listen("tcp", addr)
		if err == nil {
			return ln, nil
		}
		log.Printf("[ss] 监听 %s 失败（第 %d/%d 次）: %v", addr, i, attempts, err)
		time.Sleep(delay)
	}
	return nil, err
}

func handle(conn net.Conn, master []byte) {
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(10 * time.Second)) // 握手限时

	var salt [saltLen]byte
	if _, err := io.ReadFull(conn, salt[:]); err != nil {
		return
	}
	subkey := hkdfSHA1(master, salt[:], hkdfInfo, keyLen)
	block, err := aes.NewCipher(subkey)
	if err != nil {
		return
	}
	aead, _ := cipher.NewGCM(block)
	client := &aeadReader{r: conn, a: aead}

	// 首块 payload = 目标地址（可能还带早期数据）
	first := make([]byte, maxPayload)
	n, err := client.Read(first)
	if err != nil {
		log.Printf("[ss] 握手失败: %v", err)
		return
	}
	targetAddr, early, err := parseTarget(first[:n])
	if err != nil {
		log.Printf("[ss] 地址解析失败: %v", err)
		return
	}

	target, err := net.DialTimeout("tcp", targetAddr, 10*time.Second)
	if err != nil {
		log.Printf("[ss] 连不上 %s: %v", targetAddr, err)
		return
	}
	defer target.Close()
	if len(early) > 0 {
		target.Write(early)
	}
	conn.SetDeadline(time.Time{}) // 清除握手限时，进入转发模式

	// 回程：先发服务端自己的 salt
	var rsalt [saltLen]byte
	if _, err := rand.Read(rsalt[:]); err != nil {
		return
	}
	if _, err := conn.Write(rsalt[:]); err != nil {
		return
	}
	rsub := hkdfSHA1(master, rsalt[:], hkdfInfo, keyLen)
	rblock, _ := aes.NewCipher(rsub)
	raead, _ := cipher.NewGCM(rblock)
	resp := &aeadWriter{w: conn, a: raead}

	log.Printf("[ss] %s → %s", conn.RemoteAddr(), targetAddr)

	var once sync.Once
	closeAll := func() {
		once.Do(func() {
			conn.Close()
			target.Close()
		})
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); io.Copy(target, client); closeAll() }()
	go func() { defer wg.Done(); io.Copy(resp, target); closeAll() }()
	wg.Wait()
}
