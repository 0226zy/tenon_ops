> **实现对应说明**：本文是产品/功能设计基线。实现命名为 **tenon-ops**，API 前缀为 `/api/ops/v1`；设计中出现的 `tenon-admin` 命令对应 `tenon-ops`，`/api/admin/v1` 对应 `/api/ops/v1`。数据库 schema 仍为 `tenon_admin`。  
> 当前实现范围：P0 核心（登录/用户/模型用量/活跃/轨迹/审计/设置），**未实现 WebAuthn/TOTP MFA**，`exposure: public` 默认拒绝；模型 readiness 固定“未知”。以 README 与 docs 为准。

# Tenon 管理后台（Tenon Admin）产品与功能设计

> 版本：v0.2（远程部署修订）  
> 日期：2026-10-25  
> 分析对象：Tenon 仓库（Tenon 原型工坊，Go control plane + Vue 3 + MySQL 8）  
> 本文回答三件事：**产品设计、详细功能设计、功能流程设计**；并补齐通用管理后台应有的身份、审计、权限、数据与接口设计，作为后续实现基线。

> **v0.2 关键变更**：管理端从“只跑在 Mac 本机”改为“**部署在远程服务器**，通过 HTTPS 访问；Mac 只作为浏览器与管理员密码明文副本的保管处”。因此管理员验证用的 pepper、会话签名密钥、用户密码解密的 master key 都需要放在**服务器**的受保护密钥文件中（不再依赖 Mac 本机）。MySQL 仍然只存验证器和密文，不存管理员密码。远程部署带来的信任边界变化见 **§4.1.7**，旧版本中“本机/SSH 隧道”的描述以本版为准。

**阅读路径**

- 想快速判断方案是否可行：读 **§0 结论**、**§4.1 管理员认证**、**§4.1.7 远程信任边界**、**§4.2 用户管理**、**§7.3 创建用户流程**。
- 想评审产品范围：读 **§2 通用能力基线**、**§3 产品设计**、**§11 待确认决策**。
- 想直接实现：读 **§5 数据设计**、**§6 接口设计**、同目录 `schema.sql`、**§9 实施计划**。
- 想评估安全：读 **§4.1.6 威胁模型**、**§4.1.7 远程信任边界**、**§8.2 安全要求**、**§10 风险**。

**目录**

0. 先说结论  
1. Tenon 现状、约束与设计假设  
2. 通用管理后台需要哪些功能  
3. 产品设计  
4. 详细功能设计（认证 / 用户 / 模型用量 / 活跃 / 轨迹 / 审计 / 健康）  
5. 数据设计  
6. 接口设计  
7. 功能流程设计  
8. 非功能设计  
9. 实施计划与验收  
10. 风险与缓解  
11. 待确认决策  
附录 A–D：Tenon 数据字典 / Argon2id 兼容 / 术语 / 参考代码

---

## 0. 先说结论

1. **定位**：Tenon Admin 不是第二个 Tenon 前台，而是一个**远程部署、单人/小团队使用的运维控制台**：管身份、管模型与用量、看活跃用户、看 Agent 任务与执行轨迹、留审计。
2. **部署形态**：管理端进程跑在 **Tenon 所在/相邻的远程服务器**，只绑定 `127.0.0.1:7200`，由 nginx/Caddy 终止 TLS 后通过 `https://admin.example.com` 访问；**不直接暴露 7200**。Mac 只是浏览器和管理员密码明文副本的保管处，不是运行时依赖。数据库在服务器本机（或私有网络），不再需要 Mac 上的 SSH 隧道跑业务。
3. **管理员密码方案**：MySQL 只存 `salt + Argon2id(HMAC(pepper, password)) + 参数`；`pepper` 由服务器受保护密钥文件 `/etc/tenon-admin/keys/master.key` 派生，管理员密码明文只保存在 Mac 的 `admin.secret`。**只拿到数据库（dump/只读/写权限）不能登录，也不能离线爆破**；服务器密钥文件 + 数据库同时泄露才会被离线攻击。服务器 root/RCE 被控视为全面失守（可伪造会话/改代码），MFA 主要防凭据窃取与公网撞库，不防 root。远程场景用 HTTPS、IP 白名单/VPN、限流和 P1 WebAuthn 补强，见 §4.1.7。
4. **Tenon 用户创建**：直接事务写入 `tenon_prod.t_user`，`Fpwd_hash` 使用与 Tenon `auth.verify` 完全兼容的 Argon2id 格式；写库前后做 schema/权限/唯一性校验。这是“直接写入 Tenon 系统”的最短路径，不需要先改 Tenon。
5. **Tenon 用户密码**：管理端保存**可还原的明文**，但默认以 AES-256-GCM 密文落库，密钥在服务器受保护密钥文件中（`master.key` 派生）。对管理员来说仍是“可查看/可复制”的明文；对数据库备份/只读账号不可读。**远程部署意味着服务器是信任边界**：服务器密钥文件失守则用户密码可被解密；如果要求“服务器静态失守（只读文件/DB、不改代码）也不泄露”，需 P1 改为管理员密码派生 KEK + 恢复密钥（见 §4.1.7）。服务器 root/RCE 可在 step-up 时截获密码，不能靠密钥方案防住。若坚持字面明文入库，需要单独签字接受风险（见 §11）。
6. **读写边界**：P0 只写 `t_user` / `t_auth_session` 两张 Tenon 表；对话、Run、事件、用量、模型目录全部只读。**绝不直接改 `t_run`/`t_event`**，取消、重试、实时在线、模型就绪状态需要 P1 在 Tenon 侧加一个极小的管理只读/操作接口。
7. **数据边界**：复用同一 MySQL 实例，但使用**独立 schema `tenon_admin`**，与 Tenon 业务表分离；同一实例内可做跨 schema 事务，保证“建 Tenon 用户 + 存密码 + 写审计”原子提交。
8. **通用后台必备但原始需求没提的能力**：审计日志、RBAC-ready 权限模型、危险操作二次确认/step-up、幂等、系统健康与数据新鲜度、CSV 导出、服务器密钥备份/恢复。这些纳入 P0/P1，不做“只有五个页面”的薄壳。
9. **必须诚实的四件事**：Tenon 的 `t_auth_session` 没有 last_seen/IP/UA，**不能伪造“在线”**；“模型是否 ready”取决于 control 进程的环境变量，**管理端不能靠猜**；Run 的取消/重试不能靠改表实现；**远程管理端必须持有 pepper/vault key 才能校验/解密，服务器失守是最高风险**。产品上分别用“活跃分层”“未知/由控制层报告”“P1 管理接口”“VPN/白名单 + MFA + 密钥保护”处理。
10. **实施节奏**：P0 约 5 周（单人全栈，35 人天 ±30%，含 systemd/nginx/TLS/部署验收），先交付“能安全登录、能正确建用户、能看用量/活跃/轨迹、能审计”的闭环；P1 再加 WebAuthn/MFA、多角色、预算告警、Tenon 管理接口、批量导入。**若入口是公网，WebAuthn 必须提前到 P0（+5–7 人天）**。

### 0.1 原始需求 → 设计映射

| 原始需求 | 设计回答 | 章节 |
|---|---|---|
| 1. 管理员登录；创建一个管理员身份/密码；密码存 Mac；复用 Tenon MySQL；MySQL 不存管理员密码，只存可验证信息 | 单管理员 bootstrap；服务器 `master.key` 派生 pepper + Mac `admin.secret`；DB 存 `salt + Argon2id(HMAC(pepper,pwd))`；只拿 DB 不能登录 | §4.1、§4.1.7、§5.2、§5.3、§7.1、§7.2 |
| 2. 为 Tenon 创建用户、分配密码；直接写入 Tenon；密码明文存在管理系统 | 事务直插 `tenon_prod.t_user`（兼容 Argon2id）；管理端保存可还原密码（AES-GCM 密文 + 服务器密钥）；step-up + 审计查看 | §4.2、§5.3、§7.3、§7.4、§7.5 |
| 3. 查看当前接入的模型、模型 token 使用情况 | 模型目录镜像 + 价格表；按模型/用户/对话的请求/token/错误率/估算成本；诚实标注“就绪未知”与“成本为估算” | §4.3、§7.6、§7.7 |
| 4. 查看当前活跃用户 | 按业务活动分层（运行中/在线/活跃/最近/已登录未活跃）；明确不是“真实在线”；10 秒轮询 | §4.4、§7.8 |
| 5. 查看 Agent 任务与执行轨迹 | Run 列表/详情；`t_event` 时间线；工具调用/结果按 call_id 配对；实时跟随；错误归因 | §4.5、§7.9 |
| 通用后台还需要什么 | 身份/MFA/RBAC、审计、step-up、幂等、健康检查、导出、备份恢复 | §2、§4.6、§4.7、§9 |

---

## 1. Tenon 现状、约束与设计假设

### 1.1 系统结构

```text
Mac 浏览器 ──HTTPS──> nginx/Caddy :443 ──> tenon-admin :7200（127.0.0.1）
                                                   │
                                                   ├── tenon_admin（管理库）
                                                   └── tenon_prod（只读 + 用户身份写）
浏览器 ──> control :8080 ──> wsd ──> agent-runtime
              │  │
              │  └── LLM 代理 /llm/v1 ──> 上游模型
              ├── 对话、Run、事件、文件、预览、用量
              └── MySQL 8（tenon_prod / tenon）
```

- `cmd/control`：认证、会话、对话、Run 状态机、事件、文件、预览、模型目录、LLM 代理、用量记账。
- `cmd/wsd` + `agent-runtime`：每对话独立工作区、沙箱进程、ReAct 循环、工具调用。
- 生产库：`tenon_prod`；开发库：`tenon`（同一 MySQL 实例；管理端在服务器上通过 `127.0.0.1`/Unix socket 访问，Mac 只在 bootstrap/运维时用 SSH 隧道）。
- Tenon 应用账号：只有 `SELECT/INSERT/UPDATE`，没有 DDL/DELETE；建表迁移由管理员账号执行。
- 管理端：独立二进制/服务，nginx 终止 TLS，只监听 loopback；不直接暴露 7200。

### 1.2 与管理端直接相关的实现事实

| # | 事实 | 对管理端的含义 |
|---|---|---|
| 1 | 用户密码是 Argon2id，格式 `$argon2id$v=19$m=65536,t=2,p=2$<salt>$<hash>`，`auth.verify` 按此解析 | 管理端可以自己生成兼容 hash，直接插入 `t_user` |
| 2 | `t_user` 关键列：`Fuser_id`（唯一，1–64 位 `[A-Za-z0-9_.-]`）、`Fname`、`Fpwd_hash`、`Fstatus`（1 正常 / 2 禁用）、`Fmax_conv` | 创建/停用/改密都是对这几列的安全更新 |
| 3 | 登录失败按“用户 + IP”内存计数，5 次锁 15 分钟；Tenon 自带注册开放，按 IP 限流 | 管理端不能依赖 Tenon 的登录锁定来保护管理员；管理员登录自己限流 |
| 4 | 会话表 `t_auth_session` 只有 `Fsession_hash/Fuser_id/Fexpire_time/Fcreate_time`；没有 IP、UA、最后活跃时间 | **无法从 DB 判断“真正在线”**；只能按业务活跃度分层 |
| 5 | 注销/停用/改密通过提前 `Fexpire_time` 实现；应用账号无 DELETE | 管理端停用/改密时同样用过期会话，不删行 |
| 6 | `t_llm_usage` 每次上游请求一行：`Fconv_id/Frun_id/Fuser_id/Fmodel/Fprompt_tokens/Fcompl_tokens/Fcost_ms/Fhttp_status/Fcreate_time` | 用量/成本/错误率的主数据源；`Fhttp_status=0` 表示网络/流中断，不是 HTTP 0 |
| 7 | 模型目录是 YAML（`deploy/models.local.yaml`），声明 `api_key_env`，credential 只在 control 进程环境变量里；`/api/models` 返回 `ready/missing_env`，但需要登录 | 管理端不能看到密钥；P0 只能展示目录元数据，“就绪”必须标未知或接 P1 管理接口 |
| 8 | `t_run`：Run 状态机、`Factive`、`Fgeneration`、`Fiterations`、token 汇总、错误码、起止时间 | 任务列表/详情的主表；终态后 `Factive=NULL` |
| 9 | `t_event`：每对话一个 `Fseq` 序号域，`Fsource=0` 运行时事件、`1` 控制事件；payload 是完整 JSON | 执行轨迹可以按 `Fconv_id+Frun_id` 还原；工具调用/结果在 payload 里 |
| 10 | `t_message`：`Frole=1` 用户、`2` 助手；Run 终态时写入最终助手消息 | 轨迹详情可展示“用户输入 + 最终回答”，不必抓全部对话 |
| 11 | Run 状态：`queued/dispatching/running/cancelling/finalizing` 活跃，`finished/failed/cancelled` 终态 | 任务页的筛选、实时刷新、失败分析都按这套状态 |
| 12 | 没有管理员表、审计表、模型价格表、用户标签/备注表、按用户 token 配额表 | 这些是管理端要新增的 `tadm_*` 数据 |
| 13 | `t_run` 的取消是 `run.Service.Cancel`，有 owner 校验、停止确认、generation fencing | **不能直接 UPDATE `t_run` 取消**；P0 只读，P1 走 Tenon 管理接口 |
| 14 | `t_event` 的 payload 保存完整工具结果（UI 映射时截断） | 管理端轨迹可能遇到超大 payload，必须分页/截断/下载，不能一次性全查 |

依据代码：`migrations/000001_init.mysql.sql`、`internal/control/auth/auth.go`、`internal/control/store/auth.go`、`internal/control/llm/{catalog,proxy,usage}.go`、`internal/control/store/{run,event,conversation,sandbox}.go`、`internal/control/event/event.go`、`internal/control/consumer/consumer.go`、`pkg/contract/runtime.go`。

### 1.3 设计假设（若不成立，相关结论要调整）

| 编号 | 假设 | 影响 |
|---|---|---|
| A1 | 管理端跑在远程服务器，`127.0.0.1:7200` 监听，nginx/Caddy 终止 TLS；Mac 只是浏览器 + `admin.secret` 保管处 | 服务器需持有 pepper/session/vault 密钥；“密码只在 Mac”仍成立，但服务器是信任边界 |
| A2 | 复用同一 MySQL 实例，不新建实例；使用独立 schema `tenon_admin` | 跨 schema 单事务成立；若换实例需改成 saga 补偿 |
| A3 | P0 不改 Tenon 代码；只写 `t_user`/`t_auth_session` | 实时就绪、取消、真正在线只能 P1 做 |
| A4 | P0 只有一个超级管理员；表结构预留角色 | RBAC 可后置，不返工 |
| A5 | Tenon 用户密码需要“可再次查看/交付”，因此可还原存储 | 必须引入服务器主密钥 + AES-GCM；不接受则降级为“只显示一次” |
| A6 | 管理端一次只管理一个 Tenon 环境（`tenon_prod` 或 `tenon`） | UI 有环境徽标；避免误操作生产/开发 |
| A7 | 只支持 MySQL 8；不支持 SQLite/其他库 | 简化 DDL、时间、跨 schema 事务；测试可用 MySQL 容器 |
| A8 | 服务器是信任边界：能读 `master.key` 的人可校验/解密；Mac 的 `admin.secret` 只是记录 | 服务器需 systemd credential/0600；P1 加 WebAuthn/密码派生密钥 |
| A9 | P0 管理端不直接暴露公网：HTTPS + IP 白名单/VPN；若必须公网，MFA 进入 P0 | 决定安全范围与工期 |

### 1.4 设计原则

1. **先看后改**：所有破坏性操作显式分区、二次确认、可解释影响；能停用就不删除。
2. **最小权限**：管理端数据库账号只拿需要的库/表/动作；P0 不授予 `t_user` 的 DELETE，不授予任何 Tenon 业务表的写权限。
3. **密钥分离**：管理员验证器在 DB，pepper 在服务器受保护密钥文件；用户密码密文在 DB，主密钥在服务器密钥文件；管理员密码明文只在 Mac；密钥都不进日志、不进 Git。
4. **审计优先**：写操作与审计同事务；查看密码必须 step-up + 原因 + 审计；审计失败则操作失败（fail closed）。
5. **数据来源诚实**：每个页面标“数据截至”；“在线/就绪/成本”有明确定义，不用模糊词掩盖数据缺口。
6. **失败可诊断**：权限不足、schema 不匹配、反向代理/TLS 配置错误、密钥缺失，都给可执行的修复提示，不把 500 直接甩给用户。
7. **不过度建设**：不做多租户、支付、SSO、内容审核、Prompt 编辑；这些不是当前阶段的主要矛盾。
8. **远程先收紧入口**：默认不公网直连；HTTPS、白名单/VPN、限流、MFA 逐层加，不能把管理端当成普通网站。

---

## 2. 通用管理后台需要哪些功能

### 2.1 通用能力基线（AI Agent / 开发平台类）

| 能力域 | 通用后台通常需要 | 为什么需要 |
|---|---|---|
| 身份与访问 | 管理员账号、密码/MFA/SSO、RBAC、会话管理、登录限流、IP 白名单、break-glass | 后台拥有跨用户权限，必须比业务账号更强 |
| 用户与租户 | 用户 CRUD、状态、改密、配额/套餐、标签、批量导入、用户 360、代操作 | 客服/运营的日常主路径 |
| 产品与模型 | 模型/供应商目录、凭证引用、路由/降级、价格表、功能开关 | 模型是成本与体验的核心变量 |
| 用量与成本 | 请求/token/成本计量、趋势、Top N、预算、告警、账单/对账、导出 | 回答“钱花在哪、是否超支” |
| 运行与任务 | 队列、实时状态、重试/取消、日志/轨迹、失败归因、SLA | Agent 类系统的核心排障面 |
| 在线与运营 | 活跃用户、会话、并发、公告、运营触达 | 判断系统是否被使用、是否健康 |
| 可观测与告警 | 指标、日志、链路、SLO、告警通道、看板 | 出问题要能先发现再定位 |
| 治理与审计 | 审计日志、保留期、导出、双人审批、合规留痕 | 敏感操作可追溯，满足内控 |
| 平台与运维 | 迁移、备份/恢复、健康检查、配置、API Token/Webhook | 后台自身也要可运维 |
| 支持工具 | 用户检索、解锁、会话吊销、查看对话/Run、重置 MFA | 客服/值班的高频动作 |

