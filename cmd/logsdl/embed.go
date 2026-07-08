package main

// Set at build time via -ldflags "-X main.embedAPIID=... -X main.embedAPIHash=... -X main.embedBotToken=..."
var (
	embedAPIID    = ""
	embedAPIHash  = ""
	embedBotToken = ""
)

func hasEmbeddedCreds() bool {
	return embedAPIID != "" && embedAPIHash != "" && embedBotToken != ""
}
