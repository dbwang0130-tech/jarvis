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
[ -d ios/Mobile.xcframework ] || die "还没生成 framework：
  make ios-framework"

# --- 3. 签名身份 ---
# 免费 Apple ID 签出来的证书也叫 Apple Development，一样能用，只是七天过期。
IDENT=$(security find-identity -v -p codesigning 2>/dev/null \
        | grep -o '"Apple Develop[^"]*"' | head -1 | tr -d '"')
[ -n "$IDENT" ] || die "Xcode 里还没加 Apple ID（现在 0 个签名身份）：
  Xcode → Settings → Accounts → + → Apple ID
加完再跑一次。"

# 证书的 OU 就是 Team ID。没显式指定就从证书里取。
TEAM=${DEVELOPMENT_TEAM:-$(security find-certificate -c "$IDENT" -p 2>/dev/null \
      | openssl x509 -noout -subject 2>/dev/null \
      | tr ',/' '\n\n' | awk -F= '$1=="OU"||$1==" OU"{gsub(/ /,"",$2); print $2}' | head -1)}
[ -n "$TEAM" ] || die "取不到 Team ID，用环境变量指定：
  DEVELOPMENT_TEAM=XXXXXXXXXX sh ios/install-device.sh"

# --- 4. 真机 ---
# devicectl 会把模拟器也列出来，Reality 那列写着 simulated，要滤掉。
UDID=${DEVICE_ID:-$(xcrun devicectl list devices 2>/dev/null \
      | awk '$0 !~ /simulated/ && /connected/ {for(i=1;i<=NF;i++) if($i ~ /^[0-9A-F-]{25,}$/) {print $i; exit}}')}
[ -n "$UDID" ] || die "没有插着的 iPhone。
插上数据线，手机上点「信任此电脑」，解锁屏幕，再跑一次。
（第一次连还要在 Xcode 里等它 Preparing device 走完）"

say "身份 $IDENT / Team $TEAM / 设备 $UDID"

say "编译并签名"
xcodebuild -project "$PROJ" -scheme Jarvis -configuration Debug \
  -destination "id=$UDID" -allowProvisioningUpdates \
  DEVELOPMENT_TEAM="$TEAM" PRODUCT_BUNDLE_IDENTIFIER="$BUNDLE" \
  build

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
