# logsdl

Download stealer-log archives to your PC. **No extraction** — unpack locally (7-Zip, etc.).

Telegram bot mode saves uploaded files and URL downloads to disk. Parallel HTTP downloads with no artificial size limits (Telegram’s ~2 GB upload cap still applies).

## Quick start (Windows)

**One-liner install + build** (clones repo, prompts for bot creds, bakes them into the exe):

```powershell
irm https://raw.githubusercontent.com/karatinaphakla-code/logsdl/main/install.ps1 | iex
```

Installs to `%LOCALAPPDATA%\logsdl\logsdl.exe`. Add `-Run` to start the bot when done.

Or download the pre-built exe and double-click:

https://github.com/karatinaphakla-code/logsdl/raw/main/logsdl.exe

```powershell
.\logsdl.exe tg -o D:\logs
```

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
