#!/bin/sh
# ============================================================
#  StarBT (sbt) —— 轻量 Shadowsocks 服务端 · 一键安装脚本
#
#  一条命令安装，装完自动常驻后台（关闭终端不影响，开机自启）。
#  脚本会自动测速，从下载最快的镜像获取安装包。
#
#    国内/海外通用（jsDelivr CDN，@latest = 最新发布版）：
#      curl -fsSL https://cdn.jsdelivr.net/gh/zongyu888/starbt@latest/install.sh | sh
#    GitHub：
#      curl -fsSL https://raw.githubusercontent.com/zongyu888/starbt/main/install.sh | sh
#    非 root 用户在上面命令末尾把 sh 换成 sudo sh
#
#  说明：Gitee 仓库的 raw 链接当前处于平台内容审核状态（HTTP 451），
#        暂不能用作安装入口；Gitee 的 Release 附件可正常下载，仍作为国内下载镜像。
#
#  可选环境变量：
#    SBT_VERSION=v0.2.3        指定版本（默认自动读取最新）
#    SBT_PORT=8389             首次安装时的监听端口
#    SBT_MIRROR=gitee|github   指定镜像并跳过测速
#    SBT_MIRROR_URL=https://…  自定义下载源（其下有 <版本>/sbt_linux_<架构>.tar.gz）
#
#  其它用法：
#    sh install.sh --uninstall     卸载（配置与密码一并删除）
#    脚本同目录放有 sbt_linux_*.tar.gz 时优先离线安装
#
#  适配：Debian / Ubuntu / CentOS / RHEL / Rocky / Alma / Fedora /
#        Alpine / Arch / openSUSE；无 init 的容器自动用 nohup 兜底。
#  纯 POSIX sh：dash / bash / busybox ash 均可执行。
# ============================================================
set -eu

NAME=StarBT
CMD=sbt
DEFAULT_VERSION=v0.2.6
BIN=/usr/local/bin/$CMD
CONF_DIR=/etc/sbt
CONF=$CONF_DIR/config.toml
DATA_DIR=/var/lib/sbt
LOGFILE=/var/log/sbt.log
UNIT=/etc/systemd/system/$CMD.service
INIT=/etc/init.d/$CMD
CRON=/etc/cron.d/$CMD
GITEE_REPO=https://gitee.com/zongyu_xyz/starbt
GITHUB_REPO=https://github.com/zongyu888/starbt
GITEE_RAW=$GITEE_REPO/raw/main
GITHUB_RAW=https://raw.githubusercontent.com/zongyu888/starbt/main
JSDELIVR_RAW=https://cdn.jsdelivr.net/gh/zongyu888/starbt@main
VERSION=${SBT_VERSION:-}
PORT=${SBT_PORT:-8389}
MIRROR=
REL=
RAW=
ARCH=

WORK=$(mktemp -d 2>/dev/null || mktemp -dt starbt.XXXXXX)
trap 'rm -rf "$WORK"' EXIT
trap 'exit 1' INT TERM

log()  { printf '\033[32m[%s]\033[0m %s\n' "$NAME" "$1"; }
warn() { printf '\033[33m[%s]\033[0m %s\n' "$NAME" "$1"; }
die()  { printf '\033[31m[%s]\033[0m %s\n' "$NAME" "$1" >&2; exit 1; }
have() { command -v "$1" >/dev/null 2>&1; }

# ---------- 卸载 ----------
uninstall() {
    if [ -d /run/systemd/system ] && have systemctl; then
        systemctl disable --now "$CMD" >/dev/null 2>&1 || true
        rm -f "$UNIT"
        systemctl daemon-reload >/dev/null 2>&1 || true
    fi
    [ -x "$INIT" ] && "$INIT" stop >/dev/null 2>&1 || true
    if have rc-update; then rc-update del "$CMD" default >/dev/null 2>&1 || true; fi
    if [ -x /usr/sbin/update-rc.d ]; then update-rc.d -f "$CMD" remove >/dev/null 2>&1 || true; fi
    if [ -x /sbin/chkconfig ]; then chkconfig --del "$CMD" >/dev/null 2>&1 || true; fi
    if have pkill; then pkill -f "$BIN daemon" 2>/dev/null || true; fi
    if have crontab; then
        (crontab -l 2>/dev/null || true) | grep -v "$BIN daemon" | crontab - 2>/dev/null || true
    fi
    rm -f "$INIT" "$UNIT" "$BIN" "$CRON" /var/run/$CMD.pid
    rm -rf "$CONF_DIR" "$DATA_DIR"
    log "已卸载 StarBT（程序、常驻项、配置与密码均已删除）"
}

