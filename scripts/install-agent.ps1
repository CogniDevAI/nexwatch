#Requires -Version 5.1
<#
.SYNOPSIS
    NexWatch Agent installer for Windows.

.DESCRIPTION
    Downloads the nexwatch-agent Windows release, verifies its checksum
    (and optionally its GPG signature), extracts it, writes agent.yaml
    under %ProgramData%\NexWatch, registers it as the "NexWatchAgent"
    Windows service (via "nexwatch-agent.exe --service install" — see
    cmd/agent/service_windows.go), and starts it.

    Mirrors scripts/install-agent.sh's download/verify/install flow for
    Linux, adapted to Windows: a named pipe Docker socket, a Windows
    service instead of a systemd unit, and PowerShell's native
    Invoke-WebRequest/Get-FileHash instead of curl/sha256sum.

    This script was authored and reviewed on a machine with no Windows or
    PowerShell available and has NOT been executed against a real Windows
    host. It intentionally sticks to simple, long-standing cmdlets
    (Invoke-WebRequest, Expand-Archive, Get-FileHash, Get-Service) rather
    than newer or more exotic ones. Run scripts/test-install-agent.ps1 on
    an actual Windows machine to self-check its pure helper functions
    before relying on this in production.

.PARAMETER Hub
    Hub WebSocket URL, e.g. wss://hub.example.com/ws/agent. Required.

.PARAMETER Token
    Agent authentication token, generated from the hub's Agents page.
    Required.

.PARAMETER Version
    Agent version to install, e.g. "0.9.1" or "v0.9.1". Defaults to the
    latest published GitHub release.

.PARAMETER Interval
    Collection interval in seconds. Defaults to 10.

.PARAMETER InstallDir
    Where to install nexwatch-agent.exe. Defaults to
    "C:\Program Files\NexWatch".

