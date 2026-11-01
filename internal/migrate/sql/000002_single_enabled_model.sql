-- 同一个上游模型只允许一家供应商处于启用状态。
-- 早期版本同步多家供应商时会把同名模型都置为启用，导致 /ai/models 出现重复条目；
-- 这里保留创建最早的一条，其余关闭（幂等，可重复执行）。
UPDATE ai_provider_models m
SET enabled = false
WHERE m.enabled
  AND EXISTS (
      SELECT 1
      FROM ai_provider_models x
      WHERE x.model_id = m.model_id
        AND x.enabled
        AND (x.created_at, x.id) < (m.created_at, m.id)
  );

CREATE INDEX IF NOT EXISTS ai_provider_models_model_idx
    ON ai_provider_models (model_id)
    WHERE enabled;
