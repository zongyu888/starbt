// Package export —— 把当前节点生成各类代理客户端能直接导入的配置文件。
// 覆盖市面上几大类客户端：
//   - Clash 系（Clash Verge / Rev、mihomo 内核、Clash for Windows、ClashX、FlClash、Stash）：YAML
//   - 订阅类（Shadowrocket、v2rayN / v2rayNG、Nekoray / NekoBox、Hiddify 等）：Base64 订阅内容
//   - 分享链接（几乎所有 Shadowsocks 客户端的「从剪贴板导入 / 扫码」）：ss:// (SIP002)
//   - sing-box 系（sing-box 及以其为内核的客户端）：outbounds 节点片段
package export

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

type Node struct {
	Name     string
	Host     string
	Port     string
	Password string
	Method   string
}

// ShareLink 返回 SIP002 分享链接（ss://base64url(method:password)@host:port#name）。
func (n Node) ShareLink() string {
	user := base64.RawURLEncoding.EncodeToString([]byte(n.Method + ":" + n.Password))
	return "ss://" + user + "@" + net.JoinHostPort(n.Host, n.Port) + "#" + url.QueryEscape(n.Name)
}

// Subscription 返回 Base64 订阅内容（内容为若干条分享链接，整体再做一次 Base64）。
func (n Node) Subscription() string {
	return base64.StdEncoding.EncodeToString([]byte(n.ShareLink() + "\n"))
}

func yamlQuote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// ClashYAML 返回一份完整可用的 Clash 配置（国内直连、其余走代理）。
func (n Node) ClashYAML() string {
	return fmt.Sprintf(`mixed-port: 7897
mode: rule
log-level: info

proxies:
  - name: %s
    type: ss
    server: %s
    port: %s
    cipher: %s
    password: %s
    udp: false

proxy-groups:
  - name: "PROXY"
    type: select
    proxies:
      - %s

rules:
  - GEOIP,CN,DIRECT
  - MATCH,PROXY
`, yamlQuote(n.Name), n.Host, n.Port, n.Method, yamlQuote(n.Password), yamlQuote(n.Name))
}

// SingBoxJSON 返回 sing-box 的 outbounds 节点片段（合并进自己的配置即可）。
func (n Node) SingBoxJSON() string {
	port, _ := strconv.Atoi(n.Port)
	b, _ := json.MarshalIndent(map[string]any{
		"outbounds": []map[string]any{{
			"type":        "shadowsocks",
			"tag":         n.Name,
			"server":      n.Host,
			"server_port": port,
			"method":      n.Method,
			"password":    n.Password,
		}},
	}, "", "  ")
	return string(b) + "\n"
}
