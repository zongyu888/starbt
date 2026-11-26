# StarBT（`sbt`）—— 一条命令装好的轻量 Shadowsocks 服务端

StarBT 是一个用 Go 编写的 **Shadowsocks 服务端**，打成单个 6MB 的静态二进制。
你在自己的 Linux 服务器上执行一条命令，它就常驻后台运行；然后在本地电脑的
Shadowsocks 客户端里填上服务器地址、端口、密码，本机上网就走服务器的 IP。

设计目标只有三条：**轻**（常驻内存约 1～5MB）、**省事**（一条命令，关终端不停，开机自启）、
**不挑系统**（主流 Linux 发行版都能装）。

## 一键安装

需要 root。两条命令任选其一，脚本会**自动测速并从下载最快的镜像**获取安装包：

```sh
# Gitee（国内快）
curl -fsSL https://gitee.com/zongyu_xyz/starbt/raw/main/install.sh | sh

# GitHub（海外快）
curl -fsSL https://raw.githubusercontent.com/zongyu888/starbt/main/install.sh | sh
```

非 root 用户把末尾的 `sh` 换成 `sudo sh`。安装完成后会直接打印客户端要填的内容：

```
══════════════════════════════════════════════════════════
  StarBT Shadowsocks 服务端 · 客户端连接信息
══════════════════════════════════════════════════════════
  服务器地址   <你的服务器公网 IP>
  端口         8389
  密码         <安装时随机生成>
  加密方式     aes-256-gcm
──────────────────────────────────────────────────────────
  一键导入链接（复制整行，在 Shadowsocks 客户端选「从剪贴板导入 URL」）
  ss://…
──────────────────────────────────────────────────────────
  服务状态     运行中（systemd active）
  端口监听     ✓ 8389 正在监听
══════════════════════════════════════════════════════════
```

## 本地客户端怎么用

打开任意 Shadowsocks 客户端（Windows / macOS / Android / iOS 均可），
新建服务器，填入上面四项，开启系统代理即可。加密方式选 `aes-256-gcm`。
也可以直接复制「一键导入链接」，在客户端里选「从剪贴板导入 URL」。

> 云服务器（阿里云 / 腾讯云 / AWS 等）需要在**安全组**里放行该端口的 TCP 入站，否则外网连不上。

## 管理命令

SSH 登录服务器后直接敲 `sbt` 回车，进入管理菜单：

```
  1. 连接信息     地址 / 端口 / 密码 / 加密方式 / 一键导入链接
  2. 修改密码     自定义或随机生成，立即生效
  3. 修改端口     自定义端口，立即生效
  4. 服务管理     启动 / 停止 / 重启 / 状态
  5. 连接日志     最近的代理连接记录
  6. 服务器状态   CPU / 内存 / 磁盘 / 网络（附属）
  0. 退出
```

也支持子命令，方便写脚本：

| 命令 | 作用 |
|---|---|
| `sbt ss` | 打印客户端连接信息与一键导入链接 |
| `sbt ss passwd [新密码]` | 修改密码（不带参数则随机生成），自动重启并校验 |
| `sbt ss port <端口>` | 修改端口，自动检测占用，自动重启并校验 |
| `sbt ss log` | 实时连接日志（Ctrl+C 结束） |
| `sbt ss restart` / `start` / `stop` / `status` | 服务管理 |
| `sbt status` | 服务器状态 |

## 卸载

```sh
curl -fsSL https://gitee.com/zongyu_xyz/starbt/raw/main/install.sh | sh -s -- --uninstall
```

程序、常驻项、配置和密码一并删除。

## 适配范围

安装脚本是纯 POSIX `sh`（dash / bash / busybox ash 都能跑），按下面的顺序自动选择常驻方式：

| 优先级 | 条件 | 常驻方式 |
|---|---|---|
| 1 | 有 systemd | `sbt.service`，异常退出自动拉起 |
| 2 | Alpine / Gentoo（OpenRC） | `/etc/init.d/sbt` + `rc-update` |
| 3 | 传统 SysV init | `/etc/init.d/sbt` + `update-rc.d` / `chkconfig` |
| 4 | 都没有（容器等） | `nohup` 后台运行 + `cron @reboot` 开机自启 |

以下发行版由 GitHub Actions 在每次推送时**自动安装测试**（安装 → 校验常驻 → 校验卸载）：

Debian 12 · Ubuntu 22.04 / 24.04 · Rocky Linux 9 · AlmaLinux 9 · CentOS 7 · Fedora ·
Alpine 3.20 · Arch Linux · openSUSE Leap 15

架构：`amd64`、`arm64`。

可选环境变量：

| 变量 | 作用 |
|---|---|
| `SBT_VERSION=v0.2.3` | 安装指定版本（默认自动读取最新） |
| `SBT_PORT=8389` | 首次安装时的监听端口 |
| `SBT_MIRROR=gitee` 或 `github` | 指定镜像，跳过测速 |
| `SBT_MIRROR_URL=https://…` | 自定义下载源 |

## 安装脚本做了什么

全程无交互，失败即中止，重复执行等于升级（保留配置和密码）：

1. 识别 CPU 架构与系统；缺 `curl`/`tar` 时自动调用对应包管理器安装
2. 从仓库读取最新版本号；对 Gitee / GitHub 两个镜像做下载测速，选最快的
3. 下载 → **sha256 校验** → 解压到 `/usr/local/bin/sbt`
4. 生成 `/etc/sbt/config.toml`（权限 600，随机密码）
5. 按上表注册常驻；`ufw` / `firewalld` 处于启用状态时自动放行端口
6. 自检并打印客户端连接信息

文件位置：`/usr/local/bin/sbt`、`/etc/sbt/config.toml`、`/var/lib/sbt/`、`/var/log/sbt.log`。

## 技术说明

- 协议：Shadowsocks AEAD（TCP），加密 `aes-256-gcm`；密钥派生 EVP_BytesToKey(MD5)，
  会话子密钥 HKDF-SHA1，分块上限 0x3FFF —— 与官方客户端逐字节兼容
- 依赖：仅 Go 标准库，无任何第三方库，`CGO_ENABLED=0` 静态编译
- 采集服务器状态直接读 `/proc`，不派生子进程
- 暂不支持：UDP 转发、`chacha20-ietf-poly1305`、多用户多端口

## 从源码构建

```sh
git clone https://gitee.com/zongyu_xyz/starbt.git
cd starbt
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o sbt ./cmd/sbt
```

## 声明

本工具用于在自己的服务器上搭建加密代理。请在遵守当地法律法规的前提下使用，
作者不对任何滥用行为负责。
