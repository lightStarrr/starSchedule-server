# 星课表 · Android 客户端 API 契约

- **Base URL**：生产 `https://<域名>/api/v1`；模拟器调试 `http://10.0.2.2:8080/api/v1`
- **编码**：`application/json; charset=utf-8`（SSE 除外）
- **时间**：RFC3339 UTC，如 `2026-10-31T07:43:19Z`
- **ID**：UUIDv7 字符串
- **无信封**：成功直接返回业务对象；列表 `{items, next_cursor, has_more}`；失败 `{error:{...}}`
- **幂等**：注册按 `install_id`，启动上报按 `session_id`，重试必须复用同一值

## 1. 请求头

| Header | 方向 | 必需 | 说明 |
| --- | --- | --- | --- |
| `Authorization: Bearer <token>` | 请求 | 除注册外必需 | 设备 access_token |
| `Content-Type: application/json; charset=utf-8` | 请求 | 有 body 时 | |
| `Accept` | 请求 | 否 | 默认 `application/json`；流式必须 `text/event-stream` |
| `X-Client-Version` | 请求 | 是 | versionName，如 `1.2.0` |
| `X-Client-Version-Code` | 请求 | 是 | versionCode，如 `120` |
| `X-Request-Id` | 请求 | 否 | 客户端 UUID；缺失时服务端生成 |
| `User-Agent` | 请求 | 是 | `StarSchedule-Android/1.2.0 (Android 14; sdk 34; arm64-v8a)` |
| `X-Request-Id` | 响应 | 是 | 与请求一致或服务端生成，报错时展示给用户便于排查 |
| `X-Server-Time` | 响应 | 是 | 服务端 UTC 时间，用于时钟校正 |
| `X-Refresh-Token` | 响应 | 条件 | token 剩余 <7 天时出现，客户端静默替换本地 token |
| `X-RateLimit-Limit` / `-Remaining` / `-Reset` | 响应 | 条件 | 被限流接口返回，`Reset` 为 Unix 秒 |
| `Retry-After` | 响应 | 条件 | 429 时建议等待秒数 |

## 2. 统一错误体

```json
{
  "error": {
    "code": "RATE_LIMITED",
    "message": "请求过于频繁，请 12 秒后重试",
    "request_id": "01JB8Z3K4M5N6P7Q8R9S0T1U2V",
    "details": { "retry_after": 12 }
  }
}
```

`code` 稳定不变供程序判断；`message` 可直接展示；`details` 可选。

## 3. POST /devices/register

注册并换取 token，**无需 Authorization**。

```json
{
  "install_id": "8f14e45f-ea4b-4c1e-9a3f-2b7c6d5e4f30",
  "android_id": "9774d56d682e549c",
  "platform": "android",
  "app_version": "1.2.0",
  "version_code": 120,
  "os_version": "14",
  "sdk_int": 34,
  "arch": "arm64-v8a",
  "locale": "zh-CN",
  "timezone": "Asia/Shanghai"
}
```

| 字段 | 类型 | 必需 | 约束 |
| --- | --- | --- | --- |
| `install_id` | string | 是 | 首启生成并存 DataStore，UUID，长度 8–64 |
| `android_id` | string | 是 | `Settings.Secure.ANDROID_ID`，8–64 位；服务端只存加盐哈希 |
| `platform` | string | 是 | 固定 `android` |
| `app_version` | string | 是 | ≤32 字符 |
| `version_code` | int | 是 | 1–2100000000 |
| `os_version` | string | 是 | 如 `14`，≤16 字符 |
| `sdk_int` | int | 是 | 21–99 |
| `arch` | string | 是 | `arm64-v8a` / `armeabi-v7a` / `x86_64` |
| `locale`、`timezone` | string | 否 | 仅用于统计与排查 |

响应 `200`：

```json
{
  "device_id": "01JB8Z3K4M5N6P7Q8R9S0T1U2V",
  "access_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "token_type": "Bearer",
  "expires_in": 2592000,
  "expires_at": "2026-11-30T07:43:19Z",
  "is_new": true,
  "server_time": "2026-10-31T07:43:19Z"
}
```

- 同 `install_id` 重复注册 → 同一 `device_id`、新 token、`is_new=false`
- 限流：同 `install_id` 5 次/分，同 IP 60 次/时
- 错误：`400 INVALID_ARGUMENT` / `403 DEVICE_BANNED` / `429 RATE_LIMITED` / `500 INTERNAL`

## 4. GET /devices/me

