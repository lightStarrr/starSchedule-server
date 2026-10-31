export interface AdminProfile {
  id: string
  username: string
  role: string
}

export interface DayMetrics {
  dau: number
  launches: number
  new_devices: number
  ai_requests: number
  ai_tokens: number
}

export interface Overview {
  today: DayMetrics
  yesterday: DayMetrics
  wau: number
  mau: number
  date: string
}

export interface TrendPoint {
  date: string
  dau: number
  launches: number
  new_devices: number
  ai_requests: number
  ai_tokens: number
}

export interface DistributionItem {
  key: string
  count: number
}

export interface DeviceSummary {
  id: string
  install_id: string
  platform: string
  app_version: string
  version_code: number
  os_version: string
  sdk_int: number
  arch: string
  status: 'active' | 'banned'
  ban_reason?: string
  banned_at?: string
  first_seen_at: string
  last_seen_at: string
  launch_count: number
  last_ip?: string
  suspect_reinstall: boolean
}

export interface CursorPage<T> {
  items: T[]
  next_cursor: string
  has_more: boolean
  total_estimate?: number
}

export interface LaunchRecord {
  id: string
  device_id: string
  stat_date: string
  occurred_at: string
  received_at: string
  launch_type: string
  platform: string
  app_version: string
  os_version: string
  arch: string
  session_id: string
}

export interface DeviceDetail {
  device: DeviceSummary
  activity: { stat_date: string; launch_count: number }[]
  launches: LaunchRecord[]
  usage_today: {
    request_count: number
    prompt_tokens: number
    completion_tokens: number
    tokens: number
    estimated_count: number
  }
}

export interface Provider {
  id: string
  name: string
  base_url: string
  api_key_masked: string
  enabled: boolean
  sort_order: number
  timeout_seconds: number
  last_check_at?: string
  last_check_ok?: boolean
  last_check_error?: string
  created_at: string
}

export interface ModelInfo {
  id: string
  provider_id: string
  provider_name: string
  model_id: string
  alias: string
  display_name: string
  context_window: number
  source: 'fetched' | 'manual'
  enabled: boolean
  fetched_at: string
}

export interface ActiveAI {
  provider_id: string
  provider_name: string
  model_id: string
  model: string
  alias: string
  display_name: string
  context_window: number
}

/** 可调用模型的扁平视图（供应商与模型都启用） */
export interface CatalogEntry {
  provider_id: string
  provider_name: string
  id: string
  model_id: string
  alias: string
  display_name: string
  context_window: number
}

export interface CatalogResponse {
  items: CatalogEntry[]
  /** 当前对外生效的模型；未配置时 id / alias 为空串 */
  active: { id: string; alias: string }
}

export interface UsageByDay {
  date: string
  request_count: number
  tokens: number
  errors: number
}

export interface UsageByDevice {
  device_id: string
  install_id: string
  request_count: number
  tokens: number
  errors: number
}

export type SettingKey =
  | 'ai.daily_token_quota'
  | 'ai.rpm_per_device'
  | 'ai.inflight_per_device'
  | 'ai.global_concurrency'
  | 'ai.max_output_tokens'
  | 'device.register_per_ip_hour'
  | 'retention.launch_event_days'
  | 'retention.ai_log_days'

export interface SettingsPayload {
  overrides: Partial<Record<SettingKey, number>>
  effective: Record<SettingKey, number>
  defaults: Record<SettingKey, number>
}
