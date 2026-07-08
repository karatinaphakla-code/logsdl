package downloader

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	DefaultParallel = 8
	UserAgent       = "logsdl/1.0 (go)"
	ChunkMinBytes   = 10 * 1024 * 1024
)

type ProgressFn func(downloaded, total int64, speedBps float64)

type Result struct {
	Path               string
	Bytes              int64
	Duration           time.Duration
	Parallel           int
	RangeUsed          bool
	ContentType        string
	ContentDisposition string
	FileName           string
	FinalURL           string
}

func DefaultHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DisableCompression:    true,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          64,
			MaxIdleConnsPerHost:   32,
			MaxConnsPerHost:       32,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   30 * time.Second,
			ResponseHeaderTimeout: 60 * time.Second,
			ExpectContinueTimeout: 5 * time.Second,
		},
		Timeout: 0,
	}
}

type meta struct {
	size         int64
	acceptRanges bool
	contentType  string
	disposition  string
	filename     string
	finalURL     string
}

// FetchMeta probes remote size and range support (HEAD, then ranged GET).
func FetchMeta(ctx context.Context, client *http.Client, url string) (size int64, acceptRanges bool, filename string, finalURL string, err error) {
	m, err := fetchMeta(ctx, client, url)
	if err != nil {
		return 0, false, "", "", err
	}
	return m.size, m.acceptRanges, m.filename, m.finalURL, nil
}

func fetchMeta(ctx context.Context, client *http.Client, url string) (meta, error) {
	headReq, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return meta{}, err
	}
	headReq.Header.Set("User-Agent", UserAgent)
	headReq.Header.Set("Accept-Encoding", "identity")
	headResp, err := client.Do(headReq)
	if err == nil {
		headResp.Body.Close()
		if headResp.StatusCode/100 == 2 {
			cdisp := headResp.Header.Get("Content-Disposition")
			return meta{
				size:         headResp.ContentLength,
				acceptRanges: headResp.Header.Get("Accept-Ranges") == "bytes",
				contentType:  headResp.Header.Get("Content-Type"),
				disposition:  cdisp,
				filename:     filenameFromDisposition(cdisp),
				finalURL:     headResp.Request.URL.String(),
			}, nil
		}
		if isCloudflareChallenge(headResp.Header) {
			return meta{}, errors.New("cloudflare challenge blocked this download; use the direct final .zip/.rar URL")
		}
	}
	probe, probeErr := probeMeta(ctx, client, url)
	if probeErr == nil {
		return probe, nil
	}
	if err != nil {
		return meta{}, fmt.Errorf("HEAD: %w", err)
	}
	if headResp.StatusCode == http.StatusMethodNotAllowed || headResp.StatusCode == http.StatusForbidden {
		return meta{finalURL: headResp.Request.URL.String()}, nil
	}
	return meta{}, fmt.Errorf("HEAD status %d (range probe: %v)", headResp.StatusCode, probeErr)
}

func probeMeta(ctx context.Context, client *http.Client, url string) (meta, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return meta{}, err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("Range", "bytes=0-0")

	resp, err := client.Do(req)
	if err != nil {
		return meta{}, err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))

	if isCloudflareChallenge(resp.Header) {
		return meta{}, errors.New("cloudflare challenge blocked this download; use the direct final .zip/.rar URL")
	}

	cdisp := resp.Header.Get("Content-Disposition")
	m := meta{
		contentType: resp.Header.Get("Content-Type"),
		disposition: cdisp,
		filename:    filenameFromDisposition(cdisp),
		finalURL:    resp.Request.URL.String(),
	}

	switch resp.StatusCode {
	case http.StatusPartialContent:
		size, err := parseContentRangeTotal(resp.Header.Get("Content-Range"))
		if err != nil {
			return meta{}, err
		}
		m.size = size
		m.acceptRanges = resp.Header.Get("Accept-Ranges") == "bytes" || resp.Header.Get("Content-Range") != ""
		return m, nil
	case http.StatusOK:
		m.size = resp.ContentLength
		return m, nil
	default:
		return meta{}, fmt.Errorf("probe status %d", resp.StatusCode)
	}
}

func parseContentRangeTotal(v string) (int64, error) {
	if v == "" {
		return -1, errors.New("missing Content-Range")
	}
	i := strings.LastIndex(v, "/")
	if i < 0 {
		return -1, fmt.Errorf("bad Content-Range: %q", v)
	}
	total := strings.TrimSpace(v[i+1:])
	if total == "*" {
		return -1, nil
	}
	return strconv.ParseInt(total, 10, 64)
}

