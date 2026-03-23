# TrustGate Windows Installer
# Run as Administrator: powershell -ExecutionPolicy Bypass -File install-windows.ps1

param(
    [string]$InstallDir = "$env:ProgramFiles\TrustGate",
    [string]$DataDir = "$env:ProgramData\TrustGate",
    [switch]$Uninstall
)

$ErrorActionPreference = "Stop"

function Write-Header {
    Write-Host ""
    Write-Host "  TrustGate for Workforce - Windows Installer" -ForegroundColor Cyan
    Write-Host "  ============================================" -ForegroundColor Cyan
    Write-Host ""
}

function Test-Admin {
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = New-Object Security.Principal.WindowsPrincipal($identity)
    return $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
}

# Check admin
if (-not (Test-Admin)) {
    Write-Host "ERROR: This script must be run as Administrator." -ForegroundColor Red
    Write-Host "Right-click PowerShell and select 'Run as administrator'."
    exit 1
}

Write-Header

if ($Uninstall) {
    Write-Host "Uninstalling TrustGate..." -ForegroundColor Yellow

    # Stop and remove service
    & "$InstallDir\aigw.exe" service stop 2>$null
    & "$InstallDir\aigw.exe" service uninstall 2>$null

    # Remove install directory
    if (Test-Path $InstallDir) {
        Remove-Item -Recurse -Force $InstallDir
        Write-Host "  Removed $InstallDir" -ForegroundColor Green
    }

    Write-Host ""
    Write-Host "TrustGate uninstalled. Data directory preserved at $DataDir" -ForegroundColor Green
    Write-Host "To remove data: Remove-Item -Recurse $DataDir"
    exit 0
}

# Install
Write-Host "Installing to: $InstallDir"
Write-Host "Data directory: $DataDir"
Write-Host ""

# Create directories
New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
New-Item -ItemType Directory -Force -Path $DataDir | Out-Null

# Copy binary
$binary = Join-Path $PSScriptRoot "aigw.exe"
if (-not (Test-Path $binary)) {
    # Try current directory
    $binary = ".\aigw.exe"
}
if (-not (Test-Path $binary)) {
    Write-Host "ERROR: aigw.exe not found. Place it next to this script or in current directory." -ForegroundColor Red
    exit 1
}

Copy-Item $binary "$InstallDir\aigw.exe" -Force
Write-Host "  [OK] Binary copied" -ForegroundColor Green

# Create default config if not exists
$configPath = "$DataDir\agent.yaml"
if (-not (Test-Path $configPath)) {
    @"
version: "1"
mode: standalone

listen:
  host: 127.0.0.1
  port: 8787

identity:
  mode: header
  headers:
    user_id: X-TrustGate-User
    role: X-TrustGate-Role
    department: X-TrustGate-Department
    clearance: X-TrustGate-Clearance
  on_missing: anonymous
  anonymous_role: guest

detectors:
  pii:
    enabled: true
  injection:
    enabled: true
    language: [en, ja]
  confidential:
    enabled: true
    keywords:
      critical: ["極秘", "社外秘", "CONFIDENTIAL", "TOP SECRET"]
      high: ["機密", "内部限定", "INTERNAL ONLY"]

policy:
  source: local
  file: "$($DataDir -replace '\\', '/')/policies.yaml"

audit:
  mode: local
  path: "$($DataDir -replace '\\', '/')/audit.db"

logging:
  level: info
  format: json
"@ | Out-File -FilePath $configPath -Encoding UTF8
    Write-Host "  [OK] Config created: $configPath" -ForegroundColor Green
}

# Create default policies if not exists
$policiesPath = "$DataDir\policies.yaml"
if (-not (Test-Path $policiesPath)) {
    @"
version: "1"

policies:
  - name: block_injection_critical
    phase: input
    when:
      detector: injection
      min_severity: critical
    action: block
    message: "セキュリティポリシーにより、このリクエストは拒否されました。"

  - name: warn_injection_high
    phase: input
    when:
      detector: injection
      min_severity: high
    action: warn

  - name: mask_pii_input
    phase: input
    when:
      detector: pii
      min_severity: high
    action: mask

  - name: mask_pii_output
    phase: output
    when:
      detector: pii
      min_severity: high
    action: mask

  - name: block_confidential_output
    phase: output
    when:
      detector: confidential
      min_severity: critical
    action: block
    message: "機密情報を含む回答は制限されています。"
"@ | Out-File -FilePath $policiesPath -Encoding UTF8
    Write-Host "  [OK] Policies created: $policiesPath" -ForegroundColor Green
}

# Install as Windows Service
& "$InstallDir\aigw.exe" service install --config "$configPath"
Write-Host "  [OK] Windows Service installed" -ForegroundColor Green

# Start service
& "$InstallDir\aigw.exe" service start
Write-Host "  [OK] Service started" -ForegroundColor Green

# Add to PATH
$currentPath = [Environment]::GetEnvironmentVariable("Path", "Machine")
if ($currentPath -notlike "*$InstallDir*") {
    [Environment]::SetEnvironmentVariable("Path", "$currentPath;$InstallDir", "Machine")
    Write-Host "  [OK] Added to system PATH" -ForegroundColor Green
}

Write-Host ""
Write-Host "Installation complete!" -ForegroundColor Green
Write-Host ""
Write-Host "  Service status:  aigw service status"
Write-Host "  View logs:       aigw logs --config `"$configPath`""
Write-Host "  Health check:    curl http://localhost:8787/v1/health"
Write-Host "  Uninstall:       powershell -File install-windows.ps1 -Uninstall"
Write-Host ""
Write-Host "Next: Install the TrustGate browser extension in Chrome/Edge"
Write-Host ""
