package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/amarnathcjd/gogram/telegram"

	"logsdl/downloader"
)

var urlRe = regexp.MustCompile(`https?://[^\s)]+`)

type tgSession struct {
	mu      sync.Mutex
	dir     string
	files   []string
	active  bool
}

var (
	sessions   = map[int64]*tgSession{}
	sessionsMu sync.Mutex
)

func runTelegram(root string, args []string) {
	fs := flag.NewFlagSet("tg", flag.ExitOnError)
	outDir := fs.String("o", defaultDownloadsDir(root), "output directory")
	parallel := fs.Int("p", downloader.DefaultParallel, "parallel URL connections")
	_ = fs.Parse(args)

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		log.Fatalf("mkdir: %v", err)
	}

	apiID, apiHash, token, err := telegramEnv()
	if err != nil {
		log.Fatalf("%v", err)
	}

	sessionPath := filepath.Join(*outDir, ".logsdl.session")
	client, err := telegram.NewClient(telegram.ClientConfig{
		AppID:   int32(apiID),
		AppHash: apiHash,
		Session: sessionPath,
	})
	if err != nil {
		log.Fatalf("telegram client: %v", err)
	}

	const maxConnect = 5
	var connErr error
	for attempt := 1; attempt <= maxConnect; attempt++ {
		if _, connErr = client.Conn(); connErr == nil {
			break
		}
		log.Printf("connect attempt %d/%d: %v", attempt, maxConnect, connErr)
		_ = client.Disconnect()
		_ = os.Remove(sessionPath)
		if attempt == maxConnect {
			log.Fatalf("telegram connect: %v", connErr)
		}
		time.Sleep(time.Duration(attempt) * 2 * time.Second)
	}

	if err := client.LoginBot(token); err != nil {
		log.Fatalf("bot login: %v", err)
	}
	me, err := client.GetMe()
	if err != nil {
		log.Fatalf("getMe: %v", err)
	}
	log.Printf("logsdl online: @%s", me.Username)
	log.Printf("saving files to: %s", mustAbs(*outDir))
	log.Printf("parallel=%d", *parallel)

	client.On(telegram.OnMessage, func(m *telegram.NewMessage) error {
		safeHandleTGMessage(client, m, *outDir, *parallel)
		return nil
	})

	client.Idle()
}

func telegramEnv() (apiID int, apiHash, token string, err error) {
	if v := strings.TrimSpace(os.Getenv("TELEGRAM_API_ID")); v != "" {
		apiID, _ = strconv.Atoi(v)
	} else if embedAPIID != "" {
		apiID, _ = strconv.Atoi(embedAPIID)
	}
	apiHash = strings.TrimSpace(os.Getenv("TELEGRAM_API_HASH"))
	if apiHash == "" {
		apiHash = embedAPIHash
	}
	token = strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN"))
	if token == "" {
		token = strings.TrimSpace(os.Getenv("BOT_TOKEN"))
	}
	if token == "" {
		token = embedBotToken
	}
	if apiID == 0 || apiHash == "" || token == "" {
		err = fmt.Errorf("telegram credentials missing (rebuild with embedded creds or set .env)")
	}
	return
}

func getSession(chatID int64, outRoot string) *tgSession {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	if s, ok := sessions[chatID]; ok {
		return s
	}
	s := &tgSession{dir: filepath.Join(outRoot, fmt.Sprintf("chat-%d", chatID))}
	sessions[chatID] = s
	return s
}

func sendOpts() *telegram.SendOptions {
	return &telegram.SendOptions{ParseMode: "markdown"}
}

func safeHandleTGMessage(client *telegram.Client, m *telegram.NewMessage, outRoot string, parallel int) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("panic handling message: %v", r)
			reply(client, m, fmt.Sprintf("internal error: %v", r))
		}
	}()
	handleTGMessage(client, m, outRoot, parallel)
}

func handleTGMessage(client *telegram.Client, m *telegram.NewMessage, outRoot string, parallel int) {
	chatID := m.ChatID()
	cmd := commandName(m)

	switch cmd {
	case "start", "help":
		reply(client, m, helpTG())
		return
	case "cancel":
		s := getSession(chatID, outRoot)
		s.mu.Lock()
		if s.active && s.dir != "" {
			os.RemoveAll(s.dir)
		}
		s.files = nil
		s.active = false
		s.mu.Unlock()
		reply(client, m, "batch cancelled")
		return
	case "done":
		listBatch(client, m, getSession(chatID, outRoot))
		return
	}

	if url := extractURL(m.Text()); url != "" {
		downloadURL(client, m, url, outRoot, parallel)
		return
	}

	if doc := m.Document(); doc != nil {
		downloadDoc(client, m, outRoot)
		return
	}
}

func helpTG() string {
	return strings.Join([]string{
		"logsdl — download-only bot",
		"",
		"• send any file or archive",
		"• paste a direct download URL",
		"• `/done` — list files saved in this batch",
		"• `/cancel` — delete current batch folder",
		"",
		"Files land on the PC running logsdl (not extracted).",
	}, "\n")
}

func extractURL(text string) string {
	if text == "" {
		return ""
	}
	return urlRe.FindString(text)
}

func commandName(m *telegram.NewMessage) string {
	cmd := strings.TrimSpace(m.GetCommand())
	cmd = strings.TrimPrefix(cmd, "/")
	if i := strings.IndexByte(cmd, '@'); i >= 0 {
		cmd = cmd[:i]
	}
	return strings.ToLower(cmd)
}