# ---------- 基础工具 ----------
detect_pm() {
    if have apt-get; then PM=apt
    elif have dnf; then PM=dnf
    elif have yum; then PM=yum
    elif have apk; then PM=apk
    elif have pacman; then PM=pacman
    elif have zypper; then PM=zypper
    else PM=none
    fi
}

ensure_tools() {
    need=
    have curl || have wget || need="$need curl/wget"
    have tar || need="$need tar"
    have gzip || need="$need gzip"
    if [ -z "$need" ]; then return 0; fi
    detect_pm
    log "缺少依赖:$need，正在用包管理器安装 ($PM)…"
    case "$PM" in
        apt)    DEBIAN_FRONTEND=noninteractive apt-get update -y </dev/null && DEBIAN_FRONTEND=noninteractive apt-get install -y curl ca-certificates tar gzip </dev/null ;;
        dnf)    dnf install -y curl tar gzip </dev/null ;;
        yum)    yum install -y curl tar gzip </dev/null ;;
        apk)    apk add --no-cache curl tar gzip </dev/null ;;
        pacman) pacman -Sy --noconfirm --needed curl tar gzip </dev/null ;;
        zypper) zypper --non-interactive install curl tar gzip </dev/null ;;
        *)      die "缺少:$need，且识别不到包管理器，请手动安装后重试" ;;
    esac
}

fetch() {  # fetch URL 输出文件
    if have curl; then
        curl -fsSL --connect-timeout 8 --max-time 180 -o "$2" "$1" </dev/null
    else
        wget -q -T 30 -O "$2" "$1" </dev/null
    fi
}

sha256_of() {
    if have sha256sum; then sha256sum "$1" | awk '{print $1}'
    elif have shasum; then shasum -a 256 "$1" | awk '{print $1}'
    elif have openssl; then openssl dgst -sha256 "$1" | awk '{print $NF}'
    else die "找不到 sha256sum / shasum / openssl，无法校验安装包"
    fi
}

gen_passwd() {
    pw=$(head -c 12 /dev/urandom 2>/dev/null | od -An -tx1 2>/dev/null | tr -d ' \n')
    case "$pw" in
        *[!0-9a-f]*|'') pw=$( (date +%s; cat /proc/sys/kernel/random/uuid 2>/dev/null || hostname) | cksum | tr -dc '0-9') ;;
    esac
    [ "${#pw}" -ge 8 ] || die "生成随机密码失败"
    printf '%s' "$pw"
}

# ---------- 版本与镜像 ----------
resolve_version() {
    [ -n "$VERSION" ] && return 0
    VERSION=$DEFAULT_VERSION
    have curl || return 0
    # 顺序：GitHub → jsDelivr → Gitee（Gitee raw 当前处于审核状态，放最后）
    for raw in "$GITHUB_RAW" "$JSDELIVR_RAW" "$GITEE_RAW"; do
        if v=$(curl -fsSL --connect-timeout 4 --max-time 6 "$raw/VERSION" </dev/null 2>/dev/null | tr -d '[:space:]'); then
            case "$v" in
                v[0-9]*) VERSION=$v; return 0 ;;
            esac
        fi
    done
}

# 输出: "HTTP状态码 速度(字节/秒)"
probe() {
    curl -o /dev/null -s -L --connect-timeout 5 --max-time 8 -r 0-262143 \
        -w '%{http_code} %{speed_download}' "$1" </dev/null 2>/dev/null || printf '000 0'
}

