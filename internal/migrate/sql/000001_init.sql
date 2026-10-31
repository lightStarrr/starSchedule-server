-- 星课表后端初始化：设备、启动统计、AI 供应商/用量、管理端、配置。

CREATE TABLE IF NOT EXISTS devices (
    id                 uuid PRIMARY KEY,
    install_id         text        NOT NULL,
    android_id_hash    text        NOT NULL,
    platform           text        NOT NULL DEFAULT 'android',
    app_version        text        NOT NULL DEFAULT '',
    version_code       integer     NOT NULL DEFAULT 0,
    os_version         text        NOT NULL DEFAULT '',
    sdk_int            integer     NOT NULL DEFAULT 0,
    arch               text        NOT NULL DEFAULT '',
    status             text        NOT NULL DEFAULT 'active',
    ban_reason         text,
    banned_at          timestamptz,
    token_version      integer     NOT NULL DEFAULT 1,
    first_seen_at      timestamptz NOT NULL,
    last_seen_at       timestamptz NOT NULL,
    launch_count       bigint      NOT NULL DEFAULT 0,
    first_ip           text,
    last_ip            text,
    suspect_reinstall  boolean     NOT NULL DEFAULT false,
    extra              jsonb       NOT NULL DEFAULT '{}'::jsonb,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT devices_install_id_key UNIQUE (install_id),
    CONSTRAINT devices_status_check CHECK (status IN ('active', 'banned'))
);