func reply(client *telegram.Client, m *telegram.NewMessage, text string) {
	if _, err := m.Reply(text, sendOpts()); err != nil {
		log.Printf("reply failed: %v", err)
	}
}

func batchDir(s *tgSession) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active {
		s.dir = filepath.Join(filepath.Dir(s.dir), fmt.Sprintf("batch-%s", time.Now().Format("20060102-150405")))
		os.MkdirAll(s.dir, 0o755)
		s.active = true
		s.files = nil
	}
	return s.dir
}

func trackFile(s *tgSession, path string) {
	s.mu.Lock()
	s.files = append(s.files, path)
	s.mu.Unlock()
}

func listBatch(client *telegram.Client, m *telegram.NewMessage, s *tgSession) {
	s.mu.Lock()
	files := append([]string(nil), s.files...)
	dir := s.dir
	active := s.active
	s.mu.Unlock()

	if !active || len(files) == 0 {
		reply(client, m, "no files in current batch — send archives or URLs first")
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "saved %d file(s) in:\n`%s`\n\n", len(files), dir)
	for _, f := range files {
		info, err := os.Stat(f)
		if err != nil {
			fmt.Fprintf(&b, "• `%s`\n", filepath.Base(f))
			continue
		}
		fmt.Fprintf(&b, "• `%s` (%s)\n", filepath.Base(f), downloader.FormatBytes(info.Size()))
	}
	reply(client, m, b.String())
}

func downloadURL(client *telegram.Client, m *telegram.NewMessage, rawURL string, outRoot string, parallel int) {
	s := getSession(m.ChatID(), outRoot)
	dir := batchDir(s)
	name := downloader.SanitizeFilename(downloader.NameFromURL(rawURL))
	dst := filepath.Join(dir, name)

	status, _ := m.Reply(fmt.Sprintf("⬇️ downloading URL…\n`%s`", name), sendOpts())

	ctx := context.Background()

	prog := func(d, total int64, bps float64) {
		if status == nil {
			return
		}
		pct := 0.0
		if total > 0 {
			pct = float64(d) / float64(total) * 100
		}
		text := fmt.Sprintf("⬇️ `%s`\n%s / %s · %.1f%% · %s/s",
			name, downloader.FormatBytes(d), downloader.FormatBytes(total), pct, downloader.FormatBytes(int64(bps)))
		client.EditMessage(m.ChatID(), status.ID, text, sendOpts())
	}

	res, err := downloader.ParallelDownload(ctx, rawURL, dst, parallel, 0, prog)
	if err != nil {
		if status != nil {
			client.EditMessage(m.ChatID(), status.ID, fmt.Sprintf("❌ %v", err), sendOpts())
		}
		return
	}

	if ext, err := downloader.DetectArchiveExt(res.Path); err == nil && filepath.Ext(res.Path) != ext {
		final := strings.TrimSuffix(res.Path, filepath.Ext(res.Path)) + ext
		if err := os.Rename(res.Path, final); err == nil {
			res.Path = final
		}
	}

	trackFile(s, res.Path)
	if status != nil {
		client.EditMessage(m.ChatID(), status.ID, fmt.Sprintf("✅ saved `%s`\n%s · %s",
			filepath.Base(res.Path), downloader.FormatBytes(res.Bytes), dir), sendOpts())
	}
}

func downloadDoc(client *telegram.Client, m *telegram.NewMessage, outRoot string) {
	doc := m.Document()
	if doc == nil {
		return
	}
	name := docFileName(doc)

	s := getSession(m.ChatID(), outRoot)
	dir := batchDir(s)
	dstName := downloader.SanitizeFilename(name)
	dst := filepath.Join(dir, dstName)

	status, _ := m.Reply(fmt.Sprintf("⬇️ downloading `%s`…", dstName), sendOpts())

	out, err := os.Create(dst)
	if err != nil {
		reply(client, m, fmt.Sprintf("❌ %v", err))
		return
	}

	var lastEdit time.Time
	_, err = m.Download(&telegram.DownloadOptions{
		Buffer:           out,
		ProgressInterval: 2,
		ProgressCallback: func(pi *telegram.ProgressInfo) {
			if status == nil || time.Since(lastEdit) < 2*time.Second {
				return
			}
			lastEdit = time.Now()
			pct := 0.0
			if pi.TotalSize > 0 {
				pct = float64(pi.Current) / float64(pi.TotalSize) * 100
			}
			client.EditMessage(m.ChatID(), status.ID, fmt.Sprintf("⬇️ `%s`\n%s / %s · %.1f%%",
				dstName, downloader.FormatBytes(pi.Current), downloader.FormatBytes(pi.TotalSize), pct), sendOpts())
		},
	})
	out.Close()
	if err != nil {
		os.Remove(dst)
		if status != nil {
			client.EditMessage(m.ChatID(), status.ID, fmt.Sprintf("❌ %v", err), sendOpts())
		}
		return
	}

	trackFile(s, dst)
	if status != nil {
		client.EditMessage(m.ChatID(), status.ID, fmt.Sprintf("✅ saved `%s`\n%s · %s",
			dstName, downloader.FormatBytes(doc.Size), dir), sendOpts())
	}
}

func docFileName(doc *telegram.DocumentObj) string {
	if doc == nil {
		return "download"
	}
	for _, attr := range doc.Attributes {
		if fn, ok := attr.(*telegram.DocumentAttributeFilename); ok && fn.FileName != "" {
			return fn.FileName
		}
	}
	return "download"
}

func mustAbs(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}