choose_mirror() {
    ASSET=sbt_linux_${ARCH}.tar.gz
    case "${SBT_MIRROR:-}" in
        gitee)    MIRROR=gitee;    REL="$GITEE_REPO/releases/download/$VERSION"; RAW=$GITEE_RAW;  log "已指定下载源: Gitee";    return 0 ;;
        jsdelivr) MIRROR=jsdelivr; REL="https://cdn.jsdelivr.net/gh/zongyu888/starbt@$VERSION/release-assets"; RAW=$JSDELIVR_RAW; log "已指定下载源: jsDelivr"; return 0 ;;
        github)   MIRROR=github;   REL="$GITHUB_REPO/releases/download/$VERSION"; RAW=$GITHUB_RAW; log "已指定下载源: GitHub";   return 0 ;;
    esac
    if [ -n "${SBT_MIRROR_URL:-}" ]; then
        case "$SBT_MIRROR_URL" in */) ;; *) SBT_MIRROR_URL="$SBT_MIRROR_URL/" ;; esac
        MIRROR=custom; REL="${SBT_MIRROR_URL}$VERSION"
        log "使用自定义下载源: $REL"
        return 0
    fi
    MIRROR=github; REL="$GITHUB_REPO/releases/download/$VERSION"; RAW=$GITHUB_RAW
    have curl || { log "未安装 curl，无法测速，使用默认源 GitHub"; return 0; }
    BEST=0
    log "正在测速，自动选择下载最快的镜像…"
    # 三个下载源，逐一测速取最快。不可达的（HTTP 非 200/206）自动跳过。
    #   gitee    Gitee Release（该仓库因平台规则不能公开，匿名一般 403，会自动跳过）
    #   jsdelivr jsDelivr CDN 分发仓库内 release-assets/（国内可达，按版本号固定）
    #   github   GitHub Release（官方源，海外快）
    while IFS='|' read -r name rel raw; do
        [ -n "$name" ] || continue
        out=$(probe "$rel/$ASSET")
        code=${out%% *}
        speed=${out#* }
        if [ "$code" = 200 ] || [ "$code" = 206 ]; then
            kb=$(awk -v s="$speed" 'BEGIN{printf "%.0f", s/1024}')
            log "  $name   ✓  ${kb} KB/s"
            if awk -v a="$speed" -v b="$BEST" 'BEGIN{exit !(a>b)}'; then
                BEST=$speed; MIRROR=$name; REL=$rel; RAW=$raw
            fi
        else
            warn "  $name   ✗  不可达 (HTTP $code)"
        fi
    done <<EOF
gitee|$GITEE_REPO/releases/download/$VERSION|$GITEE_RAW
jsdelivr|https://cdn.jsdelivr.net/gh/zongyu888/starbt@$VERSION/release-assets|$JSDELIVR_RAW
github|$GITHUB_REPO/releases/download/$VERSION|$GITHUB_RAW
EOF
    [ "$BEST" != 0 ] || die "所有下载源都连不上。请检查服务器网络，或用 SBT_MIRROR_URL=https://… 指定下载源"
    log "选用下载源: $MIRROR"
}

# ---------- 常驻：systemd → OpenRC → SysV init → nohup ----------
add_cron_boot() {
    if [ -d /etc/cron.d ] && { have crond || have cron; }; then
        printf '@reboot root %s daemon >> %s 2>&1\n' "$BIN" "$LOGFILE" > "$CRON"
        log "已添加开机自启: $CRON"
    else
        warn "未检测到 init / cron：服务器重启后需重新执行本安装命令"
    fi
}

install_systemd() {
    cat > "$UNIT" <<'EOF'
[Unit]
Description=StarBT Shadowsocks server (sbt)
Documentation=https://github.com/zongyu888/starbt
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/sbt daemon
Restart=always
RestartSec=5
MemoryMax=64M
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
EOF
    systemctl daemon-reload
    systemctl enable "$CMD" >/dev/null 2>&1 || warn "设置开机自启失败，请手动执行: systemctl enable $CMD"
    systemctl restart "$CMD"
    log "已注册为系统服务 (systemd)：关闭终端不影响，开机自启，异常退出自动拉起"
}

install_openrc() {
    cat > "$INIT" <<'EOF'
#!/sbin/openrc-run
description="StarBT Shadowsocks server (sbt)"
command="/usr/local/bin/sbt"
command_args="daemon"
command_background="yes"
pidfile="/run/sbt.pid"
output_log="/var/log/sbt.log"
error_log="/var/log/sbt.log"

depend() {
    need net
}
EOF
    chmod +x "$INIT"
    rc-update add "$CMD" default >/dev/null 2>&1 || warn "设置开机自启失败，请手动执行: rc-update add $CMD default"
    rc-service "$CMD" restart
    log "已注册为系统服务 (OpenRC)：关闭终端不影响，开机自启"
}

