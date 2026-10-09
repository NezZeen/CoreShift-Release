# Publishes a release made by release.ps1 to the public
# NezZeen/CoreShift-Release, where people download CoreShift and, since
# 0.8.1, installed copies on every platform look for updates: from
# dist\release\<version>, latest.json, latest.json.sig and the installer for
# Windows, latest-android.json, its .sig and the APK for Android,
# latest-linux.json, its .sig and the Linux packages under their fixed names
# (CoreShift-amd64.deb, -x86_64.rpm, -x86_64.pkg.tar.zst,
# -linux-amd64.tar.gz), or the files of some of them. The installer and the
# APK go up as CoreShift-Setup.exe and CoreShift.apk (release.ps1 names them
# so since 0.8.2; an older release folder still gets the copies), the README's
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
#
# Then the same files go to the mirror on GitLab (-GitLabProject, by default
# NezZeen/coreshift), for those who cannot reach GitHub: installed copies
# ask it when GitHub is out of reach (engine/internal/selfupdate/gitlab.go).
# Each file is uploaded to the project's generic package registry
# (coreshift/<version>/<file>), then a release v<version> is made whose
# links point at them; direct_asset_path makes
# https://gitlab.com/<project>/-/releases/permalink/latest/downloads/<file>
# a fixed link to the latest one. The token is read from
# %USERPROFILE%\.coreshift\gitlab-token (or gitlab-token.txt) and never
# printed; see packaging/README.md, the GitLab mirror section. -NoGitLab skips
# the mirror, -GitLabOnly publishes to it alone (a retry, or a release
# already on GitHub).
param(
    [Parameter(Mandatory = $true)][string]$Version,
    [string]$Repo = 'NezZeen/CoreShift-Release',
    [string]$Notes = '',
    # A test build: marked pre-release, which installed copies never pick up
    # by themselves. Make it a normal release later with
    #   gh release edit v<version> --prerelease=false --latest
    # GitLab has no pre-releases: a test build does not go there; publish it
    # with -GitLabOnly once it is a normal release.
    [switch]$Prerelease,
    [string]$GitLabProject = 'NezZeen/coreshift',
    [switch]$NoGitLab,
    [switch]$GitLabOnly
)
$ErrorActionPreference = 'Stop'
$root = (Resolve-Path "$PSScriptRoot\..").Path
$dir = Join-Path $root "dist\release\$Version"
if ($NoGitLab -and $GitLabOnly) { throw '-NoGitLab and -GitLabOnly exclude each other' }
if ($Prerelease -and $GitLabOnly) { throw 'GitLab has no pre-releases: publish a test build to GitHub only' }
$toGitLab = -not $NoGitLab -and -not $Prerelease

# The GitLab token, read before anything is published: a missing one stops
# here, not halfway. Never printed.
$gitlabToken = $null
if ($toGitLab) {
    $tokenDir = Join-Path $env:USERPROFILE '.coreshift'
    $tokenFile = @('gitlab-token', 'gitlab-token.txt') | ForEach-Object { Join-Path $tokenDir $_ } | Where-Object { Test-Path $_ } | Select-Object -First 1
    if (-not $tokenFile) {
        throw ("No GitLab token: save a token of $GitLabProject (a fine-grained one with Release: Create, Release Link: Create, Package: Create and tag creation, " +
            "or a legacy one with the api scope) to $tokenDir\gitlab-token or $tokenDir\gitlab-token.txt; see packaging\README.md, the GitLab mirror section. " +
            'Or publish without the mirror: -NoGitLab.')
    }
    # ReadAllText drops a byte order mark it recognises; Trim the rest
    # (Notepad's line end, a stray BOM character).
    $gitlabToken = [IO.File]::ReadAllText($tokenFile).Trim().Trim([char]0xFEFF).Trim()
    if (-not $gitlabToken) { throw "$tokenFile is empty" }
}

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
if (-not $GitLabOnly) {
    gh release create "v$Version" $files.FullName --repo $Repo --title "CoreShift $Version" --notes-file $notesFile $latest @pre
    if ($LASTEXITCODE -ne 0) { throw "gh release create failed (exit code $LASTEXITCODE)" }
    Write-Host "Published v$Version to $Repo" -ForegroundColor Green
}