```json
{
  "device_id": "01JB8Z3K4M5N6P7Q8R9S0T1U2V",
  "status": "active",
  "platform": "android",
  "app_version": "1.2.0",
  "first_seen_at": "2026-10-01T02:11:00Z",
  "last_seen_at": "2026-10-31T07:40:12Z",
  "launch_count": 42,
  "suspect_reinstall": false,
  "ai": { "used_tokens": 1500, "daily_quota": 200000, "reset_at": "2026-11-01T00:00:00+08:00" },
  "limits": { "rpm": 20, "inflight": 2 },
  "server_time": "2026-10-31T07:43:19Z"
}
```

`200` 继续；`401` 用同一 `install_id` 重新注册；`403` 停止上报与 AI 请求。

## 5. POST /launches

```json
{
  "session_id": "b1d2c3a4-5e6f-4a7b-8c9d-0e1f2a3b4c5d",
  "launched_at": "2026-10-31T07:39:58Z",
  "launch_type": "cold",
  "app_version": "1.2.0",
  "version_code": 120,
  "os_version": "14",
  "sdk_int": 34,
  "arch": "arm64-v8a",
  "extra": { "cold_start_ms": 820 }
}
```

| 字段 | 类型 | 必需 | 说明 |
| --- | --- | --- | --- |
| `session_id` | string | 是 | 每次启动一个 UUID，重试不变 |
| `launched_at` | string | 否 | 偏差 >±7 天或未来 >10 分钟时，服务端改用接收时间 |
| `launch_type` | string | 否 | `cold` / `warm`，默认 `cold` |
| 其余 | — | 否 | 缺失时取设备档案值 |
| `extra` | object | 否 | 预留扩展，≤1KB |

响应：`204 No Content`。重复上报同样 204。

## 6. GET /ai/models

```json
{
  "items": [
    {
      "alias": "deepseek-chat",
      "display_name": "DeepSeek Chat",
      "provider_name": "deepseek",
      "context_window": 65536,
      "supports_stream": true,
      "is_current": true
    }
  ],
  "current": "deepseek-chat",
  "server_time": "2026-10-31T07:43:19Z"
}
```

未配置供应商时 `503 AI_NOT_CONFIGURED`（客户端隐藏 AI 入口）。

## 7. GET /ai/config

```json
{
  "model": { "alias": "deepseek-chat", "display_name": "DeepSeek Chat", "context_window": 65536, "supports_stream": true },
  "limits": {
    "rpm": 20,
    "inflight": 2,
    "max_output_tokens": 4096,
    "max_input_bytes": 131072,
    "daily_token_quota": 200000
  },
  "usage": {
    "used_tokens": 1500,
    "remaining_tokens": 198500,
    "request_count": 3,
    "reset_at": "2026-11-01T00:00:00+08:00"
  },
  "server_time": "2026-10-31T07:43:19Z"
}
```

## 8. POST /ai/chat/completions

请求体（只接受下列字段，其余忽略）：

```json
{
  "messages": [
    { "role": "system", "content": "你是星课表助手" },
    { "role": "user", "content": "帮我把周三的课挪到周五" }
  ],
  "temperature": 0.7,
  "top_p": 0.9,
  "max_tokens": 1024,
  "stop": ["\n\n"],
  "stream": true
}
```

| 字段 | 类型 | 必需 | 约束 |
| --- | --- | --- | --- |
| `messages` | array | 是 | 1–100 条；`role ∈ {system,user,assistant}`；整段 ≤128KB |
| `temperature` | float | 否 | 0–2，默认 0.7 |
| `top_p` | float | 否 | 0–1 |
| `max_tokens` | int | 否 | 超出服务端上限会被截断 |
| `stop` | string[] | 否 | ≤4 条 |
| `stream` | bool | 否 | 默认 `false` |
| `model` | string | 否 | 必须等于当前 alias，否则 `400 MODEL_NOT_AVAILABLE` |

**非流式** `200 application/json`：

```json
{
  "id": "01JB8Z3K4M5N6P7Q8R9S0T1U2V",
  "object": "chat.completion",
  "created": 1761896599,
  "model": "deepseek-chat",
  "choices": [
    { "index": 0, "message": { "role": "assistant", "content": "可以，已为你调整。" }, "finish_reason": "stop" }
  ],
  "usage": { "prompt_tokens": 12, "completion_tokens": 34, "total_tokens": 46, "estimated": false },
  "request_id": "01JB8Z3K4M5N6P7Q8R9S0T1U2V"
}
```

**流式**响应头：

```
HTTP/1.1 200 OK
Content-Type: text/event-stream; charset=utf-8
Cache-Control: no-cache, no-transform
Connection: keep-alive
X-Accel-Buffering: no
X-Request-Id: 01JB8Z3K4M5N6P7Q8R9S0T1U2V
```

帧序列（每帧以空行结束）：