// ParallelDownload saves url to dst using parallel ranged GET when supported.
func ParallelDownload(ctx context.Context, url, dst string, parallel int, maxBytes int64, prog ProgressFn) (*Result, error) {
	if parallel <= 0 {
		parallel = DefaultParallel
	}
	client := DefaultHTTPClient()

	m, err := fetchMeta(ctx, client, url)
	if err != nil {
		return nil, err
	}
	size := m.size
	acceptRanges := m.acceptRanges
	ctype := m.contentType
	cdisp := m.disposition
	filename := m.filename
	finalURL := m.finalURL
	if maxBytes > 0 && size > maxBytes {
		return nil, fmt.Errorf("file too large (%s, cap %s)", FormatBytes(size), FormatBytes(maxBytes))
	}

	if !acceptRanges || size <= 0 || size < ChunkMinBytes {
		return singleStream(ctx, client, url, dst, size, ctype, cdisp, maxBytes, prog)
	}

	chunkSize := size / int64(parallel)
	if chunkSize < ChunkMinBytes {
		parallel = int(size / ChunkMinBytes)
		if parallel < 1 {
			parallel = 1
		}
		chunkSize = size / int64(parallel)
	}

	f, err := os.Create(dst)
	if err != nil {
		return nil, err
	}
	if err := f.Truncate(size); err != nil {
		f.Close()
		return nil, err
	}

	var downloaded int64
	start := time.Now()

	stopProg := make(chan struct{})
	if prog != nil {
		go progLoop(stopProg, &downloaded, size, start, prog)
	}

	var wg sync.WaitGroup
	errCh := make(chan error, parallel)
	for i := 0; i < parallel; i++ {
		from := int64(i) * chunkSize
		to := from + chunkSize - 1
		if i == parallel-1 {
			to = size - 1
		}
		wg.Add(1)
		go func(idx int, from, to int64) {
			defer wg.Done()
			if err := downloadChunk(ctx, client, url, f, from, to, &downloaded); err != nil {
				errCh <- fmt.Errorf("chunk %d (%d-%d): %w", idx, from, to, err)
			}
		}(i, from, to)
	}
	wg.Wait()
	close(stopProg)
	close(errCh)
	for e := range errCh {
		f.Close()
		os.Remove(dst)
		return nil, e
	}
	if err := f.Close(); err != nil {
		return nil, err
	}

	return &Result{
		Path:               dst,
		Bytes:              size,
		Duration:           time.Since(start),
		Parallel:           parallel,
		RangeUsed:          true,
		ContentType:        ctype,
		ContentDisposition: cdisp,
		FileName:           filename,
		FinalURL:           finalURL,
	}, nil
}

func progLoop(stop chan struct{}, counter *int64, total int64, start time.Time, prog ProgressFn) {
	t := time.NewTicker(1 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			d := atomic.LoadInt64(counter)
			prog(d, total, float64(d)/time.Since(start).Seconds())
		}
	}
}

func isRetryableStatus(code int) bool {
	switch code {
	case 408, 429, 500, 502, 503, 504:
		return true
	}
	return false
}

func backoff(attempt int) time.Duration {
	d := time.Duration(500*(attempt+1)*(attempt+1)) * time.Millisecond
	if d > 10*time.Second {
		d = 10 * time.Second
	}
	return d
}

func downloadChunk(ctx context.Context, client *http.Client, url string, f *os.File, from, to int64, counter *int64) error {
	const maxAttempts = 8
	curStart := from
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if curStart > to {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		req.Header.Set("User-Agent", UserAgent)
		req.Header.Set("Accept-Encoding", "identity")
		req.Header.Set("Range", "bytes="+strconv.FormatInt(curStart, 10)+"-"+strconv.FormatInt(to, 10))
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(backoff(attempt))
			continue
		}
		if resp.StatusCode != 206 {
			io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
			resp.Body.Close()
			lastErr = fmt.Errorf("status %d (range not honored)", resp.StatusCode)
			if resp.StatusCode == 200 || isRetryableStatus(resp.StatusCode) {
				client.CloseIdleConnections()
				time.Sleep(backoff(attempt))
				continue
			}
			return lastErr
		}

		want := to - curStart + 1
		limited := io.LimitReader(resp.Body, want)
		offset := curStart
		buf := make([]byte, 256*1024)
		copyErr := func() error {
			for {
				if err := ctx.Err(); err != nil {
					return err
				}
				n, rerr := limited.Read(buf)
				if n > 0 {
					if _, werr := f.WriteAt(buf[:n], offset); werr != nil {
						return werr
					}
					offset += int64(n)
					atomic.AddInt64(counter, int64(n))
				}
				if rerr == io.EOF {
					return nil
				}
				if rerr != nil {
					return rerr
				}
			}
		}()
		resp.Body.Close()

		if copyErr == nil {
			return nil
		}
		if errors.Is(copyErr, context.Canceled) || errors.Is(copyErr, context.DeadlineExceeded) {
			return copyErr
		}
		lastErr = copyErr
		curStart = offset
		time.Sleep(backoff(attempt))
	}
	if lastErr != nil {
		return fmt.Errorf("max retries exceeded: %w", lastErr)
	}
	return errors.New("max retries exceeded")
}

