#!/bin/sh
# 把 Jarvis 装到插着的 iPhone 上。
#
# 前两步没法自动做：Apple ID 要在 Xcode 图形界面里登，手机要物理插上。
# 所以这个脚本先把条件检查清楚、把缺什么说明白，条件齐了再一路做到底。
set -e

PROJ=ios/Jarvis.xcodeproj
BUNDLE=${BUNDLE_ID:-com.dabao.jarvis}

say() { printf '\n==> %s\n' "$1"; }
die() { printf '\n%s\n' "$1" >&2; exit 1; }

# --- 1. 完整 Xcode ---
xcrun --sdk iphoneos --show-sdk-path >/dev/null 2>&1 || die "需要完整 Xcode：
  sudo xcode-select -s /Applications/Xcode.app/Contents/Developer"

# --- 2. framework ---
#
# 不光要在，还得**比 Go 源码新**。
#
# 踩过：直接跑这个脚本（不走 make ios-install）时，Go 那边的改动根本没有
# 重新编进 framework。Swift 照常编过、照常装上、照常跑起来，只是桥那头
# 还是旧的——出参里少了刚加的字段，界面拿默认值顶上，看着一切正常，
# 实际上改的东西一点没生效。查了半天才发现装的是旧 Go。
#
# 所以这儿自己判、自己补，别指望每次都记得先跑 make。
[ -d ios/Mobile.xcframework ] || NEED_FW=1
if [ -z "$NEED_FW" ]; then
	NEWER=$(find . -name '*.go' -not -path './ios/*' -not -name '*_test.go' \
	        -newer ios/Mobile.xcframework -print -quit 2>/dev/null)
	[ -n "$NEWER" ] && NEED_FW=1
fi
if [ -n "$NEED_FW" ]; then
	say "Go 改过了，重新编 framework"
	make ios-framework || die "framework 没编出来"
fi

# --- 3. Team ID ---
# 证书不用预先存在：第一次带 -allowProvisioningUpdates 签名构建时，
# Xcode 会把开发证书和描述文件一起办好。所以这里只认 Team，不认证书。
# 找 Team 的顺序：环境变量 → Xcode 里登的账号 → 已有证书的 OU。
TEAM=$DEVELOPMENT_TEAM
if [ -z "$TEAM" ]; then
	# plist 里写的是 `teamID = XXXXXXXXXX;`，带不带引号都有可能。
	TEAM=$(defaults read com.apple.dt.Xcode IDEProvisioningTeamByIdentifier 2>/dev/null \
	       | sed -n 's/.*teamID[[:space:]]*=[[:space:]]*"\{0,1\}\([A-Z0-9]\{10\}\)"\{0,1\};.*/\1/p' \
	       | head -1)
fi
if [ -z "$TEAM" ]; then
	IDENT=$(security find-identity -v -p codesigning 2>/dev/null \
	        | grep -o '"Apple Develop[^"]*"' | head -1 | tr -d '"')
	[ -n "$IDENT" ] && TEAM=$(security find-certificate -c "$IDENT" -p 2>/dev/null \
	      | openssl x509 -noout -subject 2>/dev/null \
	      | tr ',/' '\n\n' | awk -F= '$1=="OU"||$1==" OU"{gsub(/ /,"",$2); print $2}' | head -1)
fi
[ -n "$TEAM" ] || die "Xcode 里还没登 Apple ID：
  Xcode → Settings → Accounts → + → Apple ID
免费账号就行。登完再跑一次。"

# --- 4. 真机 ---
# devicectl 会把模拟器也列出来，Reality 那列写着 simulated，要滤掉。
# 状态有 connected / available (paired) / unavailable 几种。只认 connected
# 的话，无线配对或者刚插上还没完全就绪的设备会被自己的守卫拦下来——
# 真不可用的话 xcodebuild 会报得更清楚，不必在这儿提前判死。
UDID=${DEVICE_ID:-$(xcrun devicectl list devices 2>/dev/null \
      | awk '$0 !~ /simulated/ && $0 !~ /unavailable/ && /(connected|available)/ \
             {for(i=1;i<=NF;i++) if($i ~ /^[0-9A-F-]{25,}$/) {print $i; exit}}')}
[ -n "$UDID" ] || die "没有插着的 iPhone。
插上数据线，手机上点「信任此电脑」，解锁屏幕，再跑一次。
（第一次连还要在 Xcode 里等它 Preparing device 走完）

