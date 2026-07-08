# One-shot bootstrap — saves install.ps1 to disk (no broken irm|iex pipe)
# Run in PowerShell:
#   iwr https://raw.githubusercontent.com/karatinaphakla-code/logsdl/main/bootstrap.ps1 -UseBasicParsing | iex

$ErrorActionPreference = "Stop"
$Repo = "karatinaphakla-code/logsdl"
$Branch = "main"
$InstallScript = "https://raw.githubusercontent.com/$Repo/$Branch/install.ps1"
$LocalScript = Join-Path $env:TEMP "logsdl-install.ps1"

Write-Host "Downloading install.ps1 ..."
Invoke-WebRequest -Uri $InstallScript -OutFile $LocalScript -UseBasicParsing
Write-Host "Running $LocalScript ..."
& $LocalScript @args
