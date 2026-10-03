# Builds the Linux packages from Windows, inside WSL: a clean clone of
# -Ref (a tag such as v0.7.0, or a branch) in the distribution's own file
# system, then packaging/linux/build.sh there. The packages land in
# -OutDir (default dist\linux):
#
#   powershell -ExecutionPolicy Bypass -File packaging\linux\build-wsl.ps1 -Ref v0.7.0
#
# The distribution (-Distro, default Ubuntu-24.04) needs Go, Flutter and
# the build tools of packaging/linux/README.md; packaging\release.ps1 calls
# this script for the Linux part of a release. Cores come from
# engine\testdata\bin\linux-amd64 when it holds them, else from
# /root/coreshift-cores/linux-amd64 in the distribution (downloaded there
# the first time; delete the folder to take newer ones).
param(
    [Parameter(Mandatory = $true)][string]$Ref,
    [string]$Distro = 'Ubuntu-24.04',
    [string]$OutDir
)
$ErrorActionPreference = 'Stop'
$root = (Resolve-Path "$PSScriptRoot\..\..").Path
if (-not $OutDir) { $OutDir = Join-Path $root 'dist\linux' }
New-Item -ItemType Directory $OutDir -Force | Out-Null

# The distribution must be there: a missing one would make wsl.exe print a
# hint in UTF-16 and return, which reads as a failed build much later.
if (-not (Get-Command wsl.exe -ErrorAction SilentlyContinue)) {
    throw 'WSL is not installed: the Linux packages are built in WSL (or pass -NoLinux to release.ps1)'
}
$env:WSL_UTF8 = '1'
$distros = (wsl.exe -l -q) | ForEach-Object { $_.Trim([char]0, ' ') } | Where-Object { $_ }
if ($distros -notcontains $Distro) {
    throw "WSL distribution $Distro not found (have: $($distros -join ', ')); install it or pass -NoLinux to release.ps1"
}

function WslPath([string]$p) {
    $full = (Resolve-Path $p).Path
    '/mnt/' + $full.Substring(0, 1).ToLower() + $full.Substring(2).Replace('\', '/')
}

# Clone from the repository itself (.git), which also works from a worktree.
Push-Location $root
try { $gitDir = (Resolve-Path (git rev-parse --git-common-dir).Trim()).Path } finally { Pop-Location }
# Cores: engine\testdata\bin\linux-amd64 when it holds them; else a cache
# in the distribution (/root/coreshift-cores), filled with the latest
# releases the first time, so a release does not hang on GitHub's speed.
$cores = Join-Path $root 'engine\testdata\bin\linux-amd64'
$haveCores = (Test-Path "$cores\xray") -and (Test-Path "$cores\sing-box") -and (Test-Path "$cores\mihomo")
$coresDir = if ($haveCores) { WslPath $cores } else { '/root/coreshift-cores/linux-amd64' }

# Through a script file: quotes and pipes do not survive the way from
# Windows PowerShell through wsl.exe to bash.
$script = @"
set -euo pipefail
work=/root/coreshift-release-build
rm -rf "`$work"
# The repository on the Windows disk belongs to another user as WSL sees
# it; git refuses to read it unless told it is safe.
git config --global --get-all safe.directory | grep -qx '$(WslPath $gitDir)' ||
	git config --global --add safe.directory '$(WslPath $gitDir)'
git clone -q --branch '$Ref' '$(WslPath $gitDir)' "`$work"
cd "`$work"
cores='$coresDir'
if [ ! -x "`$cores/xray" ] || [ ! -x "`$cores/sing-box" ] || [ ! -x "`$cores/mihomo" ]; then
	(cd engine && go run ./cmd/coreshiftd cores fetch -dir "`$cores")
fi
packaging/linux/build.sh --cores "`$cores" --out "`$work/dist"
cp "`$work"/dist/coreshift[-_]* '$(WslPath $OutDir)/'
rm -rf "`$work"
"@
$tmp = Join-Path $env:TEMP "coreshift-build-linux-$PID.sh"
[IO.File]::WriteAllText($tmp, $script.Replace("`r`n", "`n"), (New-Object Text.UTF8Encoding $false))
try {
    wsl.exe -d $Distro -u root -- bash -l (WslPath $tmp)
    if ($LASTEXITCODE -ne 0) { throw "the Linux build in WSL $Distro failed (exit code $LASTEXITCODE)" }
} finally { Remove-Item $tmp -ErrorAction SilentlyContinue }
Get-ChildItem $OutDir -Filter 'coreshift*' | ForEach-Object { Write-Host "  $($_.FullName)" }
