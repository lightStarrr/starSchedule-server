#!/usr/bin/env bash
# 星课表后端一键部署（复用服务器上已有的 PostgreSQL / Redis）
#
# 用法：
#   sudo PG_PASSWORD=xxx bash deploy/install.sh
#   sudo PG_PASSWORD=xxx REGISTRY=ghcr.nju.edu.cn bash deploy/install.sh
#
# 可选环境变量：
#   REGISTRY         镜像仓库域名，默认 ghcr.io；可换成加速站
#   IMAGE_PATH       镜像路径，默认 lightstarrr/starschedule-server
#   IMAGE_TAG        镜像标签，默认 latest
#   APP_IMAGE        直接指定完整镜像地址，优先级最高
#   PG_CONTAINER     PostgreSQL 容器名，默认自动探测
#   REDIS_CONTAINER  Redis 容器名，默认自动探测
#   PG_USER          PostgreSQL 用户名，默认在候选用户里自动探测
#   PG_PASSWORD      PostgreSQL 密码（必填或交互输入）
#   REDIS_PASSWORD   Redis 密码，默认自动探测
#   DB_NAME          数据库名，默认 starschedule
#   HTTP_PORT        对外端口，默认 8080
#   CONTAINER_NAME   应用容器名，默认 starschedule
#   ENV_FILE         配置文件路径，默认 /etc/starschedule/app.env
#   ADMIN_PASSWORD   后台管理员密码，默认随机生成
#
# 可重复执行：已有 ENV_FILE 会复用其中的密钥与管理员密码。

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
ENV_FILE="${ENV_FILE:-/etc/starschedule/app.env}"

info() { printf '\033[36m[信息]\033[0m %s\n' "$*"; }
ok()   { printf '\033[32m[成功]\033[0m %s\n' "$*"; }
warn() { printf '\033[33m[注意]\033[0m %s\n' "$*"; }
die()  { printf '\033[31m[失败]\033[0m %s\n' "$*" >&2; exit 1; }

command -v docker >/dev/null 2>&1 || die "未找到 docker"
docker info >/dev/null 2>&1 || die "无法连接 Docker 守护进程，请用 root 或加入 docker 组"

detect_container() {
  docker ps --format '{{.Names}}' | grep -Ei "$1" | head -n1 || true
}
container_env() {
  docker inspect -f '{{range .Config.Env}}{{println .}}{{end}}' "$1" 2>/dev/null |
    sed -n "s/^$2=//p" | head -n1
}
networks_of() {
  docker inspect -f '{{range $k, $v := .NetworkSettings.Networks}}{{$k}}{{"\n"}}{{end}}' "$1" 2>/dev/null | sed '/^$/d'
}

PG_CONTAINER="${PG_CONTAINER:-$(detect_container 'postgres')}"
REDIS_CONTAINER="${REDIS_CONTAINER:-$(detect_container 'redis')}"
[ -n "$PG_CONTAINER" ] || die "未找到 PostgreSQL 容器，可用 PG_CONTAINER=名称 指定"
[ -n "$REDIS_CONTAINER" ] || die "未找到 Redis 容器，可用 REDIS_CONTAINER=名称 指定"
info "PostgreSQL 容器：$PG_CONTAINER"
info "Redis 容器：     $REDIS_CONTAINER"

# ---------- 1. 密码 ----------
PG_PASSWORD="${PG_PASSWORD:-$(container_env "$PG_CONTAINER" POSTGRES_PASSWORD)}"
if [ -z "${PG_PASSWORD:-}" ]; then
  read -rsp "请输入 PostgreSQL 密码: " PG_PASSWORD
  echo
fi
[ -n "$PG_PASSWORD" ] || die "PostgreSQL 密码不能为空"

if printf '%s' "$PG_PASSWORD" | grep -qE '[^A-Za-z0-9._~-]'; then
  die "密码含 URL 特殊字符，连接串会解析失败，请先改成字母数字组合"
fi

REDIS_PASSWORD="${REDIS_PASSWORD:-$(container_env "$REDIS_CONTAINER" REDIS_PASSWORD)}"
if [ -z "${REDIS_PASSWORD:-}" ]; then
  REDIS_PASSWORD="$(docker inspect -f '{{join .Args " "}}' "$REDIS_CONTAINER" 2>/dev/null |
    sed -n 's/.*--requirepass[= ]\([^ ]*\).*/\1/p' | head -n1)"
fi

