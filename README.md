# 星课表后端（starSchedule-server）

「星课表」Android 客户端的后端服务：设备注册、启动统计、AI 模型代理，附带一套 React + shadcn/ui 管理后台（构建产物 embed 进同一个二进制）。

一条命令拉起全部依赖（镜像由 GitHub Actions 发布到 GHCR）：

```bash
cp .env.example .env   # 改 APP_IMAGE 与七个必填项
docker compose pull && docker compose up -d
curl http://localhost:8080/readyz
```

本地改完代码要立即生效，用 `make up-local` 源码构建，不走镜像仓库。

- 客户端接口：`/api/v1/*`
- 管理后台：`http://localhost:8080/admin/`
- 指标：`/metrics`，健康检查：`/healthz`、`/readyz`

## 技术栈

| 层 | 选型 |
| --- | --- |
| HTTP | Gin（net/http 生态，SSE 用 `http.NewResponseController` 透传） |
| 存储 | PostgreSQL 16（pgx/v5 + 手写 SQL） |
| 缓存/限流 | Redis 7（Lua 原子脚本） |
| 鉴权 | JWT（设备 30 天、管理端 7 天，滑动续期） |
| 前端 | React 19 + Vite + shadcn/ui + Tailwind v4 |
| 部署 | 多阶段 Dockerfile + docker compose（自带 PG 与 Redis） |

## 目录结构

```
cmd/api/                入口（-migrate-only / -version）
internal/
  aiproxy/              OpenAI 兼容上游转发、SSE 透传、token 估算
  auth/ crypto/         JWT、口令哈希、AES-256-GCM
  config/               环境变量加载与校验
  handler/              路由处理器（设备、AI、管理端）
  job/                  定时任务（日汇总校正、分区维护、清理）
  middleware/           请求 ID、鉴权、限流、CORS、日志
  migrate/sql/          内嵌 SQL 迁移
  service/              业务逻辑
  store/postgres|redis/ 存储层
  webui/                前端产物 embed + SPA 静态服务
web/                    管理后台（Vite + shadcn/ui）
```

## 环境变量

全部通过环境变量注入，缺必填项启动即失败。见 `.env.example`。

| 变量 | 说明 |
| --- | --- |
| `DATABASE_URL` / `REDIS_ADDR` | 依赖地址（compose 内用服务名） |
| `DEVICE_JWT_SECRET` / `ADMIN_JWT_SECRET` | ≥32 字符，两套独立密钥 |
| `DEVICE_FINGERPRINT_PEPPER` | ANDROID_ID 加盐，泄露后哈希可被反推 |
| `APP_ENCRYPTION_KEY` | base64 编码的 32 字节，用于加密上游 API Key |
| `ADMIN_USERNAME` / `ADMIN_PASSWORD` | 首次启动创建管理员，之后不会覆盖已改密码 |
| `APP_TZ` | 统计切日时区，默认 `Asia/Shanghai` |
| `ALLOW_INSECURE_COOKIE` | 仅本地 http 调试置 true |

生成密钥：

```bash
openssl rand -base64 48   # JWT secret
openssl rand -base64 32   # APP_ENCRYPTION_KEY
```

## 本地开发

```bash
# 需要本机有 PostgreSQL 与 Redis（或把 DATABASE_URL / REDIS_ADDR 指向远程）
export $(grep -v '^#' .env | xargs)   # 或自己 export
go run ./cmd/api

# 另开终端启前端（5173，已代理 /api 到 8080）
cd web && pnpm install && pnpm dev
```

只用 Docker 验证时：`make up-local`（源码构建，不拉镜像）。

## 管理后台

访问 `http://<host>:8080/admin/`，用 `ADMIN_USERNAME` / `ADMIN_PASSWORD` 登录（首次启动自动创建，之后不覆盖已改密码）。

