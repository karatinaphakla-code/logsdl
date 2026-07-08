package downloader

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestParseContentRangeTotal(t *testing.T) {
	cases := []struct {
		in   string
		want int64
		err  bool
	}{
		{"bytes 0-0/1958628046", 1958628046, false},
		{"bytes 0-1023/999", 999, false},
		{"bytes 0-0/*", -1, false},
		{"", 0, true},
		{"garbage", 0, true},
	}
	for _, c := range cases {
		got, err := parseContentRangeTotal(c.in)
		if c.err {
			if err == nil {
				t.Errorf("parseContentRangeTotal(%q): want error", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseContentRangeTotal(%q): %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("parseContentRangeTotal(%q): got %d want %d", c.in, got, c.want)
		}
	}
}

func TestFetchMeta_HEAD404_RangeProbeOK(t *testing.T) {
	const total = int64(50 * 1024 * 1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodHead:
			http.NotFound(w, r)
		case http.MethodGet:
			if r.Header.Get("Range") != "bytes=0-0" {
				http.Error(w, "expected range probe", 400)
				return
			}
			w.Header().Set("Accept-Ranges", "bytes")
			w.Header().Set("Content-Disposition", `attachment; filename="test.rar"`)
			w.Header().Set("Content-Type", "application/x-rar-compressed")
			w.Header().Set("Content-Range", "bytes 0-0/"+strconv.FormatInt(total, 10))
			w.WriteHeader(http.StatusPartialContent)
			w.Write([]byte{0x52, 0x61, 0x72, 0x21})
		default:
			http.Error(w, "method", 405)
		}
	}))
	defer srv.Close()

	size, acceptRanges, filename, _, err := FetchMeta(context.Background(), DefaultHTTPClient(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if size != total {
		t.Errorf("size: got %d want %d", size, total)
	}
	if !acceptRanges {
		t.Error("acceptRanges: want true")
	}
	if filename != "test.rar" {
		t.Errorf("filename: got %q", filename)
	}
}
