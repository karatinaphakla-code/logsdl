#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"

if [[ ! -f .env ]]; then
  echo "missing .env — need TELEGRAM_API_ID, TELEGRAM_API_HASH, TELEGRAM_BOT_TOKEN" >&2
  exit 1
fi

set -a
source .env
set +a

: "${TELEGRAM_API_ID:?TELEGRAM_API_ID required in .env}"
: "${TELEGRAM_API_HASH:?TELEGRAM_API_HASH required in .env}"
: "${TELEGRAM_BOT_TOKEN:?TELEGRAM_BOT_TOKEN required in .env}"

export GOOS=windows GOARCH=amd64
export GOPROXY="${GOPROXY:-https://proxy.golang.org,direct}"

LDFLAGS="-s -w"
LDFLAGS="$LDFLAGS -X main.embedAPIID=${TELEGRAM_API_ID}"
LDFLAGS="$LDFLAGS -X main.embedAPIHash=${TELEGRAM_API_HASH}"
LDFLAGS="$LDFLAGS -X main.embedBotToken=${TELEGRAM_BOT_TOKEN}"

go build -ldflags "$LDFLAGS" -o logsdl.exe ./cmd/logsdl
echo "built logsdl.exe ($(ls -lh logsdl.exe | awk '{print $5}'))"
echo "double-click or run: logsdl.exe tg -o D:\\logs"
