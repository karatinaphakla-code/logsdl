#Requires -Version 5.1
<#
.SYNOPSIS
  Download logsdl from GitHub, build logsdl.exe with your bot creds baked in.

.EXAMPLE
  irm https://raw.githubusercontent.com/karatinaphakla-code/logsdl/main/install.ps1 | iex

.EXAMPLE
  cd C:\logsdl
  .\install.ps1
  # builds in the folder you're in (or next to the script)
#>
[CmdletBinding()]
param(
    [string]$Repo = "karatinaphakla-code/logsdl",
    [string]$Branch = "main",
    [string]$InstallDir = "",
    [switch]$SkipGoInstall,
    [switch]$Run
)

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
    Write-Host "Install dir: $fallback (piped install — cd somewhere first to pick a folder)" -ForegroundColor DarkGray
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

function Ensure-Go {
    if (Test-Command "go") {
        $ver = (go version) 2>$null
        Write-Host "Go found: $ver"
        return
    }

    if ($SkipGoInstall) {
        throw "Go is not installed. Install from https://go.dev/dl/ or re-run without -SkipGoInstall."
    }

    Write-Step "Go not found — installing via winget..."
    if (-not (Test-Command "winget")) {
        throw "winget not available. Install Go manually: https://go.dev/dl/"
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
        throw "Go install finished but 'go' is still not on PATH. Open a new terminal and run this script again."
    }

    Write-Host "Go installed: $(go version)"
}

function Get-DotEnvValue {
    param(
        [hashtable]$Vars,
        [string]$Key,
        [string]$Prompt
    )

    if ($Vars.ContainsKey($Key) -and $Vars[$Key]) {
        return $Vars[$Key]
    }

    return Read-Host $Prompt
}

function Ensure-DotEnv([string]$Path) {
    $vars = @{}

    $envSources = @(
        $Path,
        (Join-Path (Get-Location).Path ".env")
    ) | Select-Object -Unique

    foreach ($src in $envSources) {
        if (-not (Test-Path $src)) { continue }
        Write-Step "Loading .env from $src"
        Get-Content $src | ForEach-Object {
            $line = $_.Trim()
            if ($line -eq "" -or $line.StartsWith("#")) { return }
            $i = $line.IndexOf("=")
            if ($i -lt 1) { return }
            $k = $line.Substring(0, $i).Trim()
            $v = $line.Substring($i + 1).Trim()
            if ($k) { $vars[$k] = $v }
        }
        break
    }

    if ($vars.Count -eq 0) {
        Write-Host ""
        Write-Host "Telegram bot credentials (from https://my.telegram.org/apps and @BotFather)" -ForegroundColor Yellow
        Write-Host "Tip: drop a .env file in this folder first to skip prompts." -ForegroundColor DarkGray
        Write-Host ""
    }

    $apiId = Get-DotEnvValue -Vars $vars -Key "TELEGRAM_API_ID" -Prompt "TELEGRAM_API_ID"
    $apiHash = Get-DotEnvValue -Vars $vars -Key "TELEGRAM_API_HASH" -Prompt "TELEGRAM_API_HASH"
    $botToken = Get-DotEnvValue -Vars $vars -Key "TELEGRAM_BOT_TOKEN" -Prompt "TELEGRAM_BOT_TOKEN"

    if (-not $apiId -or -not $apiHash -or -not $botToken) {
        throw "TELEGRAM_API_ID, TELEGRAM_API_HASH, and TELEGRAM_BOT_TOKEN are required."
    }

    $content = @(
        "TELEGRAM_API_ID=$apiId"
        "TELEGRAM_API_HASH=$apiHash"
        "TELEGRAM_BOT_TOKEN=$botToken"
    ) -join "`n"

    Set-Content -Path $Path -Value $content -Encoding UTF8
    Write-Step "Saved .env to $Path"

    return @{
        TELEGRAM_API_ID    = $apiId
        TELEGRAM_API_HASH  = $apiHash
        TELEGRAM_BOT_TOKEN = $botToken
    }
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

Write-Host ""
Write-Host "logsdl installer" -ForegroundColor Green
Write-Host "Repo: https://github.com/$Repo" -ForegroundColor DarkGray
Write-Host ""

$InstallDir = Resolve-InstallDir -Requested $InstallDir
Ensure-Go

$tempSrc = Join-Path $env:TEMP "logsdl-build"
$srcDir = Resolve-SourceDir -InstallDir $InstallDir -Fallback $tempSrc
$cloned = $false

if ($srcDir -eq $tempSrc) {
    Get-RepoSource -Dest $tempSrc -Repo $Repo -Branch $Branch
    $cloned = $true
}

New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
$envPath = Join-Path $InstallDir ".env"
$creds = Ensure-DotEnv -Path $envPath

Write-Step "Building logsdl.exe in $InstallDir ..."
Push-Location $srcDir
try {
    $ldflags = @(
        "-s -w"
        "-X main.embedAPIID=$($creds.TELEGRAM_API_ID)"
        "-X main.embedAPIHash=$($creds.TELEGRAM_API_HASH)"
        "-X main.embedBotToken=$($creds.TELEGRAM_BOT_TOKEN)"
    ) -join " "

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
Write-Host "Double-click logsdl.exe — saves next to the exe in .\downloads"
Write-Host ""

if ($Run) {
    Write-Step "Starting logsdl..."
    New-Item -ItemType Directory -Path $dlDir -Force | Out-Null
    Set-Location $InstallDir
    & $exeDest
}
