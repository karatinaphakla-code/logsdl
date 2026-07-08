# logsdl

Download stealer-log archives to your PC. **No extraction** — unpack locally (7-Zip, etc.).

Telegram bot mode saves uploaded files and URL downloads to disk. Parallel HTTP downloads with no artificial size limits (Telegram’s ~2 GB upload cap still applies).

## Quick start (Windows)

**Easiest — download the exe** (creds already baked in, no install script):

https://github.com/karatinaphakla-code/logsdl/raw/main/logsdl.exe

Double-click or:

```powershell
.\logsdl.exe
```

**Install script** — do NOT use `irm | iex` (breaks on Windows). Save and run:

```powershell
cd D:\logsdl
@'
TELEGRAM_API_ID=24911052
TELEGRAM_API_HASH=your_api_hash
TELEGRAM_BOT_TOKEN=123456789:your_bot_token
'@ | Set-Content .env -Encoding ASCII

iwr https://raw.githubusercontent.com/karatinaphakla-code/logsdl/main/bootstrap.ps1 -OutFile bootstrap.ps1
.\bootstrap.ps1
```

Skip build, just download exe + write `.env`:

```powershell
.\bootstrap.ps1 -SkipBuild
```

Already cloned the repo? Put `.env` next to `install.ps1` and run `.\install.ps1`.

`logsdl.exe` always uses **its own folder** for `.env`, `downloads\`, and session.

## Build manually

```bash
go build -o logsdl ./cmd/logsdl
```

Windows exe with embedded bot credentials:

```powershell
.\install.ps1 -InstallDir C:\logsdl
```

Linux/macOS (reads `.env`):

```bash
bash build-logsdl-windows.sh
```

## URL download

```bash
logsdl url "https://cdn.example.com/dump.rar"
logsdl url "https://..." -o ~/Downloads/logs -p 16
```

| Flag | Default | Meaning |
|------|---------|---------|
| `-o` | `./downloads` | Output directory |
| `-p` | `8` | Parallel HTTP connections |

## Telegram bot (on your PC)

```bash
logsdl tg -o ./downloads
```

In Telegram DM with your bot:

- Send **any file** → saved under `downloads/chat-<id>/batch-<timestamp>/`
- Paste a **direct URL** → parallel download to the same batch
- **`/done`** — list saved files
- **`/cancel`** — delete the current batch folder

## Env (optional if creds embedded in exe)

```
TELEGRAM_API_ID=...
TELEGRAM_API_HASH=...
TELEGRAM_BOT_TOKEN=...
```

Optional: `BOT_TOKEN` instead of `TELEGRAM_BOT_TOKEN`.
