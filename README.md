# StarBT（`sbt`）—— 一条命令装好的轻量 Shadowsocks 服务端

StarBT 是一个用 Go 编写的 **Shadowsocks 服务端**，打成单个 6MB 的静态二进制。
你在自己的 Linux 服务器上执行一条命令，它就常驻后台运行；然后在本地电脑的
Shadowsocks 客户端里填上服务器地址、端口、密码，本机上网就走服务器的 IP。

设计目标只有三条：**轻**（常驻内存约 1～5MB）、**省事**（一条命令，关终端不停，开机自启）、
**不挑系统**（主流 Linux 发行版都能装）。

## 一键安装

需要 root。两条命令任选其一，脚本会**自动测速并从下载最快的镜像**获取安装包：

```sh
# GitHub（新版本发布后几分钟内即可装到最新版）
curl -fsSL https://raw.githubusercontent.com/zongyu888/starbt/main/install.sh | sh

# jsDelivr CDN（国内直连快；新版本发布后引导脚本可能延迟数小时才更新）
curl -fsSL https://cdn.jsdelivr.net/gh/zongyu888/starbt@latest/install.sh | sh
```

两个入口装出来的是同一个东西。下载安装包时会自动在三个源之间测速（jsDelivr CDN / GitHub Release / Gitee Release），选最快的那个。

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

打开任意 Shadowsocks 客户端（Windows / macOS / Android / iOS 均可，下文有各平台的下载地址），
新建服务器，填入上面四项，开启系统代理即可。加密方式选 `aes-256-gcm`。
也可以直接复制「一键导入链接」，在客户端里选「从剪贴板导入 URL」。

> 云服务器（阿里云 / 腾讯云 / AWS 等）需要在**安全组**里放行该端口的 TCP 入站，否则外网连不上。

## 客户端下载

按你用的客户端家族，配套使用服务器上生成的不同文件（见管理菜单第 7 / 8 项，或 `sbt export all`）：

**Shadowsocks 系** —— 用 `starbt-share-link.txt` 或手动填四项

| 平台 | 客户端 | 地址 |
|---|---|---|
| Windows | Shadowsocks for Windows | https://github.com/shadowsocks/shadowsocks-windows/releases |
| macOS | ShadowsocksX-NG | https://github.com/shadowsocks/ShadowsocksX-NG/releases |
| Android | shadowsocks-android | https://github.com/shadowsocks/shadowsocks-android/releases |
| iOS | Shadowrocket / Stash | App Store 搜索安装（付费，非 GitHub 项目） |
| Linux | shadowsocks-rust（内核） | https://github.com/shadowsocks/shadowsocks-rust/releases |

**Clash / mihomo 系** —— 用 `starbt-clash.yaml`，在订阅页导入本地文件

| 平台 | 客户端 | 地址 |
|---|---|---|
| Windows / macOS / Linux | Clash Verge Rev | https://github.com/clash-verge-rev/clash-verge-rev/releases |
| Android | FlClash | https://github.com/chen08209/FlClash/releases |
| macOS | ClashX Meta | https://github.com/MetaCubeX/ClashX.Meta/releases |
| 内核 | mihomo（原 Clash Meta） | https://github.com/MetaCubeX/mihomo/releases |

**订阅类 / sing-box 系** —— 用 `starbt-subscription.txt`（剪贴板导入）或 `starbt-singbox.json`（合并 outbounds）

| 平台 | 客户端 | 地址 |
|---|---|---|
| Windows | v2rayN | https://github.com/2dust/v2rayN/releases |
| Android | v2rayNG | https://github.com/2dust/v2rayNG/releases |
| 全平台 | sing-box | https://github.com/SagerNet/sing-box/releases |
| 全平台 | Hiddify | https://github.com/hiddify/hiddify-app/releases |

说明：

- 旧版「Clash for Windows」已停止更新且仓库被作者删除，不再推荐；本项目的 Clash 配置面向 mihomo（Meta）内核
- iOS 没有免费方案：Shadowrocket、Stash 都是 App Store 付费应用
- 加密方式统一选 `aes-256-gcm`；服务端目前只转发 TCP，客户端里如遇 UDP 开关保持默认即可

## 管理命令

SSH 登录服务器后直接敲 `sbt` 回车，进入管理菜单：

```
  1. 连接信息     地址 / 端口 / 密码 / 加密方式 / 一键导入链接
  2. 修改密码     自定义或随机生成，立即生效
  3. 修改端口     自定义端口，立即生效
  4. 服务管理     启动 / 停止 / 重启 / 状态
  5. 连接日志     最近的代理连接记录
  6. 服务器状态   CPU / 内存 / 磁盘 / 网络（附属）
  7. Clash 配置   生成 .yaml，直接导入 Clash Verge
  8. 通用订阅     各类客户端的配置 / 订阅 / 分享链接
  0. 退出
```

