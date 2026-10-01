// Package extract 基于 go-trafilatura（Python trafilatura 的 Go 移植）做正文提取，
// 并支持 Markdown/TXT 输出、元信息头与按 token 截断。
package extract

import (
	"bytes"
	"fmt"
	"net/url"
	"strings"
	"time"

	md "github.com/JohannesKaufmann/html-to-markdown"
	trafilatura "github.com/markusmobius/go-trafilatura"
	"golang.org/x/net/html"
)

// Options 是提取参数（对应方案文档 /extract 接口）。
type Options struct {
	OutputFormat string // markdown | txt
	WithMetadata bool
	IncludeLinks bool
	MaxTokens    int // <=0 表示不截断
}

// Result 是提取结果。
type Result struct {
	Content string
	Title   string
	Date    string
	Author  string
}

// Extract 从 HTML 中提取正文并转换为目标格式。
func Extract(htmlBytes []byte, rawURL string, opts Options) (*Result, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("解析 URL 失败: %w", err)
	}

	cfg := trafilatura.DefaultConfig()
	// 默认 MinExtractedSize=250 会对小页面触发整页兜底（baseline），
	// 导致链接被剥掉、正文混入导航。置 0 让主提取结果保持干净。
	cfg.MinExtractedSize = 0
	cfg.MinOutputSize = 0

	extracted, err := trafilatura.Extract(bytes.NewReader(htmlBytes), trafilatura.Options{
		Config:          cfg,
		OriginalURL:     parsedURL,
		EnableFallback:  true,
		ExcludeComments: true,
		ExcludeTables:   false,
		IncludeImages:   false,
		IncludeLinks:    opts.IncludeLinks,
		Deduplicate:     true,
		MaxTreeSize:     1 << 20,
	})
	if err != nil {
		return nil, err
	}
	if extracted == nil || strings.TrimSpace(extracted.ContentText) == "" {
		return nil, fmt.Errorf("未能从页面提取到正文（可能被反爬或页面为空）")
	}

	res := &Result{
		Title:  strings.TrimSpace(extracted.Metadata.Title),
		Date:   formatDate(extracted.Metadata.Date),
		Author: strings.TrimSpace(extracted.Metadata.Author),
	}

	switch opts.OutputFormat {
	case "txt":
		res.Content = extracted.ContentText
	default:
		contentHTML := &bytes.Buffer{}
		if err := html.Render(contentHTML, extracted.ContentNode); err != nil {
			return nil, fmt.Errorf("渲染提取结果失败: %w", err)
		}
		converter := md.NewConverter("", true, nil)
		markdown, err := converter.ConvertString(contentHTML.String())
		if err != nil {
			return nil, fmt.Errorf("HTML 转 Markdown 失败: %w", err)
		}
		res.Content = strings.TrimSpace(markdown)
	}

	if opts.WithMetadata {
		res.Content = metadataHeader(parsedURL, res) + res.Content
	}
	if opts.MaxTokens > 0 {
		res.Content = Truncate(res.Content, opts.MaxTokens)
	}
	return res, nil
}

// metadataHeader 生成与方案文档一致的元信息头：
//
//	Title: ...
//	URL Source: ...
//	Published Time: ...
//	Author: ...
func metadataHeader(u *url.URL, r *Result) string {
	var b strings.Builder
	b.WriteString("Title: " + firstNonEmpty(r.Title, u.String()))
	b.WriteString("\n\nURL Source: " + u.String())
	if r.Date != "" {
		b.WriteString("\n\nPublished Time: " + r.Date)
	}
	if r.Author != "" {
		b.WriteString("\n\nAuthor: " + r.Author)
	}
	b.WriteString("\n\n")
	return b.String()
}

func formatDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02 15:04:05")
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// Truncate 按粗略 token 估算截断：1 token ≈ 3 字符，按 rune 截断避免拆坏 UTF-8。
func Truncate(text string, maxTokens int) string {
	if maxTokens <= 0 {
		return text
	}
	runes := []rune(text)
	budget := maxTokens * 3
	if len(runes) <= budget {
		return text
	}
	return string(runes[:budget]) + "\n\n……[内容已按 max_tokens 截断]……"
}
