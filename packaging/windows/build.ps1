# Builds dist\coreshift-setup-<version>-b<build>.exe: the Flutter app, the
# coreshiftd service, the proxy cores and the VC++ runtime, packed by Inno
# Setup 6.
#
#   powershell -ExecutionPolicy Bypass -File packaging\windows\build.ps1
#
# Needs go, flutter, git, Visual Studio (C++) and Inno Setup 6.
#
# The version is the VERSION file in the repository root; the build number
# is the number of commits and the commit is the short hash, with "-dirty"
# when the tree has uncommitted changes. All three are stamped into the
# service, the app and the installer, so every build tells which code it is.
#
# Cores are not kept in the repository: they come from engine\testdata\bin,
# or from -Cores (packaging\build-version.ps1 passes the main checkout's).
param(
    [string]$Cores,
    [string]$OutDir
)
$ErrorActionPreference = 'Stop'
$root = (Resolve-Path "$PSScriptRoot\..\..").Path
$stage = Join-Path $root 'dist\stage'
if (-not $Cores) { $Cores = Join-Path $root 'engine\testdata\bin' }
if (-not $OutDir) { $OutDir = Join-Path $root 'dist' }

function Step($text) { Write-Host "==> $text" -ForegroundColor Cyan }

function Check($what) {
    if ($LASTEXITCODE -ne 0) { throw "$what failed (exit code $LASTEXITCODE)" }
}

function Need($command, $hint) {
    if (-not (Get-Command $command -ErrorAction SilentlyContinue)) { throw "$command not found: $hint" }
}

$version = (Get-Content "$root\VERSION" -Raw).Trim()
if ($version -notmatch '^\d+\.\d+\.\d+$') { throw "VERSION must look like 1.2.3, not '$version'" }

Need git 'install Git for Windows'
Push-Location $root
try {
    # The build number and the commit come from git, so without the
    # repository (the .git folder) a build could not say which code it is.
    # 'Continue': Windows PowerShell turns git's own complaint on stderr
    # into a terminating error under 'Stop', hiding the explanation below.
    $ErrorActionPreference = 'Continue'
    git rev-parse --verify --quiet HEAD 2>$null | Out-Null
    $haveRepo = $LASTEXITCODE -eq 0
    $ErrorActionPreference = 'Stop'
    if (-not $haveRepo) {
        throw "$root is not a git repository with commits (no .git folder?): the build number and the commit come from git"
    }
    $build = [int](git rev-list --count HEAD)
    Check 'git rev-list'
    $commit = (git rev-parse --short=7 HEAD).Trim()
    Check 'git rev-parse'
    if (git status --porcelain) { $commit += '-dirty' }
} finally { Pop-Location }
$tag = "$version-b$build"
if ($commit.EndsWith('-dirty')) { $tag += '-dirty' }

Need go 'install Go or add it to PATH'
Need flutter 'install Flutter or add it to PATH'
$iscc = @(
    (Get-Command ISCC.exe -ErrorAction SilentlyContinue).Source
    "${env:ProgramFiles(x86)}\Inno Setup 6\ISCC.exe"
    "$env:ProgramFiles\Inno Setup 6\ISCC.exe"
    "$env:LOCALAPPDATA\Programs\Inno Setup 6\ISCC.exe"
) | Where-Object { $_ -and (Test-Path $_) } | Select-Object -First 1
if (-not $iscc) { throw 'Inno Setup 6 not found: winget install --id JRSoftware.InnoSetup -e' }
if (-not (Test-Path $Cores)) { throw "cores not found in $Cores (pass -Cores <folder>)" }

# Flutter's runner links the VC++ runtime dynamically; a clean Windows lacks
# it, so its DLLs go next to coreshift.exe.
$vswhere = "${env:ProgramFiles(x86)}\Microsoft Visual Studio\Installer\vswhere.exe"
$vs = & $vswhere -latest -products * -property installationPath
$crt = Get-ChildItem "$vs\VC\Redist\MSVC\*\x64\Microsoft.VC14*.CRT" -Directory |
    Sort-Object FullName | Select-Object -Last 1
if (-not $crt) { throw "VC++ runtime not found under $vs\VC\Redist" }

if (Test-Path $stage) { Remove-Item $stage -Recurse -Force }
New-Item -ItemType Directory $stage | Out-Null

Step "coreshiftd $version build $build ($commit)"
$pkg = 'coreshift/engine/internal/service'
Push-Location "$root\engine"
try {
    go build -trimpath -ldflags "-s -w -X $pkg.Version=$version -X $pkg.Build=$build -X $pkg.Commit=$commit" -o "$stage\coreshiftd.exe" ./cmd/coreshiftd
    Check 'go build'
} finally { Pop-Location }

Step 'app'
Push-Location "$root\app"
try {
    flutter build windows --release --build-name $version --build-number $build `
        "--dart-define=CORESHIFT_VERSION=$version" "--dart-define=CORESHIFT_BUILD=$build" "--dart-define=CORESHIFT_COMMIT=$commit"
    Check 'flutter build'
} finally { Pop-Location }
Copy-Item "$root\app\build\windows\x64\runner\Release\*" $stage -Recurse

Step "cores from $Cores"
Copy-Item $Cores "$stage\cores" -Recurse
# Helper scripts that come with the xray release are not needed.
Get-ChildItem "$stage\cores" -Recurse -Include *.ps1, *.vbs, README.md | Remove-Item

Step "VC++ runtime ($($crt.Name))"
Copy-Item "$($crt.FullName)\*.dll" $stage

Step 'installer'
New-Item -ItemType Directory $OutDir -Force | Out-Null
& $iscc /Q "/DAppVersion=$version" "/DAppBuild=$build" "/DAppCommit=$commit" "/DFileTag=$tag" "/DStage=$stage" "/DOutDir=$OutDir" "$PSScriptRoot\coreshift.iss"
Check 'Inno Setup'
Write-Host "Done: $OutDir\coreshift-setup-$tag.exe" -ForegroundColor Green
