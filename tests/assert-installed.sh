#!/bin/sh
# CI 用：断言 StarBT 已正确安装并常驻，然后验证卸载是否干净。
# 由 GitHub Actions 在各发行版容器里执行（docker run --rm -v "$PWD:/src" <镜像> sh /src/tests/assert-installed.sh）
set -eu
cd "$(dirname "$0")/.."

fail() { printf '\033[31m[FAIL]\033[0m %s\n' "$1"; exit 1; }
ok()   { printf '\033[32m[ OK ]\033[0m %s\n' "$1"; }

EXPECT_VERSION=$(tr -d '[:space:]' < VERSION)

# 离线安装（sbt_linux_amd64.tar.gz 由 CI 放在仓库根目录）
sh install.sh </dev/null

BIN=/usr/local/bin/sbt
[ -x "$BIN" ] || fail "/usr/local/bin/sbt 不存在"
got=$("$BIN" version | awk '{print $2}')
[ "$got" = "$EXPECT_VERSION" ] || fail "版本不符: 期望 $EXPECT_VERSION，实际 $got"
ok "版本 $got"

grep -q '^ss_password = "' /etc/sbt/config.toml || fail "配置缺少 ss_password"
[ "$(stat -c %a /etc/sbt/config.toml 2>/dev/null || echo 600)" = 600 ] || fail "配置文件权限不是 600"
ok "配置已生成且权限 600"

"$BIN" ss | grep -q 'aes-256-gcm' || fail "sbt ss 未输出加密方式"
"$BIN" ss | grep -q 'ss://' || fail "sbt ss 未输出导入链接"
ok "sbt ss 输出连接信息"

# 进程常驻（不依赖 pgrep）
running=1
for f in /proc/[0-9]*/cmdline; do
    if tr '\0' ' ' < "$f" 2>/dev/null | grep -q 'sbt daemon'; then running=0; break; fi
done
[ "$running" = 0 ] || fail "sbt daemon 未在后台运行"
ok "sbt daemon 正在后台运行"

# 端口在监听（最多等 10 秒）
port=$(sed -n 's/^ss_port *= *"\([0-9]*\)".*/\1/p' /etc/sbt/config.toml)
i=0
until "$BIN" ss | grep -q "✓ $port 正在监听"; do
    i=$((i + 1))
    [ "$i" -lt 20 ] || fail "端口 $port 未监听"
    sleep 0.5
done
ok "端口 $port 正在监听"

# 卸载必须干净
sh install.sh --uninstall </dev/null | grep -q '已卸载' || fail "卸载无输出"
[ ! -e "$BIN" ] || fail "卸载后二进制仍在"
[ ! -e /etc/sbt/config.toml ] || fail "卸载后配置仍在"
[ ! -e /etc/systemd/system/sbt.service ] || fail "卸载后 systemd unit 仍在"
[ ! -e /etc/init.d/sbt ] || fail "卸载后 init 脚本仍在"
[ ! -e /etc/cron.d/sbt ] || fail "卸载后 cron 项仍在"
ok "卸载干净"

printf '\n全部检查通过：%s\n' "$(sed -n 's/^PRETTY_NAME=//p' /etc/os-release 2>/dev/null | tr -d '"' || uname -s)"