.PARAMETER BaseUrl
    Override the release download base (for an offline/air-gapped mirror
    that reproduces GitHub's release layout). Defaults to this repository's
    GitHub Releases.

.PARAMETER RequireSignature
    Abort unless SHA256SUMS.asc's GPG signature verifies via gpg.exe. If
    gpg.exe is not found on PATH and this switch is set, the install fails
    rather than silently skipping verification.

.EXAMPLE
    .\install-agent.ps1 -Hub wss://hub.example.com/ws/agent -Token abc123

.EXAMPLE
    irm https://raw.githubusercontent.com/CogniDevAI/nexwatch/main/scripts/install-agent.ps1 -OutFile install-agent.ps1
    .\install-agent.ps1 -Hub wss://hub.example.com/ws/agent -Token abc123
    # A two-step download-then-run is used deliberately (rather than
    # `irm ... | iex`) so the script can be reviewed before it runs with
    # the elevated privileges an agent install requires — see README's
    # "Windows agent" section for why this form is recommended over piping
    # straight into iex.
#>
[CmdletBinding()]
param(
    [string]$Hub,
    [string]$Token,
    [string]$Version = "latest",
    [int]$Interval = 10,
    [string]$InstallDir = "C:\Program Files\NexWatch",
    [string]$BaseUrl = "https://github.com/CogniDevAI/nexwatch/releases",
    [switch]$RequireSignature
)

$ErrorActionPreference = "Stop"

$script:ServiceName = "NexWatchAgent"
$script:Repo = "CogniDevAI/nexwatch"
$script:DefaultSigningKeyUrl = "https://raw.githubusercontent.com/CogniDevAI/nexwatch/main/scripts/release-signing-key.asc"

function Write-Info    { param([string]$Message) Write-Host "[INFO] $Message" -ForegroundColor Cyan }
function Write-Success { param([string]$Message) Write-Host "[OK] $Message" -ForegroundColor Green }
function Write-WarningMsg { param([string]$Message) Write-Host "[WARN] $Message" -ForegroundColor Yellow }
function Write-ErrorMsg { param([string]$Message) Write-Host "[ERROR] $Message" -ForegroundColor Red }

# Test-IsAdministrator reports whether the current process is running
# elevated. install-agent.sh's equivalent is check_root's `id -u` check.
function Test-IsAdministrator {
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = New-Object Security.Principal.WindowsPrincipal($identity)
    return $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
}

# Get-NexWatchAssetName builds the Windows release asset name for a given
# (already-normalized, no leading "v") version — pure string formatting so
# scripts/test-install-agent.ps1 can check it without any network access.
function Get-NexWatchAssetName {
    param([Parameter(Mandatory)][string]$VersionNumber)
    return "nexwatch-agent_${VersionNumber}_windows_amd64.zip"
}

# Remove-VPrefix strips a single leading "v" from a version string, the
# same normalization install-agent.sh applies via bash parameter expansion
# (${VERSION#v}) before building an asset name.
function Remove-VPrefix {
    param([Parameter(Mandatory)][string]$RawVersion)
    if ($RawVersion.StartsWith("v")) {
        return $RawVersion.Substring(1)
    }
    return $RawVersion
}

# Compare-FileHash reports whether path's SHA-256 matches expectedHash
# (case-insensitively — Get-FileHash and a published SHA256SUMS file don't
# reliably agree on letter case).
function Compare-FileHash {
    param(
        [Parameter(Mandatory)][string]$Path,
        [Parameter(Mandatory)][string]$ExpectedHash
    )
    $actual = (Get-FileHash -Path $Path -Algorithm SHA256).Hash
    return $actual.ToLowerInvariant() -eq $ExpectedHash.ToLowerInvariant()
}

# Get-ShaSumsEntry parses one "<hash>  <filename>" (or "<hash> *<filename>")
# line out of a SHA256SUMS-formatted file's content for assetName. Pure
# over its inputs, mirroring install-agent.sh's `awk` one-liner, so it is
# unit-testable without a real downloaded file.
function Get-ShaSumsEntry {
    param(
        [Parameter(Mandatory)][string[]]$Lines,
        [Parameter(Mandatory)][string]$AssetName
    )
    foreach ($line in $Lines) {
        $trimmed = $line.Trim()
        if ($trimmed -eq "") { continue }
        $parts = $trimmed -split '\s+', 2
        if ($parts.Count -lt 2) { continue }
        $hash = $parts[0]
        $name = $parts[1].TrimStart('*')
        if ($name -eq $AssetName) {
            return $hash
        }
    }
    return $null
}

function Resolve-NexWatchVersion {
    param([Parameter(Mandatory)][string]$RequestedVersion)
    if ($RequestedVersion -ne "latest") {
        return (Remove-VPrefix $RequestedVersion)
    }
    Write-Info "Resolving latest version..."
    $releaseInfo = Invoke-RestMethod -Uri "https://api.github.com/repos/$script:Repo/releases/latest" -UseBasicParsing
    if (-not $releaseInfo.tag_name) {
        throw "Failed to resolve latest version from GitHub."
    }
    $resolved = Remove-VPrefix $releaseInfo.tag_name
    Write-Info "Latest version: $resolved"
    return $resolved
}

function Test-GpgSignature {
    param(
        [Parameter(Mandatory)][string]$SumsPath,
        [Parameter(Mandatory)][string]$SigPath,
        [Parameter(Mandatory)][string]$WorkDir
    )
    $gpg = Get-Command gpg.exe -ErrorAction SilentlyContinue
    if (-not $gpg) {
        if ($RequireSignature) {
            throw "gpg.exe is required for signature verification (-RequireSignature set) but was not found on PATH."
        }
        Write-WarningMsg "gpg.exe not found — skipping release signature verification."
        return $false
    }

    $keyPath = Join-Path $WorkDir "release-signing-key.asc"
    try {
        Invoke-WebRequest -Uri $script:DefaultSigningKeyUrl -OutFile $keyPath -UseBasicParsing
    } catch {
        if ($RequireSignature) {
            throw "Could not fetch signing public key from $script:DefaultSigningKeyUrl (-RequireSignature set)."
        }
        Write-WarningMsg "Could not fetch signing public key — skipping signature verification."
        return $false
    }

    $gnupgHome = Join-Path $WorkDir "gnupg"
    New-Item -ItemType Directory -Path $gnupgHome -Force | Out-Null
    $env:GNUPGHOME = $gnupgHome

    & $gpg.Path --batch --quiet --import $keyPath 2>&1 | Out-Null
    if ($LASTEXITCODE -ne 0) {
        throw "Failed to import the release signing public key."
    }

    & $gpg.Path --batch --verify $SigPath $SumsPath 2>&1 | Out-Null
    if ($LASTEXITCODE -ne 0) {
        throw "Release signature verification FAILED. The release may be tampered with — aborting."
    }

    Write-Success "Release signature verified"
    return $true
}

function Install-NexWatchAgent {
    if ([string]::IsNullOrWhiteSpace($Hub)) {
        throw "-Hub is required (Hub WebSocket URL)."
    }
    if ([string]::IsNullOrWhiteSpace($Token)) {
        throw "-Token is required (agent authentication token)."
    }
    if (-not (Test-IsAdministrator)) {
        throw "This script must be run from an elevated (Administrator) PowerShell prompt."
    }

    $resolvedVersion = Resolve-NexWatchVersion -RequestedVersion $Version
    $assetName = Get-NexWatchAssetName -VersionNumber $resolvedVersion
    $releaseBase = "$BaseUrl/download/v$resolvedVersion"
    $downloadUrl = "$releaseBase/$assetName"

    $workDir = Join-Path ([System.IO.Path]::GetTempPath()) ("nexwatch-install-" + [System.Guid]::NewGuid().ToString("N"))
    New-Item -ItemType Directory -Path $workDir -Force | Out-Null
    try {
        $zipPath = Join-Path $workDir $assetName
        Write-Info "Downloading nexwatch-agent $resolvedVersion..."
        Invoke-WebRequest -Uri $downloadUrl -OutFile $zipPath -UseBasicParsing

        $sumsPath = Join-Path $workDir "SHA256SUMS"
        $haveSums = $true
        try {
            Invoke-WebRequest -Uri "$releaseBase/SHA256SUMS" -OutFile $sumsPath -UseBasicParsing
        } catch {
            $haveSums = $false
        }

        $expectedHash = $null
        if ($haveSums) {
            $expectedHash = Get-ShaSumsEntry -Lines (Get-Content $sumsPath) -AssetName $assetName
        }
        if (-not $expectedHash) {
            $perFilePath = Join-Path $workDir "$assetName.sha256"
            try {
                Invoke-WebRequest -Uri "$downloadUrl.sha256" -OutFile $perFilePath -UseBasicParsing
                $expectedHash = ((Get-Content $perFilePath -Raw).Trim() -split '\s+')[0]
            } catch {
                Write-WarningMsg "No checksum published for $assetName — skipping checksum verification."
            }
        }
        if ($expectedHash) {
            if (-not (Compare-FileHash -Path $zipPath -ExpectedHash $expectedHash)) {
                throw "Checksum verification failed for $assetName."
            }
            Write-Success "Checksum verified"
        }

        if ($haveSums) {
            $sigPath = Join-Path $workDir "SHA256SUMS.asc"
            try {
                Invoke-WebRequest -Uri "$releaseBase/SHA256SUMS.asc" -OutFile $sigPath -UseBasicParsing
                Test-GpgSignature -SumsPath $sumsPath -SigPath $sigPath -WorkDir $workDir | Out-Null
            } catch {
                if ($RequireSignature) {
                    throw "Release $resolvedVersion has no signed SHA256SUMS (-RequireSignature set)."
                }
                Write-WarningMsg "No signed release checksums published — skipping signature verification."
            }
        } elseif ($RequireSignature) {
            throw "Release $resolvedVersion has no SHA256SUMS to verify a signature against (-RequireSignature set)."
        }

        Write-Info "Extracting..."
        $extractDir = Join-Path $workDir "extracted"
        Expand-Archive -Path $zipPath -DestinationPath $extractDir -Force

        $exeSource = Join-Path $extractDir "nexwatch-agent.exe"
        if (-not (Test-Path $exeSource)) {
            throw "nexwatch-agent.exe not found in the downloaded archive."
        }

        New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
        $exeDest = Join-Path $InstallDir "nexwatch-agent.exe"

        $existingService = Get-Service -Name $script:ServiceName -ErrorAction SilentlyContinue
        if ($existingService) {
            Write-Info "Stopping existing $script:ServiceName service to replace its binary..."
            Stop-Service -Name $script:ServiceName -ErrorAction SilentlyContinue
        }
        Copy-Item -Path $exeSource -Destination $exeDest -Force
        Write-Success "Binary installed to $exeDest"

        $programDataDir = Join-Path $env:ProgramData "NexWatch"
        New-Item -ItemType Directory -Path $programDataDir -Force | Out-Null
        $configPath = Join-Path $programDataDir "agent.yaml"

        $configYaml = @"
# NexWatch Agent Configuration
# Generated by install-agent.ps1

hub_url: "$Hub"
token: "$Token"
interval: ${Interval}s
docker_socket: npipe:////./pipe/docker_engine
collectors_enabled:
  - cpu
  - memory
  - disk
  - network
  - sysinfo
  - docker
  - ports
  - processes
  - hardening
  - diskio
  - connections
  - services
  - cve_scan
"@
        Set-Content -Path $configPath -Value $configYaml -Encoding UTF8
        Write-Success "Config written to $configPath"

        if ($existingService) {
            Write-Info "Reinstalling service registration..."
            & $exeDest --service uninstall | Out-Null
        }

        & $exeDest --service install --config $configPath
        if ($LASTEXITCODE -ne 0) {
            throw "Service installation failed (exit code $LASTEXITCODE)."
        }

        Start-Service -Name $script:ServiceName
        Start-Sleep -Seconds 2
        $status = Get-Service -Name $script:ServiceName
        if ($status.Status -eq "Running") {
            Write-Success "Service $script:ServiceName is running"
        } else {
            Write-WarningMsg "Service may not have started correctly. Status: $($status.Status). Check Event Viewer (source: $script:ServiceName) and $programDataDir\agent.log."
        }

        Write-Host ""
        Write-Success "NexWatch Agent $resolvedVersion installed and running!"
        Write-Host ""
        Write-Info "Useful commands:"
        Write-Host "  Status:   Get-Service $script:ServiceName"
        Write-Host "  Logs:     Get-Content `"$programDataDir\agent.log`" -Tail 50 -Wait"
        Write-Host "  Restart:  Restart-Service $script:ServiceName"
        Write-Host "  Stop:     Stop-Service $script:ServiceName"
        Write-Host "  Config:   $configPath"
        Write-Host ""
    } finally {
        Remove-Item -Path $workDir -Recurse -Force -ErrorAction SilentlyContinue
    }
}

# Entrypoint guard, mirroring install-agent.sh's `return 0 2>/dev/null`
# check: $MyInvocation.InvocationName is "." when this file is dot-sourced
# (as scripts/test-install-agent.ps1 does, to exercise the pure helper
# functions above without running the installer itself) and the script's
# own path otherwise.
if ($MyInvocation.InvocationName -ne ".") {
    Install-NexWatchAgent
}