func singleStream(ctx context.Context, client *http.Client, url, dst string, expected int64, ctype, cdisp string, maxBytes int64, prog ProgressFn) (*Result, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		if isCloudflareChallenge(resp.Header) {
			return nil, errors.New("cloudflare challenge blocked this download; use the direct final .zip/.rar URL")
		}
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	if ctype == "" {
		ctype = resp.Header.Get("Content-Type")
	}
	if cdisp == "" {
		cdisp = resp.Header.Get("Content-Disposition")
	}
	if expected <= 0 {
		expected = resp.ContentLength
	}
	if maxBytes > 0 && expected > maxBytes {
		return nil, fmt.Errorf("file too large (%s, cap %s)", FormatBytes(expected), FormatBytes(maxBytes))
	}
	f, err := os.Create(dst)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var downloaded int64
	start := time.Now()
	stopProg := make(chan struct{})
	if prog != nil {
		go progLoop(stopProg, &downloaded, expected, start, prog)
	}
	buf := make([]byte, 256*1024)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if maxBytes > 0 && downloaded+int64(n) > maxBytes {
				close(stopProg)
				os.Remove(dst)
				return nil, fmt.Errorf("file too large (cap %s)", FormatBytes(maxBytes))
			}
			if _, werr := f.Write(buf[:n]); werr != nil {
				close(stopProg)
				return nil, werr
			}
			atomic.AddInt64(&downloaded, int64(n))
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			close(stopProg)
			return nil, rerr
		}
	}
	close(stopProg)
	return &Result{
		Path:               dst,
		Bytes:              atomic.LoadInt64(&downloaded),
		Duration:           time.Since(start),
		Parallel:           1,
		RangeUsed:          false,
		ContentType:        ctype,
		ContentDisposition: cdisp,
		FileName:           filenameFromDisposition(cdisp),
		FinalURL:           resp.Request.URL.String(),
	}, nil
}

func isCloudflareChallenge(h http.Header) bool {
	return h.Get("cf-mitigated") == "challenge"
}

func filenameFromDisposition(v string) string {
	if v == "" {
		return ""
	}
	_, params, err := mime.ParseMediaType(v)
	if err != nil {
		return ""
	}
	return filepath.Base(params["filename"])
}

// DetectArchiveExt sniffs zip/rar magic from the file header.
func DetectArchiveExt(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	head := make([]byte, 8)
	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return "", err
	}
	head = head[:n]
	if bytes.HasPrefix(head, []byte("PK\x03\x04")) ||
		bytes.HasPrefix(head, []byte("PK\x05\x06")) ||
		bytes.HasPrefix(head, []byte("PK\x07\x08")) {
		return ".zip", nil
	}
	if bytes.HasPrefix(head, []byte("Rar!\x1a\x07\x00")) ||
		bytes.HasPrefix(head, []byte("Rar!\x1a\x07\x01\x00")) {
		return ".rar", nil
	}
	return "", fmt.Errorf("downloaded response is not a zip/rar archive")
}

// FormatBytes renders a human-readable size.
func FormatBytes(n int64) string {
	const u = 1024.0
	if n < int64(u) {
		return fmt.Sprintf("%dB", n)
	}
	v := float64(n)
	units := []string{"KB", "MB", "GB", "TB"}
	i := 0
	v /= u
	for v >= u && i < len(units)-1 {
		v /= u
		i++
	}
	return fmt.Sprintf("%.2f%s", v, units[i])
}

// NameFromURL returns a basename for a download URL.
func NameFromURL(rawURL string) string {
	name := filepath.Base(strings.Split(rawURL, "?")[0])
	if name == "" || name == "." || name == "/" {
		return "download"
	}
	return name
}

// SanitizeFilename strips path separators from a remote filename.
func SanitizeFilename(name string) string {
	base := filepath.Base(strings.TrimSpace(name))
	base = strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', 0:
			return -1
		default:
			return r
		}
	}, base)
	if base == "" || base == "." {
		return "download"
	}
	return base
}