| 页面 | 能力 |
| --- | --- |
| 仪表盘 | 今日 DAU / 启动 / 新增 / AI token，30 天趋势（可切指标），版本分布，当前模型状态 |
| 设备 | 搜索 install_id 或设备 ID、游标分页、封禁/解封（立即生效） |
| 设备详情 | 基础信息、近 30 天活跃柱状图、今日 AI 用量、最近启动记录 |
| AI 供应商 | 新建/编辑（密钥掩码回填）、探活、同步模型、设为启用 |
| 模型 | 启用开关、手动补充、上下文长度 |
| AI 试跑 | 流式对话，直连当前供应商，展示首字延迟 / 耗时 / token，不占设备配额 |
| AI 用量 | 按天或按设备查看 token 与失败数 |
| 设置 | 配额、限流、保留天数热改，标注默认值与被覆盖项 |

前端构建产物通过 `go:embed` 打进同一个二进制：

```bash
make web        # 只构建前端到 internal/webui/dist
make build      # 前端 + 二进制
```

开发时前后端分离：

```bash
go run ./cmd/api          # 8080
cd web && pnpm dev        # 5173，/api 已代理到 8080
```

组件按 shadcn/ui 官方规范维护：只用 CLI 增删（`pnpm dlx shadcn@latest add <component>`），不要手写 `src/components/ui/*`；`components.json` 与 `pnpm-lock.yaml` 一并提交。官方 skill 已落在 `.workbuddy/skills/shadcn/`。

## 客户端接口

完整契约（请求头、请求体、响应体、错误码、重试策略）见 `docs/api.md`。

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | `/api/v1/devices/register` | `install_id` 幂等注册，返回 `device_id` + JWT |
| GET | `/api/v1/devices/me` | 验活与封禁校验 |
| POST | `/api/v1/launches` | 启动上报（`session_id` 去重） |
| GET | `/api/v1/ai/models` | 当前可用模型 |
| GET | `/api/v1/ai/config` | 当前模型 + 限额 + 今日用量 |
| POST | `/api/v1/ai/chat/completions` | OpenAI 兼容，`stream` 控制 SSE |
| GET | `/api/v1/ai/usage` | 今日用量 |

统一错误体：

```json
{ "error": { "code": "RATE_LIMITED", "message": "请求过于频繁，请 12 秒后重试", "request_id": "01JB8Z..." } }
```

## AI 供应商配置

1. 登录后台 → 「AI 供应商」→ 新建（填 `base_url`、`api_key`），key 用 AES-256-GCM 加密后落库，接口只返回掩码。
2. 「同步模型」自动拉取上游 `GET {base_url}/models`。
3. 选择模型启用并「设为启用」。
4. 客户端通过 `/ai/models`、`/ai/config` 读取当前模型，密钥永远不会下发给客户端。

配额与限流默认值可在「设置」页热改，修改后 30 秒内生效：每日 20 万 token / 设备、每分钟 20 次、并发 2。

## 部署

### 必须设置的环境变量

`.env` 与 `docker-compose.yml` 同级，`docker compose` 会自动读取。以下七项**必须改**，否则启动失败或存在安全风险：

| 变量 | 作用 | 生成方式 |
| --- | --- | --- |
| `POSTGRES_PASSWORD` | 数据库密码 | `openssl rand -base64 24` |
| `REDIS_PASSWORD` | Redis 密码（同时用于 `--requirepass`） | `openssl rand -base64 24` |
| `DEVICE_JWT_SECRET` | 设备 token 签名密钥，≥32 字符 | `openssl rand -base64 48` |
| `ADMIN_JWT_SECRET` | 后台 token 签名密钥，≥32 字符 | `openssl rand -base64 48` |
| `DEVICE_FINGERPRINT_PEPPER` | `ANDROID_ID` 哈希盐值 | `openssl rand -base64 24` |
| `APP_ENCRYPTION_KEY` | 加密上游 API Key，**必须是 base64 的 32 字节** | `openssl rand -base64 32` |
| `ADMIN_PASSWORD` | 首次启动创建的后台管理员密码 | 自定 |

注意两点：

- `APP_ENCRYPTION_KEY` 一旦上线就不要再换，换掉后后台已保存的供应商密钥会解密失败，需要重新录入。
- `ADMIN_PASSWORD` 只在 `admin_users` 为空时生效；之后改 env 不会修改已存在的密码。

