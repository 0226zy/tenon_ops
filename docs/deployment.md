# 部署（远程服务器）

目标拓扑：

```text
Mac 浏览器 ──HTTPS──> nginx/Caddy :443 ──> tenon-ops 127.0.0.1:7200
                                               ├── MySQL tenon_admin
                                               └── MySQL tenon_prod（只读 + 用户身份写）
```

P0 **不要**把 7200 直接暴露公网；用 VPN / 固定 IP 白名单。公网必须 MFA，而本版本尚未实现 WebAuthn/TOTP，因此 `exposure: public` 默认拒绝启动。

## 1. 构建

在 Mac 或 Linux 构建：

```bash
make web
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags '-s -w' -o bin/tenon-ops-linux ./cmd/tenon-ops
```

产物是单二进制（内嵌前端），不需要在服务器装 Node。

## 2. 服务器准备

```bash
sudo useradd --system --home /var/lib/tenon-ops --shell /usr/sbin/nologin tenon-ops
sudo install -d -m 0700 -o tenon-ops -g tenon-ops /etc/tenon-ops /etc/tenon-ops/keys
sudo install -d -m 0700 -o tenon-ops -g tenon-ops /var/lib/tenon-ops /var/log/tenon-ops
sudo install -m 0755 bin/tenon-ops-linux /usr/local/bin/tenon-ops
```

MySQL 8 与 Tenon 业务库已存在。管理库 DDL：

```bash
# 用有 CREATE 权限的 DBA 账号（也可由 DBA 手工执行 migrations/000001_init.mysql.sql）
TENON_OPS_DSN='root:...@tcp(127.0.0.1:3306)/?parseTime=true&loc=UTC&charset=utf8mb4' \
  tenon-ops init-db -config deploy/config.example.yaml
```

应用账号（示例；完整模板见 `migrations/000001_init.mysql.sql` 末尾）：

```sql
CREATE USER 'tenon_ops_app'@'127.0.0.1' IDENTIFIED BY '<strong-random>';
GRANT SELECT ON `tenon_prod`.* TO 'tenon_ops_app'@'127.0.0.1';
GRANT INSERT (`Fuser_id`,`Fname`,`Fpwd_hash`,`Fstatus`,`Fmax_conv`,`Fcreate_time`,`Fmodify_time`)
  ON `tenon_prod`.`t_user` TO 'tenon_ops_app'@'127.0.0.1';
GRANT UPDATE (`Fname`,`Fpwd_hash`,`Fstatus`,`Fmax_conv`,`Fmodify_time`)
  ON `tenon_prod`.`t_user` TO 'tenon_ops_app'@'127.0.0.1';
GRANT UPDATE (`Fexpire_time`) ON `tenon_prod`.`t_auth_session` TO 'tenon_ops_app'@'127.0.0.1';
GRANT SELECT, INSERT, UPDATE ON `tenon_admin`.`tadm_admin` TO 'tenon_ops_app'@'127.0.0.1';
GRANT SELECT, INSERT, UPDATE, DELETE ON `tenon_admin`.`tadm_admin_session` TO 'tenon_ops_app'@'127.0.0.1';
GRANT SELECT, INSERT, UPDATE, DELETE ON `tenon_admin`.`tadm_admin_credential` TO 'tenon_ops_app'@'127.0.0.1';
GRANT SELECT, INSERT, UPDATE ON `tenon_admin`.`tadm_tenon_user` TO 'tenon_ops_app'@'127.0.0.1';
GRANT SELECT, INSERT, UPDATE, DELETE ON `tenon_admin`.`tadm_user_secret` TO 'tenon_ops_app'@'127.0.0.1';
GRANT SELECT, INSERT, UPDATE ON `tenon_admin`.`tadm_model` TO 'tenon_ops_app'@'127.0.0.1';
GRANT SELECT, INSERT ON `tenon_admin`.`tadm_model_price` TO 'tenon_ops_app'@'127.0.0.1';
GRANT SELECT, INSERT, UPDATE, DELETE ON `tenon_admin`.`tadm_usage_daily` TO 'tenon_ops_app'@'127.0.0.1';
GRANT SELECT, INSERT, UPDATE, DELETE ON `tenon_admin`.`tadm_operation` TO 'tenon_ops_app'@'127.0.0.1';
GRANT SELECT, INSERT, UPDATE ON `tenon_admin`.`tadm_setting` TO 'tenon_ops_app'@'127.0.0.1';
GRANT SELECT, INSERT, UPDATE, DELETE ON `tenon_admin`.`tadm_export_job` TO 'tenon_ops_app'@'127.0.0.1';
GRANT SELECT, INSERT ON `tenon_admin`.`tadm_schema_migration` TO 'tenon_ops_app'@'127.0.0.1';
GRANT SELECT, INSERT ON `tenon_admin`.`tadm_audit_log` TO 'tenon_ops_app'@'127.0.0.1';
```

