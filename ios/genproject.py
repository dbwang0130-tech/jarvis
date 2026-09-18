#!/usr/bin/env python3
"""重新生成 ios/Jarvis.xcodeproj。

手写的 pbxproj 里文件清单是钉死的，加一个 .swift 就要手改，忘了改的表现是
`cannot find 'X' in scope`——看着像代码错，其实是文件没进工程。所以这里按
目录里实际有的 .swift 生成，加文件只要跑一次 `make ios-project`。

工程里的东西不多：一个 app target、十来个源文件、一个 framework，外加一个
**按需出现**的小组件 target。真到了需要更复杂依赖管理的规模，再换 XcodeGen
之类不迟。

小组件为什么是「按需」：它跟主 app 靠 App Group 共享容器传数据，而 App Group
要描述文件支持。免费 Apple ID 签不了——硬把它写进 entitlements 的话，
**连主 app 都装不上了**，等于拿一个能用的 app 换一个装不了的小组件。
所以这儿看 Jarvis.entitlements 里有没有 App Group：有就把小组件装配进来，
没有就当没这回事。在 Xcode 里点通 App Groups 之后重跑一次，它自己就进来了。
"""
import hashlib
import os
import re
import pathlib
import subprocess

IOS = pathlib.Path(__file__).resolve().parent
FRAMEWORK = "Mobile.xcframework"
ASSETS = "Assets.xcassets"
PRIVACY = "PrivacyInfo.xcprivacy"
LAUNCH = "LaunchScreen.storyboard"
ENTITLEMENTS = "Jarvis.entitlements"

# 小组件那一摊。源文件在 Widget/ 下，另外还要跟主 app 共用几个文件
# （品牌那套颜色和头像、两边都要认的那份数据结构）。
WIDGET_DIR = "Widget"
WIDGET_NAME = "JarvisWidget"
WIDGET_PLIST = "Widget/Info.plist"
WIDGET_ENTITLEMENTS = "Widget/JarvisWidget.entitlements"
WIDGET_SHARED = ["Brand.swift", "WidgetShared.swift", "WidgetViews.swift"]


def widget_ready() -> bool:
    """App Group 配好了没有。

    只看主 app 的 entitlements 里有没有那一项——Xcode 里点了
    「+ Capability → App Groups」就会写进去。没配就不生成小组件 target：
    带着签不了的能力，整个 app 都装不上。
    """
    f = IOS / ENTITLEMENTS
    if not f.is_file():
        return False
    return "application-groups" in f.read_text()


def find_team() -> str:
    """找开发者团队 ID。

    原来这里写死留空，团队 ID 由 install-device.sh 在命令行传进去。命令行
    那条路是够用的，可**在 Xcode 里按一次 ⌘B 就不行**——它只看工程里写的
    那个值，看到空的就报「Signing requires a development team」。

    而有些事只能在 Xcode 里做：加一项能力（比如 HealthKit）要跟 Apple 那边
    换新的描述文件，命令行拿不到登录态。所以工程里也得有这个值。

    找的顺序跟装机脚本一致：环境变量 → Xcode 里登的账号。都没有就留空，
    照旧由命令行传——只是那样在 Xcode 里点不动。
    """
    if t := os.environ.get("DEVELOPMENT_TEAM", "").strip():
        return t
    try:
        out = subprocess.run(
            ["defaults", "read", "com.apple.dt.Xcode",
             "IDEProvisioningTeamByIdentifier"],
            capture_output=True, text=True, timeout=10).stdout
    except Exception:
        return ""
    m = re.search(r'teamID\s*=\s*"?([A-Z0-9]{10})"?\s*;', out)
    return m.group(1) if m else ""
# 跟 install-device.sh 认同一个环境变量。写进工程而不是靠命令行覆盖——
# 命令行上的 build setting 会盖住**所有** target，小组件那个 .Widget 后缀
# 也会被冲掉，装的时候报「父 bundle 和子 bundle 同名」。
BUNDLE_ID = os.environ.get("BUNDLE_ID", "com.dabao.jarvis")
DEPLOY_TARGET = "26.0"  # Liquid Glass 那套要 26 起；设备是 iPhone 17 Pro，本来就跑 26+


