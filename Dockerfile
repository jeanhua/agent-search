# ---------- 构建阶段 ----------
FROM golang:1.25-alpine AS builder

WORKDIR /src

# Go 模块代理：默认官方源，网络受限时可 --build-arg GOPROXY=https://goproxy.cn,direct
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY}

# 先拷贝依赖清单，利用 Docker 层缓存
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# 静态编译、去调试信息，产出 ~20MB 单二进制
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/agent-search ./cmd/agent-search

# ---------- 运行阶段 ----------
FROM alpine:3.21

# HTTPS 根证书 + 时区数据；非 root 运行
RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -u 10001 app

USER app
WORKDIR /app

COPY --from=builder /out/agent-search /usr/local/bin/agent-search

EXPOSE 8000
ENTRYPOINT ["agent-search"]