常用可选项：`HTTP_PORT`（对外端口，默认 8080）、`APP_TZ`（统计切日时区，默认 Asia/Shanghai）、`LOG_LEVEL`、`ADMIN_USERNAME`、`DB_MAX_CONNS`、`METRICS_ENABLED`、`DEVICE_TOKEN_TTL`。

> `.env` 里的 `DATABASE_URL`、`REDIS_ADDR` 只在「不用 compose、直接跑二进制」时需要；compose 会用 `POSTGRES_*` 自动拼出连接串，并固定连 `redis:6379`。

### 端口与网络

**只需要对外暴露一个端口**：`api` 的 8080。

| 服务 | 容器端口 | 是否映射到宿主机 | 说明 |
| --- | --- | --- | --- |
| `api` | 8080 | 是，`${HTTP_PORT:-8080}` → 8080 | 客户端与管理后台都走这里 |
| `postgres` | 5432 | **否** | 仅容器网络内可达 |
| `redis` | 6379 | **否** | 仅容器网络内可达 |

网络方面没有特殊配置，compose 会自建一个 bridge 网络，服务之间用名字解析（`postgres:5432`、`redis:6379`）。需要满足的出站条件：

- 拉镜像：服务器能访问 `ghcr.io`（以及 `docker.io` 拉基础镜像）。
- AI 代理：`api` 需要能访问你在后台配置的供应商域名（如 `api.deepseek.com`）。
- 如果服务器只能通过代理出网，给 `api` 补上代理变量（代码里走 `http.ProxyFromEnvironment`）：

```yaml
# docker-compose.override.yml
services:
  api:
    environment:
      HTTPS_PROXY: http://your-proxy:3128
      HTTP_PROXY: http://your-proxy:3128
      NO_PROXY: postgres,redis,localhost,127.0.0.1
```

前面挂了 Caddy/Nginx 时，建议把端口只绑到本机回环，避免绕过反代直接访问 8080：

```yaml
# docker-compose.override.yml
services:
  api:
    ports:
      - "127.0.0.1:8080:8080"
```

### 数据持久化

默认用 Docker 命名卷，**不需要映射任何主机路径**：

| 卷 | 容器内路径 | 内容 |
| --- | --- | --- |
| `pgdata` | `/var/lib/postgresql/data` | 全部业务数据（设备、启动事件、AI 用量等） |
| `redisdata` | `/data` | AOF 文件（限流计数、配额、缓存） |

`api` 是无状态服务：容器只读根文件系统，仅挂 `tmpfs: /tmp`，本身不需要持久化。

#### 命名卷为什么也算持久化

容器有两层存储，别混为一谈：

| 存储 | 位置 | 容器删除后 | 宿主机重启后 |
| --- | --- | --- | --- |
| 容器可写层 | 容器内部 | **丢失** | 保留 |
| 命名卷（本项目默认） | Docker 管理区，Linux 上是 `/var/lib/docker/volumes/` | **保留** | 保留 |
| 绑定挂载（映射主机目录） | 你自己指定的目录 | **保留** | 保留 |

compose 里已经声明了 `volumes: pgdata:/var/lib/postgresql/data`，所以数据写在命名卷里，不在容器可写层。容器重建、改镜像重新 `up`、`docker compose down` 都不影响它。

**会丢数据的只有这几种操作**：

- `docker compose down -v`（`-v` 会删卷）
- `docker volume rm starschedule_pgdata`
- `docker volume prune` 或 `docker system prune --volumes`
- 换机器时不迁移卷

**不会丢数据的操作**：`docker restart`、`docker compose restart`、`docker compose down`（不带 `-v`）、`docker compose up -d`（换镜像重建容器）、宿主机重启。

确认卷是否存在：

```bash
docker volume ls | grep starschedule          # starschedule_pgdata / starschedule_redisdata
docker volume inspect starschedule_pgdata     # 看 Mountpoint
```

> 用 Docker Desktop（macOS / Windows）时，卷在虚拟机内部，宿主机文件管理器里看不到 Mountpoint 路径，这是正常的；备份用下面的命令，不要试图直接拷贝目录。

#### 备份

