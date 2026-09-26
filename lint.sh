#!/bin/sh
# 代码质量检查：go vet + 死代码。
#
# 单独一个目标而不塞进 make test：deadcode 要额外装一次，而 make test 应该
# 在任何一台机器上都能直接跑。
set -e

cd "$(dirname "$0")"

say() { printf '\n==> %s\n' "$1"; }

say "go vet"
go vet ./...

DEAD=$(go env GOPATH)/bin/deadcode
if [ ! -x "$DEAD" ]; then
	printf '\n没装 deadcode，跳过死代码检查。装它（记得绕开代理）：\n'
	printf '  unset HTTP_PROXY HTTPS_PROXY http_proxy https_proxy\n'
	printf '  export GOPROXY=https://goproxy.cn,direct GOSUMDB=off\n'
	printf '  go install golang.org/x/tools/cmd/deadcode@latest\n'
	exit 0
fi

say "死代码"

# **gomobile 那道边界 deadcode 看不过去。** `mobile` 包的导出函数是给 Swift 调的，
# Go 这边一个调用点都没有，deadcode 于是把 Ask、AskLive、Snapshot、Providers 全报成
# 不可达——十几条噪音，人看两次就再也不看这个检查了。
#
# 所以豁免名单**从 Swift 的实际调用点现取**，不手写：gomobile 把 Go 的 `AskLive`
# 导成 ObjC 的 `MobileAskLive`，所以在 ios 的 .swift 里搜 `Mobile<名字>` 就知道那一侧
# 还用不用它。手写一份名单的话，哪天 Swift 不用了这儿也不会响——而那正是最该报出来
# 的时候（Go 侧留着一个没人调的导出函数，还得跟着维护）。
USED=$(mktemp)
trap 'rm -f "$USED" "$OUT"' EXIT
grep -rhoE '\bMobile[A-Z][A-Za-z0-9_]*' ios/*.swift 2>/dev/null \
	| sed 's/^Mobile//' | sort -u > "$USED"

OUT=$(mktemp)
"$DEAD" -test ./... > "$OUT" 2>&1 || true

LEFT=0
while IFS= read -r line; do
	[ -n "$line" ] || continue
	name=${line##*unreachable func: }
	# mobile 包的导出函数：Swift 那边用着就不算死的。
	case "$line" in
	mobile/*)
		if grep -qxF "$name" "$USED"; then
			continue
		fi
		;;
	esac
	echo "$line"
	LEFT=$((LEFT + 1))
done < "$OUT"

if [ "$LEFT" -gt 0 ]; then
	printf '\n上面这些没有任何调用点。确实不要了就删掉；\n'
	printf 'mobile 包里的先确认 ios/*.swift 那边是不是真的不用了。\n'
	exit 1
fi
echo "没有死代码"
