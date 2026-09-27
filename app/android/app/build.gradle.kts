import java.util.Properties

// Release signing: the key lives outside the repository, next to the other
// release secrets (packaging/README.md). Every APK must be signed with the
// same key, or Android refuses to install it over the previous one.
val signingFile = file("${System.getProperty("user.home")}/.coreshift/android-signing.properties")
val signing = Properties().apply { if (signingFile.exists()) signingFile.inputStream().use { load(it) } }

// The one ABI of the APK, set by packaging/android/build.ps1 -Abi.
val abi = System.getenv("CORESHIFT_ABI") ?: "arm64-v8a"

plugins {
    id("com.android.application")
    // The Flutter Gradle Plugin must be applied after the Android and Kotlin Gradle plugins.
    id("dev.flutter.flutter-gradle-plugin")
}

android {
    namespace = "dev.coreshift.coreshift"
    compileSdk = flutter.compileSdkVersion
    ndkVersion = flutter.ndkVersion

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    defaultConfig {
        // TODO: Specify your own unique Application ID (https://developer.android.com/studio/build/application-id.html).
        applicationId = "dev.coreshift.coreshift"
        // You can update the following values to match your application needs.
        // For more information, see: https://flutter.dev/to/review-gradle-config.
        minSdk = flutter.minSdkVersion
        targetSdk = flutter.targetSdkVersion
        // Uses the version code from pubspec.yaml. When using split APKs, 1000 * ABI_VERSION
        // is added automatically by Flutter. (https://developer.android.com/studio/build/configure-apk-splits#configure-APK-versions)
        // You can force using the value of versionCode by specifying the `-P force-version-code-ignoring-abi=true`
        // flag during build.
        versionCode = flutter.versionCode
        versionName = flutter.versionName
        // The engine and the cores are built for 64-bit ARM, which every
        // phone of the last years has; x86_64 only for the emulator.
        ndk { abiFilters += abi }
    }

    signingConfigs {
        if (signingFile.exists()) {
            create("release") {
                storeFile = file(signing.getProperty("storeFile"))
                storePassword = signing.getProperty("storePassword")
                keyAlias = signing.getProperty("keyAlias")
                keyPassword = signing.getProperty("keyPassword")
            }
        }
    }

    packaging {
        // The cores are programs named lib*.so: they must be extracted to
        // run (extractNativeLibs in the manifest).
        jniLibs {
            useLegacyPackaging = true
            // A plugin ships its library for other ABIs too, which abiFilters
            // does not strip: Android would then pick such an ABI on an x86
            // device or a 32-bit phone and find no engine there.
            excludes += listOf("arm64-v8a", "armeabi-v7a", "x86", "x86_64").filter { it != abi }.map { "lib/$it/**" }
        }
    }

    buildTypes {
        release {
            signingConfig = signingConfigs.findByName("release") ?: signingConfigs.getByName("debug")
        }
    }
}

kotlin {
    compilerOptions {
        jvmTarget = org.jetbrains.kotlin.gradle.dsl.JvmTarget.JVM_17
    }
}

flutter {
    source = "../.."
}

dependencies {
    // The Go engine, bound by gomobile (packaging/android/build.ps1).
    implementation(files("libs/coreshift-engine.aar"))
}
