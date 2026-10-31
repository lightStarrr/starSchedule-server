#!/usr/bin/env bash
# 星课表后端一键部署（适配 1Panel 已有 PostgreSQL / Redis 的场景）
#
# 用法：
#   sudo bash deploy/install.sh
#   sudo PG_PASSWORD=xxx APP_IMAGE=ghcr.io/lightstarrr/starschedule-server:sha-b0593e0 bash deploy/install.sh
#
# 可选环境变量：
#   REGISTRY         镜像仓库域名，默认 ghcr.io；可换成加速镜像站（如 ghcr.nju.edu.cn）
#   IMAGE_PATH       镜像路径，默认 lightstarrr/starschedule-server
#   IMAGE_TAG        镜像标签，默认 latest
#   APP_IMAGE        直接指定完整镜像地址，优先级最高
#   PG_CONTAINER     PostgreSQL 容器名，默认自动探测包含 postgres 的容器
#   REDIS_CONTAINER  Redis 容器名，默认自动探测包含 redis 的容器
#   PG_USER / PG_PASSWORD / DB_NAME  默认从容器环境变量读取，DB_NAME 默认 starschedule
#   HTTP_PORT        对外端口，默认 8080
#   CONTAINER_NAME   应用容器名，默认 starschedule
#   ADMIN_PASSWORD   后台管理员密码，默认随机生成
#
# 脚本可重复执行：已存在的 .env 会复用其中的密钥与管理员密码。

set -euo pipefail

REGISTRY="${REGISTRY:-ghcr.io}"
REGISTRY="${REGISTRY%/}"
IMAGE_PATH="${IMAGE_PATH:-lightstarrr/starschedule-server}"
IMAGE_TAG="${IMAGE_TAG:-latest}"
APP_IMAGE="${APP_IMAGE:-${REGISTRY}/${IMAGE_PATH}:${IMAGE_TAG}}"
CONTAINER_NAME="${CONTAINER_NAME:-starschedule}"
HTTP_PORT="${HTTP_PORT:-8080}"
DB_NAME="${DB_NAME:-starschedule}"
ADMIN_USERNAME="${ADMIN_USERNAME:-admin}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="${ENV_FILE:-$SCRIPT_DIR/../.env}"

info()  { printf '\033[36m[信息]\033[0m %s\n' "$*"; }
ok()    { printf '\033[32m[成功]\033[0m %s\n' "$*"; }
warn()  { printf '\033[33m[注意]\033[0m %s\n' "$*"; }
die()   { printf '\033[31m[失败]\033[0m %s\n' "$*" >&2; exit 1; }

# ---------- 0. 前置检查 ----------
command -v docker >/dev/null 2>&1 || die "未找到 docker，请先安装 Docker"
docker info >/dev/null 2>&1 || die "无法连接 Docker 守护进程，请用 root 或加入 docker 组后重试"

detect_container() {
  docker ps --format '{{.Names}}' | grep -Ei "$1" | head -n1 || true
}

PG_CONTAINER="${PG_CONTAINER:-$(detect_container 'postgres')}"
REDIS_CONTAINER="${REDIS_CONTAINER:-$(detect_container 'redis')}"

[ -n "$PG_CONTAINER" ] || die "没有找到运行中的 PostgreSQL 容器，可用 PG_CONTAINER=名称 指定"
[ -n "$REDIS_CONTAINER" ] || die "没有找到运行中的 Redis 容器，可用 REDIS_CONTAINER=名称 指定"
info "PostgreSQL 容器：$PG_CONTAINER"
info "Redis 容器：    $REDIS_CONTAINER"

container_env() {
  docker inspect -f '{{range .Config.Env}}{{println .}}{{end}}' "$1" 2>/dev/null |
    sed -n "s/^$2=//p" | head -n1
}

# ---------- 1. 读取数据库凭据 ----------
PG_USER="${PG_USER:-$(container_env "$PG_CONTAINER" POSTGRES_USER)}"
PG_USER="${PG_USER:-postgres}"
PG_PASSWORD="${PG_PASSWORD:-$(container_env "$PG_CONTAINER" POSTGRES_PASSWORD)}"
if [ -z "$PG_PASSWORD" ]; then
  read -rsp "请输入 PostgreSQL 用户 $PG_USER 的密码: " PG_PASSWORD
  echo
fi
[ -n "$PG_PASSWORD" ] || die "PostgreSQL 密码不能为空"

if printf '%s' "$PG_PASSWORD" | grep -qE '[^A-Za-z0-9._~-]'; then
  die "密码含 URL 特殊字符，需要先改成字母数字组合（连接串会解析失败）"
fi

REDIS_PASSWORD="${REDIS_PASSWORD:-$(container_env "$REDIS_CONTAINER" REDIS_PASSWORD)}"
if [ -z "${REDIS_PASSWORD:-}" ]; then
  REDIS_PASSWORD="$(docker inspect -f '{{join .Args " "}}' "$REDIS_CONTAINER" 2>/dev/null |
    sed -n 's/.*--requirepass[= ]\([^ ]*\).*/\1/p' | head -n1)"
