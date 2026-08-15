param(
  [string]$Instance,
  [string]$BaseDir,
  [string]$Slot,
  [switch]$AllowDirty,
  [switch]$Help,
  [string]$GoBin = $(if ($env:GO_BIN) { $env:GO_BIN } else { "go" })
)

$ErrorActionPreference = "Stop"

function Show-Usage {
  @'
Usage: .\upgrade-local.ps1 [-Instance <id>] [-BaseDir <dir>] [-Slot <slot>] [-AllowDirty]

Pull the current branch, rebuild .\bin\codex-remote.exe, stage it into the
repo-bound daemon's fixed local-upgrade artifact path, and trigger the built-in
local upgrade transaction.

Options:
  -Instance <id>   Override the repo install target instance.
  -BaseDir <dir>   Override the install base directory resolved for that instance.
  -Slot <slot>     Optional explicit upgrade slot label.
  -AllowDirty      Skip the clean-worktree guard before git pull.
  -GoBin <path>    Go executable to use (default: $env:GO_BIN or go).
  -Help            Show this help.
'@ | Write-Output
}

function Invoke-Checked {
  param(
    [Parameter(Mandatory = $true)][string]$FilePath,
    [Parameter(ValueFromRemainingArguments = $true)][string[]]$Arguments
  )

  & $FilePath @Arguments
  if ($LASTEXITCODE -ne 0) {
    throw "$FilePath failed with exit code $LASTEXITCODE."
  }
}

function Get-BuildBranch {
  if (-not [string]::IsNullOrWhiteSpace($env:CODEX_REMOTE_BUILD_BRANCH)) {
    return $env:CODEX_REMOTE_BUILD_BRANCH.Trim()
  }
  $branch = (& git branch --show-current).Trim()
  if (-not [string]::IsNullOrWhiteSpace($branch)) {
    return $branch
  }
  return "dev"
}

if ($Help) {
  Show-Usage
  return
}

$rootDir = $PSScriptRoot
if ([string]::IsNullOrWhiteSpace($rootDir)) {
  throw "Unable to determine repository root. Run this script from a file."
}
Set-Location -LiteralPath $rootDir

if (-not $AllowDirty) {
  & git diff --quiet --ignore-submodules --
  $workingTreeDirty = $LASTEXITCODE -ne 0
  & git diff --cached --quiet --ignore-submodules --
  $indexDirty = $LASTEXITCODE -ne 0
  if ($workingTreeDirty -or $indexDirty) {
    throw "working tree has uncommitted changes; commit/stash them first or rerun with -AllowDirty"
  }
}

Write-Output "[1/5] git pull --ff-only"
Invoke-Checked git pull --ff-only

Write-Output "[2/5] resolve repo install target"
$targetArgs = @("run", "./scripts/install/repo-install-target", "--format", "json")
if (-not [string]::IsNullOrWhiteSpace($Instance)) {
  $targetArgs += @("--instance", $Instance)
}
if (-not [string]::IsNullOrWhiteSpace($BaseDir)) {
  $targetArgs += @("--base-dir", $BaseDir)
}
$targetJSON = & $GoBin @targetArgs
if ($LASTEXITCODE -ne 0) {
  throw "repo install target resolution failed with exit code $LASTEXITCODE."
}
$target = $targetJSON | ConvertFrom-Json

Write-Output "target instance: $($target.instanceId)"
Write-Output "target state: $($target.statePath)"
Write-Output "target log: $($target.logPath)"
Write-Output "target admin: $($target.admin.url)"

$binDir = Join-Path $rootDir "bin"
$buildOutput = Join-Path $binDir "codex-remote.exe"
Write-Output "[3/5] build $buildOutput"
New-Item -ItemType Directory -Force -Path $binDir | Out-Null
Invoke-Checked $GoBin build -ldflags "-X main.branch=$(Get-BuildBranch)" -o $buildOutput (Join-Path $rootDir "cmd/codex-remote")

if (-not (Test-Path -LiteralPath $target.statePath -PathType Leaf)) {
  throw "install state not found: $($target.statePath)`nBuild .\bin\codex-remote.exe and run '.\bin\codex-remote.exe install -bootstrap-only -start-daemon' first, or pass -BaseDir for the installed environment."
}

Write-Output "[4/5] stage local artifact $($target.localUpgradeArtifactPath)"
New-Item -ItemType Directory -Force -Path (Split-Path -Parent $target.localUpgradeArtifactPath) | Out-Null
Copy-Item -LiteralPath $buildOutput -Destination $target.localUpgradeArtifactPath -Force

Write-Output "[5/5] request built-in local upgrade transaction"
$proxyVariables = @("http_proxy", "https_proxy", "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "all_proxy")
$priorProxyValues = @{}
foreach ($name in $proxyVariables) {
  $priorProxyValues[$name] = [Environment]::GetEnvironmentVariable($name, "Process")
  [Environment]::SetEnvironmentVariable($name, $null, "Process")
}
$priorRepoRoot = $env:CODEX_REMOTE_REPO_ROOT
try {
  $env:CODEX_REMOTE_REPO_ROOT = $rootDir
  $upgradeArgs = @("local-upgrade", "-state-path", $target.statePath)
  if (-not [string]::IsNullOrWhiteSpace($Slot)) {
    $upgradeArgs += @("-slot", $Slot)
  }
  Invoke-Checked $buildOutput @upgradeArgs
} finally {
  $env:CODEX_REMOTE_REPO_ROOT = $priorRepoRoot
  foreach ($name in $proxyVariables) {
    [Environment]::SetEnvironmentVariable($name, $priorProxyValues[$name], "Process")
  }
}