### 2.2 Tenon 管理端的取舍

| 能力 | P0（本次） | P1 | P2 / 不做 | 取舍理由 |
|---|---|---|---|---|
| 管理员登录 | 远程部署、单管理员、服务器 pepper + DB verifier、会话/step-up、限流、HTTPS/白名单 | WebAuthn MFA、多管理员、RBAC | SSO/OIDC | 先解决“DB 泄露不能登录”，再解决公网暴露 |
| 用户管理 | 列表/详情/创建/改密/停用/启用/查看密码/标签备注 | 批量 CSV、改显示名、改 `max_conv`、会话吊销 | 硬删除、代登录 | 直接写 Tenon，先保证兼容与审计 |
| 模型目录 | YAML 导入、元数据/价格、用量关联 | Tenon 只读接口拿 `ready`、目录写回 | 自动路由/降级 | 不让管理端猜 control 的环境变量 |
| 用量与成本 | 请求/token/错误率/估算成本、趋势、Top N、CSV | 预算/告警、日汇总、价格历史重算 | 账单/对账、发票 | 先做“看得见”，再做“管得住” |
| 活跃用户 | 按业务活跃度分层、自动刷新 | Tenon 心跳/SSE 在线接口 | 地理分布/设备画像 | 现有表没有 last_seen，不能假装在线 |
| Agent 任务 | Run 列表、详情、事件轨迹、工具配对、错误提示、实时轮询 | 取消/重试、轨迹导出、失败聚合 | 直接编辑轨迹 | 不直接改 `t_run`，避免破坏状态机 |
| 审计 | 全量写操作/登录/查看密码审计、筛选、导出 | 哈希链、双人审批、归档 | 外部 SIEM | 后台的底线能力 |
| 健康与设置 | DB/反代/TLS/schema/密钥/数据新鲜度 | 控制层 metrics、告警 | 多环境切换 | 远程服务首先要能自诊断 |
| 权限 | 单 super_admin，表结构预留 | operator/support/auditor | ABAC | 避免一开始就过度设计 |
| 内容安全 | 不做 | 只读对话内容 | 审核/拦截 | 当前无此需求 |
| 多租户/计费 | 不做 | 不做 | 可另立项目 | 不是单人平台的主要矛盾 |

### 2.3 明确不做（P0）

- 不做 Tenon 前台替代：不发消息、不改文件、不操作预览。
- 不做 Prompt/技能/工作区编辑。
- 不做硬删除用户；不做“代登录”用户账号。
- 不做 Provider 密钥托管：密钥仍只在 Tenon control 的环境变量里，管理端只显示变量名。
- 不做直接改 `t_run`/`t_event`/`t_conversation` 的操作。
- 不做支付、发票、多租户、SSO、内容审核、功能开关平台。
- 不把“已登录”展示成“在线”，不把“目录里有模型”展示成“模型可用”。

---

## 3. 产品设计

### 3.1 定位与目标

**一句话**：给 Tenon 的 owner 一个“远程可访问、可信、可审计”的控制台，用 5 分钟完成用户开通，用 1 分钟定位一个失败任务，用一张图回答“今天花了多少 token、谁在用、模型是否正常”。

产品目标：

1. **身份可控**：管理员凭据不因数据库泄露而失守；Tenon 用户可开通、停用、改密，且立即生效。
2. **成本可见**：按模型/用户/对话看 token 与估算成本，能导出、能定位异常。
3. **运行可排障**：从活跃用户 → 对话 → Run → 工具调用/结果，形成可读的轨迹。
4. **操作可追溯**：所有敏感动作有审计，查看密码有 reason 和 step-up。
5. **边界可信**：页面明确标注数据新鲜度、就绪状态、成本口径，不用假数据撑门面。

非目标：替代 Tenon、做通用 BI、做多租户 SaaS 计费、做模型网关。

### 3.2 用户与角色

| 角色 | P0 | 说明 |
|---|---|---|
| `super_admin` | ✅ 唯一 | 全部权限；bootstrap 创建；可查看密码、改密、停用 |
| `operator` | 预留 | 用户开通/改密/停用、看用量/轨迹；默认不看密码明文 |
| `support` | 预留 | 只读用户/Run，重置密码/吊销会话；不看模型价格、不看审计全文 |
| `auditor` | 预留 | 只读审计、用量、用户列表；不能写入、不能看密码 |

P0 虽然只有一个管理员，但 API/表/前端路由都按“有角色”的方式写，避免 P1 改造成本。

### 3.3 信息架构

```text
Tenon Admin
├── 登录 / Step-up
├── 总览 Dashboard
├── 用户管理
│   ├── 用户列表
│   ├── 用户详情（概览 / 会话 / 任务 / 用量 / 安全 / 备注）
│   └── 新建用户（抽屉，三步）
├── 模型与用量
│   ├── 模型目录
│   ├── 模型详情（元数据 / 价格 / 用量 / 就绪）
│   └── 用量分析（趋势 / 分布 / Top N / 导出）
├── 活跃用户
├── Agent 任务
│   ├── 任务列表（运行中 / 失败 / 完成 / 全部）
│   └── 任务详情 + 执行轨迹
├── 审计日志
└── 设置
    ├── 我的管理员账号（改密 / 会话）
    ├── 数据源与密钥状态
    ├── 数据保留 / 刷新间隔 / 时区
    └── 关于 / 版本 / schema
```

### 3.4 核心对象

| 对象 | 来源 | 说明 |
|---|---|---|
| Admin | `tadm_admin` | 管理端账号；密码验证器；角色；锁定状态 |
| AdminSession | `tadm_admin_session` | 签名 cookie + DB 会话记录；可吊销；step-up 到期 |
| TenonUser | `tenon_prod.t_user` | 被管理的用户；管理端只读镜像 + 写身份字段 |
| UserProfile | `tadm_tenon_user` | 管理端标签、备注、密码状态、创建人 |
| UserSecret | `tadm_user_secret` | Tenon 用户密码的可还原密文（AES-GCM） |
| Model | `tadm_model` | 模型目录镜像（来自 YAML/手工），不含密钥 |
| ModelPrice | `tadm_model_price` | 按生效时间的输入/输出单价，用于估算成本 |
| UsageDaily | `tadm_usage_daily` | 按天/用户/模型的用量汇总（P1；P0 可实时查原始表） |
| Run | `tenon_prod.t_run` | Agent 任务；管理端只读 |
| Event | `tenon_prod.t_event` | 执行轨迹事件；管理端只读 |
| Message | `tenon_prod.t_message` | 用户输入/最终回答；只读 |
| Presence | 派生 | 由对话/Run/用量活动计算，不落库（P0） |
| AuditEntry | `tadm_audit_log` | 追加写审计 |
| Operation | `tadm_operation` | 写操作幂等记录 |
| Setting | `tadm_setting` | 非敏感配置 |

### 3.5 关键指标定义

| 指标 | 定义 | 注意 |
|---|---|---|
| 用户总数 | `t_user` 行数 | 含禁用用户 |
| 新增用户 | `Fcreate_time` 在区间内 | 自注册 + 管理员创建 |
| 活跃用户（15m） | 最近 15 分钟内有对话活动/Run/LLM 请求的**去重用户数** | 不是“在线”；页面写明窗口 |
| 运行中任务 | `t_run.Factive=1` | 含 queued/dispatching/running/cancelling/finalizing |
| 成功率 | `finished / (finished + failed)` | 取消单列；不要把取消算失败 |
| P95 耗时 | 终态 Run 的 `Fend_time - Fstart_time` 的 95 分位 | 样本量小于 20 时显示“样本不足” |
| 模型请求数 | `t_llm_usage` 行数 | 一个 Run 可能多次请求；不是 Run 数 |
| Token | `SUM(Fprompt_tokens + Fcompl_tokens)` | 上游未返回 usage 时为 0，需显示覆盖率 |
| 估算成本 | `prompt/1e6*输入单价 + completion/1e6*输出单价` | 未定价模型显示“未定价”，不按 0 计 |
| 错误率 | `http_status=0 或 >=400` 的请求占比 | `0` 是网络/流错误，不是 HTTP 0 |
| 数据新鲜度 | 最近一条 `t_llm_usage`/`t_event` 的时间 | 每页顶部展示“数据截至” |

### 3.6 页面清单

| 页面 | 目的 | 关键内容 | 优先级 | 刷新 |
|---|---|---|---|---|
| 登录 | 管理员认证 | 环境徽标、密码、锁定倒计时、无“找回密码”（CLI 恢复） | P0 | — |
| 总览 | 1 分钟看全局 | 用户/活跃/运行/Token/成本卡片、24h 趋势、Top 模型、最近失败 | P0 | 30s |
| 用户列表 | 找到人、开通人 | 搜索、状态/标签筛选、创建、分页、行操作 | P0 | 手动 |
| 用户详情 | 用户 360 | 概览、会话、任务、用量、安全、备注 | P0 | 30s |
| 新建用户 | 安全开通 | 三步表单、密码生成/强度、写库预览、成功交付 | P0 | — |
| 模型目录 | 看接入模型 | 元数据、来源、就绪（未知/已报告）、价格、近 24h 用量 | P0 | 手动 |
| 模型详情 | 价格与用量 | 价格历史、用量趋势、Top 用户 | P0 | 手动 |
| 用量分析 | 成本与异常 | 日期/模型/用户筛选、KPI、趋势、分布、Top N、导出 | P0 | 手动 |
| 活跃用户 | 当前谁在用 | 分层、窗口切换、自动刷新、操作 | P0 | 10s |
| 任务列表 | 找任务 | 状态/用户/模型/时间筛选、自动刷新活跃项 | P0 | 5s（仅活跃） |
| 任务详情/轨迹 | 排障 | 摘要、消息、事件时间线、工具配对、原始 JSON、跟随 | P0 | 2s（运行中） |
| 审计日志 | 追溯 | 操作者/动作/目标/时间/结果/详情、导出 | P0 | 手动 |
| 设置 | 自诊断 | 数据源、schema、密钥、隧道、保留期、时区 | P0 | 手动 |
| 管理员/权限 | 多管理员 | 账号、角色、会话 | P1 | — |
| 预算告警 | 控成本 | 预算、阈值、通知渠道 | P1 | — |

### 3.7 交互原则

- **环境永远可见**：顶栏固定 `tenon_prod` / `tenon` + 主机 + “身份可写 / 遥测只读”徽标，避免在开发/生产之间误操作。
- **危险操作分离**：创建/改密/停用/查看密码放在明确区域；用红色/警示文案；确认框写清“影响谁、立即生效、不可撤销点”。
- **秘密最小暴露**：密码只在创建成功/显式查看时出现；默认遮罩；30 秒自动隐藏；不在列表、导出、日志里出现。
- **数据新鲜度**：每个动态页面显示“数据截至 HH:mm:ss”；实时页失败时保留上次数据并标注“已过期”。
- **可复制标识**：`user_id`、`run_id`、`conv_id`、`model_id` 等用等宽字体 + 一键复制。
- **空状态解释定义**：没有活跃用户时说明“按最近 15 分钟业务活动计算，不含仅登录未操作”。
- **键盘可用**：搜索 `/`、刷新 `r`、关闭抽屉 `Esc`；所有操作可 Tab 到达。
- **中文优先**：界面中文，字段/ID 保留英文；时间显示 `Asia/Shanghai`，悬停显示 UTC/ISO。

### 3.8 低保真示意

**总览**

```text
┌ Tenon Admin ─ [tenon_prod] ─ DB 38ms ─ 数据截至 10:32:05 ─ admin ▾ ┐
│ 总览   用户   模型与用量   活跃用户   Agent 任务   审计   设置        │
├──────────────────────────────────────────────────────────────────────┤
│ [今日 ▾]                                                             │
│ ┌ 用户 128 ┐ ┌ 活跃 7 ┐ ┌ 运行中 2 ┐ ┌ 今日 Token 1.24M ┐ ┌ ¥3.42 ┐ │
│                                                                      │
│ 24h Token 趋势（按模型堆叠）           Run 状态分布                   │
│ ▁▂▃▅▆▇█▇▆▅▃▂                        finished 86% / failed 9% ...    │
│                                                                      │
│ Top 模型（Token）                    最近失败任务                     │
│ deepseek  820k  ████████             alice / RUN_TIMEOUT / 10:21     │
└──────────────────────────────────────────────────────────────────────┘
```

**新建用户**

```text
┌ 新建 Tenon 用户 ─────────────────────────────────────┐
│ ① 身份  ② 密码  ③ 确认                               │
│                                                       │
│ 用户 ID   [ alice                  ]  规则：1–64 位   │
│ 显示名    [ Alice                  ]                  │
│ 初始密码  [ •••••••••••••• ] [生成] [复制]           │
│ 最大会话  [ 20 ]        标签 [ team-a × ]            │
│ 备注      [ 交付给客户 A                            ] │
│                                                       │
│ ⚠ 将直接写入 tenon_prod.t_user，创建后立即生效        │
│ [取消]                                      [创建]   │
└───────────────────────────────────────────────────────┘
```

**任务轨迹**

```text
┌ Run 01JABCD...  [running]  alice  deepseek  02:13  ┐
│ tokens 12.4k  迭代 7  估算成本 ¥0.08   commit 9f3c │
├─────────────────────────────────────────────────────┤
│ 10:20:01  run_accepted                             │
│ 10:20:02  run_started                              │
│ 10:20:04  assistant  我会先检查项目结构…           │
│ 10:20:05  tool_call  bash  {"command":"ls"}        │
│ 10:20:06  tool_result ls → src/ package.json …     │
│ 10:20:09  assistant  接下来创建后端…               │
│ …                                                   │
│ [加载更多] [跟随实时] [原始 JSON] [复制 run_id]     │
└─────────────────────────────────────────────────────┘
```

---

## 4. 详细功能设计

### 4.1 管理员认证与服务器密钥

> **v0.2 前提**：管理端跑在远程服务器，Mac 只负责浏览器和 `admin.secret`。管理员登录校验、Tenon 用户密码解密都在服务器完成，因此服务器必须持有 pepper / vault key；这改变了 v0.1“密钥只在 Mac”的模型。**MySQL 仍然只存验证器和密文，不存管理员密码。**

#### 4.1.0 两种部署模式（同一套代码）

| 模式 | 运行位置 | 密钥位置 | 适用 | 说明 |
|---|---|---|---|---|
| **remote（v0.2 默认）** | 远程服务器 `127.0.0.1:7200` + nginx HTTPS | 服务器 `master.key`；Mac 只存 `admin.secret` | 日常运维、异地访问 | 本文主推；需 VPN/白名单或 MFA |
| local（v0.1 保留） | Mac 本机 `127.0.0.1:7200` | Mac `master.key`；Mac `admin.secret` | 单机离线、break-glass | 不暴露网络；数据库经 SSH 隧道 |

- 两种模式共用同一二进制、同一 schema、同一套 API；区别只在 `config.mode`、密钥路径、监听/反代。
- **一个管理库同一时间只跑一种模式**：不要 remote 与 local 同时写同一个 `tenon_admin`，否则 session/密钥/审计会混淆；需要切换时停一个、迁移 key/配置、吊销会话。
- 本文以下章节以 **remote** 为主；local 模式的差异是“密钥在 Mac、绑定 loopback、无 nginx”，其余产品/功能/数据设计相同。

#### 4.1.1 远程部署下的方案对比

| 方案 | MySQL 存什么 | 服务器存什么 | Mac 存什么 | 仅 DB 泄露 | 服务器失守 | 多设备登录 | 复杂度 | 结论 |
|---|---|---|---|---|---|---|---|---|
| **A. 服务器 pepper + DB verifier** | `salt + Argon2id(HMAC(pepper,pwd)) + 参数` | `master.key`（0600/systemd credential），派生 pepper/session/vault | `admin.secret`（明文记录） | 不能登录、难以离线爆破 | 可离线爆破管理员密码、可解密用户密码 | 可以（输入密码即可） | 中 | ✅ **P0（VPN/白名单）** |
| **B. A + WebAuthn/passkey** | A + WebAuthn 公钥/credential | A | `admin.secret` + Secure Enclave 私钥 | 不能登录 | 仅静态泄露（读密钥/DB、不能改代码）时缺私钥无法完成登录；root/RCE 仍可伪造会话 | 需要 passkey 同步/注册 | 中高 | ✅ **P1；公网必须** |
| **C. OPAQUE/PAKE** | OPAQUE registration record | 无长期 password-equivalent secret | 密码/客户端状态 | 不能离线爆破 | 只能在线猜测，受限于限流 | 可以 | 高 | P2（强合规场景） |
| D. 标准 Argon2 哈希 | `Argon2id(pwd)` | 无 | `admin.secret` | 可离线爆破 | 可离线爆破 | 可以 | 低 | 备选，不推荐 |
| E. 可逆加密 | `AES(pwd)` | 密钥 | 无 | 密钥泄露即明文 | 可解密 | 可以 | 低 | ❌ 违背约束 |

**P0 选 A**：满足“密码明文只在 Mac”“MySQL 只存可验证信息”；数据库写权限也不足以伪造登录（没有服务器 pepper 算不出 verifier）。代价是服务器是信任边界：拿到 `master.key` + DB 的人可以离线攻击管理员密码并解密用户密码。  
**暴露公网或要求更强边界时选 B**：WebAuthn 私钥在 Mac 的安全硬件/钥匙串里，服务器只有公钥。它能防凭据窃取/钓鱼/密码撞库，也让“只拿到服务器密钥和 DB、但不能改服务代码”的攻击者无法完成登录。但**取得服务器 root/RCE 的攻击者仍可伪造会话或改代码，WebAuthn 不解决 root 失守**；要防这一层需要客户端请求签名/远程证明，成本更高（P2）。密码 verifier 仍可能被离线爆破，MFA 会挡住实际登录。  
**P2 选 C**：OPAQUE 让服务器记录本身不支持离线字典攻击，适合“数据库 + 服务器都不信任”的极端要求，但实现/运维复杂度高。

#### 4.1.2 推荐方案（P0）

