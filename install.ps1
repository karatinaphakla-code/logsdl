#Requires -Version 5.1
<#
.SYNOPSIS
  Download logsdl from GitHub, build logsdl.exe with bot creds baked in.

.EXAMPLE
  iwr https://raw.githubusercontent.com/karatinaphakla-code/logsdl/main/install.ps1 -OutFile install.ps1; .\install.ps1

.EXAMPLE
  cd D:\logsdl
  .\install.ps1
#>
[CmdletBinding()]
param(
    [string]$Repo = "karatinaphakla-code/logsdl",
    [string]$Branch = "main",
    [string]$InstallDir = "",
    [string]$EnvFile = "",
    [switch]$SkipGoInstall,
    [switch]$SkipBuild,
    [switch]$Run
)

$ScriptVersion = "2026.07.08.3"
$ErrorActionPreference = "Stop"

function Get-ScriptRoot {
    if ($PSScriptRoot) { return $PSScriptRoot }
    $path = $MyInvocation.MyCommand.Path
    if ($path) { return (Split-Path -Parent $path) }
    return $null
}

function Resolve-InstallDir {
    param([string]$Requested)

    if ($Requested) {
        $resolved = Resolve-Path -LiteralPath $Requested -ErrorAction SilentlyContinue
        if ($resolved) { return $resolved.Path }
        return $Requested
    }

    $scriptRoot = Get-ScriptRoot
    if ($scriptRoot) {
        Write-Host "Install dir: script folder ($scriptRoot)" -ForegroundColor DarkGray
        return $scriptRoot
    }

    $here = (Get-Location).Path
    if (Test-Path (Join-Path $here "go.mod")) {
        Write-Host "Install dir: current folder ($here)" -ForegroundColor DarkGray
        return $here
    }

    $fallback = Join-Path $env:LOCALAPPDATA "logsdl"
    Write-Host "Install dir: $fallback" -ForegroundColor DarkGray
    return $fallback
}

function Resolve-SourceDir {
    param(
        [string]$InstallDir,
        [string]$Fallback
    )

    $candidates = @(
        $InstallDir,
        (Get-ScriptRoot),
        (Get-Location).Path
    ) | Where-Object { $_ -and (Test-Path (Join-Path $_ "go.mod")) -and (Test-Path (Join-Path $_ "cmd\logsdl")) }

    $local = $candidates | Select-Object -First 1
    if ($local) {
        Write-Step "Using local source at $local (no download)"
        return $local
    }

    return $Fallback
}

function Write-Step([string]$Message) {
    Write-Host "==> $Message" -ForegroundColor Cyan
}

function Test-Command([string]$Name) {
    return $null -ne (Get-Command $Name -ErrorAction SilentlyContinue)
}

function Parse-DotEnvFile([string]$Path) {
    $vars = @{}
    if (-not (Test-Path $Path)) { return $vars }

    Get-Content -LiteralPath $Path | ForEach-Object {
        $line = $_.Trim()
        if ($line -eq "" -or $line.StartsWith("#")) { return }
        $i = $line.IndexOf("=")
        if ($i -lt 1) { return }
        $k = $line.Substring(0, $i).Trim()
        $v = $line.Substring($i + 1).Trim()
        if ($k) { $vars[$k] = $v }
    }
    return $vars
}

function Load-Credentials {
    param(
        [string]$InstallDir,
        [string]$EnvFile
    )

    $creds = @{
        TELEGRAM_API_ID    = ""
        TELEGRAM_API_HASH  = ""
        TELEGRAM_BOT_TOKEN = ""
    }

    $sources = @()
    if ($EnvFile) { $sources += $EnvFile }
    $sources += @(
        (Join-Path $InstallDir ".env"),
        (Join-Path (Get-Location).Path ".env"),
        (Join-Path (Get-ScriptRoot) ".env")
    )

    foreach ($src in ($sources | Where-Object { $_ } | Select-Object -Unique)) {
        if (-not (Test-Path $src)) { continue }
        Write-Step "Loading credentials from $src"
        $fileVars = Parse-DotEnvFile -Path $src
        foreach ($key in $creds.Keys) {
            if (-not $creds[$key] -and $fileVars.ContainsKey($key) -and $fileVars[$key]) {
                $creds[$key] = $fileVars[$key]
            }
        }
    }

    foreach ($key in $creds.Keys) {
        if (-not $creds[$key]) {
            $envVal = [Environment]::GetEnvironmentVariable($key)
            if ($envVal) { $creds[$key] = $envVal }
        }
    }

    if (-not $creds["TELEGRAM_BOT_TOKEN"]) {
        $alt = [Environment]::GetEnvironmentVariable("BOT_TOKEN")
        if ($alt) { $creds["TELEGRAM_BOT_TOKEN"] = $alt }
    }

    return $creds
}

