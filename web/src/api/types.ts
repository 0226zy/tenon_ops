export interface Page<T> { items: T[]; next_cursor: string }

export interface Me {
  admin_id: string
  display_name: string
  role: string
  csrf_token: string
  elevated: boolean
  elevated_until?: string
  environment: string
  mode: string
  exposure: string
  version: string
  require_step_up_writes: boolean
}

export interface PublicConfig {
  product_name: string
  version: string
  environment: string
  mode: string
  exposure: string
}

export interface User {
  user_id: string
  name: string
  status: number
  status_text: string
  max_conv: number
  created_at: string
  last_seen?: string
  sessions: number
  active_runs: number
  conversations: number
  tokens_30d: number
  pwd_state: 'unknown' | 'managed' | 'delivered_removed'
  pwd_managed: boolean
  tags: string[]
  note: string
  created_by?: string
  last_reveal_at?: string
  reveal_count: number
  disabled_reason?: string
}

export interface ConversationBrief {
  conv_id: string
  title: string
  model: string
  state: number
  last_active_at: string
  created_at: string
}

export interface Run {
  run_id: string
  conv_id: string
  user_id: string
  user_name: string
  conv_title: string
  model: string
  status: number
  status_text: string
  active: boolean
  iterations: number
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
  commit?: string
  error_code?: string
  error_message?: string
  started_at?: string
  ended_at?: string
  created_at: string
  duration_ms: number
}

export interface UsageAgg {
  user_id?: string
  model?: string
  conv_id?: string
  requests: number
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
  errors: number
  error_rate: number
  cost: number
  priced_requests: number
  unpriced_requests: number
  avg_ms: number
}

export interface UsagePoint {
  day: string
  model?: string
  requests: number
  tokens: number
  errors: number
  cost: number
}

export interface UserDetail extends User {
  recent_conversations: ConversationBrief[]
  recent_runs: Run[]
  usage_by_model: UsageAgg[]
}

export interface CreateUserResult {
  user_id: string
  name: string
  password?: string
  max_conv: number
  replayed?: boolean
  created_at: string
}

export interface ResetResult { user_id: string; password?: string; replayed?: boolean }
export interface RevealResult { user_id: string; password: string; expires_in: number }

export interface Model {
  model_id: string
  display_name: string
  provider: string
  upstream_model: string
  base_url: string
  api_key_env: string
  tags: string[]
  context_window: number
  max_output: number
  enabled: boolean
  is_default: boolean
  source: string
  notes: string
  synced_at?: string
  created_at: string
  modified_at: string
}

export interface Price {
  id: number
  model_id: string
  effective_from: string
  input_per_mtok: number
  output_per_mtok: number
  currency: string
  created_by: string
  created_at: string
}

export interface ModelView extends Model {
  price?: Price
  requests_24h: number
  tokens_24h: number
  errors_24h: number
  cost_24h: number
  ready: 'unknown' | 'ready' | 'missing_env'
  ready_basis: string
  last_used?: string
}

export interface UsageOverview {
  from: string
  to: string
  total: UsageAgg
  items: UsageAgg[]
  series: UsagePoint[]
  group_by: string
}

export interface PresenceItem {
  user_id: string
  name: string
  status: number
  tier: 'running' | 'online' | 'active' | 'recent' | 'today' | 'idle'
  last_seen?: string
  active_runs: number
  sessions: number
  tokens_window: number
  tokens_24h: number
  model?: string
}

export interface PresenceResult {
  as_of: string
  window_seconds: number
  items: PresenceItem[]
}

export interface Event {
  seq: number
  run_id?: string
  source: number
  type: string
  payload: unknown
  created_at: string
  truncated?: boolean
}

export interface Message { msg_id: string; role: number; content: string; created_at: string }
export interface UsageRow {
  created_at: string
  user_id: string
  conv_id: string
  run_id: string
  model: string
  prompt_tokens: number
  completion_tokens: number
  cost_ms: number
  http_status: number
  cost: number
  priced: boolean
}

export interface RunDetail { run: Run; messages: Message[]; usage: UsageRow[] }

export interface AuditEntry {
  id: number
  ts: string
  admin_id: string
  action: string
  target_type: string
  target_id: string
  result: number
  ip: string
  ua: string
  request_id: string
  reason: string
  detail?: unknown
}

export interface Dashboard {
  users_total: number
  users_disabled: number
  users_new_7d: number
  users_unmanaged: number
  active_users_15m: number
  active_runs: number
  run_status_24h: Record<string, number>
  p95_duration_seconds: number
  today_requests: number
  today_tokens: number
  today_cost: number
  today_errors: number
  error_rate: number
  usage_series: UsagePoint[]
  top_models: UsageAgg[]
  top_users: UsageAgg[]
  recent_failures: Run[]
  as_of: string
  data_through?: string
}

export interface Check { name: string; status: 'ok' | 'warn' | 'error'; message: string }
export interface Health {
  ok: boolean
  version: string
  mode: string
  exposure: string
  environment: string
  listen: string
  public_url: string
  schema_version: string
  tenon_schema: string
  admin_count: number
  last_usage_at?: string
  last_event_at?: string
  checks: Check[]
  time: string
}

export interface Setting { key: string; value: string; modified_at: string }