```text
Mac 浏览器                    远程服务器                                      MySQL
──────────                   ─────────────────────────────────────           ─────────────────
输入管理员密码 ──HTTPS──> nginx/Caddy :443
                              │ proxy_pass 127.0.0.1:7200
                              ▼
                         tenon-admin
                              │ 读 /etc/tenon-admin/keys/master.key（0600/systemd credential）
                              │ HKDF-SHA256(info="admin-pepper-v1") → K_pepper
                              │
                              │ HMAC-SHA256(K_pepper, password) = material
                              │ Argon2id(material, salt, t=3, m=64MiB, p=2, 32B)
                              └──────────────► 比较 tadm_admin.Fpwd_hash
                                               成功 → 签发签名 cookie（K_session）+ DB 会话行（可吊销）
```

登录时：

1. 浏览器 POST `https://admin.example.com/api/admin/v1/auth/login`；TLS 终止在 nginx/Caddy。
2. 服务端按 `Fadmin_id` 取管理员行；不存在则用 dummy verifier，保证耗时一致。
3. 读服务器 `master.key` → 派生 `K_pepper`；对输入密码做 `HMAC-SHA256`，再用该行 `salt/params` 做 Argon2id。
4. `subtle.ConstantTimeCompare` 比较；检查 `Fstatus`、锁定时间。
5. 成功签发会话（cookie 用 `K_session` 签名，DB 写 `tadm_admin_session` 供吊销）；失败计数/锁定 + 审计；错误统一“用户名或密码错误”。

要点：

- **算法只允许 `argon2id-pepper-v1`**。`Fpwd_algo` 只做版本校验；未知版本拒绝，防止降级到无 pepper 的算法。
- 管理员密码长度 12–128 字节；不强制字符组合，长度优先；禁止首尾空格。
- **服务器不存管理员密码明文**；`admin.secret` 只在 Mac，是恢复副本，登录不读它。
- 服务器失去 `master.key`：DB 里的 verifier 不可用 → 走 `recover-admin` 重设（§4.1.5），不是“忘记密码邮件”。
- 管理员密码强度直接决定“服务器 + DB 同时泄露”后的离线破解成本；P0 至少 20 位随机密码（bootstrap 生成 24 位）。

#### 4.1.3 服务器与 Mac 的文件布局

**服务器（运行时依赖）**

```text
/etc/tenon-admin/                       0700，tenon-admin:tenon-admin
├── config.yaml                         监听、schema、时区、保留期、反代信任
├── env                                 0600：TENON_ADMIN_DSN（含数据库口令）
└── keys/
    └── master.key                      32B，0600；HKDF 派生 pepper/session/vault/audit
/var/lib/tenon-admin/exports/           0700；导出文件，24h 清理
/var/log/tenon-admin/                   日志轮转，30 天
```

**Mac（只保留管理员密码记录）**

```text
~/.tenon-admin/                         0700，禁止放 iCloud/Dropbox/Git
├── admin.secret                        0600：管理员 ID + 明文密码 + 创建时间
└── bootstrap/                          一次性 bootstrap 产物；部署后删除/加密归档
```

> **Mac 副本的写入边界**：服务器不能写 Mac 文件。`init-admin --mode remote` 在 Mac 本地生成 `admin.secret`；管理员改密后，UI 只能一次性显示/下载新文件，由人保存到 Mac。若副本过期，登录不受影响，但“直接读文件找回密码”会失败，需走 `recover-admin`。FileVault + 锁屏是前提；P1 建议存 macOS Keychain。

启动时自检并**拒绝启动**的条件：

- 服务器密钥目录/文件权限宽于 `0700/0600`，或属主不是服务账号；
- `master.key` 不是 32 字节，或路径是符号链接；
- 监听地址不是 `127.0.0.1`（除非显式 `--allow-remote` 且已配置 TLS + 白名单）；
- 检测到 `admin.secret` 出现在服务器文件系统（这是错误部署，应告警）；
- 反向代理信任配置缺失（`trusted_proxies` 未设置时忽略 `X-Forwarded-For`）。

> **密钥派生**：P0 用单根服务器 `master.key` + HKDF 域分离（admin-pepper/session/vault/audit）。P1 建议拆成 `admin.key`、`session.key`、`vault.key` 三个独立文件，各自用 systemd credential 注入、各自轮换，减少单点暴露。
>
> **systemd**：`LoadCredential=master:/etc/tenon-admin/keys/master.key`，服务从 `$CREDENTIALS_DIRECTORY/master` 读取；比直接把路径写进环境变量更不容易被同机其他进程读取。若 systemd 版本不支持 credential，则用 0600 文件 + `ProtectSystem=strict` + 专用用户。

#### 4.1.4 会话、step-up 与 CSRF（HTTPS 前提）

- 会话：32B 随机 `sid` + 服务器 `K_session` 签名；cookie `__Host-tadm_sid`，`Secure; HttpOnly; SameSite=Strict; Path=/`。**HTTPS 是硬前提**；HTTP 只允许 `127.0.0.1` 健康检查。
- 同时写 `tadm_admin_session`：`Fsid_hash=SHA256(sid)`、过期时间、吊销位、IP、UA、`Felevated_until`。DB 行只用于吊销/审计，不能单独伪造 cookie。
- TTL：绝对 8 小时；空闲 30 分钟；step-up 5 分钟。
- 登出：吊销当前会话 + 清 cookie + 审计；改密：`Fsession_epoch++`，吊销全部会话。
- step-up：敏感操作前重输密码（启用 WebAuthn 时还要 assertion）；成功写 `Felevated_until`（5 分钟）。
  - P0 默认：查看密码、批量导出、改管理员密码。
  - 公网模式扩展到所有身份写操作：创建用户、重置密码、停用/启用、吊销会话、模型价格/导入、设置修改。这样即使会话 cookie 被窃，攻击者也不能直接开通/改密用户。
- 权限不足或未 step-up 时返回 `STEP_UP_REQUIRED`，前端弹重认证，不静默失败。
- CSRF：`/auth/me` 返回 `csrf_token = HMAC(K_session, "csrf:"+sid)`；写请求带 `X-CSRF-Token`；服务端重算 + `Origin` 白名单（`https://admin.example.com`）+ `SameSite=Strict`。
- 反向代理：只信任来自 loopback 的代理；P0 默认 nginx 与管理端同机。若分离，`trusted_proxies` 只配 nginx 的私网 IP，绝不信公网来源；真实客户端 IP 从受信 `X-Forwarded-For` 取，用于限流/审计；禁止客户端直连 7200。
- 限流：登录/step-up 5 次/15 分钟；reveal 30 次/小时；导出 10 次/小时；nginx 层再加 `limit_req`。
- HSTS、TLS 1.2+、禁用弱套件；CSP 见 §8.2。

#### 4.1.5 远程初始化、恢复与轮换

**推荐：在 Mac 上生成，密码不经过服务器**

```bash
# 0) DBA 在服务器建 schema/账号/目录（schema.sql）
# 1) Mac 上通过 SSH 隧道连服务器 MySQL（仅 bootstrap 用）
ssh -N -L 13306:127.0.0.1:3306 root@<server>
# 2) Mac 生成密码、verifier、master.key，并写 DB（密码只在本机）
tenon-admin init-admin --mode remote \
  --db "tenon_admin_app:...@tcp(127.0.0.1:13306)/tenon_admin" \
  --admin-id zhangyong --out ./tenon-admin-bootstrap
# 3) 把 bootstrap/server/master.key 安全复制到服务器
scp ./tenon-admin-bootstrap/server/master.key root@<server>:/etc/tenon-admin/keys/master.key
# 4) 服务器配置 systemd + nginx，启动
systemctl enable --now tenon-admin
# 5) Mac 浏览器访问 https://admin.example.com，用终端打印的密码登录
```

- `init-admin --mode remote` 在本机生成 `admin.secret`、`master.key`、verifier；通过隧道写 `tadm_admin`；输出 `bootstrap/server/master.key`。服务器从未看到明文密码。
- 复制后删除本机 `bootstrap/server/`（或加密归档）；`admin.secret` 留在 Mac。
- 若无法在 Mac 生成（例如没有隧道），可回退为服务器执行 `init-admin --password-stdin --no-local-secret`：服务器生成 key/verifier，密码只打印一次，由人抄到 Mac；此时密码会经过服务器进程内存，安全等级略低，需在审计中记录。

**恢复**

- 忘记密码：先读 Mac 的 `admin.secret`；文件丢失则在服务器执行 `tenon-admin recover-admin --admin-id ...`（需要 `master.key` + DB 写权限），重设 verifier、生成新密码并打印；吊销全部会话、审计。
- 服务器 `master.key` 丢失：生成新 master key、重设 verifier；**已有 Tenon 用户密码密文不可解**（除非 vault key 有独立备份），只能逐个重置用户密码。
- Mac 丢失：`admin.secret` 丢失，但服务器 key/DB 完好；用 `recover-admin` 重设管理员密码即可，用户密码不受影响。
- 服务器损坏：恢复 `tenon_admin` 备份 + `master.key` 备份；`admin.secret` 从 Mac 恢复。两者缺一不可。

**轮换**

- 管理员改密：pepper 不变 → 新 salt/verifier → 更新 DB → 服务器**无法直接写 Mac**，UI 一次性显示/下载新的 `admin.secret`，由管理员保存到 Mac → `session_epoch++` → 审计。Mac 副本可能暂时过期，不影响登录，但会影响“忘记密码时直接读文件”的恢复路径；下次登录前应更新。
- `master.key` 轮换：生成新 key → 解密所有 Tenon 密码并用新 key 重加密 → 重算管理员 verifier（pepper 变了）→ 部署新 key → 吊销会话 → 审计。失败保留旧 key 与旧 verifier 的恢复包。
- P1 拆钥后：`admin.key` 轮换只影响管理员登录；`vault.key` 轮换只重加密用户密码；`session.key` 轮换使全部会话失效。轮换低峰执行，备份先于删除。

#### 4.1.6 威胁模型（远程）

| 威胁 | 结果 | 缓解 |
|---|---|---|
| 只拿到 MySQL dump（含 `tadm_admin`/`tadm_user_secret`） | 不能登录（无 pepper）；不能解密用户密码（无 master key）；管理员 verifier 不能离线爆破（无 pepper） | 密钥不在 DB；DB 账号最小权限；备份加密 |
| 只有 DB 写权限 | 不能伪造会话（`K_session` 在服务器）；不能算 verifier（无 pepper）；可造成 DoS | 签名 cookie；算法白名单；审计告警 |
| 服务器文件系统/进程被攻破 | 拿到 `master.key` + DB → 可离线爆破管理员密码、解密用户密码、伪造会话 | 服务器是信任边界；systemd credential/0600；最小化服务；P1 WebAuthn + 密码派生 vault；网络隔离 |
| 公网暴力破解 | 管理员账号被撞库/爆破 | HTTPS、强密码、Argon2id、限流、IP 白名单/VPN；公网必须 MFA |
| 反向代理配置错误 | IP 白名单失效、`X-Forwarded-For` 伪造、TLS 降级 | 只信任 loopback 代理；固定 `trusted_proxies`；强制 HTTPS/HSTS；配置测试 |
| 会话 cookie 被窃 | 冒用至过期/吊销 | `__Host-` + Secure + HttpOnly + SameSite=Strict；8h TTL；可吊销；改密全吊销 |
| Mac 被攻破 | 拿到 `admin.secret`/浏览器会话；可登录管理端 | FileVault/锁屏/Keychain；短会话；异常登录告警；改密/吊销 |
| 服务器 root/云厂商控制台 | 完全失守（等同 Tenon 被控） | 这是不可防御边界；靠云账号 MFA、审计、最小权限、离线备份 |
| 备份泄露 | 密文 + 可能含 key | DB 备份与 key 备份分开加密；恢复演练；访问审计 |
| 管理端 RCE | 读 key、写 DB | 最小依赖、CSP、无 shell 调用、只读挂载、非 root 服务账号、`govulncheck` |

#### 4.1.7 远程暴露与 MFA 决策（关键）

**先明确一句话**：远程部署后，服务器必须能校验管理员密码（pepper）并解密 Tenon 用户密码（vault key）。因此“只拿到数据库不能登录/解密”仍然成立，但**“服务器被攻破也不泄露”不成立**。这是架构事实，不是实现缺陷。

按暴露程度选控制：

| 暴露方式 | P0 必须 | 建议 | 说明 |
|---|---|---|---|
| 仅 VPN/Tailscale/WireGuard，或固定 IP 白名单 | HTTPS + 强密码 + 限流 + 服务器密钥保护 | WebAuthn P1 | 最小攻击面，P0 可接受 |
| 公网 HTTPS，无 VPN | **WebAuthn/passkey 或等效 MFA 必须进 P0** | 密码 + WebAuthn + IP 白名单 | 仅有密码不够；公网爆破/撞库风险高 |
| 公网 + 高合规/高价值 | 密码 + WebAuthn + 密码派生 vault key + 审计外送 | OPAQUE P2 | 静态泄露（读文件/DB、不改代码）时仍不直接泄露用户密码；root 失守仍需客户端签名/远程证明 |

**P1 WebAuthn 设计要点**：

- 管理员注册 passkey 后，服务器只存 credential 公钥/ID/sign_count；私钥在 Mac 的 Secure Enclave/iCloud Keychain。
- 登录 = 密码验证 + WebAuthn assertion；**不做 passwordless-only**，否则拥有 DB 写权限的攻击者可以直接插入自己的 credential 绕过密码。WebAuthn 只作为第二因素。
- step-up 也要求 assertion；passkey 丢失时用恢复码 + 密码，恢复码一次性、离线保存。
- 在“攻击者只拿到服务器 `master.key` + DB、但不能改服务代码/伪造会话”的前提下，没有 Mac 私钥不能完成登录；密码 verifier 仍可能被离线爆破，但 MFA 阻断实际会话。若攻击者取得 root/RCE，则可以直接伪造会话，WebAuthn 不提供保护。
- 至少注册两把 passkey（Mac + 备用硬件钥匙），并提供一次性恢复码；恢复码只存离线。
- WebAuthn RP ID/Origin 固定为 `admin.example.com` / `https://admin.example.com`；不要用 IP 或临时域名，否则 passkey 失效；更换域名需要重新注册。
- 新增表（P1）：`tadm_admin_credential`（`Fadmin_id`、`Fcredential_id`、`Fpublic_key`、`Fsign_count`、`Ftransports`、`Faaguid`、`Fcreated_at`、`Flast_used_at`）。

**P1 密码派生 vault key（可选，强边界）**：

- 不再把长期 `vault.key` 放服务器；随机 data key 用管理员密码派生的 KEK 包裹，存 `tadm_vault_key`；step-up 时在内存解包，用完即弃。
- 管理员改密时用旧密码解包、新密码重包；另存离线 recovery key 包裹一份，防止忘记密码导致全员密码不可恢复。
- 服务器静态失守（只读文件/DB、未主动改代码）且管理员不在线/密码未泄露时，用户密码密文仍受保护；离线爆破仍需先攻破管理员密码（Argon2id）。root/RCE 可以在 step-up 时截获密码，不能防。
- 代价：reveal 依赖管理员 step-up、改密需 rewrap、恢复流程更复杂；建议 P1 再做。

**“密码在 Mac”的准确含义**：管理员密码**明文**只在 Mac；服务器只有 pepper + verifier；MySQL 只有 salt + verifier。服务器不保存、也不需要知道管理员密码明文。

### 4.2 用户管理

#### 4.2.1 用户列表

列：`user_id`、显示名、状态、`max_conv`、创建时间、最近活跃、会话数、活跃 Run、近 30 天 Token、密码状态（可查看/已交付清除/历史未知）、标签、操作。

筛选：关键词（ID/名称，前缀优先）、状态、标签、密码状态、最近活跃窗口。分页：keyset（`Fid` 或 `Fcreate_time+Fid`），默认 50，最大 100。

操作：查看详情、重置密码、停用/启用、查看密码（step-up）、复制 ID、打开任务/用量（带筛选）。

#### 4.2.2 新建用户

三步抽屉：

1. **身份**：`user_id`、显示名、`max_conv`、标签、备注。
2. **密码**：默认生成 24 位 URL-safe 随机密码（约 144 bit）；可自定义；显示强度/长度；可复制；提示“创建后仍可查看，但每次查看会审计”。
3. **确认**：展示将执行的写库动作（`tenon_prod.t_user` 插入、`tenon_admin` 两条记录、审计），确认后创建。

校验规则（与 Tenon 保持一致）：

| 字段 | 规则 | 说明 |
|---|---|---|
| `user_id` | `^[A-Za-z0-9_.-]{1,64}$`；忽略大小写唯一 | MySQL 默认 `utf8mb4_0900_ai_ci` 下 `Alice/alice` 冲突；预检要按 collation 实际行为 |
| `name` | ≤64 个 Unicode 字符；空则用 `user_id` | 与 Tenon `Register` 一致 |
| `password` | 8–128 字节；建议 ≥12；不 trim | Tenon 登录不校验规则，但保持与注册/CLI 一致 |
| `max_conv` | 1–1000，默认 20 | `Fmax_conv` 在创建对话时做上限判断，0 会导致无法建对话 |
| `tags` | ≤10 个，每个 ≤32 字符 | 仅管理端使用 |
| `note` | ≤255 字符 | 仅管理端使用 |

创建成功返回：`user_id`、`name`、`password`（仅在创建响应中一次性返回，`Cache-Control: no-store`）、`created_at`。UI 显示“复制交付”面板；离开后需走“查看密码”（step-up + reason）。

#### 4.2.3 写库映射（与 Tenon 完全兼容）

目标 SQL（同一事务，跨 schema）：

```sql
INSERT INTO `tenon_prod`.`t_user`
  (`Fuser_id`,`Fname`,`Fpwd_hash`,`Fstatus`,`Fmax_conv`,`Fcreate_time`,`Fmodify_time`)
VALUES (?,?,?,1,?,?,?);
-- Fcreate_time/Fmodify_time 传 Go 的 time.Now().UTC()，与 Tenon 的 CreateUser 一致
```

Argon2id 生成（必须与 Tenon `verify` 的解析格式一致）：

```text
salt   = crypto/rand 16 bytes
key    = argon2.IDKey([]byte(password), salt, time=2, memory=64*1024 KiB, threads=2, keyLen=32)
hash   = "$argon2id$v=19$m=65536,t=2,p=2$" + base64.RawStdEncoding(salt) + "$" + base64.RawStdEncoding(key)
```