```
event: meta
data: {"id":"01JB8Z...","model":"deepseek-chat","alias":"deepseek-chat","request_id":"01JB8Z..."}

data: {"choices":[{"index":0,"delta":{"role":"assistant","content":""}}]}

data: {"choices":[{"index":0,"delta":{"content":"可以，"}}]}

data: {"choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":12,"completion_tokens":34,"total_tokens":46,"estimated":false}}

data: [DONE]
```

- 每 15 秒发注释帧 `: ping`，客户端忽略，不得当作结束
- 流内错误发 `event: error` 后关闭，**不再发 `[DONE]`**：

```
event: error
data: {"error":{"code":"UPSTREAM_TIMEOUT","message":"模型响应超时，请重试","request_id":"01JB8Z..."}}
```

- 客户端以 `[DONE]` 与 `event: error` 作为终止条件，按 `delta.content` 增量拼接
- **不自动重连**：重连会重复计费，由用户手动重发

错误（写 200 之前）：`400 INVALID_ARGUMENT` / `400 MODEL_NOT_AVAILABLE` / `401` / `403 DEVICE_BANNED` / `413 PAYLOAD_TOO_LARGE` / `429 RATE_LIMITED` / `429 AI_QUOTA_EXCEEDED` / `502 UPSTREAM_ERROR` / `503 QUOTA_SERVICE_UNAVAILABLE` / `504 UPSTREAM_TIMEOUT`

## 9. GET /ai/usage

```json
{
  "date": "2026-10-31",
  "daily_quota": 200000,
  "used_tokens": 1500,
  "remaining_tokens": 198500,
  "request_count": 3,
  "reset_at": "2026-11-01T00:00:00+08:00"
}
```

## 10. 错误码与客户端动作

| HTTP | code | 场景 | 客户端动作 |
| --- | --- | --- | --- |
| 400 | `INVALID_ARGUMENT` | 参数缺失/越界 | 修正后重试，不自动重试 |
| 400 | `MODEL_NOT_AVAILABLE` | 传了未启用的 model | 重新拉 `/ai/config` |
| 401 | `UNAUTHENTICATED` | 缺 token / 格式错 | 用 `install_id` 重新注册 |
| 401 | `TOKEN_EXPIRED` / `TOKEN_INVALID` | 过期或验签失败 | 静默重注册后重放一次 |
| 403 | `DEVICE_BANNED` | 设备被封禁 | 停止上报与 AI，不重试 |
| 404 | `NOT_FOUND` | 路径错误 | 客户端 bug |
| 413 | `PAYLOAD_TOO_LARGE` | 请求体 / messages 过大 | 截断上下文后重试 |
| 429 | `RATE_LIMITED` | 频率超限 | 按 `Retry-After` 退避 |
| 429 | `AI_QUOTA_EXCEEDED` | 当日额度耗尽 | 展示额度与 `reset_at`，当日不再请求 |
| 500 | `INTERNAL` | 服务端异常 | 指数退避重试 2 次 |
| 502 | `UPSTREAM_ERROR` | 上游报错/密钥失效 | 提示「服务暂时不可用」 |
| 503 | `AI_NOT_CONFIGURED` | 后台未配置模型 | 隐藏 AI 入口 |
| 503 | `QUOTA_SERVICE_UNAVAILABLE` | 配额存储不可用 | 提示稍后再试 |
| 504 | `UPSTREAM_TIMEOUT` | 上游超时 | 允许用户手动重发 |

### 重试矩阵

| 接口 | 幂等 | 自动重试 |
| --- | --- | --- |
| `/devices/register` | 是（install_id） | 网络错误重试 3 次，退避 1s/3s/7s |
| `/devices/me` | 是 | 2 次 |
| `/launches` | 是（session_id） | 最多 5 次；进程被杀后下次启动补投 |
| `/ai/models`、`/ai/config`、`/ai/usage` | 是 | 2 次 |
| `/ai/chat/completions` | 否 | 不自动重试 |

通用限流：单设备 600 次/分；启动上报 120 次/分；读接口 120 次/分。

## 11. Android 实现要点

- OkHttp：普通请求 `callTimeout=15s`、`connectTimeout=10s`；SSE `callTimeout=0`、`readTimeout=0`，连续 45 秒无任何帧则主动断开
- SSE 用 `EventSource` 或手写 `BufferedSource.readUtf8Line()`：累积 `event:` 与多行 `data:`，遇空行派发
- token 存 EncryptedSharedPreferences；收到 `X-Refresh-Token` 立即覆盖写入
- 每次请求带 `X-Request-Id`（UUID，重试复用），异常提示带上它
- 启动上报在 `Application.onCreate` 与 `ProcessLifecycleOwner.onStart` 各触发一次，各自生成新的 `session_id`
- 客户端不得内置任何上游 API Key，全部经 `/ai/chat/completions`