> 如果通过 SSH 隧道 bootstrap，服务器侧看到的是 `127.0.0.1`；如果用 Unix socket，则用 `@'localhost'`。两个 host 的 GRANT 需分别执行。

## 3. 配置

```bash
sudo cp deploy/config.example.yaml /etc/tenon-ops/config.yaml
sudo cp deploy/env.example /etc/tenon-ops/env
sudo chown root:tenon-ops /etc/tenon-ops/env
sudo chmod 0640 /etc/tenon-ops/env
sudo editor /etc/tenon-ops/env          # 写入强随机数据库口令
sudo editor /etc/tenon-ops/config.yaml  # 修改 public_url/allowed_origins/allowed_hosts/tenon_schema
```

至少确认：

- `public_url` / `allowed_origins` / `allowed_hosts` 与 nginx 域名一致。
- `db.admin_schema` / `db.tenon_schema` 正确。
- `security.keys_dir` 是 `/etc/tenon-ops/keys`。
- `exposure: private`；只允许 VPN/白名单。

## 4. 创建管理员（在 Mac 上生成，密码不经过服务器）

```bash
# Mac 建隧道（仅 bootstrap）
ssh -N -L 13306:127.0.0.1:3306 root@<server>

# 使用同一份 config，但覆盖 keys 输出目录到 Mac 的 bootstrap 目录
TENON_OPS_DSN='tenon_ops_app:<password>@tcp(127.0.0.1:13306)/tenon_admin?parseTime=true&loc=UTC&charset=utf8mb4' \
  ./bin/tenon-ops init-admin -config deploy/config.example.yaml \
  -admin-id zhangyong \
  -keys-dir ./tenon-ops-bootstrap/keys \
  -secret-dir ~/.tenon-ops

# 只复制 master.key 到服务器
scp ./tenon-ops-bootstrap/keys/master.key root@<server>:/etc/tenon-ops/keys/master.key
ssh root@<server> 'chown tenon-ops:tenon-ops /etc/tenon-ops/keys/master.key && chmod 600 /etc/tenon-ops/keys/master.key'
```

- `admin.secret` 留在 Mac（`~/.tenon-ops/admin.secret`，0600）。服务器上不应存在该文件。
- 终端打印的密码只出现一次；记录到 Mac 的密码管理器。
- 复制后删除本机 `tenon-ops-bootstrap/keys/master.key`，或加密归档到离线介质。

## 5. systemd + nginx

```bash
sudo cp deploy/tenon-ops.service /etc/systemd/system/tenon-ops.service
sudo cp deploy/nginx.conf /etc/nginx/sites-available/tenon-ops.conf
sudo ln -s /etc/nginx/sites-available/tenon-ops.conf /etc/nginx/sites-enabled/tenon-ops.conf
sudo cp deploy/nginx-limit.conf /etc/nginx/conf.d/tenon-ops-limit.conf

sudo nginx -t && sudo systemctl reload nginx
sudo systemctl daemon-reload
sudo systemctl enable --now tenon-ops
sudo systemctl status tenon-ops --no-pager
```

