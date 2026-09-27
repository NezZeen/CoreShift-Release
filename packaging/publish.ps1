# Publishes a release made by release.ps1 to the private releases
# repository, where installed copies of CoreShift look for updates: from
# dist\release\<version>, latest.json, latest.json.sig and the installer for
# Windows, latest-android.json, its .sig and the APK for Android, or the
# files of one of them.
#
#   powershell -ExecutionPolicy Bypass -File packaging\publish.ps1 -Version 0.3.0
#
# Needs the GitHub CLI signed in with access to the repository (gh auth
# login). The repository holds only releases, no code; create it once with
#   gh repo create NezZeen/coreshift-releases --private --add-readme
param(
    [Parameter(Mandatory = $true)][string]$Version,
    [string]$Repo = 'NezZeen/coreshift-releases',
    [string]$Notes = ''
)
$ErrorActionPreference = 'Stop'
$root = (Resolve-Path "$PSScriptRoot\..").Path
$dir = Join-Path $root "dist\release\$Version"

$files = @()
$manifest = $null
foreach ($name in 'latest.json', 'latest-android.json') {
    $path = Join-Path $dir $name
    if (-not (Test-Path $path)) { continue }
    $m = Get-Content $path -Raw | ConvertFrom-Json
    if ($m.version -ne $Version) { throw "$path is for version $($m.version)" }
    foreach ($f in $path, "$path.sig", (Join-Path $dir $m.installer)) {
        if (-not (Test-Path $f)) { throw "$f is missing (run release.ps1 again)" }
        $files += (Get-Item $f)
    }
    $manifest = $m
}
if (-not $manifest) { throw "no latest.json or latest-android.json in $dir (run release.ps1 first)" }

if (-not $Notes) { $Notes = "CoreShift $Version (build $($manifest.build), $($manifest.commit))" }
# Installed copies take the newest release that has their manifest.
gh release create "v$Version" $files.FullName --repo $Repo --title "CoreShift $Version" --notes $Notes --latest
if ($LASTEXITCODE -ne 0) { throw "gh release create failed (exit code $LASTEXITCODE)" }
Write-Host "Published v$Version to $Repo" -ForegroundColor Green
