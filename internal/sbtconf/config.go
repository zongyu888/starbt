// Package sbtconf —— /etc/sbt/config.toml 的极简读取（key = "value"）。
// 故意不引第三方 toml 库：配置项少且都是自己写的。
package sbtconf

import (
	"bufio"
	"os"
	"strings"
)

type Config struct {
	Version      string
	InstallToken string
	SSMethod     string
	SSPort       string
	SSPassword   string
}

func Load(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	c := &Config{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		switch k {
		case "version":
			c.Version = v
		case "install_token":
			c.InstallToken = v
		case "ss_method":
			c.SSMethod = v
		case "ss_port":
			c.SSPort = v
		case "ss_password":
			c.SSPassword = v
		}
	}
	return c, sc.Err()
}

// SetValue 修改（或追加）配置项，并把文件权限收紧为 600（内含代理密码）。
func SetValue(path, key, value string) error {
	var lines []string
	if b, err := os.ReadFile(path); err == nil {
		lines = strings.Split(string(b), "\n")
	}
	found := false
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "#") {
			continue
		}
		k, _, ok := strings.Cut(t, "=")
		if ok && strings.TrimSpace(k) == key {
			lines[i] = key + " = \"" + value + "\""
			found = true
		}
	}
	if !found {
		lines = append(lines, key+" = \""+value+"\"")
	}
	out := strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n"
	if err := os.WriteFile(path, []byte(out), 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}