- 参数从 Tenon 配置读取（默认 `t=2,m=65536,p=2`）；hash 里自带参数，所以即使 Tenon 以后调整默认参数，旧 hash 仍可验证。
- 写库前校验 `Fpwd_hash` 长度 ≤255；实测字符串约 97 字符。
- 生产库的 `t_user` 可能还有未来新增列；写库前 `SELECT` 列清单，若出现未知 NOT NULL 无默认列则**失败并要求升级管理端**（schema 契约检查），不要盲插。

#### 4.2.4 密码的可还原存储

| 项 | 设计 |
|---|---|
| 存储位置 | `tenon_admin.tadm_user_secret.Fciphertext` |
| 算法 | AES-256-GCM；`K_vault = HKDF-SHA256(服务器 master.key, info="tenon-user-pwd-v1")` |
| Nonce | 每条 12B 随机 |
| AAD | `v1 / tenon_user_id / key_version`，防止密文被复制到另一个用户 |
| 字段 | `Fkey_version`、`Fnonce`、`Fciphertext`；密码状态在 `tadm_tenon_user.Fpwd_state`（managed / delivered_removed / unknown） |
| 查看 | step-up + reason + 审计；只在服务器内存解密；响应 `no-store`；UI 30 秒自动隐藏 |
| 清除 | 支持“交付后清除密文”（`Fpwd_state=delivered_removed`），之后只能重置密码 |
| 日志 | 任何路径都不得记录明文、密文、nonce、密钥；错误只记录 `user_id` 和错误码 |

> **v0.2 信任边界**：远程管理端要能在 reveal 时解密，所以 `vault.key` 必须在服务器可读。DB 单独泄露仍然安全（只有密文）；服务器被攻破则用户密码可被解密。若要求“服务器静态失守（只读文件/DB、不改代码）也不泄露”，P1 改为“管理员密码派生 KEK + 包裹 data key + 离线 recovery key”（§4.1.7）；root/RCE 可在 step-up 时截获密码，不能防。
>
> 需求原文是“明文存储在管理系统”。本设计将其解释为“管理员可还原查看的明文”，但物理上加密落库。若要求 `SELECT` 直接看到明文，见 §11 的 P0 决策；那会把数据库备份、只读账号、慢查询日志都变成密码泄露面，不建议。

#### 4.2.5 重置密码、停用、启用

**重置密码**（step-up + reason）：

```sql
UPDATE `tenon_prod`.`t_user`
   SET `Fpwd_hash`=?, `Fmodify_time`=?
 WHERE `Fuser_id`=?;

UPDATE `tenon_prod`.`t_auth_session`
   SET `Fexpire_time`=?
 WHERE `Fuser_id`=?;
```

- 同时 upsert `tadm_user_secret`（新密文）与 `tadm_tenon_user`（`Fpwd_state=managed`），写审计。
- 旧密码立即失效；用户已登录的浏览器下一次请求 401；正在跑的 Run 不受影响（Run 是服务端状态，不依赖浏览器会话）。
- 返回新密码一次；UI 提示“旧密码和所有会话已失效”。
- **并发说明**：Tenon 的会话不绑定密码版本。若用户在改密提交的瞬间完成登录，理论上可能出现一个仍有效的旧密码会话；管理端在提交后再补一次“过期该用户全部会话”作为收敛，并记录审计。彻底解决需要 P1 在 Tenon 侧增加 session epoch / password version 校验。

**停用**：

```sql
UPDATE `tenon_prod`.`t_user` SET `Fstatus`=2,`Fmodify_time`=? WHERE `Fuser_id`=?;
UPDATE `tenon_prod`.`t_auth_session` SET `Fexpire_time`=? WHERE `Fuser_id`=?;
```

- Tenon 登录校验 `Status=1`；已登录请求经 `UserBySession` 也会因 join 条件失败而 401。
- 已运行的 Run 不会被杀；若需要立即中止，P1 走 Tenon 管理接口取消。
- 停用原因写 `tadm_tenon_user.Fdisabled_reason`，审计。

**启用**：只改 `Fstatus=1`，不自动改密；提示“用户可用旧密码登录，建议同时重置”。

#### 4.2.6 查看密码

流程：点击“查看” → 若 step-up 已过期，弹窗重输管理员密码 → 填写 reason（必填，≥4 字）→ 服务端解密 → 返回 `{password, expires_in: 30}` → UI 显示/复制 → 30 秒自动隐藏。

- 限制：每个管理员每小时最多 30 次 reveal；同一用户连续 reveal 有 1 秒节流。
- 审计：`user.password.reveal`，记录 reason、结果、IP/UA；**不记录密码**。
- 失败：密文损坏 → `VAULT_DECRYPT_FAILED`，提示“请重置密码”；不要回退显示空/乱码。

#### 4.2.7 历史用户与自注册用户

- Tenon 的 `t_user` 里可能有管理员创建之前的用户（CLI/注册）。管理端首次打开时同步列表：`tadm_tenon_user.Fpwd_state=unknown`，不显示“查看密码”，只显示“密码未知（历史用户）”。
- 管理员可以对这些用户“重置密码”以纳入管理；重置后写入密文，状态变 `managed`。
- 自注册用户同样处理；管理端不尝试撞库/猜密码。
- 用户名冲突：创建时若已存在（按 collation），返回 409，并给“查看已有用户/重置其密码”的入口。

**旁路风险（必须明确）**：Tenon 现有 CLI `control user add/disable/passwd` 直接写 `t_user`，不经过管理端。因此：

- 管理端不会知道这些用户的密码（`Fpwd_state=unknown`），也不会产生管理端审计；
- P0 约定：**日常开通只走管理端，CLI 仅作 break-glass**；管理端启动/定时扫描 `t_user` 与 `tadm_tenon_user` 的差异，把未纳管用户标为 unknown 并提示；
- P1 若要强制治理，需要给 CLI 增加审计，或限制/废弃 CLI 的用户写路径（需改 Tenon）；仅靠管理端无法审计旁路写。

#### 4.2.8 批量与导出（P1）

- CSV 导入：列 `user_id,name,password,max_conv,tags,note`；先 dry-run，逐行校验，展示冲突/弱密码/非法 ID；确认后逐行事务写入；输出成功/失败报告。
- 导出用户清单：默认不含密码；可选“含密码交付包”，必须 step-up + 二次确认 + 审计，文件加密（zip + 密码）并 24 小时后删除。
- 批量停用：需输入数量确认；禁止“全选所有用户一键停用”这种高风险操作。

#### 4.2.9 边界与失败处理

| 场景 | 行为 |
|---|---|
| `user_id` 已存在（大小写不同） | 409 `USER_EXISTS`，展示已有用户链接；不自动覆盖 |
| 无 `INSERT` 权限 | 失败并提示具体 GRANT；不落半条数据（事务回滚） |
| `t_user` 出现未知 NOT NULL 列 | 失败 `TENON_SCHEMA_MISMATCH`，提示升级管理端 |
| 审计写失败 | 整个创建/改密/停用回滚（fail closed） |
| 响应丢失后前端重试 | 用 `client_request_id` 幂等；重试返回同一结果或“已存在，请查看” |
| 用户正在跑 Run 时被停用 | 身份立即失效；Run 继续，页面提示；需要中止走 P1 取消 |
| 密码超过 128 字节/含前后空格 | 拒绝并提示；不静默 trim |
| `max_conv=0` | 拒绝（Tenon 会导致无法创建对话） |
| 数据库连接中断 | 页面横幅“数据源不可用”；所有写按钮禁用；不做自动重试写 |

### 4.3 模型与用量

#### 4.3.1 模型目录：管理端镜像，不是第二套真相

- Tenon 的实际模型目录是 control 读取的 YAML（热加载）。管理端**不直接改这个文件**（P0），而是导入/镜像到 `tadm_model`，用于展示、计价、关联用量。
- 来源两种：
  1. **导入 YAML**：上传或指定服务器路径；严格按 Tenon 的字段解析（`id/name/tags/desc/default/protocol/base_url/api_key_env/upstream_model/max_tokens/demo`）；拒绝未知字段；不导入任何密钥，只保留 `api_key_env` 变量名。
  2. **手工登记**：用于 Tenon 目录不可达时补录；标记 `Fsource=manual`。
- 导入时展示 diff：新增、删除、元数据变化、默认模型变化；确认后 upsert；被删除的模型标 `Fenabled=0` 而不是物理删除（保留历史用量关联）。
- 页面显著标注：“目录镜像，不改变 Tenon 路由；实际生效以 control 的 YAML 为准。”

#### 4.3.2 “就绪”状态必须诚实

| 状态 | 含义 | P0 展示 |
|---|---|---|
| `已报告就绪` | 来自 Tenon 管理接口的 `ready=true`（P1） | 绿色 + 来源时间 |
| `已报告缺失凭证` | 来自 Tenon 管理接口的 `ready=false, missing_env` | 黄色 + 变量名 |
| `未知` | 管理端看不到 control 进程环境（P0 默认） | 灰色 + “需接控制层” |
| `目录已移除` | 镜像里不存在/被禁用 | 灰色 |

- P0 **不要**用管理端自己的环境变量推断 control 的 `ready`，两者可能是不同机器/不同进程。
- P1 在 Tenon 增加 `GET /internal/admin/v1/models`（loopback + token），直接复用 `Catalog.List()` 的 `ready/missing_env`，管理端只读展示。
- P1 可加“主动探测”（用上游 `/models` + 密钥）但会消耗额度/泄露调用模式，默认关闭。

#### 4.3.3 价格与估算成本

- 价格是**管理端数据**，不写 Tenon：`tadm_model_price(model_id, effective_from, input_per_mtok, output_per_mtok, currency)`。
- 一条用量在时间 T 的成本 = 使用 `effective_from <= T` 的最近一条价格；没有价格 → `NULL`，页面显示“未定价”，并统计“未定价请求占比”。
- 价格变更用新行（生效时间），不覆盖历史；修改历史价格需要显式“重算”任务并审计。
- 跨价格生效日的统计区间必须按每条用量的时间匹配价格；日汇总要按价格区间拆分，或在价格变更后重算受影响日期，不能用一个“当前价”乘整段 token。
- 日汇总的 `Fcost` 只累计“已定价请求”的成本，用 `Fpriced_requests / Frequests` 表示覆盖；覆盖不全时显示“部分未定价”，不要用 0 掩盖缺失价格。
- 成本是**估算**，不等于供应商账单：缓存命中、阶梯价、汇率、免费额度都不在 `t_llm_usage` 里。页面标注“估算值，用于趋势与预算，不作为结算依据”。
- 币种默认 `CNY`；价格精度 `DECIMAL(18,6)`；展示保留 2–4 位。

```text
cost = prompt_tokens / 1e6 * input_price_per_mtok
     + completion_tokens / 1e6 * output_price_per_mtok
```

#### 4.3.4 用量分析口径

- 数据源：`tenon_prod.t_llm_usage`；一次上游请求一行。
- 维度：时间、模型、用户、对话、Run、HTTP 状态。
- 指标：请求数、prompt/completion/total tokens、错误请求数/错误率、平均/ P95 代理耗时（`Fcost_ms`）、估算成本、未定价占比。
- `Fhttp_status`：`200` 成功；`0` 网络/流中断；`4xx/5xx` 上游状态。错误请求也可能有 token（上游已计费），不要默认排除。
- 一个 Run 有多次请求：Run 详情显示 `t_run.Fprompt_tokens/Fcompl_tokens`（Tenon 汇总）与 `t_llm_usage` 明细，两者可能因上游未返回 usage 而不一致；页面并列展示并注明来源。
- 时间边界：DB 存 UTC；查询用半开区间 `[from, to)`；展示按 `Asia/Shanghai`；按天聚合用 `CONVERT_TZ(Fcreate_time,'+00:00','+08:00')`（或应用层转换，避免 MySQL 时区表缺失）。
- 数据新鲜度：显示 `MAX(Fcreate_time)`；若落后超过 5 分钟，标注“可能延迟”。

#### 4.3.5 查询、汇总与索引

- P0 规模（十万级用量行）直接查原始表 + 60 秒内存缓存即可；P1 引入 `tadm_usage_daily` 日汇总（按 `Fday/Fuser_id/Fmodel`），用 `Fid` 水位增量构建，价格变化时重算受影响日期。
- 现有索引：`t_llm_usage(idx_user_time)`、`idx_conv_time`、`idx_run`；没有 `(Fmodel,Fcreate_time)`。P0 不做模型维度的全表扫描；先用时间范围收窄，再在应用层聚合。若数据量增大，向 DBA 申请加索引或读副本，**不要**让管理端擅自改 Tenon 表结构。
- 导出：CSV；单次上限 5 万行；超过则 P1 异步任务 + 下载链接；导出写审计（谁、什么条件、多少行）。

#### 4.3.6 预算与告警（P1）

- 预算按全局/用户/模型/月或日；只做**提示**，不阻断 Tenon 请求。
- 阈值 80%/100%；通知渠道：本地通知/邮件/Webhook。
- 真正“超限阻断”需要 Tenon 在 LLM 代理前查预算（P2），需要改动 Tenon；不要在产品里暗示 P0 已能限流。

### 4.4 活跃用户

#### 4.4.1 先定义“活跃”，再谈“在线”

Tenon 没有用户级 last_seen，`t_auth_session` 只有过期时间；7 天 TTL 的会话可能只是“浏览器没退出”。因此：

| 层级 | 判定 | UI 语义 |
|---|---|---|
| `运行中` | 存在 `t_run.Factive=1` | 正在执行 Agent 任务 |
| `在线` | 最近业务活动 ≤ 5 分钟 | 正在使用（对话/Run/模型请求） |
| `活跃` | ≤ 15 分钟（默认窗口） | 当前活跃 |
| `最近` | ≤ 60 分钟 | 刚用过 |
| `今日` | ≤ 24 小时 | 今天来过 |
| `已登录未活跃` | 有未过期会话，但 24h 无业务活动 | 只表示会话未过期，**不是在线** |

页面默认窗口 15 分钟；顶部写明“按业务活动计算，不含仅登录未操作”。不要用绿色“在线”点表示“有会话”。

#### 4.4.2 数据来源与查询

活动取三路并集（最近窗口内）：

- `t_conversation.Flast_active_time`（事件写入时更新）
- `t_run.Fcreate_time`（任务创建；活跃 Run 另行无条件纳入，见下）
- `t_llm_usage.Fcreate_time`（模型请求）

示例（窗口参数在应用层算好）：

```sql
SELECT x.user_id, MAX(x.ts) AS last_seen,
       SUM(x.tokens) AS tokens_window
FROM (
  SELECT Fuser_id AS user_id, Flast_active_time AS ts, 0 AS tokens
    FROM tenon_prod.t_conversation
   WHERE Flast_active_time >= ?
  UNION ALL
  SELECT Fuser_id, Fcreate_time, 0
    FROM tenon_prod.t_run
   WHERE Fcreate_time >= ?
  UNION ALL
  SELECT Fuser_id, Fcreate_time, Fprompt_tokens + Fcompl_tokens
    FROM tenon_prod.t_llm_usage
   WHERE Fcreate_time >= ?
) x
JOIN tenon_prod.t_user u ON u.Fuser_id = x.user_id
GROUP BY x.user_id
ORDER BY last_seen DESC
LIMIT ?;
```

再补：活跃 Run 数、当前模型（最近用量/对话模型）、会话数、近 24h/7d token、状态。**`t_run.Factive=1` 的用户无条件纳入结果**，避免“跑了 30 分钟但创建时间在窗口外”的长任务被漏掉。查询分两步，避免大 join；结果缓存 5 秒，页面 10 秒轮询，标签页隐藏时暂停。

#### 4.4.3 页面与操作

列：用户、层级徽标、最近活动、活跃 Run、当前模型、窗口 Token、24h Token、会话数、操作（详情 / 停用 / 吊销会话 / 看任务）。

顶部：窗口切换（5m/15m/60m/24h）、自动刷新开关、“数据截至”时间、在线/活跃/运行中计数。

P1：如果 Tenon 增加 presence 心跳（API 中间件写 `t_user_presence`），可显示真实 last_seen/IP/UA；P0 不承诺。

### 4.5 Agent 任务与执行轨迹

#### 4.5.1 对象关系

```text
TenonUser 1 ── N Conversation 1 ── N Run 1 ── N Event
                                      │
                                      └── 1..N Usage（模型请求）
                                      └── 2 Message（用户输入 + 最终回答）
```

- 一个“任务”= 一个 Run；一个对话可以有多个 Run（多轮）。
- 子 Agent 的 `task` 工具调用仍属于父 Run 的轨迹；它的中间过程在 agent-runtime 的 JSONL 里，不一定全部回传控制层。管理端 P0 只展示控制层 `t_event` 可见的部分，并明确这个边界。

#### 4.5.2 任务列表

列：`run_id`、用户、对话标题、模型、状态、开始时间、耗时、迭代、Token、错误码、操作。

筛选：状态（运行中/失败/完成/取消/全部）、用户、模型、错误码、时间范围。默认“运行中 + 最近 2 小时”。活跃项 5 秒轮询，终态不刷新。

排序：`Fcreate_time DESC, Fid DESC`；keyset 分页。跨时间范围的大表查询先用 `Fcreate_time` 收窄，再按 `Fid` 翻页；P0 不改 Tenon 索引。

#### 4.5.3 任务详情与轨迹

上方摘要：状态、用户、对话、模型、起止、耗时、迭代、`Fprompt_tokens/Fcompl_tokens`（Run 汇总）、`t_llm_usage` 明细合计、估算成本、commit、checkpoint、错误码/错误信息。

消息区：用户输入（`t_message.Frole=1`）+ 最终回答（`Frole=2`）；没有最终回答时显示“未产生最终回答”。

轨迹区：按 `Fseq` 升序分页加载 `t_event`，按类型渲染：

