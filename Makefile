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
	@echo "make up         拉取 GHCR 镜像并启动（pg + redis + migrate + api）"
	@echo "make update     等价于 pull + up，用于服务器升级"
	@echo "make pull       只拉取镜像"
	@echo "make up-local   本地源码构建并启动（不拉 GHCR 镜像）"
	@echo "make down       停止 compose（保留数据卷）"
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

.PHONY: pull
pull:
	docker compose pull

# 服务器部署：用 GHCR 上的镜像启动，不在本机构建
.PHONY: up
up: pull
	docker compose up -d

.PHONY: update
update: pull
	docker compose up -d

# 本地开发：源码构建，镜像名仍取 APP_IMAGE
.PHONY: up-local
up-local:
	docker compose -f docker-compose.yml -f docker-compose.build.yml up -d --build

.PHONY: down
down:
	docker compose down

.PHONY: logs
logs:
	docker compose logs -f api