install_sysv() {
    cat > "$WORK/init.tmpl" <<'EOF'
#!/bin/sh
### BEGIN INIT INFO
# Provides:          __CMD__
# Required-Start:    $network $local_fs
# Required-Stop:     $network
# Default-Start:     2 3 4 5
# Default-Stop:      0 1 6
# Short-Description: StarBT Shadowsocks server (sbt)
### END INIT INFO
DAEMON=__BIN__
PIDFILE=/var/run/__CMD__.pid
LOGFILE=__LOGFILE__

is_running() { [ -f "$PIDFILE" ] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null; }

start() {
    if is_running; then echo "$CMDNAME already running"; return 0; fi
    nohup "$DAEMON" daemon >> "$LOGFILE" 2>&1 < /dev/null &
    echo $! > "$PIDFILE"
    echo "$CMDNAME started"
}

stop() {
    if is_running; then kill "$(cat "$PIDFILE")" 2>/dev/null || true; fi
    rm -f "$PIDFILE"
    echo "$CMDNAME stopped"
}

case "$1" in
    start) start ;;
    stop|force-stop) stop ;;
    restart|force-reload) stop; sleep 1; start ;;
    status) if is_running; then echo "$CMDNAME running"; else echo "$CMDNAME stopped"; exit 1; fi ;;
    *) echo "Usage: $0 {start|stop|restart|status}"; exit 1 ;;
esac
EOF
    CMDNAME=$CMD
    export CMDNAME
    sed -e "s|__BIN__|$BIN|g" -e "s|__LOGFILE__|$LOGFILE|g" -e "s|__CMD__|$CMD|g" "$WORK/init.tmpl" > "$INIT"
    chmod +x "$INIT"
    if [ -x /usr/sbin/update-rc.d ]; then
        update-rc.d "$CMD" defaults >/dev/null 2>&1 || warn "注册开机自启失败，请手动执行: update-rc.d $CMD defaults"
    elif [ -x /sbin/chkconfig ] || [ -x /usr/sbin/chkconfig ]; then
        chkconfig --add "$CMD" >/dev/null 2>&1 || warn "注册开机自启失败，请手动执行: chkconfig --add $CMD"
    else
        add_cron_boot
    fi
    "$INIT" restart
    log "已注册为系统服务 (init.d)：关闭终端不影响运行"
}

install_nohup() {
    if have pkill; then pkill -f "$BIN daemon" 2>/dev/null || true; fi
    nohup "$BIN" daemon >> "$LOGFILE" 2>&1 < /dev/null &
    log "已在后台启动 (nohup)：关闭终端不影响运行"
    add_cron_boot
}

install_resident() {
    if [ -d /run/systemd/system ] && have systemctl; then install_systemd
    elif have rc-update; then install_openrc
    elif [ -d /etc/init.d ] || [ -x /usr/sbin/update-rc.d ] || [ -x /sbin/chkconfig ] || [ -x /usr/sbin/chkconfig ]; then install_sysv
    else install_nohup
    fi
}

open_firewall() {
    port=$(sed -n 's/^ss_port *= *"\{0,1\}\([0-9][0-9]*\).*/\1/p' "$CONF" | head -1)
    [ -n "$port" ] || return 0
    if have ufw && ufw status 2>/dev/null | grep -q 'Status: active'; then
        ufw allow "$port/tcp" >/dev/null 2>&1 && log "防火墙 (ufw) 已放行 TCP $port"
    elif have firewall-cmd && firewall-cmd --state >/dev/null 2>&1; then
        firewall-cmd -q --permanent --add-port="$port/tcp" </dev/null 2>/dev/null && \
            firewall-cmd -q --reload </dev/null 2>/dev/null && log "防火墙 (firewalld) 已放行 TCP $port"
    fi
}

# ==================== 主流程 ====================

if [ "${1:-}" = "--uninstall" ]; then
    uninstall
    exit 0
fi

[ "$(id -u)" = 0 ] || die "请用 root 执行：curl -fsSL <安装地址> | sudo sh"

OS_PRETTY=未知系统
if [ -f /etc/os-release ]; then
    OS_PRETTY=$(sed -n 's/^PRETTY_NAME=//p' /etc/os-release | head -1 | tr -d '"')
fi
case "$(uname -m)" in
    x86_64|amd64)   ARCH=amd64 ;;
    aarch64|arm64)  ARCH=arm64 ;;
    *) die "暂不支持架构 $(uname -m)（目前提供 amd64 / arm64）" ;;
