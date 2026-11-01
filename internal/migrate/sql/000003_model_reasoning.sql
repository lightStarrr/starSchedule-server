-- 思考强度能力：unknown 表示尚未探测，none 表示不支持，effort 表示可设置，
-- fixed 表示模型固定带思考、没有强度参数。
ALTER TABLE ai_provider_models
    ADD COLUMN IF NOT EXISTS reasoning_mode text NOT NULL DEFAULT 'unknown',
    ADD COLUMN IF NOT EXISTS reasoning_efforts text[] NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS reasoning_default text NOT NULL DEFAULT '';

ALTER TABLE ai_provider_models
    ADD CONSTRAINT ai_provider_models_reasoning_mode_check
        CHECK (reasoning_mode IN ('unknown', 'none', 'effort', 'fixed'));
