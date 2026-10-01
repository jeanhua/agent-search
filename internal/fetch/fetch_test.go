package fetch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func newTestFetcher(allowPrivate bool, maxBytes int64) *Fetcher {
	return New(Config{
		UserAgent:      "test-agent/1.0",
		MaxRedirects:   5,
		MaxHTMLBytes:   maxBytes,
		AllowPrivate:   allowPrivate,
		ConnectTimeout: 3 * time.Second,
		ReadTimeout:    5 * time.Second,
		RobotsTimeout:  3 * time.Second,
	}, nil)
}

func TestFetchBasic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<html><body><p>hello</p></body></html>"))
	}))
	defer srv.Close()

	body, err := newTestFetcher(true, 1<<20).Fetch(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("fetch error: %v", err)
	}
	if !strings.Contains(string(body), "hello") {
		t.Fatalf("unexpected body: %s", body)
	}
}

func TestFetchBlockedBySSRF(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	_, err := newTestFetcher(false, 1<<20).Fetch(context.Background(), srv.URL)
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != 422 {
		t.Fatalf("expected 422 SSRF error, got %v", err)
	}
	if !strings.Contains(he.Message, "非公网") {
		t.Fatalf("unexpected message: %s", he.Message)
	}
}

func TestFetchBlocksNonHTTP(t *testing.T) {
	_, err := newTestFetcher(true, 1<<20).Fetch(context.Background(), "file:///etc/passwd")
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != 422 {
		t.Fatalf("expected 422, got %v", err)
	}
}

func TestFetchFollowsRedirects(t *testing.T) {
	var target *httptest.Server
	target = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("target content"))
	}))
	defer target.Close()

	src := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer src.Close()

	body, err := newTestFetcher(true, 1<<20).Fetch(context.Background(), src.URL)
	if err != nil {
		t.Fatalf("fetch error: %v", err)
	}
	if string(body) != "target content" {
		t.Fatalf("unexpected body: %s", body)
	}
}

func TestFetchRedirectLoopLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, r.URL.String(), http.StatusFound)
	}))
	defer srv.Close()

	_, err := newTestFetcher(true, 1<<20).Fetch(context.Background(), srv.URL)
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != 502 {
		t.Fatalf("expected 502 redirect limit, got %v", err)
	}
}

func TestFetchSizeLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(make([]byte, 4096))
	}))
	defer srv.Close()

	_, err := newTestFetcher(true, 1024).Fetch(context.Background(), srv.URL)
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != 413 {
		t.Fatalf("expected 413, got %v", err)
	}
}

func TestFetchCharsetDecode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=gbk")
		body, _ := simplifiedchinese.GBK.NewEncoder().Bytes([]byte("<html><body><p>你好世界</p></body></html>"))
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	body, err := newTestFetcher(true, 1<<20).Fetch(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("fetch error: %v", err)
	}
	if !strings.Contains(string(body), "你好世界") {
		t.Fatalf("expected gbk decoded, got %q", body)
	}
}

func TestValidateURL(t *testing.T) {
	for _, raw := range []string{"", "ftp://example.com/x", "http://", "javascript:alert(1)"} {
		if _, err := ValidateURL(raw); err == nil {
			t.Errorf("expected error for %q", raw)
		}
	}
}

func TestIsPrivateTarget(t *testing.T) {
	ctx := context.Background()
	if !IsPrivateTarget(ctx, "127.0.0.1", false) {
		t.Fatal("expected 127.0.0.1 private")
	}
	if IsPrivateTarget(ctx, "1.1.1.1", false) {
		t.Fatal("expected 1.1.1.1 public")
	}
}
