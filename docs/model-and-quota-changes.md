# 模型选择、启动信息与日额度配置

Android 继续从 `GET /api/v1/ai/models` 获取 `alias`，并在聊天请求的 `model` 中提交该 alias。聊天和后台试跑均严格按 alias 解析，不接受数据库 UUID 或上游 model_id（除非它本身就是 alias）。服务端只在转发给供应商时使用上游 model_id。

同一上游 model_id 的供应商记录共用 alias，只允许一家处于启用状态。新增第二家供应商时继承已有 alias。修改 alias 会同步该模型的全部供应商记录；不同 model_id 不得占用同一 alias，包括停用记录。切换供应商、默认模型指向在同一个事务中提交，切换失败会回滚。

迁移会统一同组供应商的 alias，优先保留当前启用记录的别名。若历史数据存在不同模型共用 alias，迁移会报错并回滚，需要管理员先处理冲突，不会静默改名。可用以下查询排查：

```sql
SELECT alias, array_agg(DISTINCT model_id)
FROM ai_provider_models GROUP BY alias HAVING count(DISTINCT model_id) > 1;
```

后台设置 `ai.quota_unlimited` 是正式布尔配置：true 表示不限制每日 Token，false 表示按正整数 `ai.daily_token_quota` 限制。旧值 0 会迁移为 true，额度数值恢复为 200000，供关闭开关时使用。以后写入额度 0 会返回 400，应通过独立开关控制不限量。

- `GET /ai/config` 在 `limits.quota_unlimited` 返回布尔值。
- `GET /ai/usage` 在顶层 `quota_unlimited` 返回布尔值。
- `GET /devices/me` 在 `ai.quota_unlimited` 返回布尔值。
- 不限量时，用量与配置中的 `remaining_tokens` 为 null；有限额时为非负整数。
- 日额度开关不影响请求频率、并发上限和单次输出上限，用量仍正常记账。

Android 需要同步读取布尔配置并支持 nullable remaining_tokens，开启时显示“不限量”。后端变更无法单独修复旧客户端“剩余 0”的展示。本次未改动 Android 源码或放宽令牌鉴权。

启动上报在事件、日聚合、设备计数的同一事务中更新版本信息。`app_version`、`version_code`、`os_version`、`sdk_int`、`arch` 缺省时保留已有值；旧事件不能覆盖更近上报的信息。同一统计日内重复 session 不重复计数，也不更新元信息。越出允许时间窗口的客户端时间可用于现有接收时间归档规则，但不用于覆盖设备元信息；无时间的在线上报使用接收时间。

后台模型页展示当前供应商的具体思考档位，例如 low、medium、high。固定开启、不支持、未检测分别展示；没有档位信息时提示重新检测，不推测能力。

新检测会逐档发送最多 4 次小请求，全部档位被拒绝时再发送一次不带参数的基线请求。这些请求可能产生供应商费用。超时、限流、鉴权失败等返回“未检测”，不会当成“不支持”。检测只能确认网关接受参数，无法证明上游内部没有忽略参数。旧版本只测 low 后推断全部档位，迁移后会重置这类检测结果，请在模型页重新检测。