# The mirror on GitLab.
function Invoke-GitLab {
    param([string]$Method, [string]$Path, $Body = $null, [string]$InFile = '', [int]$TimeoutSec = 120)
    $params = @{
        Method          = $Method
        Uri             = "https://gitlab.com/api/v4$Path"
        Headers         = @{ 'PRIVATE-TOKEN' = $gitlabToken }
        TimeoutSec      = $TimeoutSec
        UseBasicParsing = $true
    }
    if ($InFile) {
        $params.InFile = $InFile
        $params.ContentType = 'application/octet-stream'
    } elseif ($null -ne $Body) {
        # As UTF-8 bytes: Windows PowerShell would send a string as Latin-1,
        # and the notes are Russian.
        $params.Body = [Text.Encoding]::UTF8.GetBytes(($Body | ConvertTo-Json -Depth 6 -Compress))
        $params.ContentType = 'application/json; charset=utf-8'
    }
    for ($attempt = 1; ; $attempt++) {
        try {
            return Invoke-RestMethod @params
        } catch {
            $err = $_.Exception # $_ is something else inside switch
            $code = 0
            if ($err.Response) { $code = [int]$err.Response.StatusCode }
            # Only what may pass is tried again: no answer, a timeout, a rate
            # limit, GitLab's own failures.
            $transient = $code -eq 0 -or $code -eq 408 -or $code -eq 429 -or $code -ge 500
            if (-not $transient -or $attempt -ge 5) {
                $what = switch ($code) {
                    401 { 'the GitLab token is invalid or expired (401)' }
                    403 { 'the GitLab token may not do this (403): a fine-grained token needs Release: Create, Release Link: Create, Package: Create and tag creation (or use a legacy token with the api scope)' }
                    404 { "no project $GitLabProject on GitLab, or the token cannot see it (404)" }
                    default { $err.Message }
                }
                $e = New-Object Exception("GitLab: $Method $($Path -replace '\?.*$', ''): $what", $err)
                $e.Data['StatusCode'] = $code
                throw $e
            }
            Write-Host "GitLab: $Method failed ($(if ($code) { $code } else { $err.Message })), trying again" -ForegroundColor Yellow
            Start-Sleep -Seconds (5 * $attempt)
        }
    }
}

if ($toGitLab) {
    # Old Windows PowerShell may not offer TLS 1.2 by itself; this process only.
    [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
    # The progress bar slows uploads in Windows PowerShell many times over.
    $ProgressPreference = 'SilentlyContinue'
    $project = Invoke-GitLab GET "/projects/$([uri]::EscapeDataString($GitLabProject))"
    $id = $project.id
    $links = @()
    foreach ($f in $files) {
        $name = $f.Name
        $pkgPath = "/projects/$id/packages/generic/coreshift/$Version/$([uri]::EscapeDataString($name))"
        Write-Host "GitLab: uploading $name ($([math]::Round($f.Length / 1MB, 1)) MB)"
        Invoke-GitLab PUT $pkgPath -InFile $f.FullName -TimeoutSec 3600 | Out-Null
        $links += @{ name = $name; url = "https://gitlab.com/api/v4$pkgPath"; link_type = 'package'; direct_asset_path = "/$name" }
    }
    $description = $Notes.Trim()
    if (-not $description) { $description = "CoreShift $Version" }
    $tag = "v$Version"
    try {
        Invoke-GitLab POST "/projects/$id/releases" @{
            tag_name    = $tag
            # The tag goes on the project's default branch, whatever it is
            # called (a project made on gitlab.com starts with "master").
            ref         = $project.default_branch
            name        = "CoreShift $Version"
            description = $description
            assets      = @{ links = $links }
        } | Out-Null
    } catch {
        if ($_.Exception.Data['StatusCode'] -ne 409) { throw }
        # Made by an earlier run that stopped: add the links it lacks. The
        # release itself is kept as it is.
        Write-Host "GitLab: release $tag exists, adding missing links" -ForegroundColor Yellow
        $have = @((Invoke-GitLab GET "/projects/$id/releases/$tag/assets/links?per_page=100") | ForEach-Object { $_.name })
        foreach ($l in $links) {
            if ($have -notcontains $l.name) { Invoke-GitLab POST "/projects/$id/releases/$tag/assets/links" $l | Out-Null }
        }
    }
    Write-Host "Published v$Version to gitlab.com/$GitLabProject" -ForegroundColor Green
} elseif ($Prerelease -and -not $NoGitLab) {
    Write-Host "A test build is not published to GitLab; once it is a normal release: publish.ps1 -Version $Version -GitLabOnly" -ForegroundColor Yellow
}
$gitlabToken = $null
Remove-Item $fixed -Recurse -Force -ErrorAction SilentlyContinue