fi

# ---------- 1.5 连通性自检（凭据不对时给出明确提示）----------
info "校验 PostgreSQL 连接"
if ! docker exec -e PGPASSWORD="$PG_PASSWORD" "$PG_CONTAINER" \
  psql -U "$PG_USER" -d postgres -tAc 'SELECT 1' >/dev/null 2>&1; then
  die "PostgreSQL 认证失败，请用 PG_PASSWORD=... 指定正确密码后重试"
fi

redis_ping() {
  if [ -n "${REDIS_PASSWORD:-}" ]; then
    docker exec "$REDIS_CONTAINER" redis-cli -a "$REDIS_PASSWORD" --no-auth-warning ping 2>/dev/null | grep -q PONG
  else
    docker exec "$REDIS_CONTAINER" redis-cli ping 2>/dev/null | grep -q PONG
  fi
}

info "校验 Redis 连接"
if ! redis_ping; then
  read -rsp "Redis 需要密码或密码不正确，请输入: " REDIS_PASSWORD
  echo
  redis_ping || die "Redis 认证失败，请用 REDIS_PASSWORD=... 指定正确密码后重试"
fi
ok "数据库连接正常"

# ---------- 2. 确定宿主机端口 ----------
host_port() {
  local mapping
  mapping="$(docker port "$1" "$2" 2>/dev/null | head -n1)"
  [ -n "$mapping" ] || die "容器 $1 没有发布端口 $2，无法从外部访问"
  printf '%s' "${mapping##*:}"
}

host_bind() {
  local mapping
  mapping="$(docker port "$1" "$2" 2>/dev/null | head -n1)"
  printf '%s' "${mapping%%:*}"
}

PG_PORT="$(host_port "$PG_CONTAINER" 5432)"
REDIS_PORT="$(host_port "$REDIS_CONTAINER" 6379)"
PG_BIND="$(host_bind "$PG_CONTAINER" 5432)"
REDIS_BIND="$(host_bind "$REDIS_CONTAINER" 6379)"

# 只绑回环时容器经 bridge 访问不到，改用 host 网络
USE_HOST_NET=0
case "$PG_BIND$REDIS_BIND" in
  *127.0.0.1*|*::1*) USE_HOST_NET=1 ;;
esac
if [ "$USE_HOST_NET" = "1" ]; then
  DB_HOST="127.0.0.1"
  warn "数据库仅监听回环地址，将使用 host 网络模式（容器与宿主机共用网络栈）"
else
  DB_HOST="host.docker.internal"
fi

# ---------- 3. 创建数据库（不存在时）----------
info "检查数据库 $DB_NAME 是否存在"
exists="$(docker exec -e PGPASSWORD="$PG_PASSWORD" "$PG_CONTAINER" \
  psql -U "$PG_USER" -d postgres -tAc "SELECT 1 FROM pg_database WHERE datname='$DB_NAME'" 2>/dev/null | tr -d '[:space:]')"
if [ "$exists" = "1" ]; then
  ok "数据库已存在，跳过创建"
else
  docker exec -e PGPASSWORD="$PG_PASSWORD" "$PG_CONTAINER" \
    psql -U "$PG_USER" -d postgres -c "CREATE DATABASE \"$DB_NAME\" OWNER \"$PG_USER\"" >/dev/null
  ok "已创建数据库 $DB_NAME"
fi

# ---------- 4. 生成或复用密钥 ----------
load_env() {
  [ -f "$ENV_FILE" ] && sed -n "s/^$1=//p" "$ENV_FILE" | head -n1 || true
}

gen_secret() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -base64 "$1" | tr -d '\n'
  else
    head -c "$1" /dev/urandom | base64 | tr -d '\n'
  fi
}

DEVICE_JWT_SECRET="${DEVICE_JWT_SECRET:-$(load_env DEVICE_JWT_SECRET)}"
DEVICE_JWT_SECRET="${DEVICE_JWT_SECRET:-$(gen_secret 48)}"
ADMIN_JWT_SECRET="${ADMIN_JWT_SECRET:-$(load_env ADMIN_JWT_SECRET)}"
ADMIN_JWT_SECRET="${ADMIN_JWT_SECRET:-$(gen_secret 48)}"
DEVICE_FINGERPRINT_PEPPER="${DEVICE_FINGERPRINT_PEPPER:-$(load_env DEVICE_FINGERPRINT_PEPPER)}"
DEVICE_FINGERPRINT_PEPPER="${DEVICE_FINGERPRINT_PEPPER:-$(gen_secret 24)}"
APP_ENCRYPTION_KEY="${APP_ENCRYPTION_KEY:-$(load_env APP_ENCRYPTION_KEY)}"
APP_ENCRYPTION_KEY="${APP_ENCRYPTION_KEY:-$(gen_secret 32)}"
ADMIN_PASSWORD="${ADMIN_PASSWORD:-$(load_env ADMIN_PASSWORD)}"
ADMIN_PASSWORD="${ADMIN_PASSWORD:-$(gen_secret 18)}"

