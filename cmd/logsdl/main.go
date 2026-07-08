package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"logsdl/downloader"
)

func main() {
	root := initAppRoot()
	loadDotEnv(filepath.Join(root, ".env"))

	if len(os.Args) < 2 {
		if hasEmbeddedCreds() {
			runTelegram(root, nil)
			return
		}
		printUsage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "url", "download":
		runURL(root, os.Args[2:])
	case "tg", "telegram", "bot":
		runTelegram(root, os.Args[2:])
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		printUsage()
		os.Exit(2)
	}
}

func printUsage() {
	fmt.Print(`logsdl — download stealer-log archives to your PC (no extraction)

Usage:
  logsdl url <direct-url> [flags]
  logsdl tg  [flags]

URL flags:
  -o dir       output directory (default: <exe-folder>/downloads)
  -p N         parallel connections (default: 8)

Telegram flags:
  -o dir       save downloads here (default: <exe-folder>/downloads)
  -p N         parallel connections for URL downloads (default: 8)

Telegram commands (in bot DM):
  send any file as document  → saves to output folder
  paste direct URL           → parallel download
  /done                  → list saved files in current batch
  /cancel                → discard current batch folder
  /help                  → show commands

Env (from .env or environment):
  TELEGRAM_API_ID, TELEGRAM_API_HASH, TELEGRAM_BOT_TOKEN (or BOT_TOKEN)

Examples:
  logsdl url "https://cdn.example.com/logs.rar" -o ~/Downloads/logs
  logsdl tg -o D:/logs
`)
}

func runURL(root string, args []string) {
	fs := flag.NewFlagSet("url", flag.ExitOnError)
	outDir := fs.String("o", defaultDownloadsDir(root), "output directory")
	parallel := fs.Int("p", downloader.DefaultParallel, "parallel connections")
	_ = fs.Parse(args)

	if fs.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "error: URL required")
		os.Exit(2)
	}
	rawURL := strings.TrimSpace(fs.Arg(0))
	if rawURL == "" {
		fmt.Fprintln(os.Stderr, "error: URL required")
		os.Exit(2)
	}

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		log.Fatalf("mkdir: %v", err)
	}

	name := downloader.SanitizeFilename(downloader.NameFromURL(rawURL))
	dst := filepath.Join(*outDir, name)

	log.Printf("downloading %s", rawURL)
	log.Printf("  -> %s (parallel=%d)", dst, *parallel)

	ctx := context.Background()

	prog := func(d, total int64, bps float64) {
		pct := 0.0
		if total > 0 {
			pct = float64(d) / float64(total) * 100
		}
		fmt.Fprintf(os.Stderr, "\r  %s / %s  %.1f%%  @ %s/s   ",
			downloader.FormatBytes(d), downloader.FormatBytes(total), pct, downloader.FormatBytes(int64(bps)))
	}

	res, err := downloader.ParallelDownload(ctx, rawURL, dst, *parallel, 0, prog)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		log.Fatalf("download failed: %v", err)
	}

	ext, err := downloader.DetectArchiveExt(res.Path)
	if err != nil {
		log.Printf("warning: %v", err)
	} else if filepath.Ext(res.Path) != ext {
		final := strings.TrimSuffix(res.Path, filepath.Ext(res.Path)) + ext
		if err := os.Rename(res.Path, final); err == nil {
			res.Path = final
		}
	}

	mbps := float64(res.Bytes) / 1024 / 1024 / res.Duration.Seconds()
	log.Printf("done: %s", res.Path)
	log.Printf("  size=%s  time=%v  speed=%.2f MB/s  parallel=%d  ranged=%v",
		downloader.FormatBytes(res.Bytes), res.Duration, mbps, res.Parallel, res.RangeUsed)
}

func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if key != "" && os.Getenv(key) == "" {
			os.Setenv(key, val)
		}
	}
}
