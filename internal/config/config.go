// Package config 从环境变量加载服务配置。
package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Config 是 agent-search 服务的全部配置项。
type Config struct {
	// ListenAddr 是 HTTP 监听地址，默认 127.0.0.1:8000。
	ListenAddr string
	// SearxngURL 是 SearXNG 实例地址，默认 http://127.0.0.1:8888。
	SearxngURL string
	// AllowPrivate 为 true 时允许访问内网/回环地址（SSRF 防护开关），默认 false。
	AllowPrivate bool
	// APIKey 非空时启用 Bearer Token 鉴权。
	APIKey string

	// MaxRedirects 手动跟随重定向上限。
	MaxRedirects int
	// MaxHTMLBytes 单页最大下载字节数。
	MaxHTMLBytes int64
	// ConnectTimeout 建连超时。
	ConnectTimeout time.Duration
	// ReadTimeout 读取超时。
	ReadTimeout time.Duration
	// RobotsTimeout robots.txt 获取超时。
	RobotsTimeout time.Duration
	// UserAgent 抓取网页时使用的 UA。
	UserAgent string

	// CacheSize 内存缓存条目上限。
	CacheSize int
	// CacheTTL 缓存有效期。
	CacheTTL time.Duration
}

// FromEnv 从环境变量构建配置，未设置时使用默认值（匹配方案文档里的轻量私有部署）。
func FromEnv() Config {
	cfg := Config{
		ListenAddr:     getenv("LISTEN_ADDR", "127.0.0.1:8000"),
		SearxngURL:     strings.TrimRight(getenv("SEARXNG_URL", "http://127.0.0.1:8888"), "/"),
		AllowPrivate:   getbool("ALLOW_PRIVATE", false),
		APIKey:         os.Getenv("API_KEY"),
		MaxRedirects:   getint("MAX_REDIRECTS", 5),
		MaxHTMLBytes:   getint64("MAX_HTML_BYTES", 4*1024*1024),
		ConnectTimeout: getdur("CONNECT_TIMEOUT", 5*time.Second),
		ReadTimeout:    getdur("READ_TIMEOUT", 20*time.Second),
		RobotsTimeout:  getdur("ROBOTS_TIMEOUT", 5*time.Second),
		UserAgent: getenv("USER_AGENT",
			"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 "+
				"(KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36 agent-search/0.1"),
		CacheSize: getint("CACHE_SIZE", 512),
		CacheTTL:  getdur("CACHE_TTL", time.Hour),
	}
	return cfg
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getbool(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

func getint(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func getint64(key string, def int64) int64 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return def
	}
	return n
}

func getdur(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}
