# Releases a version: writes VERSION, commits it, tags the commit v<version>,
# creates the branch release/<version> there and builds its Windows
# installer, Android APK and Linux packages.
#
#   powershell -ExecutionPolicy Bypass -File packaging\release.ps1 -Version 0.3.0
#
# -Platform windows, android or linux builds one of them only; the other
# platforms keep updating to their previous release. The Linux packages are
# built in WSL (Ubuntu-24.04, packaging\linux\build-wsl.ps1) from the
# tagged commit; without WSL the release fails, unless -NoLinux leaves
# Linux out.
#
# Run it on main with everything committed. main then goes on; the branch
# keeps the released code, so the version can be rebuilt, compared or fixed
# later (packaging\build-version.ps1 builds any of them).
#
# The version commit also takes SagerNet's current rule sets into the copies
# CoreShift carries (engine\internal\ruleset\data, coreshift-release
# rulesets): the service starts from them and checks the sets it downloads
# later against them. If they cannot be downloaded, or one is far from the
# previous copy, the release stops before anything is committed;
# -KeepRuleSets releases with the copies already committed.
param(
    [Parameter(Mandatory = $true)][string]$Version,
    # Only commit, tag and branch; build later.
    [switch]$NoBuild,
    # Leave the built-in rule sets as they are.
    [switch]$KeepRuleSets,
    [ValidateSet('all', 'windows', 'android', 'linux')]
    [string]$Platform = 'all',
    # Leave the Linux packages out, e.g. on a computer without WSL.
    [switch]$NoLinux,
    [string]$LinuxDistro = 'Ubuntu-24.04'
)
$ErrorActionPreference = 'Stop'
$root = (Resolve-Path "$PSScriptRoot\..").Path

function Check($what) {
    if ($LASTEXITCODE -ne 0) { throw "$what failed (exit code $LASTEXITCODE)" }
}

if ($Version -notmatch '^\d+\.\d+\.\d+$') { throw "the version must look like 1.2.3, not '$Version'" }
$linux = ($Platform -eq 'all' -or $Platform -eq 'linux') -and -not $NoLinux
if ($Platform -eq 'linux' -and $NoLinux) { throw '-Platform linux with -NoLinux builds nothing' }
# Before anything is committed or tagged: a release without its Linux
# packages must be asked for.
if ($linux -and -not $NoBuild) {
    if (-not (Get-Command wsl.exe -ErrorAction SilentlyContinue)) {
        throw 'WSL is not installed: the Linux packages are built in WSL. Install it, or pass -NoLinux to release without Linux'
    }
    $env:WSL_UTF8 = '1'
    $distros = (wsl.exe -l -q) | ForEach-Object { $_.Trim([char]0, ' ') } | Where-Object { $_ }
    if ($distros -notcontains $LinuxDistro) {
        throw "WSL distribution $LinuxDistro not found: install it (see packaging\linux\README.md), or pass -NoLinux to release without Linux"
    }
}
Push-Location $root
try {
    if (git status --porcelain) { throw 'commit or stash your changes first: the release must be exactly what is committed' }
    $branch = (git rev-parse --abbrev-ref HEAD).Trim()
    if ($branch -ne 'main') { Write-Warning "releasing from '$branch', not main" }
    if (git tag --list "v$Version") { throw "v$Version already exists" }

    $current = (Get-Content VERSION -Raw).Trim()
    if ([version]$Version -le [version]$current -and (git tag --list "v$current")) {
        throw "$Version is not later than the released $current"
    }

    if (-not $KeepRuleSets) {
        Push-Location "$root\engine"
        try {
            go run ./cmd/coreshift-release rulesets
            Check 'coreshift-release rulesets (-KeepRuleSets releases with the committed ones)'
        } finally { Pop-Location }
        git add engine/internal/ruleset/data
        Check 'git add'
    }

    # ASCII, no BOM: build.ps1 and the tools read it as plain text.
    [IO.File]::WriteAllText("$root\VERSION", "$Version`n")
    git add VERSION
    Check 'git add'
    if (git diff --cached --name-only) {
        git commit -q -m "Version $Version"
        Check 'git commit'
    }
    git tag -a "v$Version" -m "CoreShift $Version"
    Check 'git tag'
    git branch "release/$Version"
    Check 'git branch'
    Write-Host "Tagged v$Version, branch release/$Version" -ForegroundColor Green
} finally { Pop-Location }

