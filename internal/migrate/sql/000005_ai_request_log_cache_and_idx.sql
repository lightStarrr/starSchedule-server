-- 记录上游缓存命中 token，并补齐请求日志分页与按模型筛选索引。
ALTER TABLE ai_request_log ADD COLUMN IF NOT EXISTS cached_tokens integer NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS ai_request_log_created_idx ON ai_request_log (created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS ai_request_log_model_created_idx ON ai_request_log (model, created_at DESC);
