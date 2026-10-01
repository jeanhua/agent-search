// Package fetch 实现带 SSRF 防护的网页抓取与 robots.txt 检查。
package fetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/temoto/robotstxt"
	"golang.org/x/net/html/charset"
)

// MaxBody 与 HTTPError 供外部识别错误类型。
const MaxBody = int64(4 * 1024 * 1024)

// HTTPError 表示带 HTTP 状态码的业务错误，由 handler 映射为 JSON 响应。
type HTTPError struct {
	Status  int
	Message string
}

func (e *HTTPError) Error() string { return e.Message }

// ErrNotGlobalIP 表示目标解析到了非公网 IP（SSRF 拦截）。
var ErrNotGlobalIP = errors.New("禁止访问非公网地址")

// Fetcher 负责按安全策略抓取 URL。
type Fetcher struct {
	Client        *http.Client
	UserAgent     string
	MaxRedirects  int
	MaxHTMLBytes  int64
	AllowPrivate  bool
	RobotsTimeout time.Duration
}

// New 创建 Fetcher。transport 可为 nil。
func New(cfg Config, transport http.RoundTripper) *Fetcher {
	if transport == nil {
		transport = &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: cfg.ConnectTimeout}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          32,
			IdleConnTimeout:       60 * time.Second,
			TLSHandshakeTimeout:   cfg.ConnectTimeout,
			ExpectContinueTimeout: time.Second,
		}
	}
	return &Fetcher{
		Client:        &http.Client{Transport: transport, Timeout: cfg.ConnectTimeout + cfg.ReadTimeout + 5*time.Second},
		UserAgent:     cfg.UserAgent,
		MaxRedirects:  cfg.MaxRedirects,
		MaxHTMLBytes:  cfg.MaxHTMLBytes,
		AllowPrivate:  cfg.AllowPrivate,
		RobotsTimeout: cfg.RobotsTimeout,
	}
}

// Config 是 New 的入参。
type Config struct {
	UserAgent      string
	MaxRedirects   int
	MaxHTMLBytes   int64
	AllowPrivate   bool
	ConnectTimeout time.Duration
	ReadTimeout    time.Duration
	RobotsTimeout  time.Duration
}

// ValidateURL 校验 URL 仅允许 http/https 且带主机名。
func ValidateURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, &HTTPError{Status: 422, Message: "URL 解析失败: " + err.Error()}
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, &HTTPError{Status: 422, Message: "仅支持 http/https URL"}
	}
	if u.Hostname() == "" {
		return nil, &HTTPError{Status: 422, Message: "URL 缺少主机名"}
	}
	return u, nil
}

// Fetch 抓取 rawURL 并返回解码为 UTF-8 的 HTML 字节。
// 手动跟随重定向，每一跳都重新做 SSRF 校验。
func (f *Fetcher) Fetch(ctx context.Context, rawURL string) ([]byte, error) {
	u, err := ValidateURL(rawURL)
	if err != nil {
		return nil, err
	}

	current := u
	for hop := 0; hop <= f.MaxRedirects; hop++ {
		if err := f.checkHost(ctx, current.Hostname()); err != nil {
			return nil, err
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, current.String(), nil)
		if err != nil {
			return nil, &HTTPError{Status: 422, Message: "请求构造失败: " + err.Error()}
		}
		req.Header.Set("User-Agent", f.UserAgent)
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
		req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")

		resp, err := f.Client.Do(req)
		if err != nil {
			return nil, &HTTPError{Status: 502, Message: "目标站点请求失败: " + err.Error()}
		}

		switch {
		case resp.StatusCode >= 300 && resp.StatusCode < 400:
			loc := resp.Header.Get("Location")
			resp.Body.Close()
			if loc == "" {
				return nil, &HTTPError{Status: 502, Message: "重定向缺少 Location 头"}
			}
			next, err := current.Parse(loc)
			if err != nil || next.Hostname() == "" {
				return nil, &HTTPError{Status: 502, Message: "非法重定向地址: " + loc}
			}
			current = next
			continue
		case resp.StatusCode >= 400:
			resp.Body.Close()
			return nil, &HTTPError{Status: 502, Message: fmt.Sprintf("目标站点返回 %d", resp.StatusCode)}
		}

		ct := resp.Header.Get("Content-Type")
		reader, err := charset.NewReader(resp.Body, ct)
		if err != nil {
			resp.Body.Close()
			return nil, &HTTPError{Status: 502, Message: "响应编码识别失败: " + err.Error()}
		}
		body, err := io.ReadAll(io.LimitReader(reader, f.MaxHTMLBytes+1))
		resp.Body.Close()
		if err != nil {
			return nil, &HTTPError{Status: 502, Message: "读取响应失败: " + err.Error()}
		}
		if int64(len(body)) > f.MaxHTMLBytes {
			return nil, &HTTPError{Status: 413, Message: "目标页面过大"}
		}
		return body, nil
	}

	return nil, &HTTPError{Status: 502, Message: fmt.Sprintf("重定向次数超过上限 %d", f.MaxRedirects)}
}

