package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agent-search/internal/cache"
	"agent-search/internal/config"
	"agent-search/internal/fetch"
	"agent-search/internal/search"
)

const testArticle = `<!DOCTYPE html>
<html lang="zh-CN">
<head><meta charset="utf-8"><title>测试文章</title></head>
<body>
  <nav><a href="/">首页</a></nav>
  <article>
    <h1>测试文章</h1>
    <p>第一段正文内容。</p>
    <p>第二段正文内容。</p>
  </article>
</body>
</html>`

func newTestServer(t *testing.T, mutate func(*config.Config)) (*httptest.Server, *config.Config) {
	t.Helper()
	cfg := config.FromEnv()
	cfg.ListenAddr = "127.0.0.1:0"
	cfg.AllowPrivate = true
	if mutate != nil {
		mutate(&cfg)
	}
	fetcher := fetch.New(fetch.Config{
		UserAgent:      cfg.UserAgent,
		MaxRedirects:   cfg.MaxRedirects,
		MaxHTMLBytes:   cfg.MaxHTMLBytes,
		AllowPrivate:   cfg.AllowPrivate,
		ConnectTimeout: cfg.ConnectTimeout,
		ReadTimeout:    cfg.ReadTimeout,
		RobotsTimeout:  cfg.RobotsTimeout,
	}, nil)
	c := cache.New(cfg.CacheSize, cfg.CacheTTL)
	searxng := search.NewClient(cfg.SearxngURL, cfg.ReadTimeout)
	s := New(cfg, fetcher, c, searxng)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts, &cfg
}

func doJSON(t *testing.T, method, url string, body any, apiKey string) (*http.Response, []byte) {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, r)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp, data
}

func TestHealthz(t *testing.T) {
	ts, _ := newTestServer(t, nil)
	resp, body := doJSON(t, http.MethodGet, ts.URL+"/healthz", nil, "")
	if resp.StatusCode != 200 || !strings.Contains(string(body), `"ok":true`) {
		t.Fatalf("unexpected healthz: %d %s", resp.StatusCode, body)
	}
}

func TestExtractEndToEndAndCache(t *testing.T) {
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(testArticle))
	}))
	defer page.Close()

	ts, _ := newTestServer(t, nil)
	req := map[string]any{"url": page.URL, "max_tokens": 2000}
	resp, body := doJSON(t, http.MethodPost, ts.URL+"/extract", req, "")
	if resp.StatusCode != 200 {
		t.Fatalf("extract failed: %d %s", resp.StatusCode, body)
	}
	var out struct {
		Cached  bool   `json:"cached"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	if out.Cached {
		t.Fatal("first call should not be cached")
	}
	if !strings.Contains(out.Content, "第一段正文内容") {
		t.Fatalf("missing content: %s", out.Content)
	}
	if !strings.Contains(out.Content, "URL Source") {
		t.Fatalf("missing metadata: %s", out.Content)
	}

	resp2, body2 := doJSON(t, http.MethodPost, ts.URL+"/extract", req, "")
	if resp2.StatusCode != 200 {
		t.Fatalf("second extract failed: %d %s", resp2.StatusCode, body2)
	}
	if err := json.Unmarshal(body2, &out); err != nil {
		t.Fatal(err)
	}
	if !out.Cached {
		t.Fatal("second call should hit cache")
	}
}

func TestExtractValidation(t *testing.T) {
	ts, _ := newTestServer(t, nil)
	cases := []map[string]any{
		{"url": ""},
		{"url": "https://example.com/x", "max_tokens": 10},
		{"url": "https://example.com/x", "max_tokens": 200000},
		{"url": "https://example.com/x", "output_format": "xml"},
	}
	for _, body := range cases {
		resp, _ := doJSON(t, http.MethodPost, ts.URL+"/extract", body, "")
		if resp.StatusCode != 422 {
			t.Errorf("body %v: expected 422, got %d", body, resp.StatusCode)
		}
	}
}

func TestExtractSSRF(t *testing.T) {
	ts, _ := newTestServer(t, func(c *config.Config) { c.AllowPrivate = false })
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(testArticle))
	}))
	defer page.Close()

	resp, body := doJSON(t, http.MethodPost, ts.URL+"/extract", map[string]any{"url": page.URL}, "")
	if resp.StatusCode != 422 {
		t.Fatalf("expected 422, got %d: %s", resp.StatusCode, body)
	}
}

func TestSearchProxy(t *testing.T) {
	searxng := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("format") != "json" {
			t.Errorf("missing format=json")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"query":"go","results":[
			{"title":"Go","url":"https://go.dev/","content":"The Go Programming Language","engine":"google","publishedDate":"2026-08-01 10:00:00"}
		]}`))
	}))
	defer searxng.Close()

	ts, _ := newTestServer(t, func(c *config.Config) { c.SearxngURL = searxng.URL })
	resp, body := doJSON(t, http.MethodGet, ts.URL+"/search?q=go&n=5", nil, "")
	if resp.StatusCode != 200 {
		t.Fatalf("search failed: %d %s", resp.StatusCode, body)
	}
	var out struct {
		Query   string `json:"query"`
		Results []struct {
			Title string `json:"title"`
			URL   string `json:"url"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	if out.Query != "go" || len(out.Results) != 1 || out.Results[0].URL != "https://go.dev/" {
		t.Fatalf("unexpected search response: %s", body)
	}
}

func TestAPIKey(t *testing.T) {
	searxng := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"query":"x","results":[]}`))
	}))
	defer searxng.Close()

	ts, _ := newTestServer(t, func(c *config.Config) { c.APIKey = "secret"; c.SearxngURL = searxng.URL })
	resp, _ := doJSON(t, http.MethodGet, ts.URL+"/healthz", nil, "")
	if resp.StatusCode != 200 {
		t.Fatalf("healthz should be public, got %d", resp.StatusCode)
	}
	resp, _ = doJSON(t, http.MethodGet, ts.URL+"/search?q=x", nil, "")
	if resp.StatusCode != 401 {
		t.Fatalf("expected 401 without key, got %d", resp.StatusCode)
	}
	resp, _ = doJSON(t, http.MethodGet, ts.URL+"/search?q=x", nil, "secret")
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200 with key, got %d", resp.StatusCode)
	}
}

func TestExtractRejectsNonJSONBody(t *testing.T) {
	ts, _ := newTestServer(t, nil)
	resp, err := http.Post(ts.URL+"/extract", "application/json", strings.NewReader("not json"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 422 {
		t.Fatalf("expected 422, got %d", resp.StatusCode)
	}
}