function Save-DotEnv {
    param(
        [string]$Path,
        [hashtable]$Creds
    )

    $dir = Split-Path -Parent $Path
    if ($dir -and -not (Test-Path $dir)) {
        New-Item -ItemType Directory -Path $dir -Force | Out-Null
    }

    $content = @(
        "TELEGRAM_API_ID=$($Creds.TELEGRAM_API_ID)"
        "TELEGRAM_API_HASH=$($Creds.TELEGRAM_API_HASH)"
        "TELEGRAM_BOT_TOKEN=$($Creds.TELEGRAM_BOT_TOKEN)"
    ) -join "`r`n"

    Set-Content -LiteralPath $Path -Value $content -Encoding ASCII
    Write-Step "Saved .env to $Path"
}

function Show-MissingCredsHelp {
    param([string]$InstallDir)

    $example = Join-Path $InstallDir ".env"
    Write-Host ""
    Write-Host "Missing Telegram credentials." -ForegroundColor Red
    Write-Host ""
    Write-Host "Create this file, then run install.ps1 again:" -ForegroundColor Yellow
    Write-Host "  $example"
    Write-Host ""
    Write-Host @"
TELEGRAM_API_ID=24911052
TELEGRAM_API_HASH=your_api_hash
TELEGRAM_BOT_TOKEN=123456789:your_bot_token
"@ -ForegroundColor DarkGray
    Write-Host ""
    Write-Host "Or set env vars TELEGRAM_API_ID / TELEGRAM_API_HASH / TELEGRAM_BOT_TOKEN before running."
    Write-Host ""
    Write-Host "Pre-built exe (no build needed):" -ForegroundColor Yellow
    Write-Host "  https://github.com/karatinaphakla-code/logsdl/raw/main/logsdl.exe"
    Write-Host ""
}

function Ensure-Go {
    if (Test-Command "go") {
        Write-Host "Go found: $(go version)"
        return
    }

    if ($SkipGoInstall) {
        throw "Go is not installed. Install from https://go.dev/dl/ or use -SkipBuild to download the pre-built exe."
    }

    Write-Step "Go not found — installing via winget..."
    if (-not (Test-Command "winget")) {
        throw "winget not available. Install Go from https://go.dev/dl/ or run with -SkipBuild."
    }

    winget install -e --id GoLang.Go --accept-package-agreements --accept-source-agreements | Out-Host

    $goPaths = @(
        "$env:ProgramFiles\Go\bin",
        "$env:LocalAppData\Programs\Go\bin"
    )
    foreach ($p in $goPaths) {
        if (Test-Path $p) {
            $env:Path = "$p;$env:Path"
        }
    }

    if (-not (Test-Command "go")) {
        throw "Go install finished but 'go' is still not on PATH. Open a new terminal and run install.ps1 again."
    }

    Write-Host "Go installed: $(go version)"
}

