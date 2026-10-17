# Builds dist\coreshift-<version>.apk (-<version>-b<build> between releases):
# the Flutter app with the Go engine (bound by gomobile into an AAR) and the
# proxy cores for 64-bit ARM.
#
#   powershell -ExecutionPolicy Bypass -File packaging\android\build.ps1
#
# -Abi armeabi-v7a builds coreshift-<version>-armeabi-v7a.apk for 32-bit ARM
# phones, -Abi universal coreshift-<version>-universal.apk with both ARM
# kinds, the one people download: Android takes what fits the phone. A
# release has all three; the phones update with the APK of their own kind
# (latest-android.json, latest-android-armv7.json), half the size.
#
# With -Abi x86_64 it builds coreshift-<version>-b<build>-x86_64.apk for the
# Android emulator on a PC instead: its ARM translation cannot run the Go
# engine. Such an APK is for testing, not for phones, and does not update
# itself: the releases are for phones.
#
# The phone's APK updates itself from the public NezZeen/CoreShift-Release,
# without a token (see packaging\README.md).
#
# Needs go, gomobile (go install golang.org/x/mobile/cmd/gomobile@latest),
# flutter, git and the Android SDK with NDK 28.2 (%LOCALAPPDATA%\Android\Sdk).
#
# Version, build number and commit come from VERSION and git, as for the
# Windows installer. The cores come from engine\testdata\bin\android-arm64
# (libxray.so, libsingbox.so, libmihomo.so), for 32-bit ARM from
# engine\testdata\bin\android-arm, for x86_64 from
# engine\testdata\bin\android-x86_64, or from -Cores (one ABI only). The APK is
# signed with the key from %USERPROFILE%\.coreshift\android-signing.properties;
# without it, with the debug key (such an APK cannot be updated by one
# signed with the release key).
param(
    [string]$Cores,
    [string]$OutDir,
    [ValidateSet('arm64-v8a', 'armeabi-v7a', 'universal', 'x86_64')]
    [string]$Abi = 'arm64-v8a'
)
$ErrorActionPreference = 'Stop'
$root = (Resolve-Path "$PSScriptRoot\..\..").Path
# The names each tool gives an ABI: Go's, Flutter's, the cores' folder.
$names = @{
    'arm64-v8a'   = @{ go = 'arm64'; flutter = 'android-arm64'; cores = 'android-arm64' }
    'armeabi-v7a' = @{ go = 'arm'; flutter = 'android-arm'; cores = 'android-arm' }
    'x86_64'      = @{ go = 'amd64'; flutter = 'android-x64'; cores = 'android-x86_64' }
}
$abis = if ($Abi -eq 'universal') { @('arm64-v8a', 'armeabi-v7a') } else { @($Abi) }
if ($Cores -and $abis.Count -gt 1) { throw '-Cores names the cores of one ABI; the universal APK takes them from engine\testdata\bin' }
$coresOf = @{}
foreach ($a in $abis) { $coresOf[$a] = if ($Cores) { $Cores } else { Join-Path $root "engine\testdata\bin\$($names[$a].cores)" } }
if (-not $OutDir) { $OutDir = Join-Path $root 'dist' }

function Step($text) { Write-Host "==> $text" -ForegroundColor Cyan }

function Check($what) {
    if ($LASTEXITCODE -ne 0) { throw "$what failed (exit code $LASTEXITCODE)" }
}

