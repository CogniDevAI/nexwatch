#Requires -Version 5.1
<#
.SYNOPSIS
    Uninstalls the NexWatch Agent Windows service.

.DESCRIPTION
    Stops and removes the "NexWatchAgent" Windows service (via
    "nexwatch-agent.exe --service uninstall" — see
    cmd/agent/service_windows.go, which also removes the Event Log source),
    then removes the installed binary. Configuration and logs under
    %ProgramData%\NexWatch are preserved unless -RemoveData is passed.

    This script was authored and reviewed on a machine with no Windows or
    PowerShell available and has NOT been executed against a real Windows
    host.

.PARAMETER InstallDir
    Where nexwatch-agent.exe was installed. Defaults to
    "C:\Program Files\NexWatch" (install-agent.ps1's own default).

.PARAMETER RemoveData
    Also delete %ProgramData%\NexWatch (agent.yaml, agent.log, the
    cve_scan collector's scanner cache). Off by default so a reinstall
    keeps the existing configuration.

.EXAMPLE
    .\uninstall-agent.ps1

.EXAMPLE
    .\uninstall-agent.ps1 -RemoveData
#>
[CmdletBinding()]
param(
    [string]$InstallDir = "C:\Program Files\NexWatch",
    [switch]$RemoveData
)

$ErrorActionPreference = "Stop"

$script:ServiceName = "NexWatchAgent"

function Write-Info    { param([string]$Message) Write-Host "[INFO] $Message" -ForegroundColor Cyan }
function Write-Success { param([string]$Message) Write-Host "[OK] $Message" -ForegroundColor Green }
function Write-WarningMsg { param([string]$Message) Write-Host "[WARN] $Message" -ForegroundColor Yellow }

function Test-IsAdministrator {
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = New-Object Security.Principal.WindowsPrincipal($identity)
    return $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
}

function Uninstall-NexWatchAgent {
    if (-not (Test-IsAdministrator)) {
        throw "This script must be run from an elevated (Administrator) PowerShell prompt."
    }

    $exePath = Join-Path $InstallDir "nexwatch-agent.exe"
    $service = Get-Service -Name $script:ServiceName -ErrorAction SilentlyContinue

    if ($service) {
        if (Test-Path $exePath) {
            Write-Info "Uninstalling $script:ServiceName service..."
            & $exePath --service uninstall
            if ($LASTEXITCODE -ne 0) {
                Write-WarningMsg "nexwatch-agent.exe --service uninstall exited with code $LASTEXITCODE — falling back to sc.exe delete."
                Stop-Service -Name $script:ServiceName -ErrorAction SilentlyContinue
                sc.exe delete $script:ServiceName | Out-Null
            }
        } else {
            Write-WarningMsg "$exePath not found — removing the service registration directly."
            Stop-Service -Name $script:ServiceName -ErrorAction SilentlyContinue
            sc.exe delete $script:ServiceName | Out-Null
        }
        Write-Success "Service $script:ServiceName removed"
    } else {
        Write-Info "Service $script:ServiceName is not installed — nothing to remove."
    }

    if (Test-Path $InstallDir) {
        Remove-Item -Path $InstallDir -Recurse -Force
        Write-Success "Removed $InstallDir"
    }

    if ($RemoveData) {
        $programDataDir = Join-Path $env:ProgramData "NexWatch"
        if (Test-Path $programDataDir) {
            Remove-Item -Path $programDataDir -Recurse -Force
            Write-Success "Removed $programDataDir (config, logs, scanner cache)"
        }
    } else {
        Write-Info "Configuration and logs under %ProgramData%\NexWatch were kept (pass -RemoveData to delete them too)."
    }

    Write-Success "NexWatch Agent uninstalled"
}

# See install-agent.ps1's identical entrypoint guard comment.
if ($MyInvocation.InvocationName -ne ".") {
    Uninstall-NexWatchAgent
}
