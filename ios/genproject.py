#!/usr/bin/env python3
"""重新生成 ios/Jarvis.xcodeproj。

手写的 pbxproj 里文件清单是钉死的，加一个 .swift 就要手改，忘了改的表现是
`cannot find 'X' in scope`——看着像代码错，其实是文件没进工程。所以这里按
目录里实际有的 .swift 生成，加文件只要跑一次 `make ios-project`。

工程里的东西很少：一个 app target、八九个源文件、一个 framework。真到了
需要多 target 和依赖管理的规模，再换 XcodeGen 之类不迟。
"""
import hashlib
import os
import pathlib

IOS = pathlib.Path(__file__).resolve().parent
FRAMEWORK = "Mobile.xcframework"
ASSETS = "Assets.xcassets"
PRIVACY = "PrivacyInfo.xcprivacy"
LAUNCH = "LaunchScreen.storyboard"
ENTITLEMENTS = "Jarvis.entitlements"
BUNDLE_ID = "com.dabao.jarvis"
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

    common = f'''				ALWAYS_SEARCH_USER_PATHS = NO;
				CLANG_ENABLE_MODULES = YES;
				CLANG_ENABLE_OBJC_ARC = YES;
				ENABLE_STRICT_OBJC_MSGSEND = YES;
				IPHONEOS_DEPLOYMENT_TARGET = {DEPLOY_TARGET};
				SDKROOT = iphoneos;
				SWIFT_VERSION = 5.0;'''

    appicon = ("\n\t\t\t\tASSETCATALOG_COMPILER_APPICON_NAME = AppIcon;"
               if has_assets else "")
    target_common = f'''				ASSETCATALOG_COMPILER_GLOBAL_ACCENT_COLOR_NAME = "";{appicon}
				CODE_SIGN_ENTITLEMENTS = {ENTITLEMENTS};
				CODE_SIGN_STYLE = Automatic;
				CURRENT_PROJECT_VERSION = 1;
				DEVELOPMENT_TEAM = "";
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
    pbx = f'''// !$*UTF8*$!
{{
	archiveVersion = 1;
	classes = {{
	}};
	objectVersion = 56;
	objects = {{

/* Begin PBXBuildFile section */
{nl.join(builds)}
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
		}};
/* End PBXCopyFilesBuildPhase section */

/* Begin PBXFileReference section */
{nl.join(refs)}
		{i["product"]} /* Jarvis.app */ = {{isa = PBXFileReference; explicitFileType = wrapper.application; includeInIndex = 0; path = Jarvis.app; sourceTree = BUILT_PRODUCTS_DIR; }};
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
{nl.join(children)}
				{i["productsGroup"]} /* Products */,
			);
			sourceTree = "<group>";
		}};
		{i["productsGroup"]} /* Products */ = {{
			isa = PBXGroup;
			children = (
				{i["product"]} /* Jarvis.app */,
			);
			name = Products;
			sourceTree = "<group>";
		}};
/* End PBXGroup section */

/* Begin PBXNativeTarget section */
		{i["target"]} /* Jarvis */ = {{
			isa = PBXNativeTarget;
			buildConfigurationList = {i["targetCfgList"]} /* Build configuration list for PBXNativeTarget "Jarvis" */;
			buildPhases = (
				{i["sources"]} /* Sources */,
				{i["frameworks"]} /* Frameworks */,
				{i["resources"]} /* Resources */,
				{i["embed"]} /* Embed Frameworks */,
			);
			buildRules = (
			);
			dependencies = (
			);
			name = Jarvis;
			productName = Jarvis;
			productReference = {i["product"]} /* Jarvis.app */;
			productType = "com.apple.product-type.application";
		}};
/* End PBXNativeTarget section */

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
					}};
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
				{i["target"]} /* Jarvis */,
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
		}};
/* End PBXResourcesBuildPhase section */

/* Begin PBXSourcesBuildPhase section */
		{i["sources"]} /* Sources */ = {{
			isa = PBXSourcesBuildPhase;
			buildActionMask = 2147483647;
			files = (
{nl.join(sources)}
			);
			runOnlyForDeploymentPostprocessing = 0;
		}};
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
		}};
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
		}};
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


if __name__ == "__main__":
    main()
