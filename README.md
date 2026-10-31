# 星课表后端（starSchedule-server）

「星课表」Android 客户端的后端服务：设备注册、启动统计、AI 模型代理，附带一套 React + shadcn/ui 管理后台（构建产物 embed 进同一个二进制）。

一条命令拉起全部依赖：

```bash
cp .env.example .env   # 按提示改掉所有 change-me
docker compose up -d --build
curl http://localhost:8080/readyz
```

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

只用 Docker 验证时：`docker compose up -d --build`。

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

## 部署注意

- 对外必须走 HTTPS（Android 9+ 默认禁止明文），可用 Caddy/Nginx 反代 8080。
- `postgres` / `redis` 不对外暴露端口，仅容器网络内可达。
- `api` 容器以只读根文件系统 + uid 10001 运行。
- 数据库迁移由一次性 `migrate` 容器执行，成功后才启动 `api`。
- 备份：`pgdata` 与 `redisdata` 两个卷；Redis 开了 AOF。

## 测试

```bash
go test ./... -race      # 单元测试（token 估算、SSE 透传、JWT、AES）
go vet ./...
cd web && pnpm exec tsc -b && pnpm build
```
