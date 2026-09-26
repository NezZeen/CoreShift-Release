# Builds dist\coreshift-<version>-b<build>.apk: the Flutter app with the Go
# engine (bound by gomobile into an AAR) and the proxy cores for 64-bit ARM.
#
#   powershell -ExecutionPolicy Bypass -File packaging\android\build.ps1
#
# Needs go, gomobile (go install golang.org/x/mobile/cmd/gomobile@latest),
# flutter, git and the Android SDK with NDK 28.2 (%LOCALAPPDATA%\Android\Sdk).
#
# Version, build number and commit come from VERSION and git, as for the
# Windows installer. The cores come from engine\testdata\bin\android-arm64
# (libxray.so, libsingbox.so, libmihomo.so), or from -Cores. The APK is
# signed with the key from %USERPROFILE%\.coreshift\android-signing.properties;
# without it, with the debug key (such an APK cannot be updated by one
# signed with the release key).
param(
    [string]$Cores,
    [string]$OutDir
)
$ErrorActionPreference = 'Stop'
$root = (Resolve-Path "$PSScriptRoot\..\..").Path
if (-not $Cores) { $Cores = Join-Path $root 'engine\testdata\bin\android-arm64' }
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
$tag = "$version-b$build"
if ($commit.EndsWith('-dirty')) { $tag += '-dirty' }

$sdk = if ($env:ANDROID_HOME) { $env:ANDROID_HOME } else { Join-Path $env:LOCALAPPDATA 'Android\Sdk' }
$ndk = Get-ChildItem "$sdk\ndk" -Directory -ErrorAction SilentlyContinue | Sort-Object Name | Select-Object -Last 1
if (-not $ndk) { throw "no NDK in $sdk\ndk (see packaging\README.md)" }
$env:ANDROID_HOME = $sdk
$env:ANDROID_NDK_HOME = $ndk.FullName
$env:Path = "$env:USERPROFILE\go\bin;$env:Path"
foreach ($c in 'go', 'gomobile', 'flutter') {
    if (-not (Get-Command $c -ErrorAction SilentlyContinue)) { throw "$c not found" }
}
foreach ($f in 'libxray.so', 'libsingbox.so', 'libmihomo.so') {
    if (-not (Test-Path "$Cores\$f")) { throw "$Cores\$f is missing (see packaging\README.md)" }
}

Step "engine $version build $build ($commit)"
$pkg = 'coreshift/engine/internal/service'
$aar = "$root\app\android\app\libs\coreshift-engine.aar"
New-Item -ItemType Directory (Split-Path $aar) -Force | Out-Null
Push-Location "$root\engine"
try {
    gomobile bind -target=android/arm64 -androidapi 24 -tags with_gvisor -javapkg dev.coreshift -trimpath `
        -ldflags "-s -w -X $pkg.Version=$version -X $pkg.Build=$build -X $pkg.Commit=$commit" -o $aar ./mobile
    Check 'gomobile bind'
} finally { Pop-Location }

Step "cores from $Cores"
$jni = "$root\app\android\app\src\main\jniLibs\arm64-v8a"
New-Item -ItemType Directory $jni -Force | Out-Null
Copy-Item "$Cores\lib*.so" $jni -Force

Step 'app'
Push-Location "$root\app"
try {
    flutter build apk --release --target-platform android-arm64 --build-name $version --build-number $build `
        "--dart-define=CORESHIFT_VERSION=$version" "--dart-define=CORESHIFT_BUILD=$build" "--dart-define=CORESHIFT_COMMIT=$commit"
    Check 'flutter build apk'
} finally { Pop-Location }

New-Item -ItemType Directory $OutDir -Force | Out-Null
$apk = Join-Path $OutDir "coreshift-$tag.apk"
Copy-Item "$root\app\build\app\outputs\flutter-apk\app-release.apk" $apk -Force
Write-Host "Done: $apk" -ForegroundColor Green