def uid(seed: str) -> str:
    """pbxproj 的对象 id 是 24 位十六进制。按名字算，保证每次生成结果一样——
    否则每跑一次 git 里就是一整片改动。"""
    return hashlib.sha1(seed.encode()).hexdigest()[:24].upper()


def main() -> None:
    # JarvisApp.swift 排最前只是为了在 Xcode 的导航器里好找。
    swift = sorted(p.name for p in IOS.glob("*.swift"))
    swift.sort(key=lambda n: (n != "JarvisApp.swift", n))
    if not swift:
        raise SystemExit("ios/ 下一个 .swift 都没有")

    refs, builds, sources, children = [], [], [], []
    for name in swift:
        fr, bf = uid("fr:" + name), uid("bf:" + name)
        refs.append(f'\t\t{fr} /* {name} */ = {{isa = PBXFileReference; '
                    f'lastKnownFileType = sourcecode.swift; path = {name}; '
                    f'sourceTree = "<group>"; }};')
        builds.append(f'\t\t{bf} /* {name} in Sources */ = {{isa = PBXBuildFile; '
                      f'fileRef = {fr} /* {name} */; }};')
        sources.append(f'\t\t\t\t{bf} /* {name} in Sources */,')
        children.append(f'\t\t\t\t{fr} /* {name} */,')

    # 资源目录：有就带上，没有就当没这回事——图标是可选的。
    assets_ref, assets_build = uid("fr:assets"), uid("bf:assets")
    has_assets = (IOS / ASSETS).is_dir()
    resources: list[str] = []
    if has_assets:
        refs.append(f'\t\t{assets_ref} /* {ASSETS} */ = {{isa = PBXFileReference; '
                    f'lastKnownFileType = folder.assetcatalog; path = {ASSETS}; '
                    f'sourceTree = "<group>"; }};')
        builds.append(f'\t\t{assets_build} /* {ASSETS} in Resources */ = '
                      f'{{isa = PBXBuildFile; fileRef = {assets_ref} /* {ASSETS} */; }};')
        resources.append(f'\t\t\t\t{assets_build} /* {ASSETS} in Resources */,')
        children.append(f'\t\t\t\t{assets_ref} /* {ASSETS} */,')

    # 隐私清单要进 bundle（不是 Info.plist 的一部分），所以走资源那条路。
    priv_ref, priv_build = uid("fr:privacy"), uid("bf:privacy")
    if (IOS / PRIVACY).is_file():
        refs.append(f'\t\t{priv_ref} /* {PRIVACY} */ = {{isa = PBXFileReference; '
                    f'lastKnownFileType = text.xml; path = {PRIVACY}; '
                    f'sourceTree = "<group>"; }};')
        builds.append(f'\t\t{priv_build} /* {PRIVACY} in Resources */ = '
                      f'{{isa = PBXBuildFile; fileRef = {priv_ref} /* {PRIVACY} */; }};')
        resources.append(f'\t\t\t\t{priv_build} /* {PRIVACY} in Resources */,')
        children.append(f'\t\t\t\t{priv_ref} /* {PRIVACY} */,')

    # 启动屏。也是资源，编译成 storyboardc 进 bundle。
    ls_ref, ls_build = uid("fr:launch"), uid("bf:launch")
    if (IOS / LAUNCH).is_file():
        refs.append(f'\t\t{ls_ref} /* {LAUNCH} */ = {{isa = PBXFileReference; '
                    f'lastKnownFileType = file.storyboard; path = {LAUNCH}; '
                    f'sourceTree = "<group>"; }};')
        builds.append(f'\t\t{ls_build} /* {LAUNCH} in Resources */ = '
                      f'{{isa = PBXBuildFile; fileRef = {ls_ref} /* {LAUNCH} */; }};')
        resources.append(f'\t\t\t\t{ls_build} /* {LAUNCH} in Resources */,')
        children.append(f'\t\t\t\t{ls_ref} /* {LAUNCH} */,')

    fw_ref, fw_build, fw_embed = uid("fr:fw"), uid("bf:fw"), uid("bf:fwembed")
    refs.append(f'\t\t{fw_ref} /* {FRAMEWORK} */ = {{isa = PBXFileReference; '
                f'lastKnownFileType = wrapper.xcframework; path = {FRAMEWORK}; '
                f'sourceTree = "<group>"; }};')
    builds.append(f'\t\t{fw_build} /* {FRAMEWORK} in Frameworks */ = '
                  f'{{isa = PBXBuildFile; fileRef = {fw_ref} /* {FRAMEWORK} */; }};')
    builds.append(f'\t\t{fw_embed} /* {FRAMEWORK} in Embed Frameworks */ = '
                  f'{{isa = PBXBuildFile; fileRef = {fw_ref} /* {FRAMEWORK} */; '
                  f'settings = {{ATTRIBUTES = (CodeSignOnCopy, RemoveHeadersOnCopy, ); }}; }};')
    children.append(f'\t\t\t\t{fw_ref} /* {FRAMEWORK} */,')

    i = {k: uid(k) for k in (
        "project target mainGroup productsGroup product sources frameworks "
        "resources embed projCfgList targetCfgList projDebug projRelease "
        "tgtDebug tgtRelease").split()}

    # ---- 小组件 ----
    #
    # 装配与否看 App Group 配没配（widget_ready）。下面这一堆片段在没配的
    # 时候全是空串，拼进模板里就等于不存在——这样模板只有一份，不必为
    # 「带/不带小组件」写两套。
    w = {k: uid("w:" + k) for k in (
        "target product sources resources embed embedFile cfgList debug release "
        "dep proxy group plistRef entRef").split()}
    widget = widget_ready()
    w_builds, w_refs, w_sources, w_resources, w_children = [], [], [], [], []
    if widget:
        wdir = IOS / WIDGET_DIR
        own = sorted(p.name for p in wdir.glob("*.swift"))
        if not own:
            raise SystemExit(f"{WIDGET_DIR}/ 下一个 .swift 都没有")

        # 小组件自己的源文件。
        for name in own:
            path = f"{WIDGET_DIR}/{name}"
            fr, bf = uid("wfr:" + path), uid("wbf:" + path)
            w_refs.append(f'\t\t{fr} /* {name} */ = {{isa = PBXFileReference; '
                          f'lastKnownFileType = sourcecode.swift; path = {name}; '
                          f'sourceTree = "<group>"; }};')
            w_builds.append(f'\t\t{bf} /* {name} in Sources */ = {{isa = PBXBuildFile; '
                            f'fileRef = {fr} /* {name} */; }};')
            w_sources.append(f'\t\t\t\t{bf} /* {name} in Sources */,')
            w_children.append(f'\t\t\t\t{fr} /* {name} */,')

        # 跟主 app 共用的那几个：**文件引用是同一个，build file 各是各的**。
        # 一个文件进两个 target，就得有两条 PBXBuildFile——共用一条的话
        # Xcode 会把它当成只属于先出现的那个 target。
        for name in WIDGET_SHARED:
            if not (IOS / name).is_file():
                raise SystemExit(f"小组件要用的 {name} 不见了")
            bf = uid("wbf:shared:" + name)
            w_builds.append(f'\t\t{bf} /* {name} in Sources */ = {{isa = PBXBuildFile; '
                            f'fileRef = {uid("fr:" + name)} /* {name} */; }};')
            w_sources.append(f'\t\t\t\t{bf} /* {name} in Sources */,')

        # 头像和配色都在资源目录里，小组件也得带一份。
        if has_assets:
            bf = uid("wbf:assets")
            w_builds.append(f'\t\t{bf} /* {ASSETS} in Resources */ = '
                            f'{{isa = PBXBuildFile; fileRef = {assets_ref} /* {ASSETS} */; }};')
            w_resources.append(f'\t\t\t\t{bf} /* {ASSETS} in Resources */,')

        # Info.plist 和 entitlements 只是被 build settings 指着，不参与编译，
        # 但列进工程里 Xcode 的导航器才看得到。
        w_refs.append(f'\t\t{w["plistRef"]} /* Info.plist */ = {{isa = PBXFileReference; '
                      f'lastKnownFileType = text.plist.xml; path = Info.plist; '
                      f'sourceTree = "<group>"; }};')
        w_refs.append(f'\t\t{w["entRef"]} /* {WIDGET_NAME}.entitlements */ = '
                      f'{{isa = PBXFileReference; lastKnownFileType = text.plist.entitlements; '
                      f'path = {WIDGET_NAME}.entitlements; sourceTree = "<group>"; }};')
        w_children.append(f'\t\t\t\t{w["plistRef"]} /* Info.plist */,')
        w_children.append(f'\t\t\t\t{w["entRef"]} /* {WIDGET_NAME}.entitlements */,')

        # .appex 要塞进主 app 的 PlugIns 里，所以主 app 那边也多一条 build file。
        builds.append(f'\t\t{w["embedFile"]} /* {WIDGET_NAME}.appex in Embed Foundation Extensions */ = '
                      f'{{isa = PBXBuildFile; fileRef = {w["product"]} /* {WIDGET_NAME}.appex */; '
                      f'settings = {{ATTRIBUTES = (RemoveHeadersOnCopy, ); }}; }};')

    common = f'''				ALWAYS_SEARCH_USER_PATHS = NO;
				CLANG_ENABLE_MODULES = YES;
				CLANG_ENABLE_OBJC_ARC = YES;
				ENABLE_STRICT_OBJC_MSGSEND = YES;
				IPHONEOS_DEPLOYMENT_TARGET = {DEPLOY_TARGET};
				SDKROOT = iphoneos;
				SWIFT_VERSION = 5.0;'''

    appicon = ("\n\t\t\t\tASSETCATALOG_COMPILER_APPICON_NAME = AppIcon;"
               if has_assets else "")
    team = find_team()
    target_common = f'''				ASSETCATALOG_COMPILER_GLOBAL_ACCENT_COLOR_NAME = "";{appicon}
				CODE_SIGN_ENTITLEMENTS = {ENTITLEMENTS};
				CODE_SIGN_STYLE = Automatic;
				CURRENT_PROJECT_VERSION = 1;
				DEVELOPMENT_TEAM = "{team}";
				ENABLE_PREVIEWS = YES;
				FRAMEWORK_SEARCH_PATHS = (
					"$(inherited)",
					"$(PROJECT_DIR)",
				);
				GENERATE_INFOPLIST_FILE = YES;
				INFOPLIST_KEY_CFBundleDisplayName = Jarvis;
				INFOPLIST_KEY_ITSAppUsesNonExemptEncryption = NO;
				INFOPLIST_KEY_NSHealthShareUsageDescription = "读你在「健康」里记下的体重、睡眠、步数这些，存进你自己的健康记录文件。只读不写，也不上传。";
				INFOPLIST_KEY_UIApplicationSceneManifest_Generation = YES;
				INFOPLIST_KEY_UIApplicationSupportsIndirectInputEvents = YES;
				INFOPLIST_KEY_UILaunchStoryboardName = LaunchScreen;
				INFOPLIST_KEY_UISupportedInterfaceOrientations_iPad = "UIInterfaceOrientationPortrait UIInterfaceOrientationPortraitUpsideDown UIInterfaceOrientationLandscapeLeft UIInterfaceOrientationLandscapeRight";
				INFOPLIST_KEY_UISupportedInterfaceOrientations_iPhone = "UIInterfaceOrientationPortrait";
				LD_RUNPATH_SEARCH_PATHS = (
					"$(inherited)",
					"@executable_path/Frameworks",
				);
				MARKETING_VERSION = 1.0;
				PRODUCT_BUNDLE_IDENTIFIER = {BUNDLE_ID};
				PRODUCT_NAME = "$(TARGET_NAME)";
				SUPPORTED_PLATFORMS = "iphoneos iphonesimulator";
				SWIFT_EMIT_LOC_STRINGS = YES;
				TARGETED_DEVICE_FAMILY = 1;'''

    nl = "\n"

    # ---- 小组件在 pbxproj 里的那些块 ----
    #
    # 没配 App Group 时全是空串：模板照拼不误，拼出来就跟没有小组件一样。
    w_target_cfg = f'''				ASSETCATALOG_COMPILER_GLOBAL_ACCENT_COLOR_NAME = "";
				CODE_SIGN_ENTITLEMENTS = {WIDGET_ENTITLEMENTS};
				CODE_SIGN_STYLE = Automatic;
				CURRENT_PROJECT_VERSION = 1;
				DEVELOPMENT_TEAM = "{team}";
				ENABLE_PREVIEWS = YES;
				GENERATE_INFOPLIST_FILE = YES;
				INFOPLIST_FILE = {WIDGET_PLIST};
				INFOPLIST_KEY_CFBundleDisplayName = "今天";
				INFOPLIST_KEY_NSHumanReadableCopyright = "";
				LD_RUNPATH_SEARCH_PATHS = (
					"$(inherited)",
					"@executable_path/Frameworks",
					"@executable_path/../../Frameworks",
				);
				MARKETING_VERSION = 1.0;
				PRODUCT_BUNDLE_IDENTIFIER = {BUNDLE_ID}.Widget;
				PRODUCT_NAME = "$(TARGET_NAME)";
				SKIP_INSTALL = YES;
				SUPPORTED_PLATFORMS = "iphoneos iphonesimulator";
				SWIFT_EMIT_LOC_STRINGS = YES;
				TARGETED_DEVICE_FAMILY = 1;''' if widget else ""

    w_product_ref = (f'\n\t\t{w["product"]} /* {WIDGET_NAME}.appex */ = {{isa = PBXFileReference; '
                     f'explicitFileType = "wrapper.app-extension"; includeInIndex = 0; '
                     f'path = {WIDGET_NAME}.appex; sourceTree = BUILT_PRODUCTS_DIR; }};'
                     ) if widget else ""

    w_products_child = (f'\n\t\t\t\t{w["product"]} /* {WIDGET_NAME}.appex */,'
                        ) if widget else ""

    w_group_block = (f'''
		{w["group"]} /* {WIDGET_DIR} */ = {{
			isa = PBXGroup;
			children = (
{nl.join(w_children)}
			);
			path = {WIDGET_DIR};
			sourceTree = "<group>";
		}};''') if widget else ""

    w_group_child = f'\n\t\t\t\t{w["group"]} /* {WIDGET_DIR} */,' if widget else ""

    w_main_phase = (f'\n\t\t\t\t{w["embed"]} /* Embed Foundation Extensions */,'
                    ) if widget else ""
    w_main_dep = f'\n\t\t\t\t{w["dep"]} /* PBXTargetDependency */,' if widget else ""

    w_embed_block = (f'''
		{w["embed"]} /* Embed Foundation Extensions */ = {{
			isa = PBXCopyFilesBuildPhase;
			buildActionMask = 2147483647;
			dstPath = "";
			dstSubfolderSpec = 13;
			files = (
				{w["embedFile"]} /* {WIDGET_NAME}.appex in Embed Foundation Extensions */,
			);
			name = "Embed Foundation Extensions";
			runOnlyForDeploymentPostprocessing = 0;
		}};''') if widget else ""

    w_target_block = (f'''
		{w["target"]} /* {WIDGET_NAME} */ = {{
			isa = PBXNativeTarget;
			buildConfigurationList = {w["cfgList"]} /* Build configuration list for PBXNativeTarget "{WIDGET_NAME}" */;
			buildPhases = (
				{w["sources"]} /* Sources */,
				{w["resources"]} /* Resources */,
			);
			buildRules = (
			);
			dependencies = (
			);
			name = {WIDGET_NAME};
			productName = {WIDGET_NAME};
			productReference = {w["product"]} /* {WIDGET_NAME}.appex */;
			productType = "com.apple.product-type.app-extension";
		}};''') if widget else ""

    w_dep_block = (f'''
/* Begin PBXContainerItemProxy section */
		{w["proxy"]} /* PBXContainerItemProxy */ = {{
			isa = PBXContainerItemProxy;
			containerPortal = {i["project"]} /* Project object */;
			proxyType = 1;
			remoteGlobalIDString = {w["target"]};
			remoteInfo = {WIDGET_NAME};
		}};
/* End PBXContainerItemProxy section */

/* Begin PBXTargetDependency section */
		{w["dep"]} /* PBXTargetDependency */ = {{
			isa = PBXTargetDependency;
			target = {w["target"]} /* {WIDGET_NAME} */;
			targetProxy = {w["proxy"]} /* PBXContainerItemProxy */;
		}};
/* End PBXTargetDependency section */
''') if widget else ""

    w_phases_block = (f'''
		{w["sources"]} /* Sources */ = {{
			isa = PBXSourcesBuildPhase;
			buildActionMask = 2147483647;
			files = (
{nl.join(w_sources)}
			);
			runOnlyForDeploymentPostprocessing = 0;
		}};''') if widget else ""

    w_res_block = (f'''
		{w["resources"]} /* Resources */ = {{
			isa = PBXResourcesBuildPhase;
			buildActionMask = 2147483647;
			files = (
{nl.join(w_resources)}
			);
			runOnlyForDeploymentPostprocessing = 0;
		}};''') if widget else ""

    w_cfg_block = (f'''
		{w["debug"]} /* Debug */ = {{
			isa = XCBuildConfiguration;
			buildSettings = {{
{w_target_cfg}
			}};
			name = Debug;
		}};
		{w["release"]} /* Release */ = {{
			isa = XCBuildConfiguration;
			buildSettings = {{
{w_target_cfg}
			}};
			name = Release;
		}};''') if widget else ""

    w_cfglist_block = (f'''
		{w["cfgList"]} /* Build configuration list for PBXNativeTarget "{WIDGET_NAME}" */ = {{
			isa = XCConfigurationList;
			buildConfigurations = (
				{w["debug"]} /* Debug */,
				{w["release"]} /* Release */,
			);
			defaultConfigurationIsVisible = 0;
			defaultConfigurationName = Release;
		}};''') if widget else ""

    w_targets_entry = f'\n\t\t\t\t{w["target"]} /* {WIDGET_NAME} */,' if widget else ""
    w_target_attr = (f'''
					{w["target"]} = {{
						CreatedOnToolsVersion = 16.0;
					}};''') if widget else ""

    all_builds = builds + w_builds
    all_refs = refs + w_refs

    pbx = f'''// !$*UTF8*$!
{{
	archiveVersion = 1;
	classes = {{
	}};
	objectVersion = 56;
	objects = {{

/* Begin PBXBuildFile section */
{nl.join(all_builds)}
/* End PBXBuildFile section */

/* Begin PBXCopyFilesBuildPhase section */
		{i["embed"]} /* Embed Frameworks */ = {{
			isa = PBXCopyFilesBuildPhase;
			buildActionMask = 2147483647;
			dstPath = "";
			dstSubfolderSpec = 10;
			files = (
				{fw_embed} /* {FRAMEWORK} in Embed Frameworks */,
			);
			name = "Embed Frameworks";
			runOnlyForDeploymentPostprocessing = 0;
		}};{w_embed_block}
/* End PBXCopyFilesBuildPhase section */

/* Begin PBXFileReference section */
{nl.join(all_refs)}
		{i["product"]} /* Jarvis.app */ = {{isa = PBXFileReference; explicitFileType = wrapper.application; includeInIndex = 0; path = Jarvis.app; sourceTree = BUILT_PRODUCTS_DIR; }};{w_product_ref}
/* End PBXFileReference section */

/* Begin PBXFrameworksBuildPhase section */
		{i["frameworks"]} /* Frameworks */ = {{
			isa = PBXFrameworksBuildPhase;
			buildActionMask = 2147483647;
			files = (
				{fw_build} /* {FRAMEWORK} in Frameworks */,
			);
			runOnlyForDeploymentPostprocessing = 0;
		}};
/* End PBXFrameworksBuildPhase section */

/* Begin PBXGroup section */
		{i["mainGroup"]} = {{
			isa = PBXGroup;
			children = (
{nl.join(children)}{w_group_child}
				{i["productsGroup"]} /* Products */,
			);
			sourceTree = "<group>";
		}};
		{i["productsGroup"]} /* Products */ = {{
			isa = PBXGroup;
			children = (
				{i["product"]} /* Jarvis.app */,{w_products_child}
			);
			name = Products;
			sourceTree = "<group>";
		}};{w_group_block}
/* End PBXGroup section */

/* Begin PBXNativeTarget section */
		{i["target"]} /* Jarvis */ = {{
			isa = PBXNativeTarget;
			buildConfigurationList = {i["targetCfgList"]} /* Build configuration list for PBXNativeTarget "Jarvis" */;
			buildPhases = (
				{i["sources"]} /* Sources */,
				{i["frameworks"]} /* Frameworks */,
				{i["resources"]} /* Resources */,
				{i["embed"]} /* Embed Frameworks */,{w_main_phase}
			);
			buildRules = (
			);
			dependencies = ({w_main_dep}
			);
			name = Jarvis;
			productName = Jarvis;
			productReference = {i["product"]} /* Jarvis.app */;
			productType = "com.apple.product-type.application";
		}};{w_target_block}
/* End PBXNativeTarget section */
{w_dep_block}

/* Begin PBXProject section */
		{i["project"]} /* Project object */ = {{
			isa = PBXProject;
			attributes = {{
				BuildIndependentTargetsInParallel = 1;
				LastSwiftUpdateCheck = 1600;
				LastUpgradeCheck = 1600;
				TargetAttributes = {{
					{i["target"]} = {{
						CreatedOnToolsVersion = 16.0;
					}};{w_target_attr}
				}};
			}};
			buildConfigurationList = {i["projCfgList"]} /* Build configuration list for PBXProject "Jarvis" */;
			developmentRegion = "zh-Hans";
			hasScannedForEncodings = 0;
			knownRegions = (
				"zh-Hans",
				Base,
				en,
			);
			mainGroup = {i["mainGroup"]};
			productRefGroup = {i["productsGroup"]} /* Products */;
			projectDirPath = "";
			projectRoot = "";
			targets = (
				{i["target"]} /* Jarvis */,{w_targets_entry}
			);
		}};
/* End PBXProject section */

/* Begin PBXResourcesBuildPhase section */
		{i["resources"]} /* Resources */ = {{
			isa = PBXResourcesBuildPhase;
			buildActionMask = 2147483647;
			files = (
{nl.join(resources)}
			);
			runOnlyForDeploymentPostprocessing = 0;
		}};{w_res_block}
/* End PBXResourcesBuildPhase section */

/* Begin PBXSourcesBuildPhase section */
		{i["sources"]} /* Sources */ = {{
			isa = PBXSourcesBuildPhase;
			buildActionMask = 2147483647;
			files = (
{nl.join(sources)}
			);
			runOnlyForDeploymentPostprocessing = 0;
		}};{w_phases_block}
/* End PBXSourcesBuildPhase section */

/* Begin XCBuildConfiguration section */
		{i["projDebug"]} /* Debug */ = {{
			isa = XCBuildConfiguration;
			buildSettings = {{
{common}
				COPY_PHASE_STRIP = NO;
				DEBUG_INFORMATION_FORMAT = dwarf;
				ENABLE_TESTABILITY = YES;
				GCC_OPTIMIZATION_LEVEL = 0;
				GCC_PREPROCESSOR_DEFINITIONS = (
					"DEBUG=1",
					"$(inherited)",
				);
				MTL_ENABLE_DEBUG_INFO = INCLUDE_SOURCE;
				ONLY_ACTIVE_ARCH = YES;
				SWIFT_ACTIVE_COMPILATION_CONDITIONS = "DEBUG $(inherited)";
				SWIFT_OPTIMIZATION_LEVEL = "-Onone";
			}};
			name = Debug;
		}};
		{i["projRelease"]} /* Release */ = {{
			isa = XCBuildConfiguration;
			buildSettings = {{
{common}
				COPY_PHASE_STRIP = NO;
				DEBUG_INFORMATION_FORMAT = "dwarf-with-dsym";
				ENABLE_NS_ASSERTIONS = NO;
				MTL_ENABLE_DEBUG_INFO = NO;
				SWIFT_COMPILATION_MODE = wholemodule;
				VALIDATE_PRODUCT = YES;
			}};
			name = Release;
		}};
		{i["tgtDebug"]} /* Debug */ = {{
			isa = XCBuildConfiguration;
			buildSettings = {{
{target_common}
			}};
			name = Debug;
		}};
		{i["tgtRelease"]} /* Release */ = {{
			isa = XCBuildConfiguration;
			buildSettings = {{
{target_common}
			}};
			name = Release;
		}};{w_cfg_block}
/* End XCBuildConfiguration section */

/* Begin XCConfigurationList section */
		{i["projCfgList"]} /* Build configuration list for PBXProject "Jarvis" */ = {{
			isa = XCConfigurationList;
			buildConfigurations = (
				{i["projDebug"]} /* Debug */,
				{i["projRelease"]} /* Release */,
			);
			defaultConfigurationIsVisible = 0;
			defaultConfigurationName = Release;
		}};
		{i["targetCfgList"]} /* Build configuration list for PBXNativeTarget "Jarvis" */ = {{
			isa = XCConfigurationList;
			buildConfigurations = (
				{i["tgtDebug"]} /* Debug */,
				{i["tgtRelease"]} /* Release */,
			);
			defaultConfigurationIsVisible = 0;
			defaultConfigurationName = Release;
		}};{w_cfglist_block}
/* End XCConfigurationList section */
	}};
	rootObject = {i["project"]} /* Project object */;
}}
'''

    proj = IOS / "Jarvis.xcodeproj"
    (proj / "project.xcworkspace" / "xcshareddata").mkdir(parents=True, exist_ok=True)
    (proj / "xcshareddata" / "xcschemes").mkdir(parents=True, exist_ok=True)
    (proj / "project.pbxproj").write_text(pbx)

    (proj / "project.xcworkspace" / "contents.xcworkspacedata").write_text(
        '<?xml version="1.0" encoding="UTF-8"?>\n'
        '<Workspace version = "1.0">\n'
        '   <FileRef location = "self:"></FileRef>\n'
        '</Workspace>\n')
    (proj / "project.xcworkspace" / "xcshareddata" / "IDEWorkspaceChecks.plist").write_text(
        '<?xml version="1.0" encoding="UTF-8"?>\n'
        '<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" '
        '"http://www.apple.com/DTDs/PropertyList-1.0.dtd">\n'
        '<plist version="1.0"><dict>\n'
        '<key>IDEDidComputeMac32BitWarning</key><true/>\n'
        '</dict></plist>\n')

    ref = (f'            BuildableIdentifier = "primary"\n'
           f'            BlueprintIdentifier = "{i["target"]}"\n'
           f'            BuildableName = "Jarvis.app"\n'
           f'            BlueprintName = "Jarvis"\n'
           f'            ReferencedContainer = "container:Jarvis.xcodeproj"')
    (proj / "xcshareddata" / "xcschemes" / "Jarvis.xcscheme").write_text(f'''<?xml version="1.0" encoding="UTF-8"?>
<Scheme LastUpgradeVersion = "1600" version = "1.7">
   <BuildAction parallelizeBuildables = "YES" buildImplicitDependencies = "YES">
      <BuildActionEntries>
         <BuildActionEntry buildForTesting = "YES" buildForRunning = "YES" buildForProfiling = "YES" buildForArchiving = "YES" buildForAnalyzing = "YES">
            <BuildableReference
{ref}>
            </BuildableReference>
         </BuildActionEntry>
      </BuildActionEntries>
   </BuildAction>
   <TestAction buildConfiguration = "Debug" selectedDebuggerIdentifier = "Xcode.DebuggerFoundation.Debugger.LLDB" selectedLauncherIdentifier = "Xcode.DebuggerFoundation.Launcher.LLDB" shouldUseLaunchSchemeArgsEnv = "YES">
      <Testables></Testables>
   </TestAction>
   <LaunchAction buildConfiguration = "Debug" selectedDebuggerIdentifier = "Xcode.DebuggerFoundation.Debugger.LLDB" selectedLauncherIdentifier = "Xcode.DebuggerFoundation.Launcher.LLDB" launchStyle = "0" useCustomWorkingDirectory = "NO" ignoresPersistentStateOnLaunch = "NO" debugDocumentVersioning = "YES" debugServiceExtension = "internal" allowLocationSimulation = "YES">
      <BuildableProductRunnable runnableDebuggingMode = "0">
         <BuildableReference
{ref}>
         </BuildableReference>
      </BuildableProductRunnable>
   </LaunchAction>
   <ProfileAction buildConfiguration = "Release" shouldUseLaunchSchemeArgsEnv = "YES" savedToolIdentifier = "" useCustomWorkingDirectory = "NO" debugDocumentVersioning = "YES">
      <BuildableProductRunnable runnableDebuggingMode = "0">
         <BuildableReference
{ref}>
         </BuildableReference>
      </BuildableProductRunnable>
   </ProfileAction>
   <AnalyzeAction buildConfiguration = "Debug"></AnalyzeAction>
   <ArchiveAction buildConfiguration = "Release" revealArchiveInOrganizer = "YES"></ArchiveAction>
</Scheme>
''')
    print(f"工程已生成，{len(swift)} 个源文件"
          f"{'，带资源目录' if has_assets else ''}：{' '.join(swift)}")
    if widget:
        print(f"小组件 target 也装配上了（{WIDGET_DIR}/）")
    else:
        print(f"没有小组件：{ENTITLEMENTS} 里还没有 App Group。"
              "在 Xcode 里 + Capability → App Groups 之后重跑一次。")


if __name__ == "__main__":
    main()