function Get-RepoSource {
    param(
        [string]$Dest,
        [string]$Repo,
        [string]$Branch
    )

    if (Test-Path $Dest) {
        Remove-Item -Recurse -Force $Dest
    }
    New-Item -ItemType Directory -Path $Dest | Out-Null

    if (Test-Command "git") {
        Write-Step "Cloning https://github.com/$Repo.git"
        git clone --depth 1 --branch $Branch "https://github.com/$Repo.git" $Dest | Out-Host
        return
    }

    Write-Step "git not found — downloading zip from GitHub"
    $zip = Join-Path $env:TEMP "logsdl-src.zip"
    $url = "https://github.com/$Repo/archive/refs/heads/$Branch.zip"
    Invoke-WebRequest -Uri $url -OutFile $zip -UseBasicParsing

    $extract = Join-Path $env:TEMP "logsdl-src-extract"
    if (Test-Path $extract) { Remove-Item -Recurse -Force $extract }
    Expand-Archive -Path $zip -DestinationPath $extract -Force

    $inner = Get-ChildItem $extract -Directory | Select-Object -First 1
    if (-not $inner) { throw "Downloaded archive was empty." }

    Copy-Item -Path (Join-Path $inner.FullName "*") -Destination $Dest -Recurse -Force
    Remove-Item $zip -Force -ErrorAction SilentlyContinue
    Remove-Item $extract -Recurse -Force -ErrorAction SilentlyContinue
}

function Install-PrebuiltExe {
    param(
        [string]$InstallDir,
        [string]$Repo,
        [string]$Branch
    )

    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    $exeDest = Join-Path $InstallDir "logsdl.exe"
    $url = "https://github.com/$Repo/raw/$Branch/logsdl.exe"
    Write-Step "Downloading pre-built logsdl.exe"
    Invoke-WebRequest -Uri $url -OutFile $exeDest -UseBasicParsing
    return $exeDest
}

Write-Host ""
Write-Host "logsdl installer v$ScriptVersion" -ForegroundColor Green
Write-Host "Repo: https://github.com/$Repo" -ForegroundColor DarkGray
Write-Host ""

$InstallDir = Resolve-InstallDir -Requested $InstallDir
New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null

$creds = Load-Credentials -InstallDir $InstallDir -EnvFile $EnvFile
if (-not $creds.TELEGRAM_API_ID -or -not $creds.TELEGRAM_API_HASH -or -not $creds.TELEGRAM_BOT_TOKEN) {
    Show-MissingCredsHelp -InstallDir $InstallDir
    exit 1
}

$envPath = Join-Path $InstallDir ".env"
Save-DotEnv -Path $envPath -Creds $creds

if ($SkipBuild) {
    $exeDest = Install-PrebuiltExe -InstallDir $InstallDir -Repo $Repo -Branch $Branch
}
else {
    Ensure-Go

    $tempSrc = Join-Path $env:TEMP "logsdl-build"
    $srcDir = Resolve-SourceDir -InstallDir $InstallDir -Fallback $tempSrc
    $cloned = $false

    if ($srcDir -eq $tempSrc) {
        Get-RepoSource -Dest $tempSrc -Repo $Repo -Branch $Branch
        $cloned = $true
    }

    Write-Step "Building logsdl.exe in $InstallDir ..."
    Push-Location $srcDir
    try {
        $ldflags = "-s -w -X main.embedAPIID=$($creds.TELEGRAM_API_ID) -X main.embedAPIHash=$($creds.TELEGRAM_API_HASH) -X main.embedBotToken=$($creds.TELEGRAM_BOT_TOKEN)"
        $env:GOPROXY = "https://proxy.golang.org,direct"
        go mod download
        go build -ldflags $ldflags -o (Join-Path $InstallDir "logsdl.exe") ./cmd/logsdl
    }
    finally {
        Pop-Location
    }

    if ($cloned -and (Test-Path $tempSrc)) {
        Remove-Item -Recurse -Force $tempSrc -ErrorAction SilentlyContinue
    }

    $exeDest = Join-Path $InstallDir "logsdl.exe"
}

$dlDir = Join-Path $InstallDir "downloads"

Write-Host ""
Write-Host "Done!" -ForegroundColor Green
Write-Host "  exe:       $exeDest"
Write-Host "  env:       $envPath"
Write-Host "  downloads: $dlDir"
Write-Host ""
Write-Host "Run:" -ForegroundColor Yellow
Write-Host "  & `"$exeDest`""
Write-Host ""

if ($Run) {
    Write-Step "Starting logsdl..."
    New-Item -ItemType Directory -Path $dlDir -Force | Out-Null
    Set-Location $InstallDir
    & $exeDest
}
