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
        applicationId = "dev.coreshift.coreshift"
        minSdk = flutter.minSdkVersion
        targetSdk = flutter.targetSdkVersion
        // Both come from --build-number / --build-name (packaging/android/build.ps1):
        // the version code is the commit count, so every build can update the last.
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
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
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
    // Google's QR scanner: Play services show the camera, so the app needs
    // no camera permission (MainActivity.scanQr).
    implementation("com.google.android.gms:play-services-code-scanner:16.1.0")
}