CREATE INDEX IF NOT EXISTS devices_android_id_hash_idx ON devices (android_id_hash);
CREATE INDEX IF NOT EXISTS devices_last_seen_idx ON devices (last_seen_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS devices_first_seen_idx ON devices (first_seen_at);
CREATE INDEX IF NOT EXISTS devices_banned_idx ON devices (banned_at) WHERE status = 'banned';

-- 启动事件：按月分区。保留 DEFAULT 分区兜底，避免漏建分区导致写入失败。
CREATE TABLE IF NOT EXISTS launch_events (
    id           uuid        NOT NULL,
    device_id    uuid        NOT NULL,
    stat_date    date        NOT NULL,
    occurred_at  timestamptz NOT NULL,
    received_at  timestamptz NOT NULL DEFAULT now(),
    launch_type  text        NOT NULL DEFAULT 'cold',
    platform     text        NOT NULL DEFAULT 'android',
    app_version  text        NOT NULL DEFAULT '',
    os_version   text        NOT NULL DEFAULT '',
    arch         text        NOT NULL DEFAULT '',
    session_id   text        NOT NULL,
    extra        jsonb       NOT NULL DEFAULT '{}'::jsonb,
    PRIMARY KEY (id, stat_date),
    CONSTRAINT launch_events_session_key UNIQUE (device_id, session_id, stat_date)
) PARTITION BY RANGE (stat_date);

CREATE TABLE IF NOT EXISTS launch_events_default PARTITION OF launch_events DEFAULT;

CREATE INDEX IF NOT EXISTS launch_events_device_idx ON launch_events (device_id, occurred_at DESC);
CREATE INDEX IF NOT EXISTS launch_events_date_dim_idx ON launch_events (stat_date, platform, app_version);

-- 日 × 设备 粒度，WAU/MAU 的唯一事实来源。
CREATE TABLE IF NOT EXISTS daily_active_device (
    stat_date       date        NOT NULL,
    device_id       uuid        NOT NULL,
    platform        text        NOT NULL DEFAULT 'android',
    app_version     text        NOT NULL DEFAULT '',
    launch_count    integer     NOT NULL DEFAULT 1,
    first_launch_at timestamptz NOT NULL,
    last_launch_at  timestamptz NOT NULL,
    PRIMARY KEY (stat_date, device_id)
);

CREATE INDEX IF NOT EXISTS daily_active_device_device_idx ON daily_active_device (device_id, stat_date DESC);

CREATE TABLE IF NOT EXISTS daily_stats (
    stat_date    date PRIMARY KEY,
    dau          integer     NOT NULL DEFAULT 0,
    launches     bigint      NOT NULL DEFAULT 0,
    new_devices  integer     NOT NULL DEFAULT 0,
    ai_requests  integer     NOT NULL DEFAULT 0,
    ai_tokens    bigint      NOT NULL DEFAULT 0,
    updated_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS ai_providers (
    id               uuid PRIMARY KEY,
    name             text        NOT NULL,
    base_url         text        NOT NULL,
    api_key_cipher   bytea       NOT NULL,
    enabled          boolean     NOT NULL DEFAULT true,
    sort_order       integer     NOT NULL DEFAULT 0,
    timeout_seconds  integer     NOT NULL DEFAULT 120,
    last_check_at    timestamptz,
    last_check_ok    boolean,
    last_check_error text,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT ai_providers_name_key UNIQUE (name)
);

CREATE TABLE IF NOT EXISTS ai_provider_models (
    id             uuid PRIMARY KEY,
    provider_id    uuid        NOT NULL REFERENCES ai_providers (id) ON DELETE CASCADE,
    model_id       text        NOT NULL,
    alias          text        NOT NULL,
    display_name   text        NOT NULL DEFAULT '',
    context_window integer     NOT NULL DEFAULT 0,
    source         text        NOT NULL DEFAULT 'fetched',
    enabled        boolean     NOT NULL DEFAULT true,
    fetched_at     timestamptz NOT NULL DEFAULT now(),
    created_at     timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT ai_provider_models_key UNIQUE (provider_id, model_id),
    CONSTRAINT ai_provider_models_alias_key UNIQUE (provider_id, alias),
    CONSTRAINT ai_provider_models_source_check CHECK (source IN ('fetched', 'manual'))
);

CREATE INDEX IF NOT EXISTS ai_provider_models_provider_idx ON ai_provider_models (provider_id, enabled);

CREATE TABLE IF NOT EXISTS ai_usage_daily (
    stat_date        date        NOT NULL,
    device_id        uuid        NOT NULL,
    request_count    integer     NOT NULL DEFAULT 0,
    error_count      integer     NOT NULL DEFAULT 0,
    prompt_tokens    bigint      NOT NULL DEFAULT 0,
    completion_tokens bigint     NOT NULL DEFAULT 0,
    estimated_count  integer     NOT NULL DEFAULT 0,
    updated_at       timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (stat_date, device_id)
);

CREATE INDEX IF NOT EXISTS ai_usage_daily_device_idx ON ai_usage_daily (device_id, stat_date DESC);

CREATE TABLE IF NOT EXISTS ai_request_log (
    id                uuid        NOT NULL,
    request_id        text        NOT NULL,
    device_id         uuid,
    admin_id          uuid,
    stat_date         date        NOT NULL,
    provider_id       uuid,
    model             text        NOT NULL,
    stream            boolean     NOT NULL DEFAULT false,
    source            text        NOT NULL DEFAULT 'device',
    status            text        NOT NULL DEFAULT 'ok',
    http_status       integer     NOT NULL DEFAULT 200,
    error_code        text,
    prompt_tokens     integer     NOT NULL DEFAULT 0,
    completion_tokens integer     NOT NULL DEFAULT 0,
    total_tokens      integer     NOT NULL DEFAULT 0,
    estimated         boolean     NOT NULL DEFAULT false,
    first_token_ms    integer,
    total_ms          integer,
    created_at        timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id, stat_date)
) PARTITION BY RANGE (stat_date);

CREATE TABLE IF NOT EXISTS ai_request_log_default PARTITION OF ai_request_log DEFAULT;

CREATE INDEX IF NOT EXISTS ai_request_log_device_idx ON ai_request_log (device_id, created_at DESC);
CREATE INDEX IF NOT EXISTS ai_request_log_date_idx ON ai_request_log (stat_date);

CREATE TABLE IF NOT EXISTS app_settings (
    key        text PRIMARY KEY,
    value      jsonb       NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS admin_users (
    id            uuid PRIMARY KEY,
    username      text        NOT NULL,
    password_hash text        NOT NULL,
    role          text        NOT NULL DEFAULT 'admin',
    created_at    timestamptz NOT NULL DEFAULT now(),
    last_login_at timestamptz,
    CONSTRAINT admin_users_username_key UNIQUE (username)
);

CREATE TABLE IF NOT EXISTS admin_audit_logs (
    id             uuid PRIMARY KEY,
    admin_id       uuid,
    admin_username text        NOT NULL DEFAULT '',
    action         text        NOT NULL,
    target_type    text        NOT NULL DEFAULT '',
    target_id      text        NOT NULL DEFAULT '',
    detail         jsonb       NOT NULL DEFAULT '{}'::jsonb,
    ip             text        NOT NULL DEFAULT '',
    created_at     timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS admin_audit_logs_created_idx ON admin_audit_logs (created_at DESC);

CREATE TABLE IF NOT EXISTS job_state (
    name            text PRIMARY KEY,
    last_run_at     timestamptz,
    last_success_at timestamptz,
    last_error      text
);

-- 默认参数：仅作为兜底，后台修改后以 app_settings 为准。
INSERT INTO app_settings (key, value) VALUES
    ('ai.daily_token_quota',   '200000'::jsonb),
    ('ai.rpm_per_device',      '20'::jsonb),
    ('ai.inflight_per_device', '2'::jsonb),
    ('ai.global_concurrency',  '50'::jsonb),
    ('ai.max_output_tokens',   '4096'::jsonb),
    ('device.register_per_ip_hour', '60'::jsonb),
    ('retention.launch_event_days', '90'::jsonb),
    ('retention.ai_log_days',       '30'::jsonb)
ON CONFLICT (key) DO NOTHING;
