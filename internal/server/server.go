// Package server 提供 HTTP API：/healthz、/extract、/search。
package server

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"agent-search/internal/cache"
	"agent-search/internal/config"
	"agent-search/internal/extract"
	"agent-search/internal/fetch"
	"agent-search/internal/search"
)

// Server 聚合依赖并注册路由。
type Server struct {
	cfg       config.Config
	fetcher   *fetch.Fetcher
	cache     *cache.TTL
	searxng   *search.Client
	extractFn func(html []byte, rawURL string, opts extract.Options) (*extract.Result, error)
}

// New 创建 Server。
func New(cfg config.Config, fetcher *fetch.Fetcher, cache *cache.TTL, searxng *search.Client) *Server {
	s := &Server{
		cfg:       cfg,
		fetcher:   fetcher,
		cache:     cache,
		searxng:   searxng,
		extractFn: extract.Extract,
	}
	return s
}

// Handler 返回带中间件的完整路由。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("POST /extract", s.handleExtract)
	mux.HandleFunc("GET /search", s.handleSearch)
	mux.HandleFunc("GET /", s.handleIndex)

	var h http.Handler = mux
	h = recoverMiddleware(h)
	h = logMiddleware(h)
	if s.cfg.APIKey != "" {
		h = apiKeyMiddleware(h, s.cfg.APIKey)
	}
	return h
}

// ---- handlers ----

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		writeError(w, &fetch.HTTPError{Status: http.StatusNotFound, Message: "路径不存在"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name":        "agent-search",
		"description": "SearXNG 搜索 + trafilatura 网页阅读的轻量 AI 联网服务",
		"endpoints": map[string]string{
			"GET /healthz":  "健康检查",
			"POST /extract": "URL -> 干净 Markdown/TXT（SSRF 防护 + 缓存 + 截断）",
			"GET /search":   "搜索互联网（代理 SearXNG JSON API），参数 q、n、language",
		},
	})
}

type extractRequest struct {
	URL           string `json:"url"`
	MaxTokens     int    `json:"max_tokens"`
	OutputFormat  string `json:"output_format"`
	WithMetadata  *bool  `json:"with_metadata"`
	IncludeLinks  bool   `json:"include_links"`
	RespectRobots bool   `json:"respect_robots"`
}

func (s *Server) handleExtract(w http.ResponseWriter, r *http.Request) {
	var req extractRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, &fetch.HTTPError{Status: http.StatusUnprocessableEntity, Message: "请求体不是合法 JSON: " + err.Error()})
		return
	}
	if req.URL == "" {
		writeError(w, &fetch.HTTPError{Status: http.StatusUnprocessableEntity, Message: "url 为必填项"})
		return
	}
	if req.MaxTokens != 0 && (req.MaxTokens < 200 || req.MaxTokens > 100000) {
		writeError(w, &fetch.HTTPError{Status: http.StatusUnprocessableEntity, Message: "max_tokens 取值范围为 200 ~ 100000"})
		return
	}
	format := req.OutputFormat
	if format == "" {
		format = "markdown"
	}
	if format != "markdown" && format != "txt" {
		writeError(w, &fetch.HTTPError{Status: http.StatusUnprocessableEntity, Message: "output_format 仅支持 markdown 或 txt"})
		return
	}
	withMetadata := true
	if req.WithMetadata != nil {
		withMetadata = *req.WithMetadata
	}

	key := cacheKey(req.URL, format, withMetadata, req.IncludeLinks, req.MaxTokens)
	if content, ok := s.cache.Get(key); ok {
		writeJSON(w, http.StatusOK, map[string]any{"url": req.URL, "cached": true, "content": content})
		return
	}

	if req.RespectRobots {
		if err := s.fetcher.CheckRobots(r.Context(), req.URL); err != nil {
			writeError(w, err)
			return
		}
	}

	html, err := s.fetcher.Fetch(r.Context(), req.URL)
	if err != nil {
		writeError(w, err)
		return
	}

	res, err := s.extractFn(html, req.URL, extract.Options{
		OutputFormat: format,
		WithMetadata: withMetadata,
		IncludeLinks: req.IncludeLinks,
		MaxTokens:    req.MaxTokens,
	})
	if err != nil {
		writeError(w, &fetch.HTTPError{Status: http.StatusUnprocessableEntity, Message: err.Error()})
		return
	}

	s.cache.Set(key, res.Content)
	writeJSON(w, http.StatusOK, map[string]any{"url": req.URL, "cached": false, "content": res.Content})
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, &fetch.HTTPError{Status: http.StatusUnprocessableEntity, Message: "缺少查询参数 q"})
		return
	}
	n, _ := strconv.Atoi(r.URL.Query().Get("n"))
	if n == 0 {
		n = 5
	}
	if n < 1 || n > 50 {
		writeError(w, &fetch.HTTPError{Status: http.StatusUnprocessableEntity, Message: "n 取值范围为 1 ~ 50"})
		return
	}
	language := r.URL.Query().Get("language")

	results, err := s.searxng.Search(r.Context(), q, n, language)
	if err != nil {
		writeError(w, &fetch.HTTPError{Status: http.StatusBadGateway, Message: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"query": q, "results": results})
}

// ---- 工具 ----

func cacheKey(url, format string, withMetadata, includeLinks bool, maxTokens int) string {
	var b strings.Builder
	b.WriteString(url)
	b.WriteByte('|')
	b.WriteString(format)
	b.WriteByte('|')
	b.WriteString(strconv.FormatBool(withMetadata))
	b.WriteByte('|')
	b.WriteString(strconv.FormatBool(includeLinks))
	b.WriteByte('|')
	b.WriteString(strconv.Itoa(maxTokens))
	return b.String()
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, err error) {
	var he *fetch.HTTPError
	status := http.StatusInternalServerError
	msg := "内部错误: " + err.Error()
	if errors.As(err, &he) {
		status = he.Status
		msg = he.Message
	}
	slog.Error("request failed", "status", status, "error", msg)
	writeJSON(w, status, map[string]any{"error": map[string]any{"type": http.StatusText(status), "message": msg}})
}

// ---- 中间件 ----

func apiKeyMiddleware(next http.Handler, key string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		got := r.Header.Get("Authorization")
		got = strings.TrimPrefix(got, "Bearer ")
		if got == "" {
			got = r.Header.Get("X-API-Key")
		}
		if got != key {
			writeError(w, &fetch.HTTPError{Status: http.StatusUnauthorized, Message: "缺少或无效的 API Key"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		lw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(lw, r)
		slog.Info("http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", lw.status,
			"duration", time.Since(start).Round(time.Millisecond).String(),
			"remote", r.RemoteAddr,
		)
	})
}

func recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic recovered", "panic", rec, "path", r.URL.Path)
				writeError(w, errors.New("服务器内部错误"))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}
