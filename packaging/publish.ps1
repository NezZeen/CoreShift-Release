# Publishes a release made by release.ps1 to the public
# NezZeen/CoreShift-Release, where people download CoreShift and, since
# 0.8.1, installed copies on every platform look for updates: from
# dist\release\<version>, latest.json, latest.json.sig and the installer for
# Windows, latest-android.json, its .sig and the APK for Android,
# latest-linux.json, its .sig and the Linux packages under their fixed names
# (CoreShift-amd64.deb, -x86_64.rpm, -x86_64.pkg.tar.zst,
# -linux-amd64.tar.gz), or the files of some of them. The installer and the
# APK also go up as CoreShift-Setup.exe and CoreShift.apk, the README's
# fixed links.
#
#   powershell -ExecutionPolicy Bypass -File packaging\publish.ps1 -Version 0.3.0
#
# Copies up to 0.8.0 on Windows and Android read the private
# NezZeen/coreshift-releases instead, with a token; 0.8.1, the release that
# moves them to the public repository, went there too
# (-Repo NezZeen/coreshift-releases). Later releases go to the public one only.
#
# Needs the GitHub CLI signed in with access to the repository (gh auth
# login).
param(
    [Parameter(Mandatory = $true)][string]$Version,
    [string]$Repo = 'NezZeen/CoreShift-Release',
    [string]$Notes = '',
    # A test build: marked pre-release, which installed copies never pick up
    # by themselves. Make it a normal release later with
    #   gh release edit v<version> --prerelease=false --latest
    [switch]$Prerelease
)
$ErrorActionPreference = 'Stop'
$root = (Resolve-Path "$PSScriptRoot\..").Path
$dir = Join-Path $root "dist\release\$Version"

$files = @()
$manifest = $null
foreach ($name in 'latest.json', 'latest-android.json', 'latest-linux.json') {
    $path = Join-Path $dir $name
    if (-not (Test-Path $path)) { continue }
    $m = Get-Content $path -Raw | ConvertFrom-Json
    if ($m.version -ne $Version) { throw "$path is for version $($m.version)" }
    foreach ($f in $path, "$path.sig", (Join-Path $dir $m.installer)) {
        if (-not (Test-Path $f)) { throw "$f is missing (run release.ps1 again)" }
        $files += (Get-Item $f)
    }
    $manifest = $m
    if ($name -eq 'latest-linux.json') {
        # The other Linux packages, beside the .deb the manifest names.
        foreach ($n in 'CoreShift-x86_64.rpm', 'CoreShift-x86_64.pkg.tar.zst', 'CoreShift-linux-amd64.tar.gz') {
            $f = Join-Path $dir $n
            if (-not (Test-Path $f)) { throw "$f is missing (run release.ps1 again)" }
            $files += (Get-Item $f)
        }
    }
}
if (-not $manifest) { throw "no latest.json, latest-android.json or latest-linux.json in $dir (run release.ps1 first)" }

# The fixed names the README links to, through releases/latest/download/.
$fixed = Join-Path $env:TEMP "coreshift-fixed-$Version"
if (Test-Path $fixed) { Remove-Item $fixed -Recurse -Force }
New-Item -ItemType Directory $fixed | Out-Null
foreach ($f in @($files)) {
    $name = switch ($f.Extension) { '.exe' { 'CoreShift-Setup.exe' } '.apk' { 'CoreShift.apk' } default { $null } }
    if ($name -and $f.Name -ne $name) { $files += (Copy-Item $f.FullName (Join-Path $fixed $name) -PassThru) }
}

# The release is just the version; gh needs some notes, a space is none.
if (-not $Notes) { $Notes = ' ' }
# Installed copies take the newest release that has their manifest; those
# of Windows before 0.3.2 read only the release marked latest, so only a
# release with Windows files may be marked so.
$latest = if (Test-Path (Join-Path $dir 'latest.json')) { '--latest' } else { '--latest=false' }
if ($Prerelease) { $latest = '--latest=false' }
# An array, not the string an if-expression would give: @string splats its characters.
[string[]]$pre = if ($Prerelease) { '--prerelease' } else { @() }
# Through a UTF-8 file: several lines or Cyrillic would be split or garbled
# as an argument by Windows PowerShell.
$notesFile = Join-Path $env:TEMP "coreshift-notes-$Version.md"
[IO.File]::WriteAllText($notesFile, $Notes, (New-Object Text.UTF8Encoding $false))
gh release create "v$Version" $files.FullName --repo $Repo --title "CoreShift $Version" --notes-file $notesFile $latest @pre
if ($LASTEXITCODE -ne 0) { throw "gh release create failed (exit code $LASTEXITCODE)" }
Remove-Item $fixed -Recurse -Force -ErrorAction SilentlyContinue
Write-Host "Published v$Version to $Repo" -ForegroundColor Green