手机是硬条件，不是图省事：免费账号的开发证书和描述文件都得有一台已注册
的设备才能生成，Apple 那边直接回绝——
  Your team has no devices from which to generate a provisioning profile."

say "Team $TEAM / 设备 $UDID"

say "编译并签名"
# 第一次连这台手机时，Xcode 会顺手把开发证书和描述文件办掉
# （-allowProvisioningUpdates），所以证书不用预先存在。
# 输出重定向到文件再回放，不走管道：管道的退出码是最后一个命令（tee）的，
# 编译失败也会被当成成功，然后把上一次的旧包装到手机上。
#
# `|| STATUS=$?` 是必需的：脚本开头有 set -e，编译一失败就直接退出了，
# 下面 cat 日志和那几条对症的说明一个都跑不到——屏幕上只剩「编译并签名」
# 四个字，真正的原因躺在临时文件里没人看见。踩过。
LOG=$(mktemp)
STATUS=0
xcodebuild -project "$PROJ" -scheme Jarvis -configuration Debug \
  -destination "id=$UDID" -allowProvisioningUpdates \
  DEVELOPMENT_TEAM="$TEAM" PRODUCT_BUNDLE_IDENTIFIER="$BUNDLE" \
  build >"$LOG" 2>&1 || STATUS=$?
cat "$LOG"
if [ $STATUS -ne 0 ]; then
	# 几个第一次装必踩的，直接说清楚怎么办，别让人去翻 xcodebuild 的输出。
	if grep -q "Developer Mode disabled" "$LOG"; then
		rm -f "$LOG"
		die "手机上的「开发者模式」没开（iOS 16 以后加的一道开关，
这台手机被 Xcode 碰过一次之后才会在设置里出现）：

  设置 → 隐私与安全性 → 开发者模式 → 打开 → 按提示重启手机

重启解锁后会再弹一个确认框，点「打开」。然后再跑一次。"
	fi
	if grep -q "doesn.t include the .* capability" "$LOG"; then
		WHAT=$(sed -n "s/.*doesn't include the \(.*\) capability.*/\1/p" "$LOG" | head -1)
		rm -f "$LOG"
		die "描述文件里还没有 $WHAT 这项能力。

加一项能力要跟 Apple 那边打个照面，而命令行拿不到 Xcode 里登的账号
（它会报 No Accounts）。所以这一步得在 Xcode 里点一次，只用点一次：

  1. open ios/Jarvis.xcodeproj
  2. 左边选 Jarvis target → Signing & Capabilities
  3. 左上角 + Capability，双击 $WHAT
  4. 等它把描述文件刷新好（签名那一栏不再报红）

然后关掉 Xcode，再跑一次这个脚本。"
	fi
	if grep -q "no devices from which to generate" "$LOG"; then
		rm -f "$LOG"
		die "Apple 那头还没认下这台设备。手机保持解锁、插好，等一会儿再跑一次。"
	fi
	if grep -q "cannot find .* in scope" "$LOG"; then
		rm -f "$LOG"
		die "有源文件不在 Xcode 工程里。新加了 .swift 就跑一下：
  make ios-project"
	fi
	rm -f "$LOG"
	die "编译失败，上面是 xcodebuild 的输出。"
fi
rm -f "$LOG"

APP=$(xcodebuild -project "$PROJ" -scheme Jarvis -configuration Debug \
  -destination "id=$UDID" DEVELOPMENT_TEAM="$TEAM" -showBuildSettings 2>/dev/null \
  | awk -F' = ' '/ BUILT_PRODUCTS_DIR/{print $2; exit}')/Jarvis.app
[ -d "$APP" ] || die "编译完了但找不到 $APP"

say "装到手机"
xcrun devicectl device install app --device "$UDID" "$APP"

say "启动"
xcrun devicectl device process launch --device "$UDID" "$BUNDLE" || true

cat <<'TIP'

装上了。手机上第一次打开会提示「不受信任的开发者」：
  设置 → 通用 → VPN 与设备管理 → 点你的 Apple ID → 信任

然后在 app 里选 iCloud Drive → Jarvis 那个目录，进设置填模型密钥。

免费 Apple ID 签的七天过期，到期插上电脑再跑一次这个脚本就行。
TIP