if (-not $NoBuild) {
    Push-Location $root
    try {
        $build = [int](git rev-list --count HEAD)
        $commit = (git rev-parse --short=7 HEAD).Trim()
    } finally { Pop-Location }
    $installers = @()
    $out = Join-Path $root "dist\release\$Version"
    New-Item -ItemType Directory $out -Force | Out-Null
    # The release carries every installer once, under the fixed name the
    # public downloads link to (releases/latest/download/CoreShift-Setup.exe);
    # the manifests name these files, so self-update takes the same ones.
    # The versioned builds stay in dist\.
    if ($Platform -eq 'all' -or $Platform -eq 'windows') {
        & "$root\packaging\windows\build.ps1"
        Copy-Item "$root\dist\coreshift-setup-$Version.exe" (Join-Path $out 'CoreShift-Setup.exe') -Force
        $installers += (Join-Path $out 'CoreShift-Setup.exe')
    }
    if ($Platform -eq 'all' -or $Platform -eq 'android') {
        & "$root\packaging\android\build.ps1"
        Copy-Item "$root\dist\coreshift-$Version.apk" (Join-Path $out 'CoreShift.apk') -Force
        $installers += (Join-Path $out 'CoreShift.apk')
    }
    if ($linux) {
        $linuxDir = Join-Path $root "dist\linux\$Version"
        & "$root\packaging\linux\build-wsl.ps1" -Ref "v$Version" -Distro $LinuxDistro -OutDir $linuxDir
        # The release carries them under fixed names, the ones the public
        # downloads link to (packaging\README.md); the manifest names the .deb.
        New-Item -ItemType Directory $out -Force | Out-Null
        $fixed = [ordered]@{
            "coreshift_$($Version)_amd64.deb"                = 'CoreShift-amd64.deb'
            "coreshift-$Version-1.x86_64.rpm"                = 'CoreShift-x86_64.rpm'
            "coreshift-$Version-1-x86_64.pkg.tar.zst"        = 'CoreShift-x86_64.pkg.tar.zst'
            "coreshift-$Version-linux-amd64.tar.gz"          = 'CoreShift-linux-amd64.tar.gz'
        }
        foreach ($from in $fixed.Keys) {
            $src = Join-Path $linuxDir $from
            if (-not (Test-Path $src)) { throw "the Linux build made no $from" }
            Copy-Item $src (Join-Path $out $fixed[$from]) -Force
        }
        $installers += (Join-Path $out 'CoreShift-amd64.deb')
    }

    # The self-update files: for each installer its manifest (latest.json,
    # latest-android.json, latest-linux.json) and signature, in
    # dist\release\<version>, ready for packaging\publish.ps1. Linux only
    # announces new versions from latest-linux.json.
    $key = Join-Path $env:USERPROFILE '.coreshift\update-signing.key'
    if (-not (Test-Path $key)) {
        Write-Warning "no signing key ($key): no self-update files; installed copies will not see this release"
    } else {
        Push-Location "$root\engine"
        try {
            foreach ($installer in $installers) {
                go run ./cmd/coreshift-release manifest -installer $installer `
                    -version $Version -build $build -commit $commit -key $key -out $out
                Check 'coreshift-release manifest'
            }
        } finally { Pop-Location }
        Write-Host "Release files: $out (publish: packaging\publish.ps1 -Version $Version)" -ForegroundColor Green
    }
}