| 来源/类型 | 渲染 |
|---|---|
| 控制 `run_accepted` | 任务已接受；显示 message_id/初始状态 |
| 控制 `run_started` | 开始执行 |
| 控制 `run_cancelling` / `run_finalizing` | 取消中 / 收尾中（可折叠） |
| 控制 `run_finished` | 终态卡片：commit、改动文件数、耗时、usage、错误码 |
| 控制 `run_failed` / `run_cancelled` | 终态卡片：错误码/原因；取消单列 |
| 运行时 `message`（assistant，含文本） | 文本块 |
| 运行时 `message`（assistant，含 tool_calls） | 工具调用卡片：name、call_id、参数摘要；展开原始参数 |
| 运行时 `message`（tool） | 工具结果卡片：按 `tool_call_id` 与调用配对；`is_error` 标红；截断提示；展开/下载 |
| 运行时 `marker`（turn_done/turn_failed/turn_cancelled） | 终态标记；`turn_failed` 显示 error_code |
| 运行时 `notice` | 系统提示（如上下文压缩、模型切换） |
| `delta` | **不持久化**，不会出现在轨迹里；不要显示“缺失” |

交互：默认折叠控制类中间事件；工具结果超过阈值（建议 8KB）默认截断，提供“展开全文/下载原始 JSON”；`Fpayload` 最大按 64KB 返回，超出部分提示下载；分页 100 条，避免大 payload 拖垮浏览器。

原始 JSON：每条事件可展开 pretty-print；提供“复制”与“下载该 Run 的 JSONL”（P1，审计）。注意 payload 可能含代码/密钥/用户隐私，页面加敏感提示，访问权限限制到 super_admin/operator。

#### 4.5.4 实时跟随

- 运行中的 Run：每 2 秒 `GET /runs/{id}/events?after_seq=<last>`；有新事件追加；终态后停止；连续 10 秒无新事件显示“等待中”；网络错误指数退避，保留已加载内容。
- 切换标签页/窗口隐藏时暂停，恢复时补拉；不丢事件（按 seq 去重）。
- P1 可用 SSE（服务端轮询 DB 后推送）替代，P0 不引入额外长连接。

#### 4.5.5 失败归因提示

| 错误码 | 提示 |
|---|---|
| `LLM_NOT_CONFIGURED` | control 缺少模型环境变量；到 Tenon 配置检查 `api_key_env` |
| `RUN_TIMEOUT` | 超过 Run 最大时长；看是否卡在工具/构建 |
| `QUEUE_TIMEOUT` | 配额排队超时；看全局/用户并发 |
| `SANDBOX_START_FAILED` | 沙箱启动失败；看 wsd/镜像/端口 |
| `STOP_UNCONFIRMED` | 取消未确认停止；不要重试写入，先查 wsd |
| `CHECKPOINT_FAILED` | 收尾落库失败；看 DB/事件 |
| `LLM_ERROR` | 上游错误；看用量明细的 `Fhttp_status` 与 `Fcost_ms`（代理层可能返回 `UPSTREAM_ERROR`/`RATE_LIMITED`） |
| `RUN_TOKEN_LIMIT` | 单 Run token 上限；考虑拆分任务 |

#### 4.5.6 操作边界

- P0 **只读**：不提供取消/重试/删除。
- P1 取消：新增 Tenon 管理接口，内部调用 `run.Service.Cancel`，保留 owner 校验、停止确认、generation fencing；管理端只传 `run_id` 和 reason，不直接 SQL。
- P1 重试：本质是创建新 Run，需要走对话 API/额度；P2 再做。
- 任何“改轨迹/改状态”的能力都不做。

### 4.6 审计日志

- 追加写；应用账号只 `INSERT/SELECT`，无 `UPDATE/DELETE`。
- 动作清单：`auth.login.success/failure`、`auth.logout`、`auth.step_up`、`auth.password.change`、`admin.bootstrap`、`admin.recover`、`user.create`、`user.reset_password`、`user.disable`、`user.enable`、`user.reveal_password`、`user.sessions.revoke`、`user.note.update`、`model.import`、`model.price.create`、`model.meta.update`、`usage.export`、`run.export`、`settings.update`、`backup.keys.export`、`system.doctor`。
- 字段：时间（ms）、管理员、动作、目标类型/ID、结果、IP、UA、request_id、reason、detail JSON（脱敏）。
- 敏感动作（创建/改密/停用/查看密码/导出）**审计与业务同事务**；审计失败则操作失败。
- 查询：按管理员/动作/目标/时间/结果筛选；详情抽屉展示 before/after（价格、状态、备注），密码/密钥永不入 detail。
- 保留：默认 365 天；按月分区/归档；导出 CSV（本身也审计）。
- P1 防篡改：`Fprev_hash + Fentry_hash` 哈希链；每日把最新 hash 写到服务器只追加文件，并异地归档/打印留存。

### 4.7 总览、健康与设置

**总览卡片**：用户总数、15m 活跃、运行中、今日 Token、今日估算成本、今日失败率、未定价模型数、数据新鲜度。图表：24h Token 趋势（按模型）、Run 状态分布、Top 用户、最近失败。点击卡片跳到对应筛选页。

**系统健康**（设置页/顶栏）：

| 检查 | 方法 | 失败提示 |
|---|---|---|
| 管理库连接 | `SELECT 1` + 延迟 | 检查 DSN/MySQL/网络 |
| Tenon 库连接 | `SELECT 1`（只读账号） | 检查 GRANT/网络 |
| schema 版本 | 读 `t_schema_migration` | 未知版本 → 警告并禁用写 |
| `t_user` 列契约 | `information_schema.columns` | 缺列/未知 NOT NULL 列 → 禁止创建用户 |
| 服务器密钥 | `master.key` 存在/权限/长度；systemd credential 可读 | 拒绝启动/禁止 reveal |
| 反向代理/TLS | 配置检查：只信 loopback proxy、`X-Forwarded-Proto=https`、证书有效期 | 警告；不允许降级到 HTTP |
| Mac 恢复副本（可选） | Mac CLI 用 `admin.secret` 验证 verifier（不记录密码） | 提示“请同步恢复副本”；不影响服务器登录 |
| 数据新鲜度 | 最近事件/用量时间 | 标注可能延迟 |
| 控制层状态（P1） | `GET /debug/status` 或管理接口 | 显示“未接入”，不伪造健康 |

**设置**：时区、刷新间隔、审计/导出保留期、模型目录来源、控制层 URL（P1）、UI 偏好；所有变更审计。密钥只显示“存在/权限正确/备份时间”，不显示内容。

### 4.8 权限矩阵（P0 单角色，P1 生效）

| 动作 | super_admin | operator | support | auditor |
|---|---|---|---|---|
| 查看总览/用量 | ✅ | ✅ | ✅ | ✅ |
| 查看用户列表/详情 | ✅ | ✅ | ✅ | ✅（脱敏） |
| 创建用户 | ✅ | ✅ | ❌ | ❌ |
| 重置密码 | ✅ | ✅ | ✅ | ❌ |
| 停用/启用 | ✅ | ✅ | ❌ | ❌ |
| 查看密码 | ✅ | ❌（可配置） | ❌ | ❌ |
| 看 Run/轨迹内容 | ✅ | ✅ | ✅ | ❌（仅元数据） |
| 取消 Run（P1） | ✅ | ✅ | ❌ | ❌ |
| 模型导入/价格 | ✅ | ✅ | ❌ | ❌ |
| 审计查看 | ✅ | ❌ | ❌ | ✅ |
| 设置/密钥备份 | ✅ | ❌ | ❌ | ❌ |
| 管理员管理（P1） | ✅ | ❌ | ❌ | ❌ |

---

## 5. 数据设计

### 5.1 数据源与读写边界

| 数据源 | 管理端用途 | 读 | 写 | 备注 |
|---|---|---|---|---|
| `tenon_prod.t_user` | 用户身份 | ✅ | ✅ INSERT / UPDATE | 只改身份列；不删行 |
| `tenon_prod.t_auth_session` | 会话吊销 | ✅ | ✅ UPDATE `Fexpire_time` | 不依赖 DELETE |
| `tenon_prod.t_conversation` | 活跃度、对话上下文 | ✅ | ❌ | 只读 |
| `tenon_prod.t_message` | 用户输入/最终回答 | ✅ | ❌ | 只读；注意隐私 |
| `tenon_prod.t_run` | 任务列表/详情 | ✅ | ❌ | 绝不直接改状态 |
| `tenon_prod.t_event` | 执行轨迹 | ✅ | ❌ | payload 可能很大 |
| `tenon_prod.t_sandbox` | 沙箱/预览状态 | ✅ | ❌ | 只读 |
| `tenon_prod.t_llm_usage` | 用量/成本 | ✅ | ❌ | 只读 |
| `tenon_prod.t_schema_migration` | 兼容性检查 | ✅ | ❌ | 只读 |
| `tenon_admin.tadm_*` | 管理端自身数据 | ✅ | ✅ | 独立 schema |

### 5.2 新增表

完整可执行 DDL 见同目录 `schema.sql`。表清单：

| 表 | 作用 | 关键约束 |
|---|---|---|
| `tadm_schema_migration` | 管理端迁移版本 | `version` 主键 |
| `tadm_admin` | 管理员、验证器、角色、锁定 | `Fadmin_id` 唯一；`Fsession_epoch` |
| `tadm_admin_session` | 会话吊销/step-up/审计 | `Fsid_hash` 唯一；按 admin/expiry 索引 |
| `tadm_admin_credential` | WebAuthn/passkey 公钥（P1） | `Fcredential_id` 唯一；私钥在 Mac |
| `tadm_tenon_user` | 管理端用户扩展（标签/备注/密码状态） | `Ftenon_user_id` 主键 |
| `tadm_user_secret` | Tenon 密码密文 | `Ftenon_user_id` 主键；key version |
| `tadm_model` | 模型目录镜像 | `Fmodel_id` 主键 |
| `tadm_model_price` | 价格历史 | `(Fmodel_id,Feffective_from)` 唯一 |
| `tadm_usage_daily` | 用量日汇总（P1） | `(Fday,Fuser_id,Fmodel)` 主键 |
| `tadm_audit_log` | 审计 | 追加写；时间/动作/目标索引 |
| `tadm_operation` | 写操作幂等 | `Frequest_id` 主键；24h 清理 |
| `tadm_setting` | 非敏感设置 | `Fkey` 主键 |
| `tadm_export_job` | 异步导出（P1） | 状态/过期时间 |

设计说明：

- 管理端表统一 `tadm_` 前缀，避免与 Tenon `t_` 混淆。
- 时间统一 `DATETIME` 存 UTC；应用显式写入，不依赖 MySQL 服务器时区。
- 金额 `DECIMAL(18,6)`；token 用 `BIGINT`；JSON 用 MySQL `JSON`。
- 审计表不建外键（避免删除/锁冲突），靠应用约束。
- `tadm_tenon_user` 只存管理端扩展，不复制 `Fname/Fstatus/Fmax_conv`，避免双写不一致；这些字段实时从 `t_user` 读。
- `tadm_user_secret` 与 `tadm_tenon_user` 一对一；历史用户没有 secret 行。

**为什么独立 schema 而不是把 `tadm_*` 放进 `tenon`：**

| 方案 | 优点 | 缺点 | 结论 |
|---|---|---|---|
| 同一 schema `tenon` + `tadm_*` | 单事务最直接；一个 DSN | 管理端敏感数据与业务表混在一起；Tenon 迁移/备份/权限边界模糊；误操作面更大 | 不推荐 |
| **独立 schema `tenon_admin`（推荐）** | 清晰边界；独立备份/保留/授权；同一实例仍可跨 schema 事务 | 账号需要两个 schema 的权限；查询要写全限定名 | ✅ |
| 独立 MySQL 实例 | 隔离最强 | 无法跨实例事务；不再是“复用 Tenon 的 MySQL”；运维成本高 | 不选 |

> 跨 schema 事务只在同一 MySQL 实例内成立；如果未来把管理库拆到另一个实例，创建用户要改成“本地待提交 + Tenon 写入 + 补偿/对账”的 saga，复杂度显著上升。

### 5.3 跨 schema 事务与数据库账号

**事务**：`tenon_admin` 与 `tenon_prod` 在同一 MySQL 实例，单连接内 `BEGIN ... COMMIT` 可跨 schema 原子提交。创建用户时五类写入（`t_user`、`tadm_tenon_user`、`tadm_user_secret`、`tadm_audit_log`、`tadm_operation`）必须同一事务。

**账号设计（推荐）**：

```sql
-- 管理端读写账号：只拿必要的 Tenon 权限 + 管理库权限
CREATE USER 'tenon_admin_app'@'localhost' IDENTIFIED BY '<strong-random>';
GRANT SELECT ON `tenon_prod`.* TO 'tenon_admin_app'@'localhost';

-- 只允许写 t_user 的身份列；不是整表 UPDATE
GRANT INSERT (`Fuser_id`,`Fname`,`Fpwd_hash`,`Fstatus`,`Fmax_conv`,`Fcreate_time`,`Fmodify_time`)
  ON `tenon_prod`.`t_user` TO 'tenon_admin_app'@'localhost';
GRANT UPDATE (`Fname`,`Fpwd_hash`,`Fstatus`,`Fmax_conv`,`Fmodify_time`)
  ON `tenon_prod`.`t_user` TO 'tenon_admin_app'@'localhost';

-- 只允许提前会话过期时间
GRANT UPDATE (`Fexpire_time`) ON `tenon_prod`.`t_auth_session`
  TO 'tenon_admin_app'@'localhost';

-- 管理库逐表授权；只给真正需要的动作
GRANT SELECT, INSERT, UPDATE ON `tenon_admin`.`tadm_admin` TO 'tenon_admin_app'@'localhost';
GRANT SELECT, INSERT, UPDATE, DELETE ON `tenon_admin`.`tadm_admin_session` TO 'tenon_admin_app'@'localhost';
GRANT SELECT, INSERT, UPDATE, DELETE ON `tenon_admin`.`tadm_admin_credential` TO 'tenon_admin_app'@'localhost';
GRANT SELECT, INSERT, UPDATE ON `tenon_admin`.`tadm_tenon_user` TO 'tenon_admin_app'@'localhost';
GRANT SELECT, INSERT, UPDATE, DELETE ON `tenon_admin`.`tadm_user_secret` TO 'tenon_admin_app'@'localhost';
GRANT SELECT, INSERT, UPDATE ON `tenon_admin`.`tadm_model` TO 'tenon_admin_app'@'localhost';
GRANT SELECT, INSERT ON `tenon_admin`.`tadm_model_price` TO 'tenon_admin_app'@'localhost';
GRANT SELECT, INSERT, UPDATE, DELETE ON `tenon_admin`.`tadm_usage_daily` TO 'tenon_admin_app'@'localhost';
GRANT SELECT, INSERT, UPDATE, DELETE ON `tenon_admin`.`tadm_operation` TO 'tenon_admin_app'@'localhost';
GRANT SELECT, INSERT, UPDATE ON `tenon_admin`.`tadm_setting` TO 'tenon_admin_app'@'localhost';
GRANT SELECT, INSERT, UPDATE, DELETE ON `tenon_admin`.`tadm_export_job` TO 'tenon_admin_app'@'localhost';
GRANT SELECT, INSERT ON `tenon_admin`.`tadm_schema_migration` TO 'tenon_admin_app'@'localhost';
GRANT SELECT, INSERT ON `tenon_admin`.`tadm_audit_log` TO 'tenon_admin_app'@'localhost';

-- 可选：只读账号，供用量/轨迹等分析查询使用，进一步隔离写权限
CREATE USER 'tenon_admin_ro'@'localhost' IDENTIFIED BY '<strong-random>';
GRANT SELECT ON `tenon_prod`.* TO 'tenon_admin_ro'@'localhost';
GRANT SELECT ON `tenon_admin`.* TO 'tenon_admin_ro'@'localhost';
```

- 不授予 `t_user` 的 `DELETE`；不做硬删除。
- 不授予任何 DDL；`init-db` 由 DBA/管理员账号执行。
- P0 给管理端 Tenon 库全库 `SELECT`，便于 schema 契约检查与关联查询；如果要求更严，P1 改成列级 `SELECT`，显式排除 `t_user.Fpwd_hash` 与 `t_auth_session.Fsession_hash`。代价是 `information_schema` 的列契约检查会受权限限制，需要额外维护“期望列清单”。
- 不授予 `t_conversation/t_run/t_event/...` 的写权限；从权限层杜绝“直接改 Run”。
- 如果采用双账号，事务写路径只能用读写账号；只读查询走 `ro` 池。P0 可先单账号，P1 再拆。
- **MySQL 账号 host 要匹配连接方式**：Unix socket 用 `@'localhost'`，TCP `127.0.0.1` 用 `@'127.0.0.1'`。bootstrap 从 Mac 经 SSH 隧道写入时，服务器侧看到的是 `127.0.0.1`，需要对应账号（或同时建两个；两个 host 的 GRANT 要分别执行）。不要图省事用 `@'%'`，除非防火墙/私网已严格限制。

**网络**：管理端在服务器上通过 `127.0.0.1:3306` 或 Unix socket 连 MySQL；若数据库在另一台机器，必须走私有网络/VPN + TLS，不暴露公网 3306。Mac 只在 bootstrap/运维时用 SSH 隧道；运行时浏览器走 HTTPS 到 nginx，不直连数据库。DSN 使用 `parseTime=true&loc=UTC&timeout=5s&readTimeout=30s&writeTimeout=30s`。

### 5.4 索引、分页与查询策略

- 用户列表：`t_user` 只有 `uk_user_id`；按创建时间排序时用 `Fid` keyset（主键），避免全表排序。按关键词搜索用 `Fuser_id LIKE 'prefix%'` 或 `Fname LIKE '%kw%'`（后者小规模可接受，规模大再考虑搜索表）。
- Run 列表：`t_run` 有 `idx_user_time(Fuser_id,Fcreate_time)`、`idx_active(Factive,Fstatus)`；管理端默认按 `Fid DESC` 翻页，时间范围用 `Fcreate_time` 过滤。不要在没有索引的列上做深分页。
- 轨迹：`t_event` 有 `uk_conv_seq(Fconv_id,Fseq)`；按 `Fconv_id + Frun_id + Fseq` 查询，先走对话索引再过滤 run，单 Run 事件量可控。
- 用量：`t_llm_usage` 有 `idx_user_time`、`idx_conv_time`；模型维度先用时间范围收窄，再应用层聚合；P1 日汇总解决。
- 管理端自身表：按 `(Fadmin_id,Fts)`、`(Ftarget_type,Ftarget_id,Fts)`、`(Fexpire_time)` 建索引。
- 所有列表 keyset 分页，禁止 `OFFSET` 深分页；默认 50，最大 100。
- 热点页面（总览、活跃用户）服务端缓存 5–15 秒；缓存 key 含窗口和时间；不做跨用户缓存（单管理员，风险低）。

