package extract

import (
	"strings"
	"testing"
)

const articleHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <title>测试文章标题</title>
  <meta name="author" content="张三">
  <meta name="date" content="2026-08-01">
  <meta name="description" content="这是一篇用于测试的示例文章">
</head>
<body>
  <nav><a href="/">首页</a></nav>
  <article>
    <h1>测试文章标题</h1>
    <p>第一段：<strong>加粗内容</strong>，用于验证正文提取是否正常。</p>
    <p>第二段：包含一个<a href="https://example.com/ref">参考链接</a>。</p>
    <table><tr><td>表格内容</td></tr></table>
    <ul><li>列表项一</li><li>列表项二</li></ul>
  </article>
  <footer>页脚不应该被提取</footer>
</body>
</html>`

func TestExtractMarkdown(t *testing.T) {
	res, err := Extract([]byte(articleHTML), "https://example.com/article", Options{
		OutputFormat: "markdown",
		WithMetadata: true,
	})
	if err != nil {
		t.Fatalf("extract error: %v", err)
	}
	if !strings.Contains(res.Content, "Title: 测试文章标题") {
		t.Errorf("missing title metadata:\n%s", res.Content)
	}
	if !strings.Contains(res.Content, "URL Source: https://example.com/article") {
		t.Errorf("missing URL Source metadata:\n%s", res.Content)
	}
	if !strings.Contains(res.Content, "第一段") {
		t.Errorf("missing body text:\n%s", res.Content)
	}
	if strings.Contains(res.Content, "页脚") {
		t.Errorf("footer should be removed:\n%s", res.Content)
	}
	if strings.Contains(res.Content, "参考链接") && strings.Contains(res.Content, "https://example.com/ref") {
		// include_links=false 时不应带链接地址
		t.Errorf("links should be stripped by default:\n%s", res.Content)
	}
}

func TestExtractWithLinks(t *testing.T) {
	res, err := Extract([]byte(articleHTML), "https://example.com/article", Options{
		OutputFormat: "markdown",
		WithMetadata: false,
		IncludeLinks: true,
	})
	if err != nil {
		t.Fatalf("extract error: %v", err)
	}
	if !strings.Contains(res.Content, "https://example.com/ref") {
		t.Errorf("links should be kept when include_links=true:\n%s", res.Content)
	}
}

func TestExtractTXT(t *testing.T) {
	res, err := Extract([]byte(articleHTML), "https://example.com/article", Options{
		OutputFormat: "txt",
		WithMetadata: false,
	})
	if err != nil {
		t.Fatalf("extract error: %v", err)
	}
	if !strings.Contains(res.Content, "第一段") {
		t.Errorf("missing text content:\n%s", res.Content)
	}
	if strings.Contains(res.Content, "[") {
		t.Errorf("txt output should be plain:\n%s", res.Content)
	}
}

func TestExtractEmpty(t *testing.T) {
	if _, err := Extract([]byte("<html><body></body></html>"), "https://example.com/x", Options{}); err == nil {
		t.Fatal("expected error for empty page")
	}
}

func TestTruncateRunes(t *testing.T) {
	text := "你好世界你好世界你好世界你好世界" // 16 个 rune
	got := Truncate(text, 2)   // budget 6 runes
	if got != "你好世界你好"+"\n\n……[内容已按 max_tokens 截断]……" {
		t.Fatalf("unexpected truncate result: %q", got)
	}
	if Truncate(text, 100) != text {
		t.Fatal("no truncation expected")
	}
}