# ---------- 2. 用 TCP 验证 PostgreSQL（容器内的本地 socket 往往是 trust，验不出密码）----------
# 候选用户：容器环境变量里的用户、postgres、与库同名用户
CANDIDATES=()
[ -n "$PG_USER" ] && CANDIDATES+=("$PG_USER")
v="$(container_env "$PG_CONTAINER" POSTGRES_USER)"
[ -n "$v" ] && CANDIDATES+=("$v")
CANDIDATES+=("postgres" "$DB_NAME")

pg_auth() { # $1=user
  docker exec -e PGPASSWORD="$PG_PASSWORD" "$PG_CONTAINER" \
    psql -h 127.0.0.1 -U "$1" -d postgres -tAc 'SELECT 1' >/dev/null 2>&1
}

info "验证 PostgreSQL 账号密码"
PG_USER=""
for u in $(printf '%s\n' "${CANDIDATES[@]}" | awk '!seen[$0]++'); do
  if pg_auth "$u"; then PG_USER="$u"; break; fi
done
if [ -z "$PG_USER" ]; then
  warn "候选用户（$(printf '%s ' "${CANDIDATES[@]}")）用该密码均无法登录"
  die "密码不正确。请在 1Panel → 数据库 里查看 $DB_NAME 对应用户的密码，用 PG_PASSWORD=... 重新执行"
fi
ok "PostgreSQL 认证通过，用户：$PG_USER"

# ---------- 3. Redis ----------
redis_ping() {
  if [ -n "${REDIS_PASSWORD:-}" ]; then
    docker exec "$REDIS_CONTAINER" redis-cli -a "$REDIS_PASSWORD" --no-auth-warning ping 2>/dev/null | grep -q PONG
  else
    docker exec "$REDIS_CONTAINER" redis-cli ping 2>/dev/null | grep -q PONG
  fi
}
if ! redis_ping; then
  read -rsp "Redis 需要密码或密码不正确，请输入: " REDIS_PASSWORD
  echo
  redis_ping || die "Redis 认证失败，可用 REDIS_PASSWORD=... 指定"
fi
ok "Redis 认证通过"

# ---------- 4. 选网络：优先与 PG/Redis 共用同一个 Docker 网络 ----------
# 默认 bridge / host / none 没有内置 DNS，容器名解析不了，必须排除
SHARED_NET="$(comm -12 <(networks_of "$PG_CONTAINER" | sort) <(networks_of "$REDIS_CONTAINER" | sort) \
  | grep -vxE 'bridge|host|none' | head -n1 || true)"

port_bind() { docker port "$1" "$2" 2>/dev/null | head -n1 | cut -d: -f1; }
port_num()  { docker port "$1" "$2" 2>/dev/null | head -n1 | sed 's/.*://'; }

if [ -n "$SHARED_NET" ]; then
  NET_MODE="shared"
  DB_HOST="$PG_CONTAINER"
  DB_PORT=5432
  REDIS_HOST="$REDIS_CONTAINER"
  REDIS_PORT=6379
  info "使用共享 Docker 网络：$SHARED_NET（按容器名直连，无需走宿主机端口）"
else
  PG_BIND="$(port_bind "$PG_CONTAINER" 5432)"
  REDIS_BIND="$(port_bind "$REDIS_CONTAINER" 6379)"
  case "$PG_BIND$REDIS_BIND" in
    *127.0.0.1*|*::1*)
      NET_MODE="host"
      DB_HOST="127.0.0.1"; DB_PORT=5432; REDIS_HOST="127.0.0.1"; REDIS_PORT=6379
      warn "两个容器没有共享网络且端口仅绑回环，改用 host 网络"
      ;;
    *)
      NET_MODE="bridge"
      DB_HOST="host.docker.internal"; DB_PORT="$(port_num "$PG_CONTAINER" 5432)"
      REDIS_HOST="host.docker.internal"; REDIS_PORT="$(port_num "$REDIS_CONTAINER" 6379)"
      warn "两个容器没有共享网络，改用宿主机映射端口"
      ;;
  esac
fi

# ---------- 5. 确保数据库存在 ----------
info "检查数据库 $DB_NAME"
db_exists() {
  docker exec -e PGPASSWORD="$PG_PASSWORD" "$PG_CONTAINER" \
    psql -h 127.0.0.1 -U "$PG_USER" -d postgres -tAc \
    "SELECT 1 FROM pg_database WHERE datname='$DB_NAME'" 2>/dev/null | tr -d '[:space:]'
}
if [ "$(db_exists)" = "1" ]; then
  ok "数据库已存在"
else
  if docker exec -e PGPASSWORD="$PG_PASSWORD" "$PG_CONTAINER" \
    psql -h 127.0.0.1 -U "$PG_USER" -d postgres -c "CREATE DATABASE \"$DB_NAME\"" >/dev/null 2>&1; then
    ok "已创建数据库 $DB_NAME"
  else
    die "创建数据库失败：用户 $PG_USER 没有建库权限，请先在 1Panel 里创建 $DB_NAME"
  fi
