# Releases a version: writes VERSION, commits it, tags the commit v<version>,
# creates the branch release/<version> there and builds its Windows
# installer and Android APK.
#
#   powershell -ExecutionPolicy Bypass -File packaging\release.ps1 -Version 0.3.0
#
# -Platform windows or -Platform android builds one of them only; the other
# platform keeps updating to its previous release.
#
# Run it on main with everything committed. main then goes on; the branch
# keeps the released code, so the version can be rebuilt, compared or fixed
# later (packaging\build-version.ps1 builds any of them).
param(
    [Parameter(Mandatory = $true)][string]$Version,
    # Only commit, tag and branch; build later.
    [switch]$NoBuild,
    [ValidateSet('all', 'windows', 'android')]
    [string]$Platform = 'all'
)
$ErrorActionPreference = 'Stop'
$root = (Resolve-Path "$PSScriptRoot\..").Path

function Check($what) {
    if ($LASTEXITCODE -ne 0) { throw "$what failed (exit code $LASTEXITCODE)" }
}

if ($Version -notmatch '^\d+\.\d+\.\d+$') { throw "the version must look like 1.2.3, not '$Version'" }
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
    if ($Platform -ne 'android') {
        & "$root\packaging\windows\build.ps1"
        $installers += "$root\dist\coreshift-setup-$Version-b$build.exe"
    }
    if ($Platform -ne 'windows') {
        & "$root\packaging\android\build.ps1"
        $installers += "$root\dist\coreshift-$Version-b$build.apk"
    }

    # The self-update files: for each installer its manifest (latest.json,
    # latest-android.json) and signature, in dist\release\<version>, ready
    # for packaging\publish.ps1.
    $key = Join-Path $env:USERPROFILE '.coreshift\update-signing.key'
    if (-not (Test-Path $key)) {
        Write-Warning "no signing key ($key): no self-update files; installed copies will not see this release"
    } else {
        $out = Join-Path $root "dist\release\$Version"
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
