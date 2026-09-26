# Builds the installer of any version: a tag (v0.2.0), a branch
# (release/0.2.0) or a commit. The code is checked out into a temporary git
# worktree, so the working copy is left alone; the installer lands in dist\
# next to the others, e.g. dist\coreshift-setup-0.2.0-b14.exe.
#
#   powershell -ExecutionPolicy Bypass -File packaging\build-version.ps1 -Ref v0.2.0
#
# Installing it over a later version is allowed: the installer asks first and
# keeps settings and subscriptions. The cores come from this checkout's
# engine\testdata\bin, as the repository does not keep them.
param(
    [Parameter(Mandatory = $true)][string]$Ref,
    [string]$Cores
)
$ErrorActionPreference = 'Stop'
$root = (Resolve-Path "$PSScriptRoot\..").Path
if (-not $Cores) { $Cores = Join-Path $root 'engine\testdata\bin' }

function Check($what) {
    if ($LASTEXITCODE -ne 0) { throw "$what failed (exit code $LASTEXITCODE)" }
}

Push-Location $root
try {
    git rev-parse --verify --quiet "$Ref^{commit}" | Out-Null
    if ($LASTEXITCODE -ne 0) { throw "no such version: $Ref (see: git tag; git branch --list release/*)" }
    $src = Join-Path $root ('dist\src\' + ($Ref -replace '[^\w.\-]', '_'))
    if (Test-Path $src) {
        git worktree remove --force $src
        if (Test-Path $src) { Remove-Item $src -Recurse -Force }
    }
    git worktree prune
    git worktree add --detach $src $Ref
    Check 'git worktree add'
} finally { Pop-Location }

try {
    $build = Join-Path $src 'packaging\windows\build.ps1'
    if (-not (Test-Path $build)) { throw "$Ref has no packaging\windows\build.ps1" }
    & $build -Cores $Cores -OutDir (Join-Path $root 'dist')
} finally {
    Push-Location $root
    try { git worktree remove --force $src } finally { Pop-Location }
}