`deploy/nginx.conf` 中的 `allow 203.0.113.10` 必须替换为你的办公/VPN 出口；不要 `allow all`。

## 5a. 无域名/TLS 的私有访问（当前推荐）

如果没有域名/证书，不要直接把 7200 暴露公网。服务已经绑定 `127.0.0.1:7200`，从 Mac 建 SSH 隧道即可：

```bash
ssh -i ~/.ssh/mac1.pem -N -L 7200:127.0.0.1:7200 ubuntu@<server>
# 浏览器打开 http://127.0.0.1:7200
```

此时服务器配置应使用：

```yaml
mode: remote
exposure: private
listen: 127.0.0.1:7200
public_url: http://127.0.0.1:7200
security:
  cookie_secure: false
  allowed_origins: ["http://127.0.0.1:7200"]
  allowed_hosts: ["127.0.0.1:7200"]
```

这是“远程部署 + 端到端加密隧道”，不是公网 HTTPS。等有域名/证书后，改成：

```yaml
public_url: https://admin.example.com
security:
  cookie_secure: true
  allowed_origins: ["https://admin.example.com"]
  allowed_hosts: ["admin.example.com"]
```

再启用 nginx server block（见第 5 节），并重启 `tenon-ops`。

## 6. 首次登录与自检

```bash
sudo -u tenon-ops /usr/local/bin/tenon-ops doctor -config /etc/tenon-ops/config.yaml
curl -fsS http://127.0.0.1:7200/healthz
```

浏览器打开 `https://admin.example.com`，用 Mac 上的管理员密码登录。

## 7. 快速冒烟

1. 总览加载，无 5xx。
2. 创建一个 Tenon 用户；用该用户直接 `POST /api/login` 到 Tenon，期望 204；然后登出。
3. 用户详情 → 查看密码 → step-up + reason → 显示 30 秒。
4. 用量页有 `t_llm_usage` 数据；模型目录导入成功。
5. 任务页能打开一个 Run，轨迹工具调用/结果可读。
6. 审计页能看到创建/查看/登录记录。

## 8. 升级与回滚

```bash
sudo install -m 0755 tenon-ops-linux /usr/local/bin/tenon-ops
sudo systemctl restart tenon-ops
```

- 升级前备份 `tenon_admin` 与 `master.key`。
- 新版本不应自动执行 DDL；schema 由 DBA 审核后执行。
- 回滚保留上一版二进制与配置；不改 `master.key` 即可继续解密。

## 9. 密钥与恢复

- `master.key` 丢失：管理员登录不可用，Tenon 用户密码密文不可解。只能生成新 key、重设 verifier、逐个重置 Tenon 用户密码。
- Mac 丢失：服务器 key/DB 完好；在服务器执行 `tenon-ops recover-admin -admin-id <id>`，新密码抄回 Mac。
- 怀疑 key/DB 泄露：轮换 `master.key`（重加密所有用户密码）、重设管理员密码、吊销全部会话；评估 Tenon 用户密码是否全部重置。
- 备份：`mysqldump tenon_admin` 加密；`master.key` 单独离线加密备份；二者分开存放。

## 10. 公网限制

本版本 `exposure: public` 会拒绝启动（除非 `security.allow_public_without_mfa: true`）。原因：没有 WebAuthn/TOTP，公网只靠密码风险过高。推荐：

- 用 Tailscale/WireGuard 或云安全组固定 IP；
- 或先实现 WebAuthn/TOTP（P1）再开放 443。

如果只是短期内部试用并接受风险：

```yaml
exposure: public
security:
  allow_public_without_mfa: true
```

同时必须启用 nginx 白名单、强密码、限流，并监控登录失败。