命名卷最稳的备份方式是逻辑导出，不依赖卷在哪：

```bash
# 数据库
docker compose exec -T postgres pg_dump -U starschedule starschedule | gzip > backup-$(date +%F).sql.gz

# 恢复
gunzip -c backup-2026-10-31.sql.gz | docker compose exec -T postgres psql -U starschedule -d starschedule
```

Redis 里只有限流计数、配额与缓存，丢了会重建（配额会从 0 重新算），一般不必备份；要备就连同 `/data` 一起打包。

#### 改成映射到主机目录（可选）

如果希望数据目录在项目下、方便打包整机迁移，用 override 文件改绑定挂载，不要改动主 compose：

```yaml
# docker-compose.override.yml（compose 会自动合并）
services:
  postgres:
    volumes:
      - ./data/postgres:/var/lib/postgresql/data
  redis:
    volumes:
      - ./data/redis:/data
```

Linux 上要先授权，否则容器起不来：

```bash
mkdir -p data/postgres data/redis
sudo chown -R 70:70 data/postgres     # postgres:alpine 的 uid/gid
sudo chown -R 999:999 data/redis      # redis:alpine 的 uid/gid
```

备份也就变成：`pg_dump` 逻辑备份，或直接打包 `data/` 目录 / `pgdata` 卷。

### 镜像发布与更新（GitHub Actions → GHCR）

CI 在 `.github/workflows/ci.yml`，镜像由 `GITHUB_TOKEN` 推送到 GHCR，无需额外配置 secret（仓库需允许 Actions 写 Packages，默认允许）。

| 触发 | 行为 | 产出 tag |
| --- | --- | --- |
| 推到默认分支（`master` 或 `main`） | 构建并推送多架构镜像 | `latest`、分支名、`sha-xxxxxxx` |
| 打 `v*` tag（如 `v1.2.3`） | 构建并推送 | `1.2.3`、`1.2`、`latest`、`sha-xxxxxxx` |
| Pull Request | 只验证能否构建，不推送 | — |

镜像地址：`ghcr.io/<你的用户名或组织>/starschedule-server`，路径必须全小写，CI 里已自动转换。

**服务器首次部署：**

```bash
cp .env.example .env
vim .env                      # 改 APP_IMAGE 为上面的地址 + 七个必填项
chmod 600 .env
docker login ghcr.io -u <GitHub 用户名> -p <PAT>   # 包为私有时需要，PAT 勾 read:packages
docker compose pull && docker compose up -d
curl http://127.0.0.1:8080/readyz
```

**日常更新与回滚：**

```bash
make update                                   # = pull + up，拉 latest 并重建容器
# 回滚到某个提交的镜像
APP_IMAGE=ghcr.io/<name>/starschedule-server:sha-1a2b3c4 docker compose up -d
# 或指定版本号
APP_IMAGE=ghcr.io/<name>/starschedule-server:1.2.2 docker compose up -d
```

- 升级时 `migrate` 容器会用新镜像先跑一次迁移，成功后才替换 `api`；迁移是幂等的，回滚代码不需要回滚数据库（历史迁移不会重复执行）。
- 想让包公开（服务器免登录拉取）：仓库 → Packages → 该镜像 → Package settings → Change visibility → Public。
- 本地改代码要即时生效，用 `make up-local`（走 `docker-compose.build.yml` 本地构建），不要用 `make up`。

### 其它注意

- 对外必须走 HTTPS（Android 9+ 默认禁明文），可用 Caddy/Nginx 反代 8080，并透传 `X-Forwarded-Proto: https`。
- `postgres` / `redis` 不映射端口，仅容器网络内可达；需要本机连库时临时加 `ports: ["127.0.0.1:5432:5432"]`。
- 数据库迁移由一次性 `migrate` 容器执行，成功后才启动 `api`；升级时重复执行是幂等的。
- `api` 内存上限 512M（compose `deploy.resources`），可按机器规格调整。

## 测试

```bash
go test ./... -race      # 单元测试（token 估算、SSE 透传、JWT、AES）
go vet ./...
cd web && pnpm exec tsc -b && pnpm build
```