# ---------- 5. 写 .env ----------
umask 077
cat > "$ENV_FILE" <<EOF
# 由 deploy/install.sh 生成于 $(date '+%Y-%m-%d %H:%M:%S')
# 加密密钥不可更换，否则后台已保存的供应商密钥会失效
APP_ENV=production
APP_TZ=${APP_TZ:-Asia/Shanghai}
LOG_LEVEL=${LOG_LEVEL:-info}
HTTP_ADDR=:8080

DATABASE_URL=postgres://${PG_USER}:${PG_PASSWORD}@${DB_HOST}:${PG_PORT}/${DB_NAME}?sslmode=disable
REDIS_ADDR=${DB_HOST}:${REDIS_PORT}
REDIS_PASSWORD=${REDIS_PASSWORD:-}
REDIS_DB=${REDIS_DB:-0}

DEVICE_JWT_SECRET=${DEVICE_JWT_SECRET}
ADMIN_JWT_SECRET=${ADMIN_JWT_SECRET}
DEVICE_FINGERPRINT_PEPPER=${DEVICE_FINGERPRINT_PEPPER}
APP_ENCRYPTION_KEY=${APP_ENCRYPTION_KEY}
ADMIN_USERNAME=${ADMIN_USERNAME}
ADMIN_PASSWORD=${ADMIN_PASSWORD}
METRICS_ENABLED=${METRICS_ENABLED:-true}
EOF
chmod 600 "$ENV_FILE"
ok "已写入 $ENV_FILE"

# ---------- 6. 拉取镜像 ----------
info "拉取镜像 $APP_IMAGE"
if ! docker pull "$APP_IMAGE" >/dev/null 2>&1; then
  if [ "$REGISTRY" = "ghcr.io" ]; then
    warn "拉取失败：GHCR 私有包需要先登录"
    die "执行 docker login ghcr.io -u <GitHub 用户名> -p <PAT> 后重试，或把包设为 Public；也可用 REGISTRY=镜像加速域名 重跑"
  fi
  die "拉取失败：镜像加速站可能不支持该镜像或尚未同步，换回 REGISTRY=ghcr.io 重试"
fi

# ---------- 7. 启动容器 ----------
if docker ps -a --format '{{.Names}}' | grep -qx "$CONTAINER_NAME"; then
  info "移除旧容器 $CONTAINER_NAME（数据都在数据库里，不会丢失）"
  docker rm -f "$CONTAINER_NAME" >/dev/null
fi

RUN_ARGS=(
  -d
  --name "$CONTAINER_NAME"
  --restart unless-stopped
  --env-file "$ENV_FILE"
  --read-only
  --tmpfs /tmp
  --security-opt no-new-privileges:true
  --memory 512m
  --health-cmd "wget -qO- http://127.0.0.1:8080/healthz || exit 1"
  --health-interval 30s
  --health-timeout 3s
  --health-retries 3
)

if [ "$USE_HOST_NET" = "1" ]; then
  RUN_ARGS+=(--network host)
else
  RUN_ARGS+=(-p "${HTTP_PORT}:8080" --add-host "host.docker.internal:host-gateway")
fi

docker run "${RUN_ARGS[@]}" "$APP_IMAGE" >/dev/null
ok "容器 $CONTAINER_NAME 已启动"

# ---------- 8. 等待就绪 ----------
info "等待服务就绪（首次启动会自动建表，最多 60 秒）"
ready=0
for _ in $(seq 1 30); do
  if docker exec "$CONTAINER_NAME" wget -qO- http://127.0.0.1:8080/readyz 2>/dev/null | grep -q '"ready":true'; then
    ready=1
    break
  fi
  if ! docker ps --format '{{.Names}}' | grep -qx "$CONTAINER_NAME"; then
    break
  fi
  sleep 2
done

if [ "$ready" != "1" ]; then
  echo
  warn "服务未在预期时间内就绪，最近日志："
  docker logs --tail 40 "$CONTAINER_NAME" 2>&1 || true
  die "部署未完成，请根据上面的日志排查（常见原因：数据库密码错误、pg_hba 未放行 docker 网段）"
fi

HOST_IP="$(hostname -I 2>/dev/null | awk '{print $1}')"
[ -n "${HOST_IP:-}" ] || HOST_IP="127.0.0.1"

cat <<EOF

$(ok "部署完成")

  管理后台   http://${HOST_IP}:${HTTP_PORT}/admin/
  用户名     ${ADMIN_USERNAME}
  密码       ${ADMIN_PASSWORD}
  配置文件   ${ENV_FILE}
  镜像       ${APP_IMAGE}
  数据库     ${DB_NAME} @ ${PG_CONTAINER}:${PG_PORT}
  Redis      ${REDIS_CONTAINER}:${REDIS_PORT}

常用命令：
  docker logs -f ${CONTAINER_NAME}           # 查看日志
  docker restart ${CONTAINER_NAME}           # 重启
  docker rm -f ${CONTAINER_NAME}             # 停止并移除（数据在数据库里，不受影响）

下一步：登录后台 → 「AI 供应商」新建 → 同步模型 → 设为启用。
EOF
