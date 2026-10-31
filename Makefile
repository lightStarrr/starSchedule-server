SHELL := /bin/sh

APP      := bin/starschedule-server
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT   ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILT_AT := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
	-X github.com/starschedule/server/internal/version.Version=$(VERSION) \
	-X github.com/starschedule/server/internal/version.Commit=$(COMMIT) \
	-X github.com/starschedule/server/internal/version.BuildTime=$(BUILT_AT)

.PHONY: help
help:
	@echo "make web        构建前端到 internal/webui/dist"
	@echo "make build      构建前端 + 服务端二进制"
	@echo "make dev-api    本地跑后端（需 .env 已导出或使用 env 前缀）"
	@echo "make dev-web    启动 Vite 开发服务器"
	@echo "make test       运行单元测试"
	@echo "make vet        静态检查"
	@echo "make fmt        格式化"
	@echo "make migrate    只执行数据库迁移"
	@echo "make up         启动 compose（pg + redis + migrate + api）"
	@echo "make down       停止 compose"
	@echo "make logs       跟踪 api 日志"

.PHONY: web
web:
	cd web && pnpm install --frozen-lockfile && pnpm build

.PHONY: build
build: web
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(APP) ./cmd/api

.PHONY: build-go
build-go:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(APP) ./cmd/api

.PHONY: dev-api
dev-api:
	go run ./cmd/api

.PHONY: dev-web
dev-web:
	cd web && pnpm dev

.PHONY: test
test:
	go test ./... -race -count=1

.PHONY: vet
vet:
	go vet ./...

.PHONY: fmt
fmt:
	gofmt -w ./cmd ./internal

.PHONY: migrate
migrate:
	go run ./cmd/api -migrate-only

.PHONY: up
up:
	docker compose up -d --build

.PHONY: down
down:
	docker compose down

.PHONY: logs
logs:
	docker compose logs -f api