$version = (Get-Content "$root\VERSION" -Raw).Trim()
if ($version -notmatch '^\d+\.\d+\.\d+$') { throw "VERSION must look like 1.2.3, not '$version'" }
Push-Location $root
try {
    $ErrorActionPreference = 'Continue'
    git rev-parse --verify --quiet HEAD 2>$null | Out-Null
    $haveRepo = $LASTEXITCODE -eq 0
    $ErrorActionPreference = 'Stop'
    if (-not $haveRepo) { throw "$root is not a git repository with commits: the build number and the commit come from git" }
    $build = [int](git rev-list --count HEAD)
    $commit = (git rev-parse --short=7 HEAD).Trim()
    if (git status --porcelain) { $commit += '-dirty' }
} finally { Pop-Location }
# A release (the commit tagged v<version>, packaging\release.ps1) is named
# by its version alone; builds between releases also carry the build number,
# so they do not overwrite the released files.
$tag = $version
if (-not (git -C $root tag --points-at HEAD --list "v$version")) { $tag += "-b$build" }
if ($commit.EndsWith('-dirty')) { $tag += '-dirty' }
if ($Abi -ne 'arm64-v8a') { $tag += "-$Abi" }

$sdk = if ($env:ANDROID_HOME) { $env:ANDROID_HOME } else { Join-Path $env:LOCALAPPDATA 'Android\Sdk' }
$ndk = Get-ChildItem "$sdk\ndk" -Directory -ErrorAction SilentlyContinue | Sort-Object Name | Select-Object -Last 1
if (-not $ndk) { throw "no NDK in $sdk\ndk (see packaging\README.md)" }
$env:ANDROID_HOME = $sdk
$env:ANDROID_NDK_HOME = $ndk.FullName
$env:Path = "$env:USERPROFILE\go\bin;$env:Path"
foreach ($c in 'go', 'gomobile', 'flutter') {
    if (-not (Get-Command $c -ErrorAction SilentlyContinue)) { throw "$c not found" }
}
foreach ($a in $abis) {
    foreach ($f in 'libxray.so', 'libsingbox.so', 'libmihomo.so') {
        if (-not (Test-Path "$($coresOf[$a])\$f")) { throw "$($coresOf[$a])\$f is missing (see packaging\README.md)" }
    }
}

Step "engine $version build $build ($commit)"
# Builds carry no releases token (since 0.8.1); a token_gen.go left over
# from an older build would put one back.
$tokenGo = "$root\engine\internal\selfupdate\token_gen.go"
if (Test-Path $tokenGo) { throw "$tokenGo exists: remove it, builds carry no releases token" }
$pkg = 'coreshift/engine/internal/service'
$aar = "$root\app\android\app\libs\coreshift-engine.aar"
New-Item -ItemType Directory (Split-Path $aar) -Force | Out-Null
Push-Location "$root\engine"
try {
    $targets = ($abis | ForEach-Object { "android/$($names[$_].go)" }) -join ','
    gomobile bind "-target=$targets" -androidapi 24 -tags with_gvisor -javapkg dev.coreshift -trimpath `
        -ldflags "-s -w -X $pkg.Version=$version -X $pkg.Build=$build -X $pkg.Commit=$commit" -o $aar ./mobile
    Check 'gomobile bind'
} finally { Pop-Location }

foreach ($a in $abis) {
    Step "cores for $a from $($coresOf[$a])"
    $jni = "$root\app\android\app\src\main\jniLibs\$a"
    New-Item -ItemType Directory $jni -Force | Out-Null
    Copy-Item "$($coresOf[$a])\lib*.so" $jni -Force
}
$env:CORESHIFT_ABI = $abis -join ',' # build.gradle.kts packs these ABIs alone

Step 'app'
Push-Location "$root\app"
try {
    $platforms = ($abis | ForEach-Object { $names[$_].flutter }) -join ','
    flutter build apk --release --target-platform $platforms --build-name $version --build-number $build `
        "--dart-define=CORESHIFT_VERSION=$version" "--dart-define=CORESHIFT_BUILD=$build" "--dart-define=CORESHIFT_COMMIT=$commit"
    Check 'flutter build apk'
} finally { Pop-Location }

New-Item -ItemType Directory $OutDir -Force | Out-Null
$apk = Join-Path $OutDir "coreshift-$tag.apk"
Copy-Item "$root\app\build\app\outputs\flutter-apk\app-release.apk" $apk -Force
Write-Host "Done: $apk" -ForegroundColor Green