### 5.5 数据保留、备份与恢复

| 数据 | 默认保留 | 说明 |
|---|---|---|
| 审计 | 365 天 | 到期归档后删除；可配置 |
| 幂等操作记录 | 24 小时 | 定时清理 |
| 导出文件 | 24 小时 | 服务器 `/var/lib/tenon-admin/exports`；下载后删除 |
| 用量日汇总 | 400 天 | 可重算 |
| Tenon 原始数据 | 不归管理端管 | 不删除/不归档 Tenon 表 |
| 服务器密钥 | 永久 | `master.key` 离线加密备份；丢失不可恢复 |
| Mac `admin.secret` | 永久 | 密码记录；与服务器密钥分开备份 |

- 备份：每日 `mysqldump tenon_admin`（加密后存放）；`master.key` 单独备份，**不与数据库备份放在一起**；每季度做一次恢复演练。
- 恢复顺序：先恢复 `master.key`，再恢复 DB；否则密文不可解。反向代理/TLS 配置、systemd unit 也纳入配置备份。
- RPO/RTO（远程管理端）：RPO 24h，RTO 1h；若要求更高，改为 binlog 增量。
- 审计的归档/删除由 DBA/维护账号执行（应用账号只有 `INSERT/SELECT`，见 §5.3）；归档文件加密，删除前校验完整性。
- 安全删除：用户密码“交付后清除”只删 `tadm_user_secret` 行/密文，不删 Tenon 用户。

### 5.6 与 Tenon 的兼容性

- 启动时读取 `tenon_prod.t_schema_migration`，记录已知版本（当前 `000001_init.mysql.sql`）。未知版本：只读模式 + 顶栏警告；禁用创建/改密，直到管理端适配。
- 启动/写前检查 `t_user` 列契约：必须存在 `Fuser_id/Fname/Fpwd_hash/Fstatus/Fmax_conv/Fcreate_time/Fmodify_time`；出现未知 NOT NULL 无默认列 → 禁止写。
- 不修改 Tenon 的 `t_schema_migration`；管理端自己的迁移写在 `tadm_schema_migration`。
- 兼容性测试：在真实 Tenon schema 上跑“创建用户 → Tenon 登录”的端到端用例；Tenon 升级后必须重跑。

---

## 6. 接口设计

### 6.1 约定

- Base：`https://admin.example.com/api/admin/v1`；静态资源同源；HTTP 只保留 `127.0.0.1/healthz`。
- 认证：`__Host-tadm_sid` cookie（`Secure; HttpOnly; SameSite=Strict; Path=/`）；写请求必须带 `X-CSRF-Token`；step-up 敏感接口额外校验。
- 边缘：nginx/Caddy 终止 TLS；管理端只信任 loopback 代理；真实 IP 仅从受信 `X-Forwarded-For` 取；`Origin` 白名单 `https://admin.example.com`。
- 请求/响应：JSON；时间 RFC3339 UTC；ID 原样字符串；错误统一 `{"error":{"code","message"}}`（与 Tenon 风格一致）。
- 分页：`?limit=50&cursor=...` → `{"items":[],"next_cursor":""}`；游标不透明。
- 幂等：写请求带 `Idempotency-Key`（26 位 ULID；不合法则 400，缺省由服务端生成；也可用 body `client_request_id`）；服务端存 `tadm_operation`，24h 内同 key 重放返回相同结果元数据；同 key 不同请求体 → 409；超过 24h 按新请求处理。
- 秘密响应：`Cache-Control: no-store, no-cache`；`Pragma: no-cache`；不写 access log body。
- 限流：登录/step-up 5 次/15 分钟；reveal 30 次/小时；导入 10 次/小时；导出 10 次/小时；nginx 层 `limit_req` 兜底。
- 审计：所有写接口与 reveal/导出都写审计；审计失败则请求失败。
- P1 MFA：`POST /auth/webauthn/register/begin|finish`、`POST /auth/webauthn/login/begin|finish`、`POST /auth/recovery/consume`；P0 若走公网，这些接口必须实现后才能上线。

### 6.2 端点清单

| 方法 | 路径 | 作用 | 权限 |
|---|---|---|---|
| GET | `/healthz` | 存活/就绪检查（仅 127.0.0.1/内网） | 本机 |
| POST | `/auth/login` | 管理员登录 | 公开 |
| POST | `/auth/logout` | 登出 | 登录 |
| GET | `/auth/me` | 当前管理员 + CSRF + step-up 状态 | 登录 |
| POST | `/auth/step-up` | 敏感操作前重认证 | 登录 |
| POST | `/auth/password` | 修改管理员密码 | super |
| GET | `/dashboard/summary` | 总览卡片/趋势 | 登录 |
| GET | `/users` | 用户列表 | 登录 |
| POST | `/users` | 创建 Tenon 用户 | super |
| GET | `/users/{id}` | 用户详情 | 登录 |
| PATCH | `/users/{id}` | 改备注/标签/显示名/`max_conv`（P1） | super |
| POST | `/users/{id}/reset-password` | 重置密码 | super/operator/support |
| POST | `/users/{id}/disable` | 停用 | super/operator |
| POST | `/users/{id}/enable` | 启用 | super/operator |
| POST | `/users/{id}/reveal-password` | 查看密码（step-up） | super（可配置） |
| POST | `/users/{id}/sessions/revoke` | 吊销全部会话 | super/operator |
| GET | `/users/{id}/activity` | 用户活跃/用量摘要 | 登录 |
| GET | `/users/{id}/conversations` | 用户对话 | 登录 |
| GET | `/users/{id}/runs` | 用户任务 | 登录 |
| GET | `/models` | 模型目录 | 登录 |
| POST | `/models/import` | 导入/同步 YAML（multipart/path） | super |
| PATCH | `/models/{id}` | 改展示元数据/启用标记 | super |
| POST | `/models/{id}/prices` | 新增价格（生效时间） | super |
| GET | `/models/{id}/usage` | 模型用量 | 登录 |
| GET | `/usage/summary` | 用量 KPI | 登录 |
| GET | `/usage/series` | 趋势序列 | 登录 |
| GET | `/usage/breakdown` | 用户/模型/对话分布 | 登录 |
| GET | `/usage/export` | CSV 导出（审计） | super/operator |
| GET | `/presence` | 活跃用户 | 登录 |
| GET | `/runs` | 任务列表 | 登录 |
| GET | `/runs/{id}` | 任务详情 | 登录 |
| GET | `/runs/{id}/events` | 轨迹分页（`after_seq`） | 登录 |
| GET | `/runs/{id}/messages` | 输入/最终回答 | 登录 |
| GET | `/runs/{id}/usage` | 模型请求明细 | 登录 |
| GET | `/runs/{id}/export` | 轨迹 JSONL（P1，审计） | super |
| GET | `/audit` | 审计查询 | super/auditor |
| GET | `/audit/export` | 审计 CSV | super/auditor |
| GET | `/system/health` | 数据源/schema/密钥/新鲜度 | 登录 |
| GET | `/system/settings` | 设置 | super |
| PATCH | `/system/settings` | 改设置 | super |
| GET | `/system/doctor` | 自检详情 | super |
| POST | `/admin/accounts` | 多管理员（P1） | super |

### 6.3 关键示例

**创建用户**

```http
POST /api/admin/v1/users
Cookie: tadm_sid=...
X-CSRF-Token: ...
Idempotency-Key: 01JABCD...

{
  "user_id": "alice",
  "name": "Alice",
  "password": null,
  "max_conv": 20,
  "tags": ["team-a"],
  "note": "交付给客户 A"
}
```

```http
HTTP/1.1 201 Created
Cache-Control: no-store
Content-Type: application/json

{
  "user_id": "alice",
  "name": "Alice",
  "password": "xQ7...24chars",
  "max_conv": 20,
  "created_at": "2026-10-25T06:32:05Z"
}
```

> 重放同一个 `Idempotency-Key` 时返回 200 + 用户信息，但**不再返回密码**；UI 提示“已创建，可用查看密码获取”。

**查看密码**

```http
POST /api/admin/v1/users/alice/reveal-password
Cookie: tadm_sid=...
X-CSRF-Token: ...

{"reason":"交付给客户 A"}
```

```http
HTTP/1.1 200 OK
Cache-Control: no-store

{"password":"xQ7...24chars","expires_in":30}
```

**活跃用户**

```http
GET /api/admin/v1/presence?window=15m
```

```json
{
  "as_of": "2026-10-25T06:35:00Z",
  "window_seconds": 900,
  "items": [
    {
      "user_id": "alice",
      "name": "Alice",
      "tier": "online",
      "last_seen": "2026-10-25T06:34:41Z",
      "active_runs": 1,
      "tokens_window": 1240,
      "model": "deepseek",
      "sessions": 1
    }
  ]
}
```

**轨迹分页**

```http
GET /api/admin/v1/runs/01J.../events?after_seq=42&limit=100
```

```json
{
  "items": [
    {
      "seq": 43,
      "ts": "2026-10-25T06:20:05Z",
      "source": 0,
      "type": "message",
      "run_id": "01J...",
      "payload": {"kind":"message","message":{"role":"assistant","tool_calls":[...]}}
    }
  ],
  "next_cursor": "143"
}
```

### 6.4 错误码

| code | HTTP | 场景 |
|---|---|---|
| `UNAUTHENTICATED` | 401 | 未登录/会话过期 |
| `FORBIDDEN` | 403 | 角色不足 |
| `STEP_UP_REQUIRED` | 403 | 需要重新输入密码 |
| `RATE_LIMITED` | 429 | 登录/reveal/导出限流 |
| `BAD_REQUEST` | 400 | 参数/游标非法 |
| `BAD_USER` | 400 | 用户名不符合规则 |
| `WEAK_PASSWORD` | 400 | 密码 <8 字节/过短 |
| `USER_EXISTS` | 409 | 用户已存在（忽略大小写） |
| `NOT_FOUND` | 404 | 用户/Run/模型不存在 |
| `CONFLICT` | 409 | 并发/幂等冲突 |
| `PASSWORD_NOT_MANAGED` | 409 | 历史用户无密码密文 |
| `DB_UNAVAILABLE` | 503 | 数据库/隧道不可用 |
| `TENON_SCHEMA_MISMATCH` | 503 | Tenon schema 版本/列不兼容 |
| `ENCRYPTION_FAILED` | 500 | 加密失败（不写半条数据） |
| `VAULT_DECRYPT_FAILED` | 500 | 密文损坏/密钥不匹配；建议重置 |
| `EXPORT_TOO_LARGE` | 413 | 导出超过上限 |
| `INTERNAL` | 500 | 未分类错误（日志有 request_id） |

### 6.5 可选的 Tenon 侧管理接口（P1）

为了让“模型就绪、真正在线、取消任务”从“猜”变成“事实”，建议在 Tenon 增加一个极小的管理面：

- 新监听：`listen.admin: 127.0.0.1:8083`（仅 loopback）。若管理端与 control 不同机，只允许私有网络/VPN + token，不得暴露公网。
- 鉴权：`control.admin_token_file`（0600）中的随机 token；请求头 `X-Tenon-Admin-Token`；不使用用户会话。
- 端点：
  - `GET /internal/admin/v1/overview`：模型 `ready/missing_env`、catalog mtime、active runs/sandboxes、quota 快照、uptime、版本。
  - `GET /internal/admin/v1/models`：复用 `Catalog.List()`，绝不返回密钥。
  - `POST /internal/admin/v1/runs/{run_id}/cancel`：内部调用 `run.Service.Cancel`，保留 owner/停止确认/generation fencing；写 Tenon 审计。
  - `GET /internal/admin/v1/presence`（可选）：需要给 `event.Hub` 的订阅记录用户/对话；返回当前 SSE 订阅数，仅作为“强在线”的补充。
- 安全：独立 loopback 端口、独立 token、无 CORS、无用户数据批量导出；所有操作写审计。
- 如果 P0 不改 Tenon，则页面明确显示“模型就绪：未知（需控制层接口）”“在线：按业务活动估算”，不阻塞核心需求。

---

## 7. 功能流程设计

### 7.1 首次初始化（远程服务器）

**参与角色**：DBA / 管理员 / 管理端 CLI / 反向代理管理员。

1. DBA 在服务器 MySQL 创建 `tenon_admin` schema、管理账号、按 §5.3 授权；创建 `tenon-admin` 系统用户与 `/etc/tenon-admin`（0700）。
2. 在服务器执行 `tenon-admin init-db`（管理员 DSN）建表，写 `tadm_schema_migration`；或由 DBA 直接执行 `schema.sql`。
3. **推荐在 Mac 上生成管理员凭据**（密码不经过服务器）：
   - Mac 建 SSH 隧道：`ssh -N -L 13306:127.0.0.1:3306 root@<server>`；
   - `tenon-admin init-admin --mode remote --db "..." --admin-id <id> --out ./tenon-admin-bootstrap`；
   - 本机生成 `admin.secret`（0600）、`bootstrap/server/master.key`（32B）与 verifier；通过隧道写 `tadm_admin`；终端打印一次密码；
   - `scp bootstrap/server/master.key root@<server>:/etc/tenon-admin/keys/master.key`，服务器 `chown tenon-admin:tenon-admin`、`chmod 600`；
   - 删除 Mac 上的 `bootstrap/server/`（或加密归档），`admin.secret` 留在 Mac。
4. 配置 systemd 与 nginx/Caddy：
   - systemd：专用用户、`LoadCredential=master:/etc/tenon-admin/keys/master.key`、`EnvironmentFile=/etc/tenon-admin/env`；
   - nginx：`admin.example.com:443` → `127.0.0.1:7200`，TLS、HSTS、IP 白名单/VPN、`limit_req`、`client_max_body_size 2m`。
5. 服务器执行 `tenon-admin doctor`：DB、GRANT、schema、密钥权限、时间偏差、反向代理头、证书有效期全部通过。
6. 启动 `systemctl enable --now tenon-admin`；浏览器访问 `https://admin.example.com`，用终端密码登录。
7. 首次登录后按需导入模型目录、配置价格。

**回退：在服务器上生成**（没有 SSH 隧道时）：

```bash
tenon-admin init-admin --password-stdin --no-local-secret \
  --admin-id <id> --tenon-schema tenon_prod
```

服务器生成 key/verifier，密码只打印一次，由人抄到 Mac；密码会经过服务器进程内存，安全等级低于 Mac 侧生成，需在审计中记录。`--no-local-secret` 确保服务器不写 `admin.secret`。

**失败处理**：

- 建表无权限 → 明确提示“用管理员账号执行 `schema.sql`”。
- 已有管理员 → 拒绝覆盖；提示用 `recover-admin`。
- `master.key` 存在但 DB 无管理员 → 提示“可能上次初始化中断”；允许用同一 key 重试。
- Mac 生成成功但 `scp` 失败 → 保留 `bootstrap/server/`，修复后重传；不要启动服务。
- nginx/TLS 未就绪 → 管理端仍只监听 loopback，不对外；`doctor` 报错。
- 公网暴露但未启用 MFA → 服务拒绝以 `exposure=public` 启动（见 §4.1.7），避免“先上线再补安全”。

### 7.2 登录、失败、锁定与恢复（HTTPS）

```text
Mac 浏览器 ──HTTPS──> nginx/Caddy :443 ──> Admin API :7200
                                             ├─ 限流检查（真实 IP + admin_id）
                                             ├─ 取 tadm_admin（无则 dummy）
                                             ├─ 读服务器 master.key → K_pepper
                                             ├─ HMAC + Argon2id + 常数时间比较
                                             ├─ 检查 status/locked_until
                                             ├─ （P1）WebAuthn assertion 校验
                                             ├─ 成功：写 session 行 + 签名 cookie + 审计
                                             └─ 失败：计数/锁定 + 审计 + 统一 401
```

1. 前端提交管理员 ID/密码；按钮 loading，禁用重复提交；全程 HTTPS。
2. 服务端从受信代理头取真实 IP，限流；失败 5 次锁 15 分钟；返回 429 + `Retry-After`。
3. 成功：`204` + `Set-Cookie: __Host-tadm_sid=...; Secure; HttpOnly; SameSite=Strict`；前端 `GET /auth/me` 获取角色/CSRF/step-up 状态。
4. 失败：统一“用户名或密码错误”，不区分账号不存在/密码错/被停用；避免枚举。
5. 会话过期：任意 API 401 → 前端清状态、跳登录、保留安全的内部 redirect（只允许同源 `https://admin.example.com` 路径）。
6. 登出：吊销当前会话 + 清 cookie + 审计。
7. 恢复：在服务器执行 `tenon-admin recover-admin`（需 `master.key` + DB 写权限），重设 verifier、生成新密码、吊销全部会话、写审计；新密码抄到 Mac。若 Mac 的 `admin.secret` 尚在，可直接读取，不必恢复。
8. 公网模式：登录成功后必须完成 WebAuthn；未注册 passkey 时只允许通过 IP 白名单/VPN 登录并强制注册。

### 7.3 创建 Tenon 用户（核心流程）

```text
管理员                 Admin UI                  Admin API                     MySQL
  │ 填表/生成密码 ──────> │                          │                           │
  │                      │ POST /users ───────────> │                           │
  │                      │                          │ 校验/契约检查              │
  │                      │                          │ BEGIN ───────────────────> │
  │                      │                          │ INSERT t_user             │
  │                      │                          │ INSERT tadm_tenon_user    │
  │                      │                          │ INSERT tadm_user_secret   │
  │                      │                          │ INSERT tadm_audit_log     │
  │                      │                          │ INSERT tadm_operation     │
  │                      │                          │ COMMIT ──────────────────> │
  │                      │ <──── 201 + password ─── │                           │
  │ <── 交付面板 ──────── │                          │                           │
```

**前置条件**：管理员已登录且 step-up（如启用）；Tenon schema 兼容；DB 账号有 INSERT/UPDATE。

**步骤**：

