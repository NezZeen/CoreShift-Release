# Publishes a release made by release.ps1 to the private releases
# repository, where installed copies of CoreShift look for updates:
# latest.json, latest.json.sig and the installer from dist\release\<version>.
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

$files = @(Get-ChildItem $dir -File | Where-Object { $_.Name -in 'latest.json', 'latest.json.sig' -or $_.Extension -eq '.exe' })
if ($files.Count -ne 3) { throw "expected latest.json, latest.json.sig and one installer in $dir (run release.ps1 first)" }
$manifest = Get-Content (Join-Path $dir 'latest.json') -Raw | ConvertFrom-Json
if ($manifest.version -ne $Version) { throw "$dir\latest.json is for version $($manifest.version)" }

if (-not $Notes) { $Notes = "CoreShift $Version (build $($manifest.build), $($manifest.commit))" }
# --latest: installed copies read the release marked latest.
gh release create "v$Version" $files.FullName --repo $Repo --title "CoreShift $Version" --notes $Notes --latest
if ($LASTEXITCODE -ne 0) { throw "gh release create failed (exit code $LASTEXITCODE)" }
Write-Host "Published v$Version to $Repo" -ForegroundColor Green
