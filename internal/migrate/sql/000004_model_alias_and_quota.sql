-- 同一上游模型的所有供应商共用 alias，优先保留当前启用记录的别名。
WITH canonical AS (
    SELECT DISTINCT ON (model_id) model_id, alias
    FROM ai_provider_models
    ORDER BY model_id, enabled DESC, created_at, id
)
UPDATE ai_provider_models m SET alias = c.alias
FROM canonical c WHERE m.model_id = c.model_id AND m.alias <> c.alias;

-- 历史冲突必须由管理员明确处理，不能静默改变 Android 使用的别名。
DO $$ BEGIN
    IF EXISTS (SELECT alias FROM ai_provider_models GROUP BY alias HAVING count(DISTINCT model_id) > 1) THEN
        RAISE EXCEPTION '模型 alias 被不同 model_id 占用，请先处理 ai_provider_models 的别名冲突';
    END IF;
END $$;
CREATE UNIQUE INDEX ai_models_one_enabled_provider ON ai_provider_models (model_id) WHERE enabled;
CREATE UNIQUE INDEX ai_models_enabled_alias ON ai_provider_models (alias) WHERE enabled;

-- 旧探测仅验证 low 后推断所有档位，不能当作准确能力继续展示。
UPDATE ai_provider_models SET reasoning_mode='unknown', reasoning_efforts='{}'::text[], reasoning_default=''
WHERE reasoning_mode IN ('effort', 'fixed', 'none');

-- 将原来的 0 = 不限量转换为显式开关，并保留一个可用于恢复限制的正数额度。
INSERT INTO app_settings (key, value)
SELECT 'ai.quota_unlimited', COALESCE((SELECT value IN ('0'::jsonb, '"0"'::jsonb)
    FROM app_settings WHERE key = 'ai.daily_token_quota'), false)::text::jsonb
ON CONFLICT (key) DO NOTHING;
UPDATE app_settings SET value = '200000'::jsonb, updated_at = now()
WHERE key = 'ai.daily_token_quota' AND value IN ('0'::jsonb, '"0"'::jsonb);

ALTER TABLE devices ADD COLUMN metadata_reported_at timestamptz;
UPDATE devices SET metadata_reported_at = last_seen_at;