1. UI 校验：ID 规则、名称长度、密码长度、`max_conv`；实时显示“将写入 `tenon_prod.t_user`”。
2. 提交带 `Idempotency-Key`；服务端再校验（前端校验不可信）。
3. 服务端检查 schema 契约；按 collation 预检用户是否存在。
4. 生成密码（若为空）与 Argon2id hash；加密密码。
5. 单事务写五张表；审计/幂等失败则整体回滚。
6. 返回 201 + 密码；UI 展示交付面板（复制 ID/密码、下载单用户交付 txt）。
7. 可选自检（P1）：调用 Tenon `POST /api/login` + `POST /api/logout` 验证真实可登录；写审计；失败则明确提示“用户已创建，但登录自检失败”，不自动回滚（避免把已写入的用户删掉）。

**异常分支**：

| 分支 | 结果 |
|---|---|
| 用户已存在 | 409；事务回滚；展示已有用户 |
| 密码过短/过长 | 400；不写库 |
| 无 INSERT 权限 | 503 + GRANT 提示；回滚 |
| 加密失败 | 500 `ENCRYPTION_FAILED`；回滚 |
| 审计失败 | 500；回滚（fail closed） |
| 响应丢失/重试 | 同 Idempotency-Key 返回 200 + “已创建”；密码走 reveal |
| Tenon schema 变更 | 503 `TENON_SCHEMA_MISMATCH`；只读模式 |

### 7.4 重置密码、停用、启用

**重置密码**：

1. 用户详情 → 重置 → 选择“生成/自定义” → 填写 reason → step-up。
2. 服务端事务：更新 `Fpwd_hash/Fmodify_time`；把所有会话 `Fexpire_time` 提前；upsert 密码密文；写审计/幂等。
3. 返回新密码一次；UI 提示“旧密码和所有会话已失效；正在运行的 Run 不受影响”。
4. 用户下次登录用新密码；旧密码立即失败。

**停用**：

1. 确认框展示用户 ID/名称/活跃 Run 数；填写 reason；确认。
2. 事务：`Fstatus=2` + 过期所有会话 + 审计。
3. 结果：Tenon 登录失败；已登录请求 401；正在跑的 Run 继续（提示；P1 可取消）。

**启用**：

1. 确认；事务：`Fstatus=1` + 审计。
2. 提示“旧密码仍有效；如需强制换密，请重置密码”。

### 7.5 查看密码（step-up）

1. 点击“查看密码”；若 `Felevated_until` 已过期 → 弹窗重输管理员密码。
2. `POST /auth/step-up` 成功 → 5 分钟窗口；失败计数/锁定。
3. 填写 reason（必填）；提交 reveal。
4. 服务端：限流 → 检查 step-up → 解密（AAD 校验）→ 写审计 → 返回明文（`no-store`）。
5. UI：显示 30 秒 + 复制按钮 + “已记录审计”；30 秒后清空 DOM/内存引用。
6. 失败：密文不存在 → 409 `PASSWORD_NOT_MANAGED`，给“重置密码”；解密失败 → 500，提示重置。

### 7.6 模型目录导入与定价

1. 设置/模型页选择“导入 YAML”或“从服务器路径同步”。
2. 服务端解析（严格字段）、校验（唯一 ID、唯一 default、demo 限制）、生成 diff。
3. UI 展示 diff（新增/修改/移除/默认变化）；确认后单事务 upsert，移除项 `Fenabled=0`；审计。
4. 价格：模型详情 → 新增价格 → 生效时间/输入/输出/币种 → 校验 → 插入价格行 → 审计。
5. 历史成本：默认不重算；提供“按新价格重算某区间”的 P1 操作（审计、耗时提示）。
6. 就绪：P0 显示“未知（未接控制层）”；P1 接管理接口后显示 `ready/missing_env` 与来源时间。

**异常**：YAML 语法/未知字段/重复 ID → 422 + 行号；不部分导入；没有价格时成本显示“未定价”。

### 7.7 用量分析

1. 选择时间范围（今日/7 天/30 天/自定义）与筛选（模型/用户/状态）。
2. 服务端校验范围（最大 366 天）；检查缓存/汇总新鲜度。
3. 查询原始/汇总；计算成本与未定价占比；返回 KPI + 序列 + 分布。
4. UI 渲染图表/表格；点击模型/用户下钻到筛选后的明细。
5. 导出 CSV：确认范围/行数 → 生成 → 下载 → 审计；超过 5 万行走 P1 异步。
6. 数据为空：显示“该区间无模型请求”，并给“最近一次用量时间”。

### 7.8 活跃用户巡检

1. 打开活跃用户页；默认 15 分钟窗口；服务端缓存 5 秒。
2. 展示层级、最近活动、活跃 Run、窗口 token、当前模型、会话数。
3. 10 秒轮询；页面隐藏暂停；显示“数据截至”。
4. 行操作：看详情/任务、停用、吊销会话；写操作前确认 + 审计。
5. 如果只是“有会话无活动”，显示为“已登录未活跃”，不显示在线绿点。

### 7.9 任务排障与轨迹

1. 任务列表默认“运行中 + 最近 2 小时”；顶部计数；活跃项轮询。
2. 点击 Run → 并行加载详情、消息、第一页事件、用量明细。
3. 渲染摘要与时间线；工具调用/结果按 `call_id` 配对；错误标红；控制事件默认折叠。
4. 运行中：每 2 秒按 `after_seq` 拉新事件；终态停止；切页暂停/恢复。
5. 大 payload：截断 + 展开/下载；原始 JSON 可按事件查看。
6. 失败：展示错误码 + 归因提示 + 相关用量/事件；复制 run_id 给开发。
7. 结束：可复制/导出（P1）；不做取消/重试（P1 管理接口）。

### 7.10 审计与导出

1. 审计页按管理员/动作/目标/时间筛选；分页。
2. 详情展示 before/after（脱敏）、reason、IP/UA、request_id。
3. 导出 CSV：step-up + 二次确认；写审计；文件 24h 后删除。
4. 审计写入失败：对应业务操作失败（写路径）；读路径（如 reveal）不返回密码。

### 7.11 故障与降级

| 故障 | 页面行为 | 写操作 | 恢复 |
|---|---|---|---|
| 数据库/网络断开 | 顶栏红色“数据源不可用”；保留上次数据并标过期 | 全部禁用 | 修 DB/网络后手动刷新 |
| MySQL 只读/权限不足 | 健康页显示具体 GRANT 错误 | 创建/改密/停用禁用 | DBA 补权限 |
| Tenon schema 未知 | 顶栏黄色警告；只读模式 | 禁用用户写 | 升级管理端/确认版本 |
| `master.key` 缺失/权限错 | 启动失败或 reveal 禁用 | 禁用敏感操作 | 恢复 key/重新初始化 |
| TLS 证书过期/反代 502 | 浏览器证书错误/502；不降级 HTTP | 不可用 | 续证/修 nginx；`doctor` 告警 |
| 公网暴露但未启 MFA | 服务拒绝以 `exposure=public` 启动 | 全部禁用 | 启用 WebAuthn 或改回白名单/VPN |
| 审计写失败 | 显示“审计不可用” | 写操作失败（fail closed） | 修 DB/磁盘 |
| 控制层管理接口不可用（P1） | 就绪/在线显示“未接入” | 取消按钮禁用 | 恢复接口 |
| 浏览器网络抖动 | 读请求重试（退避）；写不自动重试 | 用 Idempotency-Key 手动重试 | — |

### 7.12 备份与恢复流程

1. 每日备份 `tenon_admin`（加密）；`master.key` 单独离线备份；`admin.secret` 在 Mac 单独备份；记录备份时间/校验和。
2. 备份 `systemd unit`、nginx 配置、证书/私钥（证书私钥按密钥级别保护）。
3. 恢复演练：新服务器 → 恢复 key → 恢复 DB → 恢复 systemd/nginx → `doctor` → 登录 → 抽查 reveal/审计。
4. 若怀疑服务器 key 泄露：立即轮换 `master.key` → 重算 verifier → 重加密所有用户密码 → 吊销管理员会话 → 审计/告警；评估 Tenon 用户密码是否需要全部重置。
5. 若怀疑 DB 泄露（但 key 未泄露）：轮换管理员密码与 DB 账号口令；监控异常登录；用户密码密文仍安全。
6. 若 Mac `admin.secret` 泄露：在服务器执行 `recover-admin` 重设密码、吊销会话；检查异常登录。
7. 若服务器 root/云控制台失守：视为 Tenon 全面失守；隔离主机、轮换全部密钥与用户密码、审计外送、评估通知。

---

## 8. 非功能设计

### 8.1 性能目标（服务器 + 本地 MySQL）

| 场景 | 目标 |
|---|---|
| 登录（含 Argon2） | P95 < 400ms（服务器 CPU；加 WebAuthn 另计） |
| 总览首屏 API | P95 < 800ms |
| 用户列表（100/页） | P95 < 400ms |
| 用户详情（含 30 天聚合） | P95 < 800ms |
| 用量 30 天（10 万行） | P0 < 1.5s；P1 汇总 < 500ms |
| 活跃用户（15m） | P95 < 500ms（缓存命中 < 50ms） |
| 轨迹首屏（100 事件） | P95 < 500ms；单条 payload 截断 |
| 前端首屏 | < 2s（nginx 静态资源 + gzip/brotli） |
| 写操作（创建用户） | P95 < 1s（含 Argon2 + 事务） |

### 8.2 安全要求

- 网络：管理端只绑 `127.0.0.1`；7200 不对公网开放；nginx/Caddy 终止 TLS；IP 白名单/VPN；MySQL 3306 不对公网开放。
- TLS：TLS 1.2+、HSTS、证书自动续期监控；禁止 HTTP 降级（`127.0.0.1/healthz` 除外）。
- 反向代理：只信任 loopback 代理；固定 `trusted_proxies`；正确处理 `X-Forwarded-For/Proto`；Host 头白名单（`admin.example.com`）；无 CORS。
- CSP：`default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'`。
- Permissions-Policy：`publickey-credentials-get=(self), publickey-credentials-create=(self)`；其余能力默认关闭。
- Cookie：`__Host-tadm_sid`，`Secure; HttpOnly; SameSite=Strict; Path=/`；仅 HTTPS。
- CSRF：SameSite + Origin 白名单 + 双重提交 token。
- 密码：Argon2id（管理员 t=3/m=64MiB/p=2；用户 t=2/m=64MiB/p=2 与 Tenon 一致）；常数时间比较；服务器 pepper。
- 加密：AES-256-GCM；随机 nonce；AAD 绑定用户；密钥版本；`master.key` 0600/systemd credential。
- MFA：公网必须 WebAuthn/passkey；VPN/白名单模式 P1；至少两把 passkey + 一次性恢复码。
- 审计：追加写；敏感操作同事务；日志脱敏；不记录密码/密文/密钥/token。
- 限流：登录/step-up/reveal/导出；失败锁定；nginx `limit_req` 兜底。
- 依赖：最小依赖；`govulncheck`；锁定版本；构建可复现；服务以非 root 专用用户运行。
- 前端：不使用 `v-html` 渲染轨迹/用户内容；Markdown（若有）走 DOMPurify；禁止内联脚本（Vite 产物 external）。
- 文件与上传：YAML 上传限 1MB、只解析不执行；导出路径固定目录 + 随机文件名校验归属与过期；不拼接用户输入为路径。
- CSV 导出：对以 `= + - @` 开头的单元格加前缀 `'`，防公式注入；原始值仍可在详情查看。
- 错误：对外通用；对内 request_id + 结构化日志。
- 服务器：专用用户、systemd 沙箱（`ProtectSystem=strict`、`PrivateTmp`、`NoNewPrivileges`）、密钥 0600；禁止把 `admin.secret` 放服务器。
- Mac：FileVault、锁屏、`admin.secret` 0600、P1 迁 Keychain；浏览器不保存管理员密码。

### 8.3 可用性与可维护性

- 远程管理端：单实例 + systemd 自动重启；允许短暂不可用；读页面可显示上次缓存并标“已过期”；写操作 fail closed，不排队。
- 结构化日志（JSON）写服务器文件，按天轮转 30 天；含 request_id、admin_id、route、status、latency；不含敏感 body。
- 服务器 metrics（可选）：请求计数/延迟、DB 延迟、错误数；由内网 Prometheus/日志采集，不直接公网暴露。
- 配置校验：启动时拒绝未知字段；schema/密钥/权限/TLS/代理自检。
- 迁移：版本化 SQL；启动时只读检查，不自动 DDL（与 Tenon 一致）。
- 部署：`systemd` + `nginx` 配置纳入版本管理；升级先备份 key/DB；支持蓝绿/滚动重启（单实例可短暂停机）。
- 备份：DB、`master.key`、`admin.secret` 三处分开；恢复演练见 §7.12。

### 8.4 兼容性

- MySQL 8.0+（生产 8.0.46）；`utf8mb4`；默认 collation 需在启动时检测并记录（大小写敏感性影响用户唯一性提示）。
- Go 1.25+（与 Tenon 一致）；Node 20+（前端构建）。
- 浏览器：macOS 最新 Chrome/Safari；最小宽度 1280；不做移动端 P0。
- Tenon schema：`000001_init.mysql.sql`；未知版本只读。

### 8.5 可访问性与体验

- WCAG AA 对比度；键盘可达；焦点可见；表格支持屏幕阅读器表头。
- 中文文案；错误提示包含“下一步做什么”；危险操作按钮不与普通按钮相邻。
- 所有时间双显示（相对 + 绝对 tooltip）；ID 可复制；长文本可展开/折叠。

### 8.6 测试策略

| 层级 | 内容 |
|---|---|
| 单元 | verifier 派生/校验、会话签名/过期、AES-GCM/AAD、密码生成、校验规则、成本计算、活跃分层、事件映射 |
| 集成 | MySQL 8 容器 + Tenon schema + admin schema；创建用户/改密/停用；审计事务回滚；幂等 |
| 兼容 | 管理端创建用户 → 真实 Tenon `POST /api/login` 204 → logout；旧密码失败 |
| 安全 | CSRF、XSS payload、SQL 注入、暴力破解锁定、会话吊销、cookie 属性、无秘密泄漏 |
| 性能 | 10 万用量行、1 万用户、1000 Run；页面预算 |
| 故障 | DB 断、网络断、TLS 证书过期、nginx 502、权限不足、schema 变更、磁盘满、审计失败 |
| E2E | Playwright：登录、创建、reveal、用量、活跃、轨迹、审计、登出 |
| 回归 | Tenon 升级后重跑兼容 + 只读检查 |

---

## 9. 实施计划与验收

### 9.0 实现形态（建议）

| 决策 | 建议 | 理由 |
|---|---|---|
| 代码位置 | 独立仓库/模块 `tenon-admin`；若必须同仓库，放 `cmd/tenon-admin` + `internal/admin` + `web-admin` | 管理端是独立安全边界，独立发版/密钥/数据库账号 |
| 后端 | Go 1.25+，`database/sql` + MySQL 驱动，`golang.org/x/crypto/argon2`，`embed` 前端产物 | 与 Tenon 技术栈一致；单二进制便于服务器部署 |
| 前端 | Vue 3 + TypeScript + Vite + Pinia + Element Plus + ECharts | 表格/表单/图表成熟；与 Tenon 生态一致 |
| 进程 | 单二进制 `tenon-admin`，`init-db`/`init-admin`/`doctor`/`serve`/`recover-admin`/`backup-keys` 子命令；systemd 管理 | 远程服务，非本机工具 |
| 部署 | nginx/Caddy + TLS + IP 白名单/VPN；管理端只监听 loopback；systemd 专用用户 + credential | 远程暴露面必须收紧 |
| 代码复用 | **不要直接复用** `internal/control/store`；只按契约复制少量 SQL/crypto，或依赖 `pkg/contract` | Tenon 的 store 绑定其迁移/权限模型；直接复用会把管理端和产品升级耦死 |
| 数据库访问 | 一个读写池（跨 schema 事务）；可选只读池 | P0 简单；P1 用只读池隔离分析查询 |
| 配置 | `/etc/tenon-admin/config.yaml` + `env`（0600）；敏感项不进 Git | 服务器密钥/密码策略 |

若选择同仓库：仍建议使用**独立 MySQL 账号、独立 schema、独立 `tadm_schema_migration`**，不要改 Tenon 的 `migrations/`；否则一次 Tenon 升级就可能把管理端表结构/权限带崩。

**部署配置要点（remote）**

`/etc/systemd/system/tenon-admin.service`：

```ini
[Unit]
Description=Tenon Admin
After=network-online.target mysql.service
Wants=network-online.target

[Service]
Type=simple
User=tenon-admin
Group=tenon-admin
EnvironmentFile=/etc/tenon-admin/env
LoadCredential=master:/etc/tenon-admin/keys/master.key
ExecStart=/usr/local/bin/tenon-admin serve --config /etc/tenon-admin/config.yaml
Restart=on-failure
RestartSec=3
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/tenon-admin /var/log/tenon-admin
CapabilityBoundingSet=
AmbientCapabilities=

[Install]
WantedBy=multi-user.target
```

`/etc/nginx/sites-enabled/tenon-admin.conf`（示意）：

```nginx
server {
  listen 443 ssl http2;
  server_name admin.example.com;

  ssl_certificate     /etc/letsencrypt/live/admin.example.com/fullchain.pem;
  ssl_certificate_key /etc/letsencrypt/live/admin.example.com/privkey.pem;
  add_header Strict-Transport-Security "max-age=31536000; includeSubDomains" always;

  # 只允许办公/VPN 出口；没有白名单就只开 VPN
  allow 203.0.113.10;
  deny  all;

  client_max_body_size 2m;

  location / {
    proxy_pass http://127.0.0.1:7200;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto https;
    proxy_read_timeout 60s;
  }
}
server {
  listen 80;
  server_name admin.example.com;
  return 301 https://$host$request_uri;
}
```

- 防火墙：只放行 22（限定管理 IP/VPN）、443（限定白名单/VPN）；**7200、3306 不对公网**。
- `trusted_proxies` 只配 `127.0.0.1`；不要让公网客户端直连 7200 伪造 `X-Forwarded-For`。
- 证书自动续期（certbot/acme.sh）+ 到期监控；`doctor` 检查证书剩余天数。
- **优先独立子域** `admin.example.com`；若必须与 Tenon 同域走 `/admin/`，需要改前端 base path、cookie `Path`、CSP、nginx rewrite，且 `__Host-` cookie 不能用（Path 必须 `/`），不推荐。