**第 7、8 项是给本地客户端用的**：在服务器上生成好配置文件，再把文件取回本机导入。

| 客户端 | 用哪个文件 | 怎么导入 |
|---|---|---|
| Clash Verge / Rev、Clash Meta(mihomo)、Clash for Windows、ClashX、FlClash、Stash | `starbt-clash.yaml` | 订阅页导入本地文件（或拖进去） |
| Shadowrocket、v2rayN / v2rayNG、Nekoray / NekoBox、Hiddify 等订阅类 | `starbt-subscription.txt` | 复制内容 → 从剪贴板导入 |
| 所有 Shadowsocks 客户端（Windows / macOS / Android / iOS） | `starbt-share-link.txt` | 复制 `ss://` 链接 → 从剪贴板导入 / 扫码 |
| sing-box 及以其为内核的客户端 | `starbt-singbox.json` | 合并进自己配置的 `outbounds` |

生成的文件在服务器的 `/etc/sbt/exports/`（权限 600，含密码）。取回本机：

```sh
scp -r root@<服务器IP>:/etc/sbt/exports .
```

生成的 Clash 配置已用 mihomo 内核（Clash Verge 用的就是它）实测：配置校验通过，流量经 `PROXY → StarBT` 出网。

也支持子命令，方便写脚本：

| 命令 | 作用 |
|---|---|
| `sbt ss` | 打印客户端连接信息与一键导入链接 |
| `sbt ss passwd [新密码]` | 修改密码（不带参数则随机生成），自动重启并校验 |
| `sbt ss port <端口>` | 修改端口，自动检测占用，自动重启并校验 |
| `sbt ss log` | 实时连接日志（Ctrl+C 结束） |
| `sbt ss restart` / `start` / `stop` / `status` | 服务管理 |
| `sbt export clash` | 只生成 Clash 配置 `starbt-clash.yaml` |
| `sbt export all` | 生成全部客户端格式（Clash / 订阅 / 分享链接 / sing-box） |
| `sbt export link` | 只打印 `ss://` 分享链接 |
| `sbt status` | 服务器状态 |

## 卸载

```sh
curl -fsSL https://cdn.jsdelivr.net/gh/zongyu888/starbt@latest/install.sh | sh -s -- --uninstall
```

程序、常驻项、配置和密码一并删除。

## 适配范围

安装脚本是纯 POSIX `sh`（dash / bash / busybox ash 都能跑），缺 `curl` / `tar` / `gzip` 时会调用对应包管理器自动安装，并按下面的顺序自动选择常驻方式：

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
| `SBT_VERSION=v0.2.7` | 安装指定版本（默认自动读取最新 Release） |
| `SBT_PORT=8389` | 首次安装时的监听端口 |
| `SBT_MIRROR=jsdelivr`、`github` 或 `gitee` | 指定下载源并跳过测速（jsDelivr CDN / GitHub Release / Gitee Release） |
| `SBT_MIRROR_URL=https://…` | 自定义下载源 |

## 安装脚本做了什么

全程无交互，失败即中止，重复执行等于升级（保留配置和密码）：

1. 识别 CPU 架构与系统；缺 `curl`/`tar`/`gzip` 时自动调用对应包管理器安装
2. 从 GitHub 最新 Release 读取版本号（失败则依次回退到仓库里的 VERSION 文件）；对三个下载源做下载测速，选最快的一个（jsDelivr CDN、GitHub Release、Gitee Release；不可达的自动跳过）
3. 下载 → **sha256 校验** → 解压到 `/usr/local/bin/sbt`
4. 生成 `/etc/sbt/config.toml`（权限 600，随机密码）
5. 按上表注册常驻；`ufw` / `firewalld` 处于启用状态时自动放行端口
6. 自检并打印客户端连接信息

文件位置：`/usr/local/bin/sbt`、`/etc/sbt/config.toml`、`/etc/sbt/exports/`（客户端配置）、`/var/lib/sbt/`、`/var/log/sbt.log`。

## 技术说明

- 协议：Shadowsocks AEAD（TCP），加密 `aes-256-gcm`；密钥派生 EVP_BytesToKey(MD5)，
  会话子密钥 HKDF-SHA1，分块上限 0x3FFF —— 与官方客户端逐字节兼容
- 依赖：仅 Go 标准库，无任何第三方库，`CGO_ENABLED=0` 静态编译
- 采集服务器状态直接读 `/proc`，不派生子进程
- 暂不支持：UDP 转发、`chacha20-ietf-poly1305`、多用户多端口

## 从源码构建

```sh
git clone https://github.com/zongyu888/starbt.git
cd starbt
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o sbt ./cmd/sbt
```

## 声明

本工具用于在自己的服务器上搭建加密代理。请在遵守当地法律法规的前提下使用，
作者不对任何滥用行为负责。
