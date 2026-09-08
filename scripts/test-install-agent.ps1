#Requires -Version 5.1
<#
.SYNOPSIS
    Self-check for install-agent.ps1's pure helper functions.

.DESCRIPTION
    A Pester-free equivalent of scripts/test-install-agent.sh: dot-sources
    install-agent.ps1 (whose entrypoint guard means dot-sourcing only
    defines its functions, never runs the actual install — see that
    script's closing `if ($MyInvocation.InvocationName -ne ".")` block)
    and exercises its pure, no-network, no-filesystem helper functions
    directly: asset-name construction, version-prefix normalization, and
    SHA256SUMS-line/hash comparison parsing.

    This script was authored and reviewed on a machine with no Windows or
    PowerShell available and has NOT actually been executed. Run it on a
    real Windows machine (or any host with PowerShell 5.1+/pwsh) before
    trusting install-agent.ps1 in production:

        pwsh -File scripts/test-install-agent.ps1

.EXAMPLE
    .\test-install-agent.ps1
#>
[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"

$script:PassCount = 0
$script:FailCount = 0

function Write-TestPass { param([string]$Message) $script:PassCount++; Write-Host "[PASS] $Message" -ForegroundColor Green }
function Write-TestFail { param([string]$Message) $script:FailCount++; Write-Host "[FAIL] $Message" -ForegroundColor Red }

function Assert-Equal {
    param(
        [Parameter(Mandatory)][string]$TestName,
        $Expected,
        $Actual
    )
    if ($Expected -eq $Actual) {
        Write-TestPass "$TestName"
    } else {
        Write-TestFail "$TestName (expected [$Expected], got [$Actual])"
    }
}

function Assert-True {
    param(
        [Parameter(Mandatory)][string]$TestName,
        [bool]$Condition
    )
    if ($Condition) {
        Write-TestPass $TestName
    } else {
        Write-TestFail $TestName
    }
}

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$installerPath = Join-Path $scriptDir "install-agent.ps1"

if (-not (Test-Path $installerPath)) {
    Write-Host "SKIP: install-agent.ps1 not found next to this script." -ForegroundColor Yellow
    exit 0
}

# Dot-source: only defines functions/params, per install-agent.ps1's own
# entrypoint guard — this never attempts a real install.
. $installerPath

# --- Get-NexWatchAssetName ---
Assert-Equal -TestName "asset name for a plain version" `
    -Expected "nexwatch-agent_0.9.1_windows_amd64.zip" `
    -Actual (Get-NexWatchAssetName -VersionNumber "0.9.1")

# --- Remove-VPrefix ---
Assert-Equal -TestName "strips a leading v" -Expected "0.9.1" -Actual (Remove-VPrefix "v0.9.1")
Assert-Equal -TestName "leaves a version with no v prefix alone" -Expected "0.9.1" -Actual (Remove-VPrefix "0.9.1")
Assert-Equal -TestName "only strips one leading v" -Expected "v0.9.1" -Actual (Remove-VPrefix "vv0.9.1")

# --- Get-ShaSumsEntry ---
$sumsLines = @(
    "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa  nexwatch-agent_0.9.1_linux_amd64.tar.gz",
    "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb  nexwatch-agent_0.9.1_windows_amd64.zip",
    "",
    "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc *nexwatch-agent_0.9.1_darwin_amd64.tar.gz"
)
Assert-Equal -TestName "finds the matching windows entry" `
    -Expected "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" `
    -Actual (Get-ShaSumsEntry -Lines $sumsLines -AssetName "nexwatch-agent_0.9.1_windows_amd64.zip")
Assert-Equal -TestName "handles a binary-mode asterisk prefix" `
    -Expected "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc" `
    -Actual (Get-ShaSumsEntry -Lines $sumsLines -AssetName "nexwatch-agent_0.9.1_darwin_amd64.tar.gz")
Assert-Equal -TestName "returns null for a missing entry" `
    -Expected $null `
    -Actual (Get-ShaSumsEntry -Lines $sumsLines -AssetName "does-not-exist.zip")

# --- Compare-FileHash ---
$tempFile = New-TemporaryFile
try {
    Set-Content -Path $tempFile.FullName -Value "hello nexwatch" -NoNewline
    $actualHash = (Get-FileHash -Path $tempFile.FullName -Algorithm SHA256).Hash

    Assert-True -TestName "matching hash compares equal" `
        -Condition (Compare-FileHash -Path $tempFile.FullName -ExpectedHash $actualHash)
    Assert-True -TestName "matching hash compares equal case-insensitively" `
        -Condition (Compare-FileHash -Path $tempFile.FullName -ExpectedHash $actualHash.ToLowerInvariant())
    Assert-True -TestName "mismatched hash compares unequal" `
        -Condition (-not (Compare-FileHash -Path $tempFile.FullName -ExpectedHash ("0" * 64)))
} finally {
    Remove-Item -Path $tempFile.FullName -Force -ErrorAction SilentlyContinue
}

# --- Test-IsAdministrator (smoke test only — just confirm it runs and
#     returns a bool, since whether *this* session is elevated depends on
#     how the test itself was launched) ---
$isAdminResult = Test-IsAdministrator
Assert-True -TestName "Test-IsAdministrator returns a boolean" -Condition ($isAdminResult -is [bool])

Write-Host ""
Write-Host "Results: $script:PassCount passed, $script:FailCount failed" -ForegroundColor $(if ($script:FailCount -eq 0) { "Green" } else { "Red" })

if ($script:FailCount -gt 0) {
    exit 1
}
exit 0