### 9.1 阶段

| 阶段 | 范围 | 交付 | 退出标准 |
|---|---|---|---|
| M0 骨架与部署 | CLI（init-db/init-admin/doctor/serve）、管理员表/会话/审计、登录/step-up/CSRF、systemd/nginx/TLS、Vue 壳 | 可通过 HTTPS 安全登录的空后台 | 登录/锁定/登出/审计通过；7200 不对外；DB 无明文管理员密码 |
| M1 用户开通 | 用户列表/详情/创建/改密/停用/启用/reveal、密文、幂等、用户 360 | 用户管理闭环 | 真实 Tenon 登录通过；停用立即生效；审计完整 |
| M2 模型与用量 | 模型导入/价格、用量 KPI/趋势/分布/导出、缓存 | 模型与用量页 | 与 SQL 对账一致；未定价处理正确 |
| M3 活跃与轨迹 | 活跃分层/轮询、Run 列表/详情/事件/消息/用量/跟随 | 活跃与任务页 | 轨迹配对正确；大数据截断；无假在线 |
| M4 总览与运维 | 总览、健康、设置、导出、备份命令、密钥轮换 | 可日常使用 | 故障提示可执行；备份恢复演练通过 |
| M5 加固 | 安全测试、性能、E2E、文档、验收 | v0.2 | 验收清单全绿；公网模式 MFA 已启用 |

P1（约 4 周）：WebAuthn/passkey、多管理员/RBAC、Tenon 管理接口（就绪/取消/presence）、预算告警、批量导入、价格重算、审计哈希链、异步导出、密码派生 vault key。  
P2：OPAQUE/PAKE、SSO、多租户、配额强制执行、内容安全、模型配置写回、账单对账。

> **公网入口的工期变化**：WebAuthn 从 P1 提前到 P0，增加约 5–7 人天；若还要“密码派生 vault key + recovery key”，再增加约 2–3 人天。若走 VPN/固定 IP 白名单，P0 可先不上 MFA。

### 9.2 工作量估算（单人全栈，±30%）

| 工作 | 人天 |
|---|---|
| 后端骨架/CLI/DB/迁移 | 4 |
| 管理员认证/密钥/会话/审计 | 4 |
| 用户开通/密文/幂等 | 5 |
| 模型/价格/用量 | 4 |
| 活跃/任务/轨迹 | 5 |
| 前端页面/组件/图表 | 6 |
| systemd/nginx/TLS/部署/密钥交付 | 3 |
| 测试/E2E/安全/文档 | 4 |
| **合计** | **35 人天**（约 5 周） |

### 9.3 P0 验收清单

- [ ] `init-admin --mode remote` 生成 `admin.secret`（Mac）与 `master.key`（部署到服务器，0600/systemd credential）；DB 无管理员明文密码；dump 搜索不含密码。
- [ ] 服务器 `master.key` 不在 Git/镜像/日志中；`admin.secret` 不在服务器文件系统。
- [ ] 7200 端口从公网/局域网不可达；只能经 HTTPS 反代访问；TLS 证书有效、HSTS 生效；HTTP 自动跳转/拒绝。
- [ ] 管理员登录成功/失败/锁定/登出/会话过期正常；`__Host-tadm_sid` 具备 Secure/HttpOnly/SameSite；CSRF、Host/Origin 校验通过。
- [ ] 只拿 DB dump（无 `master.key`）：不能登录、不能 reveal；DB 写入也不能伪造会话。
- [ ] 公网模式未启用 WebAuthn 时服务拒绝启动；启用后登录需要 passkey；恢复码可用。
- [ ] step-up 过期后 reveal 被拒；成功后 5 分钟内可 reveal；每次 reveal 有审计与 reason。
- [ ] 创建用户后 `t_user` 行存在，`Fpwd_hash` 可被真实 Tenon 登录验证（204）；密码与提交一致。
- [ ] 重复用户（含大小写差异）返回 409，且不产生半条数据；审计失败时整体回滚。
- [ ] 改密后旧密码失败、旧会话 401；停用后登录失败、已登录会话 401；启用后恢复。
- [ ] 历史用户显示“密码未知”，重置后变为可查看；已清除交付的用户显示“已清除”。
- [ ] 用量总请求/token/错误数与 SQL 对账一致；成本公式正确；未定价显示“未定价”与占比。
- [ ] 活跃用户按窗口正确聚合；仅有会话无活动的用户不显示为在线。
- [ ] Run 轨迹按 seq 正确、工具调用/结果配对、终态/错误展示；运行中可跟随；大 payload 截断/下载可用。
- [ ] 审计覆盖全部写操作/登录/reveal/导出；日志与 API 列表无密码/密文/密钥。
- [ ] DB/网络/TLS/反代故障、权限不足、schema 不匹配、密钥缺失时页面有明确提示且写入禁用。
- [ ] 备份/恢复：恢复 `master.key` + DB 后可登录并 reveal；轮换后新 key 可 reveal 新密文，旧 key 无法解密新密文；Mac 丢失可恢复管理员。
- [ ] 性能达到 §8.1 目标；10 万行用量页面可接受。
- [ ] E2E 覆盖 HTTPS 登录→创建→reveal→用量→活跃→轨迹→登出。

---

## 10. 风险与缓解

| 风险 | 影响 | 概率 | 缓解 |
|---|---|---|---|
| 直接写 `t_user` 与 Tenon 未来 schema 不兼容 | 创建失败/数据错误 | 中 | 启动列契约检查；未知版本只读；兼容测试；P1 改走 Tenon 管理 API |
| CLI/自注册旁路管理端 | 密码未知、审计缺口 | 中 | P0 约定 CLI 仅 break-glass；启动/定时扫描差异并标 unknown；P1 给 CLI 加审计或限制写路径 |
| 服务器被攻破（root/云控制台/RCE） | `master.key`+DB → 管理员被离线爆破、用户密码被解密、会话伪造 | 中 | 服务器是信任边界；systemd 沙箱/最小权限；P1 WebAuthn + 密码派生 vault；网络隔离；审计外送 |
| 管理端公网暴露且无 MFA | 撞库/爆破/接管 | 中 | P0 不直接公网；必须公网则 WebAuthn 进 P0；IP 白名单/VPN；限流 |
| 反向代理/TLS 配置错误 | IP 白名单绕过、明文、Host 注入 | 中 | 只信 loopback 代理；固定 trusted_proxies；配置测试；HSTS；`doctor` |
| 服务器 `master.key`/vault key 丢失 | 管理员无法登录/用户密码不可解 | 中 | 离线加密备份；与 DB 备份分开；季度恢复演练；P1 恢复密钥 |
| Mac `admin.secret` 丢失或泄露 | 密码记录丢失/被登录 | 中 | FileVault/锁屏/0600；P1 Keychain；异常登录告警；改密吊销 |
| 用户密码可还原带来的泄露面 | 高影响 | 中 | AES-GCM + 服务器密钥；step-up + reason + 审计；支持交付后清除；P1 密码派生 key |
| “在线”不准确引发误判 | 运营误判 | 高 | 明确分层与定义；显示窗口；P1 心跳 |
| 模型 `ready` 不可见 | 误以为可用 | 高 | P0 标“未知”；P1 管理接口；不猜环境变量 |
| 成本估算与账单不一致 | 预算偏差 | 中 | 标“估算”；价格版本；P2 对账 |
| 大数据量拖慢页面 | 体验差 | 中 | keyset 分页、缓存、日汇总、导出异步；不深分页 |
| 直接改 Run/事件 | 破坏状态机/数据 | 低但严重 | 权限层禁止写；产品不提供；P1 只走服务接口 |
| 审计被篡改 | 无法追责 | 中 | 追加写权限；哈希链；离线锚定；备份 |
| DB/nginx/TLS 故障 | 页面不可用 | 中 | 明确错误；读缓存；健康检查；写不排队；systemd 重启 |
| 管理员误操作（批量停用） | 用户不可用 | 中 | 二次确认/输入数量；dry-run；审计；可启用回滚 |
| 依赖漏洞 | 供应链风险 | 低 | 最小依赖、锁定、govulncheck、定期升级 |

---

## 11. 待确认决策（实现前请拍板）

| # | 问题 | 推荐 | 影响 |
|---|---|---|---|
| 1 | 远程管理端的入口方式：VPN/固定 IP 白名单，还是公网 HTTPS？ | **P0 先 VPN/固定 IP 白名单**；确需公网则把 WebAuthn 提进 P0 | 决定 §4.1.7、工期与验收 |
| 2 | “服务器被攻破”是否是必须防住的边界？ | 先区分：只防“读文件/DB 的静态泄露” → WebAuthn + 密码派生 vault 有意义；防 root/RCE 需客户端签名/远程证明，成本很高。推荐先接受服务器信任边界，P1 加 MFA/密码派生 | 决定 §4.1.7 与密钥方案 |
| 3 | Tenon 用户密码是“可还原的密文”还是“字面明文入库”？ | **密文 + 服务器密钥**；字面明文需书面接受备份/只读账号泄露风险 | 决定 §4.2.4 与合规 |
| 4 | 管理端管哪个库：`tenon_prod`、`tenon`，还是两个实例？ | **一个实例只绑一个库**；生产/开发各跑一个管理端 | 决定环境徽标与误操作风险 |
| 5 | 是否允许 P1 在 Tenon 增加 loopback 管理接口？ | **允许**；只读 overview/models + cancel，独立 token | 决定“就绪/取消/真实在线”能否做准 |
| 6 | P0 是否就要多管理员/RBAC？ | **P0 单管理员，表/路由预留**；P1 加 | 决定前端/接口工作量 |
| 7 | 成本只做展示，还是要预算告警/阻断？ | **P0 展示+导出，P1 告警**；阻断需改 Tenon | 决定 P1 范围 |
| 8 | 前端技术栈？ | **Vue 3 + TS + Vite + Element Plus + ECharts**（与 Tenon 生态一致，表格/图表成熟） | 决定组件与工期 |
| 9 | 审计/用量保留期与合规要求？ | 审计 365 天、用量 400 天；可配置 | 决定分区/归档/存储 |
| 10 | `admin.secret` 是否允许改为 macOS Keychain？ | **建议 P1 改 Keychain**；P0 至少 0600 + FileVault | 决定本机密码记录方式 |
| 11 | 是否需要“交付后自动清除 Tenon 密码密文”？ | **提供手动清除**；自动清除需明确交付确认流程 | 决定产品交互 |
| 12 | 是否允许管理端与 Tenon 同机部署？ | **允许**（P0 默认同机）；资源隔离 + 独立 systemd 服务/用户/DB 账号；高安全可拆机 | 决定部署拓扑 |

---

## 附录 A：Tenon 数据字典（管理相关）

### A.1 `t_user`

| 列 | 类型 | 管理端用途 |
|---|---|---|
| `Fid` | BIGINT PK | 排序/游标 |
| `Fuser_id` | VARCHAR(64) 唯一 | 登录名；创建时校验 |
| `Fname` | VARCHAR(64) | 显示名 |
| `Fpwd_hash` | VARCHAR(255) | Argon2id；管理端生成/更新 |
| `Fstatus` | TINYINT | 1 正常 / 2 禁用 |
| `Fmax_conv` | INT | 最大活跃对话数；创建时校验 |
| `Fcreate_time` / `Fmodify_time` | DATETIME | UTC；列表/详情 |

### A.2 `t_auth_session`

| 列 | 管理端用途 |
|---|---|
| `Fsession_hash` | 会话哈希（不展示） |
| `Fuser_id` | 关联用户 |
| `Fexpire_time` | 停用/改密时提前 |
| `Fcreate_time` | 会话创建时间（不是最后活跃） |

> 没有 IP/UA/last_seen；不能据此判断在线。

### A.3 `t_run`

| 列 | 管理端用途 |
|---|---|
| `Frun_id` / `Fconv_id` / `Fuser_id` | 任务标识/归属 |
| `Fmodel` | 模型 |
| `Fstatus` | 0 queued / 1 running / 2 finished / 3 failed / 4 cancelled / 5 dispatching / 6 cancelling / 7 finalizing |
| `Factive` | 1 活跃；终态 NULL |
| `Fgeneration` | 执行代际 |
| `Fcancel_req` / `Fstop_confirmed` | 取消/停止确认 |
| `Fcheckpoint_status` | 0 pending / 1 done / 2 failed |
| `Fcommit` | 预览 commit |
| `Fiterations` | 迭代数 |
| `Fprompt_tokens` / `Fcompl_tokens` | Run 汇总 token |
| `Ferror_code` / `Ferror_msg` | 失败原因 |
| `Fstart_time` / `Fend_time` / `Fcreate_time` | 耗时 |

### A.4 `t_event`

| 列 | 管理端用途 |
|---|---|
| `Fconv_id` / `Fseq` | 对话内全序 |
| `Frun_id` | 归属任务 |
| `Fsource` | 0 runtime / 1 control |
| `Ftype` | 事件类型 |
| `Fpayload` | JSON；完整事件 |
| `Fcreate_time` | 时间 |
| `Fruntime_epoch` / `Fagent_session_id` / `Frt_seq` | 源事件去重 |

### A.5 `t_llm_usage`

| 列 | 管理端用途 |
|---|---|
| `Fconv_id` / `Frun_id` / `Fuser_id` / `Fmodel` | 维度 |
| `Fprompt_tokens` / `Fcompl_tokens` | token |
| `Fcost_ms` | 代理耗时（非纯模型延迟） |
| `Fhttp_status` | 200/0/4xx/5xx |
| `Fcreate_time` | 时间（UTC） |

### A.6 其他

- `t_conversation.Flast_active_time`：活跃用户主信号。
- `t_message.Frole`：1 用户 / 2 助手。
- `t_sandbox.Fstatus`：0 absent / 1 creating / 2 running / 3 stopped / 4 error / 5 removed。
- `t_schema_migration.version`：当前 `000001_init.mysql.sql`。

## 附录 B：Argon2id 兼容细节

Tenon 的格式：

```text
$argon2id$v=19$m=<KiB>,t=<iterations>,p=<threads>$<base64rawstd(salt)>$<base64rawstd(hash)>
```

`auth.verify` 的解析约束：

- 按 `$` 分割必须 6 段；`parts[1] == "argon2id"`；`parts[2] == "v=19"`。
- `parts[3]` 必须能被 `fmt.Sscanf(..., "m=%d,t=%d,p=%d", ...)` 解析；顺序必须是 `m,t,p`。
- salt/hash 用 `base64.RawStdEncoding`（无 `=` padding）。
- `hash` 长度决定派生 key 长度（Tenon 用 32B）。
- 密码按 UTF-8 字节比较；不 trim。

管理端生成伪代码：

```go
salt := random(16)
key := argon2.IDKey([]byte(password), salt, 2, 64*1024, 2, 32)
hash := fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
    65536, 2, 2,
    base64.RawStdEncoding.EncodeToString(salt),
    base64.RawStdEncoding.EncodeToString(key))
```

验收测试：

1. 管理端创建 `alice`，密码 `P@ssw0rd-...`。
2. 直接调用 Tenon `POST /api/login`（同一 MySQL、真实 control），期望 `204` 且 Set-Cookie。
3. `POST /api/logout`；审计无密码泄漏。
4. 旧密码/错误密码期望 `401`。

注意：

- 若 Tenon 以后改 `auth.argon2` 默认参数，旧 hash 仍可验证（参数自描述）；管理端可读取 Tenon 配置保持一致，但不是必须。
- MySQL 默认 collation 大小写不敏感：`Alice`/`alice` 在唯一索引上冲突；管理端预检必须按实际 collation，不要只做 Go 字符串比较。
- 不要在日志/错误里打印 hash 或密码。

**设计阶段的本地验证**：用与 Tenon `verify` 相同的解析/校验逻辑复刻本方案生成的 hash，实测长度 97 字符，正确密码返回 `true`、错误密码返回 `false`。这只验证了格式与算法兼容，不等同于真实环境联调；真实控制层登录测试纳入 §9.3 验收。本次设计未对生产库执行任何写操作。

## 附录 C：术语

| 术语 | 含义 |
|---|---|
| Tenon | 原型工坊主系统（control/wsd/agent-runtime/web） |
| Control | Tenon 控制层，管理对话、Run、事件、模型代理 |
| Run | 一轮 Agent 任务（用户消息触发的执行） |
| Event | Run 的执行事件（文本、工具调用/结果、控制状态） |
| Presence | 由业务活动派生的活跃状态，不是真实在线心跳 |
| Step-up | 敏感操作前重新验证管理员密码 |
| Pepper | 与密码一起参与 KDF 的服务器密钥，不与 verifier 同库存放 |
| Verifier | 可验证密码但不可逆的派生值 |
| Vault | 管理端保存 Tenon 用户密码密文的模块/表 |
| P0/P1/P2 | 本期/下一期/更远期 |

## 附录 D：参考代码位置

| 路径 | 用途 |
|---|---|
| `migrations/000001_init.mysql.sql` | Tenon 表结构/状态字段 |
| `internal/control/auth/auth.go` | Argon2id 生成/校验、登录限流、会话 |
| `internal/control/store/auth.go` | 用户创建/改密/停用/会话过期 |
| `internal/control/conversation/service.go` | `Fmax_conv` 限制、消息角色 |
| `internal/control/store/run.go` | Run 状态机/活跃标记/终态 |
| `internal/control/store/event.go` | 事件序号/追加/查询 |
| `internal/control/event/event.go` | UI 事件映射/工具截断 |
| `internal/control/consumer/consumer.go` | 运行时事件落库 |
| `internal/control/llm/catalog.go` | 模型目录/就绪判断 |
| `internal/control/llm/proxy.go` | 用量记录/上游错误 |
| `internal/control/llm/usage.go` | 用量写入/汇总 |
| `internal/control/quota/quota.go` | 并发配额（Run/Sandbox/Build） |
| `internal/control/app/app.go` | 控制层装配/metrics/`/debug/status` |
| `docs/deployment.md` | 生产库 `tenon_prod`、账号权限、端口 |
| `pkg/contract/runtime.go` | 运行时事件/状态 DTO |

---

**本文是设计基线，不是实现。** 建议先确认 §11 的 1–4 项，再进入 M0；确认后可按本设计直接拆分任务与接口。