// CheckRobots 按 robots.txt 判断是否允许抓取 rawURL；拉取失败时默认放行。
func (f *Fetcher) CheckRobots(ctx context.Context, rawURL string) error {
	u, err := ValidateURL(rawURL)
	if err != nil {
		return err
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return nil
	}

	rctx, cancel := context.WithTimeout(ctx, f.RobotsTimeout)
	defer cancel()

	robotsURL := u.Scheme + "://" + u.Host + "/robots.txt"
	req, err := http.NewRequestWithContext(rctx, http.MethodGet, robotsURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", f.UserAgent)

	resp, err := f.Client.Do(req)
	if err != nil {
		return nil // robots.txt 不可达时放行
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil // 没有 robots.txt 时放行
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return nil
	}

	robots, err := robotstxt.FromStatusAndBytes(resp.StatusCode, body)
	if err != nil {
		return nil
	}
	if !robots.TestAgent(u.RequestURI(), f.UserAgent) && !robots.TestAgent(u.RequestURI(), "*") {
		return &HTTPError{Status: 403, Message: "robots.txt 禁止抓取"}
	}
	return nil
}

// checkHost 解析主机并拒绝一切非公网 IP（除非显式放行）。
func (f *Fetcher) checkHost(ctx context.Context, host string) error {
	addrs, err := net.DefaultResolver.LookupHost(ctx, host)
	if err != nil {
		return &HTTPError{Status: 422, Message: fmt.Sprintf("无法解析域名: %s", host)}
	}
	if len(addrs) == 0 {
		return &HTTPError{Status: 422, Message: fmt.Sprintf("无法解析域名: %s", host)}
	}
	for _, a := range addrs {
		ip := net.ParseIP(a)
		if ip == nil {
			continue
		}
		if !f.AllowPrivate && !isGlobalIP(ip) {
			return &HTTPError{Status: 422, Message: fmt.Sprintf("禁止访问非公网地址: %s -> %s", host, ip)}
		}
	}
	return nil
}

// isGlobalIP 近似 Python ipaddress.IP.is_global：排除私网、回环、链路本地、
// 组播、未指定等非公网地址。
func isGlobalIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip4 := ip.To4(); ip4 != nil {
		return isGlobalIPv4(ip4)
	}
	ip6 := ip.To16()
	if ip6 == nil {
		return false
	}
	return !ip6.IsPrivate() &&
		!ip6.IsLoopback() &&
		!ip6.IsLinkLocalUnicast() &&
		!ip6.IsLinkLocalMulticast() &&
		!ip6.IsMulticast() &&
		!ip6.IsUnspecified()
}

func isGlobalIPv4(ip net.IP) bool {
	if ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() ||
		ip.IsMulticast() || ip.IsUnspecified() {
		return false
	}
	// 补充排除文档/保留/未来使用等特殊段（与 Python is_global 对齐）。
	first := ip[0]
	switch {
	case first == 0, first == 127, first >= 224: // 0.0.0.0/8, 127/8, 组播与保留
		return false
	case first == 100 && ip[1] >= 64 && ip[1] <= 127: // 100.64.0.0/10 CGNAT
		return false
	case first == 169 && ip[1] == 254: // 169.254.0.0/16
		return false
	case first == 192 && ip[1] == 0 && ip[2] == 0: // 192.0.0.0/24
		return false
	case first == 192 && ip[1] == 0 && ip[2] == 2: // 192.0.2.0/24 TEST-NET-1
		return false
	case first == 198 && ip[1] == 18: // 198.18.0.0/15 基准测试
		return false
	case first == 198 && ip[1] == 51 && ip[2] == 100: // 198.51.100.0/24 TEST-NET-2
		return false
	case first == 203 && ip[1] == 0 && ip[2] == 113: // 203.0.113.0/24 TEST-NET-3
		return false
	case first == 192 && ip[1] == 88 && ip[2] == 99: // 192.88.99.0/24 已废弃 6to4
		return false
	case first == 255: // 255.255.255.255
		return false
	}
	return true
}

// IsPrivateTarget 供测试用：判断 host 是否为私网目标。
func IsPrivateTarget(ctx context.Context, host string, allowPrivate bool) bool {
	addrs, err := net.DefaultResolver.LookupHost(ctx, host)
	if err != nil {
		return true
	}
	for _, a := range addrs {
		ip := net.ParseIP(a)
		if ip != nil && !allowPrivate && !isGlobalIP(ip) {
			return true
		}
	}
	return false
}

// NormalizeHost 去掉端口与 IPv6 括号，用于日志。
func NormalizeHost(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return strings.Trim(host, "[]")
}
