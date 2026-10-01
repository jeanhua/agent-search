// Package search 封装 SearXNG JSON API 调用，把结果规范化为 Agent 友好结构。
package search

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Result 是规范化的搜索结果。
type Result struct {
	Title         string  `json:"title"`
	URL           string  `json:"url"`
	Content       string  `json:"content,omitempty"`
	Engine        string  `json:"engine,omitempty"`
	PublishedDate string  `json:"publishedDate,omitempty"`
	Score         float64 `json:"score,omitempty"`
	Category      string  `json:"category,omitempty"`
}

// Client 是 SearXNG 客户端。
type Client struct {
	BaseURL string
	HTTP    *http.Client
	Timeout time.Duration
}

// NewClient 创建 SearXNG 客户端，baseURL 例如 http://127.0.0.1:8888。
func NewClient(baseURL string, timeout time.Duration) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTP:    &http.Client{Timeout: timeout},
		Timeout: timeout,
	}
}

// Search 调用 SearXNG 的 JSON API。n<=0 时使用服务端默认数量。
func (c *Client) Search(ctx context.Context, query string, n int, language string) ([]Result, error) {
	q := url.Values{}
	q.Set("q", query)
	q.Set("format", "json")
	if language != "" {
		q.Set("language", language)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/search?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "agent-search/0.1 (+http://127.0.0.1:8000)")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("SearXNG 请求失败: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("读取 SearXNG 响应失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("SearXNG 返回 %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload struct {
		Query               string   `json:"query"`
		Results             []Result `json:"results"`
		UnresponsiveEngines []any    `json:"unresponsive_engines"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("解析 SearXNG 响应失败: %w", err)
	}
	if n > 0 && len(payload.Results) > n {
		payload.Results = payload.Results[:n]
	}
	return payload.Results, nil
}