fi

# ---------- 6. 密钥 ----------
load_env() { [ -f "$ENV_FILE" ] && sed -n "s/^$1=//p" "$ENV_FILE" | head -n1 || true; }
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

# ---------- 7. 写配置 ----------
mkdir -p "$(dirname "$ENV_FILE")"
umask 077
cat > "$ENV_FILE" <<EOF
# 由 deploy/install.sh 生成于 $(date '+%Y-%m-%d %H:%M:%S')
# 加密密钥不要更换，否则后台已保存的供应商密钥会失效
APP_ENV=production
APP_TZ=${APP_TZ:-Asia/Shanghai}
LOG_LEVEL=${LOG_LEVEL:-info}
HTTP_ADDR=:8080

DATABASE_URL=postgres://${PG_USER}:${PG_PASSWORD}@${DB_HOST}:${DB_PORT}/${DB_NAME}?sslmode=disable
REDIS_ADDR=${REDIS_HOST}:${REDIS_PORT}
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

# ---------- 8. 拉镜像并启动 ----------
info "拉取镜像 $APP_IMAGE"
if ! docker pull "$APP_IMAGE" >/dev/null 2>&1; then
  if [ "$REGISTRY" = "ghcr.io" ]; then
    die "拉取失败：私有包需先 docker login ghcr.io，或改用 REGISTRY=加速域名"
  fi
  die "拉取失败：加速站可能未同步该镜像，可换回 REGISTRY=ghcr.io"
fi

if docker ps -a --format '{{.Names}}' | grep -qx "$CONTAINER_NAME"; then
  info "移除旧容器 $CONTAINER_NAME"
  docker rm -f "$CONTAINER_NAME" >/dev/null
fi

RUN_ARGS=(
  -d --name "$CONTAINER_NAME" --restart unless-stopped
  --env-file "$ENV_FILE"
  --read-only --tmpfs /tmp
  --security-opt no-new-privileges:true
  --memory 512m
  --health-cmd "wget -qO- http://127.0.0.1:8080/healthz || exit 1"
  --health-interval 30s --health-timeout 3s --health-retries 3
)

case "$NET_MODE" in
  host)
    RUN_ARGS+=(--network host)
    ;;
  shared)
    RUN_ARGS+=(--network "$SHARED_NET" -p "${HTTP_PORT}:8080")
    ;;
  *)
    RUN_ARGS+=(-p "${HTTP_PORT}:8080" --add-host "host.docker.internal:host-gateway")
    ;;
esac

docker run "${RUN_ARGS[@]}" "$APP_IMAGE" >/dev/null
ok "容器 $CONTAINER_NAME 已启动"

# ---------- 9. 等就绪 ----------
info "等待服务就绪（首次启动自动建表，最多 60 秒）"
ready=0
for _ in $(seq 1 30); do
  if docker exec "$CONTAINER_NAME" wget -qO- http://127.0.0.1:8080/readyz 2>/dev/null | grep -q '"ready":true'; then
    ready=1; break
  fi
  docker ps --format '{{.Names}}' | grep -qx "$CONTAINER_NAME" || break
  sleep 2
done

if [ "$ready" != "1" ]; then
  echo
  warn "未就绪，最近日志："
  docker logs --tail 25 "$CONTAINER_NAME" 2>&1 || true
  echo
  case "$(docker logs --tail 50 "$CONTAINER_NAME" 2>&1)" in
    *"password authentication failed"*)
      die "数据库密码不对：1Panel 里 $DB_NAME 的用户是 $PG_USER，请用 PG_PASSWORD=该用户密码 重跑" ;;
    *"does not exist"*)
      die "数据库不存在：请先在 1Panel 里创建 $DB_NAME" ;;
    *"connection refused"*|*"no such host"*)
      die "连不上数据库：检查容器网络（当前模式 ${NET_MODE}）" ;;
  esac
  die "部署未完成，请查看上面的日志"
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
  数据库     ${DB_NAME} @ ${DB_HOST}:${DB_PORT}（用户 ${PG_USER}）
  Redis      ${REDIS_HOST}:${REDIS_PORT}
  网络模式   ${NET_MODE}

常用命令：
  docker logs -f ${CONTAINER_NAME}
  docker restart ${CONTAINER_NAME}
  docker rm -f ${CONTAINER_NAME}    # 数据在数据库里，移除容器不影响

下一步：登录后台 → 「AI 供应商」新建 → 同步模型 → 设为启用。
EOF
