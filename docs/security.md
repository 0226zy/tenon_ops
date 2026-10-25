# 安全设计

## 1. 资产与信任边界

| 资产 | 位置 | 说明 |
|---|---|---|
| 管理员密码明文 | Mac `~/.tenon-ops/admin.secret`（0600） | 服务器/MySQL 不存 |
| 管理员 verifier | MySQL `tadm_admin` | `Argon2id(HMAC(server_pepper, password))` |
| server pepper / session / vault key | 服务器 `/etc/tenon-ops/keys/master.key`（0600/systemd credential） | 不在 DB |
| Tenon 用户密码明文 | 仅 reveal 时在服务端内存 | 不落日志 |
| Tenon 用户密码密文 | MySQL `tadm_user_secret` | AES-256-GCM；AAD 绑定 user_id/version |
| 会话 | 签名 cookie + `tadm_admin_session` | DB 行可吊销；无 session key 不能伪造 |
| 审计 | `tadm_audit_log` | 追加写；应用账号仅 INSERT/SELECT |

**服务器是信任边界**：拿到 `master.key` + DB 的人可以离线攻击管理员密码、解密用户密码、伪造会话。不要把管理端当成“数据库泄露也全安全”的系统；它主要保证**只拿 DB 不能登录/解密**。

## 2. 认证与会话

- 管理员密码长度 12–128 字节；bootstrap 生成 24 位随机密码。
- 登录：
  1. 服务器用 `master.key` 派生 pepper；
  2. `HMAC-SHA256(pepper, password)` → Argon2id（t=3, m=64MiB, p=2）；
  3. 常数时间比较 verifier；
  4. 检查 `Fstatus`/锁定；
  5. 签发 32B `sid`，cookie 为 `base64(payload).base64(HMAC(session_key,payload))`。
- cookie：`__Host-tenon_ops_sid`（HTTPS）、`Secure; HttpOnly; SameSite=Strict; Path=/`。
- 绝对 TTL 8h；空闲 30m；step-up 5m。
- 登录失败：DB 计数 + 内存计数；默认 5 次锁 15m；失败/成功均审计。
- 改密：重新生成 verifier，`Fsession_epoch++`，吊销全部会话；当前 cookie 清除。
- 会话校验同时检查：签名、过期、空闲、吊销、admin 状态、session epoch。
- 无 WebAuthn/TOTP：**这是 P0 的已知限制**；公网模式默认拒绝启动。

## 3. CSRF 与反代

- 写请求必须带 `X-CSRF-Token`；token = `HMAC(session_key, "csrf:"+sid)`，由 `/auth/me` 返回。
- `Origin` 必须在 `security.allowed_origins`；`Sec-Fetch-Site: cross-site` 拒绝。
- `Host` 必须在 `allowed_hosts`。
- 只信任 `security.trusted_proxies` 的 `X-Forwarded-For`；P0 默认 `127.0.0.1`（nginx 同机）。
- 管理端只绑定 loopback；7200 不得对公网开放。
- TLS 终止在 nginx/Caddy；HSTS 在 public 模式启用。

## 4. Tenon 用户密码

- 创建/改密：明文在内存中生成或接收，立即：
  - 生成 Tenon 兼容 Argon2id 哈希写入 `t_user.Fpwd_hash`；
  - AES-256-GCM 加密后写 `tadm_user_secret`；
  - 审计（不含密码）。
- 查看：
  - 必须 step-up；
  - 必须填写 reason（≥4 字符）；
  - 解密后仅在响应中出现 30 秒（`Cache-Control: no-store`）；
  - 审计记录原因、IP/UA、request_id，不记录密码。
- 停用/改密会提前过期该用户全部 `t_auth_session`；不会杀正在运行的 Run。
- 历史用户（CLI/注册创建）没有密文，显示“历史未知”；重置后纳入管理。

## 5. 数据库权限

- 管理端账号只授予：
  - `tenon_prod` SELECT；
  - `t_user` 身份列的 INSERT/UPDATE；
  - `t_auth_session.Fexpire_time` UPDATE；
  - `tenon_admin` 逐表最小权限；
  - 审计表仅 INSERT/SELECT。
- 不授予 DDL；不授予 `t_user` DELETE。
- MySQL 账号 host 必须匹配连接方式（localhost vs 127.0.0.1）。
- 数据库/密钥备份分开加密；密钥备份离线。

## 6. 已知限制（必须知道）

1. **无 MFA/WebAuthn**：公网只靠密码不安全；`exposure: public` 默认拒绝启动。使用 VPN/固定 IP 白名单。
2. **服务器 root/RCE = 全面失守**：可读 `master.key`、伪造会话、改代码；任何服务端 MFA 都挡不住 root。要防这一层需要客户端请求签名/远程证明，成本更高。
3. **直接写 `t_user`**：绕过 Tenon 的应用层逻辑；启动时检查列契约，但 Tenon 未来新增 NOT NULL 列仍需升级管理端。
4. **CLI/自注册旁路**：Tenon 现有 `control user add/disable/passwd` 或用户自助注册不会经过管理端，不会产生管理端审计；这些用户密码状态为 unknown。日常开通请只走管理端，CLI 仅 break-glass。
5. **活跃用户不是真实在线**：Tenon 没有 last_seen/IP/UA 会话字段；按对话/Run/用量活动分层。
6. **模型 readiness 未知**：管理端不读 control 进程环境；不猜测 `api_key_env` 是否存在。
7. **成本是估算**：来自价格表 × token；不是供应商账单，不处理缓存命中/阶梯价/汇率。
8. **reveal 限流是进程内的**：每管理员每小时 30 次，重启后重置；多实例部署需要改为共享限流。
9. **无 RBAC/多管理员**：只有 `super_admin`；表字段预留角色，但 API 未按角色裁剪。
10. **审计无哈希链**：追加写权限 + 备份；防篡改哈希链是 P1。

## 6a. 当前 nginx `/ops/` 部署

- 应用只监听 `127.0.0.1:7200`，由 nginx :80 的 `/ops/` 反代。
- `/ops/` 与 `/api/ops/v1/` 都配置了来源 IP allowlist；不是无限制公网。
- 仍是 HTTP：管理员密码和会话在源 IP 到服务器之间是明文。只应在固定出口 IP/可信网络下临时使用。
- 无 MFA；不要把 allowlist 改成 `allow all`。拿到域名/证书后应切 HTTPS，并优先补 WebAuthn/TOTP。
- 来源 IP 变化会导致 403；通过 SSH 修改 nginx allowlist 并 `nginx -t && systemctl reload nginx`。

## 7. 运维检查

- `tenon-ops doctor`：DB、schema 契约、管理库、密钥权限、公开 URL、数据新鲜度。
- 定期：
  - 备份 `tenon_admin`、`master.key`、`admin.secret`（分开）；
  - 检查登录失败/审计异常；
  - 轮换数据库口令；
  - 更新依赖并运行 `go test ./...`、`npm run build`。
- 泄露响应：
  - 只 DB 泄露：轮换 DB 口令/管理员密码；密钥未泄露时用户密码密文仍安全。
  - 服务器 key 泄露：轮换 `master.key`、重加密所有用户密码、重设管理员、吊销会话。
  - Mac `admin.secret` 泄露：`recover-admin` 重设密码并吊销会话。
