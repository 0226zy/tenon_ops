# HTTP API

Base：`/api/ops/v1`  
认证：`__Host-tenon_ops_sid` cookie（HTTPS）  
写请求：必须带 `X-CSRF-Token`（从 `GET /auth/me` 获取）  
错误：`{"error":{"code":"...","message":"..."}}`  
时间：RFC3339 UTC；`from/to` 支持日期或 RFC3339  
分页：`?limit=50&cursor=...` → `{"items":[],"next_cursor":""}`

## 认证

```http
POST /api/ops/v1/auth/login
Content-Type: application/json

{"admin_id":"zhangyong","password":"..."}
```

```http
HTTP/1.1 204 No Content
Set-Cookie: __Host-tenon_ops_sid=...; Path=/; Secure; HttpOnly; SameSite=Strict
```

```http
GET /api/ops/v1/auth/me
```

```json
{
  "admin_id": "zhangyong",
  "display_name": "zhangyong",
  "role": "super_admin",
  "csrf_token": "...",
  "elevated": false,
  "environment": "tenon_prod",
  "mode": "remote",
  "exposure": "private",
  "version": "0.1.0",
  "require_step_up_writes": false
}
```

step-up（查看密码/导出前）：

```http
POST /api/ops/v1/auth/step-up
{"password":"..."}
```

## 用户

```http
GET /api/ops/v1/users?q=ali&status=1&pwd_state=managed&limit=50
```

```http
POST /api/ops/v1/users
Idempotency-Key: 01J...

{
  "user_id": "alice",
  "name": "Alice",
  "password": null,
  "max_conv": 20,
  "tags": ["team-a"],
  "note": "交付给客户 A"
}
```

`201`：

```json
{
  "user_id": "alice",
  "name": "Alice",
  "password": "24-char-generated",
  "max_conv": 20,
  "created_at": "2026-10-25T07:00:00Z"
}
```

密码只在创建响应中返回；之后必须 `POST /users/{id}/reveal-password`（step-up + reason）。

```http
POST /api/ops/v1/users/{id}/reset-password
{"password": null, "reason": "用户忘记密码"}
```

```http
POST /api/ops/v1/users/{id}/disable
{"reason": "客户停用"}
POST /api/ops/v1/users/{id}/enable
POST /api/ops/v1/users/{id}/sessions/revoke
POST /api/ops/v1/users/{id}/reveal-password
{"reason":"交付给客户 A"}
```

```json
{"user_id":"alice","password":"...","expires_in":30}
```

用户详情返回基本信息、最近对话/任务、30 天按模型用量。

## 模型

```http
GET /api/ops/v1/models
```

```http
POST /api/ops/v1/models/import
{"yaml":"models:\n  - id: deepseek\n    name: DeepSeek\n    protocol: openai\n    base_url: https://api.deepseek.com/v1\n    upstream_model: deepseek-chat\n    api_key_env: DEEPSEEK_API_KEY\n    default: true\n"}
```

```http
PATCH /api/ops/v1/models/{id}
{"display_name":"DeepSeek Chat","provider":"deepseek","notes":"...","enabled":true}
```

```http
POST /api/ops/v1/models/{id}/prices
{"effective_from":"2026-10-01T00:00:00Z","input_per_mtok":1.0,"cache_hit_per_mtok":0.02,"output_per_mtok":4.0,"peak_multiplier":2.0,"currency":"CNY"}
```

价格字段：

- `input_per_mtok`：缓存未命中输入，非高峰基础价
- `cache_hit_per_mtok`：缓存命中输入，非高峰基础价
- `output_per_mtok`：输出，非高峰基础价
- `peak_multiplier`：高峰倍率，默认 2.0

高峰为周一至周五 09:00–12:00、14:00–18:00（服务器时区，默认 Asia/Shanghai）；其他时间与周末按基础价。当前 Tenon `t_llm_usage` 没有 cache-hit token 列，成本暂按全部 prompt 未命中估算，缓存命中价待字段接入后自动生效。

模型 `ready` 固定 `"unknown"`（本版本未接控制层）。

## 用量

```http
GET /api/ops/v1/usage/overview?from=2026-10-01&to=2026-10-25&group_by=model
```

返回 `total`、`items`、`series`（按展示时区日界）。成本是价格表估算；`unpriced_requests` 表示缺价请求数。

```http
GET /api/ops/v1/usage/export?from=...&to=...
```

CSV；需要 step-up。单次最多 50,000 行；`Cache-Control: no-store`。

## 活跃用户

```http
GET /api/ops/v1/presence?window=15m
```

```json
{
  "as_of": "2026-10-25T07:00:00Z",
  "window_seconds": 900,
  "items": [
    {"user_id":"alice","name":"Alice","status":1,"tier":"online","last_seen":"...","active_runs":0,"sessions":1,"tokens_window":1200,"tokens_24h":9000,"model":"deepseek"}
  ]
}
```

`tier`：`running|online|active|recent|today|idle`。不是真实心跳。

## 任务与轨迹

```http
GET /api/ops/v1/runs?status=failed&limit=50
GET /api/ops/v1/runs/{run_id}
GET /api/ops/v1/runs/{run_id}/events?after_seq=0&limit=100
GET /api/ops/v1/runs/{run_id}/messages
GET /api/ops/v1/runs/{run_id}/usage
```

事件 `source=0` 为运行时事件（message/marker/notice），`source=1` 为控制事件（run_accepted/run_started/run_finished/...）。`payload` 超过 64KB 时返回截断字符串并带 `truncated:true`。

## 审计

```http
GET /api/ops/v1/audit?action=user.create&admin_id=zhangyong&result=1&limit=50
GET /api/ops/v1/audit/actions
GET /api/ops/v1/audit/export
```

导出需要 step-up。审计 detail 已脱敏，不记录密码/密文/密钥。

## 系统

```http
GET /api/ops/v1/system/health
GET /api/ops/v1/system/settings
PATCH /api/ops/v1/system/settings
{"ui.timezone":"Asia/Shanghai","presence.default_window":"15m"}
```

## curl 示例

```bash
BASE=https://admin.example.com
JAR=/tmp/tenon-ops.cookies

curl -sS -c "$JAR" -H 'Content-Type: application/json' \
  -d '{"admin_id":"zhangyong","password":"..."}' \
  "$BASE/api/ops/v1/auth/login" -o /dev/null -w '%{http_code}\n'

CSRF=$(curl -sS -b "$JAR" "$BASE/api/ops/v1/auth/me" | python3 -c 'import sys,json;print(json.load(sys.stdin)["csrf_token"])')

curl -sS -b "$JAR" -H "X-CSRF-Token: $CSRF" -H 'Content-Type: application/json' \
  -d '{"user_id":"alice","name":"Alice","max_conv":20}' \
  "$BASE/api/ops/v1/users"
```