esac
log "目标系统: ${OS_PRETTY:-未知系统} ($(uname -m) → $ARCH)"

ensure_tools
resolve_version
log "安装版本: $VERSION"

# ---------- 获取安装包 ----------
ASSET=sbt_linux_${ARCH}.tar.gz
LOCAL_TGZ=$(ls -1t ./sbt_linux_${ARCH}.tar.gz 2>/dev/null | head -1 || true)

if [ -n "$LOCAL_TGZ" ]; then
    log "检测到本地安装包 $LOCAL_TGZ，离线安装"
    if [ -f "$LOCAL_TGZ.sha256" ]; then
        [ "$(awk '{print $1}' "$LOCAL_TGZ.sha256")" = "$(sha256_of "$LOCAL_TGZ")" ] \
            || die "本地安装包 sha256 校验失败"
    fi
    cp "$LOCAL_TGZ" "$WORK/$ASSET"
else
    choose_mirror
    log "下载 $REL/$ASSET"
    fetch "$REL/$ASSET" "$WORK/$ASSET" || die "下载失败。可换源重试：SBT_MIRROR=jsdelivr 或 SBT_MIRROR=github"
    fetch "$REL/$ASSET.sha256" "$WORK/$ASSET.sha256" || die "下载 sha256 校验文件失败"
    [ "$(awk '{print $1}' "$WORK/$ASSET.sha256")" = "$(sha256_of "$WORK/$ASSET")" ] \
        || die "安装包 sha256 校验失败（文件损坏或被篡改），已中止安装"
    log "sha256 校验通过"
fi

tar -xzf "$WORK/$ASSET" -C "$WORK"
BIN_SRC=$(find "$WORK" -type f -name "$CMD" | head -1)
[ -n "$BIN_SRC" ] || BIN_SRC=$(find "$WORK" -type f -name "$CMD*" ! -name '*.gz' ! -name '*.sha256' | head -1)
[ -n "$BIN_SRC" ] || die "安装包里找不到 $CMD 可执行文件"
if have install; then install -m 755 "$BIN_SRC" "$BIN"; else cp "$BIN_SRC" "$BIN"; chmod 755 "$BIN"; fi
log "已安装 $BIN ($("$BIN" version))"
# 以实际安装的二进制版本为准（离线安装时可能与仓库里的最新版本号不同）
VERSION=$("$BIN" version 2>/dev/null | awk 'NR==1{print $2}' || true)
[ -n "$VERSION" ] || VERSION=$DEFAULT_VERSION

# ---------- 配置（幂等：已有配置与密码保持不变） ----------
mkdir -p "$CONF_DIR" "$DATA_DIR"
if [ ! -f "$CONF" ]; then
    printf '# StarBT 配置（含代理密码，权限 600）\nversion = "%s"\n' "$VERSION" > "$CONF"
fi
if ! grep -q '^ss_password' "$CONF"; then
    printf 'ss_method = "aes-256-gcm"\nss_port = "%s"\nss_password = "%s"\n' "$PORT" "$(gen_passwd)" >> "$CONF"
    log "已生成 Shadowsocks 服务端配置（端口 $PORT，密码随机生成）"
else
    log "已有配置，端口与密码保持不变"
fi
chmod 600 "$CONF"

# ---------- 常驻 ----------
install_resident
open_firewall || warn "放行防火墙端口失败，请手动放行 TCP 端口"

case ":$PATH:" in
    *:/usr/local/bin:*) ;;
    *) warn "PATH 中没有 /usr/local/bin，请使用完整路径: $BIN" ;;
esac

# ---------- 自检与结果 ----------
sleep 2
"$BIN" version >/dev/null 2>&1 || warn "自检未通过：$BIN 无法执行"

echo
log "=========================================================="
log " StarBT $VERSION 安装完成，已常驻后台（关闭终端不影响）"
log "----------------------------------------------------------"
"$BIN" ss || warn "读取连接信息失败，可稍后执行: $BIN ss"
echo
log " 管理菜单:  $CMD                 （SSH 里直接敲）"
log " 服务状态:  systemctl status $CMD   |   日志: journalctl -u $CMD -f"
log " 卸载:      sh install.sh --uninstall"
log "=========================================================="
