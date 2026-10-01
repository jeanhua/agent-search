# agent-search

一个轻量的 AI 联网服务：**搜索 + 网页阅读**，给 LLM / AI Agent 提供联网能力。

- **搜索**：内置 SearXNG 聚合搜索引擎（Google / Bing / DuckDuckGo 等），自带 JSON API
- **阅读**：基于 [go-trafilatura](https://github.com/markusmobius/go-trafilatura) 提取网页正文，输出干净的 Markdown / TXT
- 纯 Go 实现，无头浏览器，单二进制部署；内置 SSRF 防护、超时限制、TTL 缓存、可选 API Key 鉴权

## 快速开始

需要 Docker + Docker Compose。

```bash
# 启动（首次会自动构建镜像并拉取 SearXNG）
docker compose up -d --build
```

不想本地构建的话，可以直接用 Docker Hub 上发布好的镜像：把 `docker-compose.yml` 里 `reader` 服务的 `build:` 段去掉、`image:` 改为 `jeanhua/agent-search:latest`。

启动后：

- SearXNG：`http://127.0.0.1:8888`
- agent-search：`http://127.0.0.1:8000`

## 使用

### 搜索

```bash
curl -s 'http://127.0.0.1:8000/search?q=OpenAI&n=5'
```

返回：

```json
{
  "query": "OpenAI",
  "results": [
    { "title": "...", "url": "https://...", "content": "...", "engine": "bing" }
  ]
}
```

参数：`q`（必填，关键词）、`n`（结果数，默认 5，取值 1 ~ 50）、`language`（语言，如 `zh-CN`）。

### 阅读网页

```bash
curl -s -X POST http://127.0.0.1:8000/extract \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://en.wikipedia.org/wiki/Artificial_intelligence","max_tokens":2000}'
```

返回：

```json
{
  "url": "https://en.wikipedia.org/wiki/Artificial_intelligence",
  "cached": false,
  "content": "Title: ...\n\nURL Source: ...\n\n正文内容..."
}
```

请求参数：

| 参数 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `url` | string | 必填 | 要阅读的网页（仅 http/https） |
| `max_tokens` | int | 无 | 按 token 截断输出（200 ~ 100000） |
| `output_format` | string | `markdown` | `markdown` 或 `txt` |
| `with_metadata` | bool | `true` | 是否带标题、URL、日期等元信息 |
| `include_links` | bool | `false` | 是否保留链接 |
| `respect_robots` | bool | `false` | 是否遵守 robots.txt |

### 健康检查

```bash
curl -s http://127.0.0.1:8000/healthz
```

## Agent 集成

把下面两个函数接入 function calling / MCP：

```json
[
  {
    "type": "function",
    "function": {
      "name": "search_web",
      "description": "搜索互联网，返回相关网页的标题、链接和摘要。",
      "parameters": {
        "type": "object",
        "properties": { "query": { "type": "string", "description": "搜索关键词" } },
        "required": ["query"]
      }
    }
  },
  {
    "type": "function",
    "function": {
      "name": "read_url",
      "description": "抓取并阅读一个网页，返回干净的 Markdown 正文（支持按 token 截断）。",
      "parameters": {
        "type": "object",
        "properties": {
          "url": { "type": "string", "description": "要阅读的网页 URL" },
          "max_tokens": { "type": "integer", "description": "最多返回的 token 数，默认全部" }
        },
        "required": ["url"]
      }
    }
  }
]
```

## 配置

通过环境变量配置（`docker-compose.yml` 里可改）：

| 变量 | 默认 | 说明 |
|---|---|---|
| `LISTEN_ADDR` | `127.0.0.1:8000` | 监听地址 |
| `SEARXNG_URL` | `http://127.0.0.1:8888` | SearXNG 地址 |
| `ALLOW_PRIVATE` | `false` | 是否允许抓取内网地址（SSRF 防护开关） |
| `API_KEY` | 空 | 设置后启用 Bearer / X-API-Key 鉴权 |
| `CACHE_SIZE` | `512` | 缓存条目上限 |
| `CACHE_TTL` | `1h` | 缓存有效期 |
| `MAX_HTML_BYTES` | `4194304` | 单页最大下载大小 |

## 发布

推送 `v*.*.*` 格式的标签（如 `v0.1.0`），GitHub Actions 会自动构建 linux/amd64 + linux/arm64 镜像并发布到 Docker Hub（打 `1.2.3`、`1.2`、`latest` 三个 tag）：

```bash
git tag v0.1.0 && git push origin v0.1.0
```

## 本地开发

```bash
go run ./cmd/agent-search          # 运行（需本地已启动 SearXNG）
go test ./...                      # 测试
go vet ./...                       # 静态检查
```

## 常见问题

| 现象 | 原因与解决 |
|---|---|
| `/search` 返回 502 | SearXNG 没起来，或 `searxng/settings.yml` 没开 `search.formats: json` |
| 某些引擎一直空结果 | 引擎被目标站点封了，在 `searxng/settings.yml` 的 `engines` 里禁用 |
| 提取为空 | 页面反爬/需 JS/正文极少 → 换 UA、`respect_robots: false`，或走 JS 渲染兜底 |
| 目标网站 403 | 换 UA 轮换、必要时用代理 |
| 读大页面爆上下文 | 调 `/extract` 的 `max_tokens` 截断 |
