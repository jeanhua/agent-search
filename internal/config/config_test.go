package config

import (
	"testing"
	"time"
)

func TestFromEnvDefaults(t *testing.T) {
	cfg := FromEnv()
	if cfg.ListenAddr != "127.0.0.1:8000" {
		t.Errorf("ListenAddr = %q", cfg.ListenAddr)
	}
	if cfg.SearxngURL != "http://127.0.0.1:8888" {
		t.Errorf("SearxngURL = %q", cfg.SearxngURL)
	}
	if cfg.AllowPrivate {
		t.Error("AllowPrivate should default to false")
	}
	if cfg.APIKey != "" {
		t.Error("APIKey should default to empty")
	}
	if cfg.MaxRedirects != 5 {
		t.Errorf("MaxRedirects = %d", cfg.MaxRedirects)
	}
	if cfg.MaxHTMLBytes != 4*1024*1024 {
		t.Errorf("MaxHTMLBytes = %d", cfg.MaxHTMLBytes)
	}
	if cfg.CacheSize != 512 {
		t.Errorf("CacheSize = %d", cfg.CacheSize)
	}
	if cfg.CacheTTL != time.Hour {
		t.Errorf("CacheTTL = %s", cfg.CacheTTL)
	}
}

func TestFromEnvOverrides(t *testing.T) {
	t.Setenv("LISTEN_ADDR", "0.0.0.0:9000")
	t.Setenv("SEARXNG_URL", "http://searxng:8080/") // 末尾斜杠应被去掉
	t.Setenv("ALLOW_PRIVATE", "true")
	t.Setenv("API_KEY", "secret")
	t.Setenv("MAX_HTML_BYTES", "1024")
	t.Setenv("CACHE_TTL", "30m")

	cfg := FromEnv()
	if cfg.ListenAddr != "0.0.0.0:9000" {
		t.Errorf("ListenAddr = %q", cfg.ListenAddr)
	}
	if cfg.SearxngURL != "http://searxng:8080" {
		t.Errorf("SearxngURL = %q", cfg.SearxngURL)
	}
	if !cfg.AllowPrivate {
		t.Error("AllowPrivate should be true")
	}
	if cfg.APIKey != "secret" {
		t.Errorf("APIKey = %q", cfg.APIKey)
	}
	if cfg.MaxHTMLBytes != 1024 {
		t.Errorf("MaxHTMLBytes = %d", cfg.MaxHTMLBytes)
	}
	if cfg.CacheTTL != 30*time.Minute {
		t.Errorf("CacheTTL = %s", cfg.CacheTTL)
	}
}

func TestFromEnvInvalidValuesFallBackToDefaults(t *testing.T) {
	t.Setenv("ALLOW_PRIVATE", "not-a-bool")
	t.Setenv("MAX_HTML_BYTES", "abc")
	t.Setenv("CACHE_TTL", "xyz")

	cfg := FromEnv()
	if cfg.AllowPrivate {
		t.Error("invalid bool should fall back to false")
	}
	if cfg.MaxHTMLBytes != 4*1024*1024 {
		t.Errorf("invalid int should fall back to default, got %d", cfg.MaxHTMLBytes)
	}
	if cfg.CacheTTL != time.Hour {
		t.Errorf("invalid duration should fall back to default, got %s", cfg.CacheTTL)
	}
}
