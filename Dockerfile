# syntax=docker/dockerfile:1.7

# ---------- 前端构建 ----------
FROM node:22-alpine AS web
WORKDIR /web
RUN corepack enable && corepack prepare pnpm@10.33.0 --activate
COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
# 输出到独立目录，便于下一阶段拷贝进 embed 目录
RUN pnpm run build:docker

# ---------- Go 构建 ----------
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=none
WORKDIR /src
RUN apk add --no-cache git
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /web/dist/. ./internal/webui/dist/
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} go build \
    -trimpath \
    -ldflags "-s -w \
      -X github.com/starschedule/server/internal/version.Version=${VERSION} \
      -X github.com/starschedule/server/internal/version.Commit=${COMMIT} \
      -X github.com/starschedule/server/internal/version.BuildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    -o /out/api ./cmd/api

# ---------- 运行 ----------
FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata wget \
    && addgroup -g 10001 app \
    && adduser -D -u 10001 -G app -h /app app
WORKDIR /app
COPY --from=build /out/api /app/api
USER 10001:10001
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8080/healthz >/dev/null || exit 1
ENTRYPOINT ["/app/api"]
