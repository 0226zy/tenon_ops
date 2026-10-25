-- ============================================================================
-- Tenon Ops schema (MySQL 8.0+)
-- 独立 schema：tenon_admin
--
-- Tenon Ops 管理端部署在远程服务器（127.0.0.1:7200，nginx/Caddy 终止 TLS）。
-- 执行方式（由 DBA / 管理员账号在数据库服务器执行一次）：
--   mysql -h 127.0.0.1 -P 3306 -u <admin> -p < schema.sql
--
-- 应用账号（tenon_ops_app）只需要：
--   tenon_prod:  SELECT + t_user 身份列的 INSERT/UPDATE + t_auth_session.Fexpire_time 的 UPDATE
--   tenon_admin: 逐表最小权限（部分表无 DELETE；审计表仅 SELECT/INSERT）
-- 不授予 DDL、不授予 t_user 的 DELETE。
-- 服务器密钥（master.key/pepper/vault）不在数据库中，见设计文档 §4.1.3。
-- ============================================================================

CREATE DATABASE IF NOT EXISTS `tenon_admin`
  DEFAULT CHARACTER SET utf8mb4
  COLLATE utf8mb4_0900_ai_ci;

USE `tenon_admin`;

-- ---------------------------------------------------------------------------
-- 迁移版本（管理端自己的版本表，不碰 Tenon 的 t_schema_migration）
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `tadm_schema_migration` (
  `version`    VARCHAR(255) NOT NULL,
  `applied_at` DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`version`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- ---------------------------------------------------------------------------
-- 管理员
-- 密码验证器 = Argon2id(HMAC-SHA256(server_pepper, password), salt)
-- server_pepper 由服务器 /etc/tenon-admin/keys/master.key 派生，绝不入库。
-- 管理员密码明文只保存在 Mac 的 admin.secret，不在服务器/数据库。
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `tadm_admin` (
  `Fid`             BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `Fadmin_id`       VARCHAR(64)     NOT NULL,
  `Fdisplay_name`   VARCHAR(64)     NOT NULL DEFAULT '',
  `Frole`           VARCHAR(16)     NOT NULL DEFAULT 'super_admin'
                    COMMENT 'super_admin | operator | support | auditor',
  `Fpwd_algo`       VARCHAR(32)     NOT NULL DEFAULT 'argon2id-pepper-v1',
  `Fpwd_params`     VARCHAR(64)     NOT NULL DEFAULT 'm=65536,t=3,p=2',
  `Fpwd_salt`       VARBINARY(16)   NOT NULL,
  `Fpwd_hash`       VARBINARY(64)   NOT NULL,
  `Fpwd_version`    INT             NOT NULL DEFAULT 1,
  `Fsession_epoch`  BIGINT UNSIGNED NOT NULL DEFAULT 1
                    COMMENT '改密/强制下线时递增，旧 cookie 立即失效',
  `Fstatus`         TINYINT         NOT NULL DEFAULT 1 COMMENT '1 正常 2 禁用',
  `Ffailed_count`   INT             NOT NULL DEFAULT 0,
  `Flocked_until`   DATETIME        NULL,
  `Flast_login_at`  DATETIME        NULL,
  `Flast_login_ip`  VARCHAR(64)     NOT NULL DEFAULT '',
  `Fpwd_changed_at` DATETIME        NOT NULL,
  `Fcreate_time`    DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `Fmodify_time`    DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP
                    ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`Fid`),
  UNIQUE KEY `uk_admin_id` (`Fadmin_id`),
  CONSTRAINT `chk_admin_role`
    CHECK (`Frole` IN ('super_admin','operator','support','auditor')),
  CONSTRAINT `chk_admin_status`
    CHECK (`Fstatus` IN (1,2))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- ---------------------------------------------------------------------------
-- 管理员会话
-- cookie 本身由服务器 session key 签名；本表只负责吊销/step-up/审计。
-- 仅凭本表无法伪造 cookie。
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `tadm_admin_session` (
  `Fid`                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `Fsid_hash`           CHAR(64)        NOT NULL COMMENT 'SHA-256(session id)',
  `Fadmin_id`           VARCHAR(64)     NOT NULL,
  `Fexpire_time`        DATETIME        NOT NULL,
  `Fidle_expire_time`   DATETIME        NOT NULL,
  `Felevated_until`     DATETIME        NULL COMMENT 'step-up 有效期',
  `Fip`                 VARCHAR(64)     NOT NULL DEFAULT '',
  `Fua`                 VARCHAR(255)    NOT NULL DEFAULT '',
  `Frevoked`            TINYINT         NOT NULL DEFAULT 0,
  `Fcreate_time`        DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `Flast_seen_at`       DATETIME        NULL,
  PRIMARY KEY (`Fid`),
  UNIQUE KEY `uk_sid_hash` (`Fsid_hash`),
  KEY `idx_admin_active` (`Fadmin_id`,`Frevoked`,`Fexpire_time`),
  KEY `idx_expire` (`Fexpire_time`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- ---------------------------------------------------------------------------
-- 管理员 WebAuthn/passkey 凭据（P1；公网模式必须启用）
-- 服务器只存公钥；私钥在 Mac 的 Secure Enclave / iCloud Keychain。
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `tadm_admin_credential` (
  `Fid`           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `Fadmin_id`     VARCHAR(64)     NOT NULL,
  `Fkind`         VARCHAR(16)     NOT NULL DEFAULT 'webauthn',
  `Fcredential_id` VARBINARY(1023) NOT NULL,
  `Fpublic_key`   BLOB            NOT NULL,
  `Fsign_count`   BIGINT UNSIGNED NOT NULL DEFAULT 0,
  `Ftransports`   VARCHAR(64)     NOT NULL DEFAULT '',
  `Faaguid`       CHAR(36)        NOT NULL DEFAULT '',
  `Fname`         VARCHAR(64)     NOT NULL DEFAULT '',
  `Fcreated_at`   DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `Flast_used_at` DATETIME        NULL,
  PRIMARY KEY (`Fid`),
  UNIQUE KEY `uk_credential_id` (`Fcredential_id`),
  KEY `idx_admin` (`Fadmin_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- ---------------------------------------------------------------------------
-- Tenon 用户的管理端扩展（不复制 Fname/Fstatus/Fmax_conv，避免双写不一致）
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `tadm_tenon_user` (
  `Ftenon_user_id`  VARCHAR(64)  NOT NULL,
  `Fnote`           VARCHAR(255) NOT NULL DEFAULT '',
  `Ftags`           JSON         NULL,
  `Fpwd_state`      TINYINT      NOT NULL DEFAULT 0
                    COMMENT '0 unknown 1 managed 2 delivered_removed',
  `Fcreated_by`     VARCHAR(64)  NOT NULL DEFAULT '',
  `Flast_reveal_at` DATETIME     NULL,
  `Freveal_count`   INT UNSIGNED NOT NULL DEFAULT 0,
  `Fdisabled_reason` VARCHAR(255) NOT NULL DEFAULT '',
  `Fcreate_time`    DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `Fmodify_time`    DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP
                    ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`Ftenon_user_id`),
  KEY `idx_pwd_state` (`Fpwd_state`),
  KEY `idx_created` (`Fcreated_by`,`Fcreate_time`),
  CONSTRAINT `chk_pwd_state`
    CHECK (`Fpwd_state` IN (0,1,2))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- ---------------------------------------------------------------------------
-- Tenon 用户密码密文
-- AES-256-GCM；nonce 12B；AAD = v1 / user_id / key_version；
-- master key 在服务器 /etc/tenon-admin/keys/master.key（0600/systemd credential）。
-- DB 单独泄露不能解密；服务器密钥文件失守可解密（服务器是信任边界）。
-- 若未来支持“字面明文”模式，必须新增独立列/独立开关并书面接受风险，
-- 不要复用本表。
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `tadm_user_secret` (
  `Ftenon_user_id` VARCHAR(64)    NOT NULL,
  `Fkey_version`   INT            NOT NULL DEFAULT 1,
  `Fnonce`         VARBINARY(12)  NOT NULL,
  `Fciphertext`    VARBINARY(255) NOT NULL,
  `Fcreated_by`    VARCHAR(64)    NOT NULL DEFAULT '',
  `Fcreate_time`   DATETIME       NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `Frotated_at`    DATETIME       NULL,
  `Fdelivered_at`  DATETIME       NULL,
  PRIMARY KEY (`Ftenon_user_id`),
  KEY `idx_key_version` (`Fkey_version`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- ---------------------------------------------------------------------------
-- 模型目录镜像（不含密钥；api_key_env 只是变量名）
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `tadm_model` (
  `Fmodel_id`        VARCHAR(64)  NOT NULL,
  `Fdisplay_name`    VARCHAR(128) NOT NULL DEFAULT '',
  `Fprovider`        VARCHAR(64)  NOT NULL DEFAULT '',
  `Fupstream_model`  VARCHAR(128) NOT NULL DEFAULT '',
  `Fbase_url`        VARCHAR(255) NOT NULL DEFAULT '',
  `Fapi_key_env`     VARCHAR(64)  NOT NULL DEFAULT '',
  `Ftags`            JSON         NULL,
  `Fcontext_window`  INT          NOT NULL DEFAULT 0,
  `Fmax_output`      INT          NOT NULL DEFAULT 0,
  `Fenabled`         TINYINT      NOT NULL DEFAULT 1,
  `Fis_default`      TINYINT      NOT NULL DEFAULT 0,
  `Fsource`          VARCHAR(16)  NOT NULL DEFAULT 'yaml'
                     COMMENT 'yaml | manual',
  `Fnotes`           VARCHAR(512) NOT NULL DEFAULT '',
  `Fsynced_at`       DATETIME     NULL,
  `Fcreate_time`     DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `Fmodify_time`     DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP
                     ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`Fmodel_id`),
  KEY `idx_enabled` (`Fenabled`),
  KEY `idx_default` (`Fis_default`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- ---------------------------------------------------------------------------
-- 模型价格历史（按生效时间；成本估算用）
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `tadm_model_price` (
  `Fid`                  BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `Fmodel_id`            VARCHAR(64)     NOT NULL,
  `Feffective_from`      DATETIME        NOT NULL,
  `Finput_per_mtok`      DECIMAL(18,6)   NOT NULL DEFAULT 0,
  `Foutput_per_mtok`     DECIMAL(18,6)   NOT NULL DEFAULT 0,
  `Fcurrency`            CHAR(3)         NOT NULL DEFAULT 'CNY',
  `Fcreated_by`          VARCHAR(64)     NOT NULL DEFAULT '',
  `Fcreate_time`         DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`Fid`),
  UNIQUE KEY `uk_model_effective` (`Fmodel_id`,`Feffective_from`),
  KEY `idx_model_time` (`Fmodel_id`,`Feffective_from`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- ---------------------------------------------------------------------------
-- 用量日汇总（P1；P0 可先不写，保留表结构）
-- 以 UTC 日或展示时区日为准由应用决定，建议按 Asia/Shanghai 的日界写入。
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `tadm_usage_daily` (
  `Fday`             DATE            NOT NULL,
  `Fuser_id`         VARCHAR(64)     NOT NULL,
  `Fmodel`           VARCHAR(64)     NOT NULL,
  `Frequests`        BIGINT          NOT NULL DEFAULT 0,
  `Fprompt_tokens`   BIGINT          NOT NULL DEFAULT 0,
  `Fcompl_tokens`    BIGINT          NOT NULL DEFAULT 0,
  `Ferror_requests`  BIGINT          NOT NULL DEFAULT 0,
  `Fcost`            DECIMAL(18,6)   NOT NULL DEFAULT 0
                     COMMENT '已定价请求的估算成本；未定价覆盖看 Fpriced_requests/Frequests',
  `Fpriced_requests` BIGINT          NOT NULL DEFAULT 0,
  `Fcomputed_at`     DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`Fday`,`Fuser_id`,`Fmodel`),
  KEY `idx_day_model` (`Fday`,`Fmodel`),
  KEY `idx_user_day` (`Fuser_id`,`Fday`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- ---------------------------------------------------------------------------
-- 审计：追加写；应用账号只有 INSERT/SELECT（在 GRANT 层限制）
-- 不记录密码/密文/密钥/token。
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `tadm_audit_log` (
  `Fid`          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `Fts`          DATETIME(3)     NOT NULL,
  `Fadmin_id`    VARCHAR(64)     NOT NULL DEFAULT '',
  `Faction`      VARCHAR(64)     NOT NULL,
  `Ftarget_type` VARCHAR(32)     NOT NULL DEFAULT '',
  `Ftarget_id`   VARCHAR(64)     NOT NULL DEFAULT '',
  `Fresult`      TINYINT         NOT NULL DEFAULT 1 COMMENT '1 成功 0 失败',
  `Fip`          VARCHAR(64)     NOT NULL DEFAULT '',
  `Fua`          VARCHAR(255)    NOT NULL DEFAULT '',
  `Frequest_id`  CHAR(26)        NOT NULL DEFAULT '',
  `Freason`      VARCHAR(255)    NOT NULL DEFAULT '',
  `Fdetail`      JSON            NULL COMMENT 'before/after 等脱敏信息',
  `Fprev_hash`   CHAR(64)        NULL,
  `Fentry_hash`  CHAR(64)        NULL,
  PRIMARY KEY (`Fid`),
  KEY `idx_ts` (`Fts`),
  KEY `idx_admin_ts` (`Fadmin_id`,`Fts`),
  KEY `idx_target_ts` (`Ftarget_type`,`Ftarget_id`,`Fts`),
  KEY `idx_action_ts` (`Faction`,`Fts`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- ---------------------------------------------------------------------------
-- 写操作幂等
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `tadm_operation` (
  `Frequest_id`   CHAR(26)     NOT NULL,
  `Fkind`         VARCHAR(32)  NOT NULL,
  `Ftarget_id`    VARCHAR(64)  NOT NULL DEFAULT '',
  `Frequest_hash` CHAR(64)     NOT NULL,
  `Fstatus`       TINYINT      NOT NULL DEFAULT 0 COMMENT '0 进行中 1 成功 2 失败',
  `Fresult`       JSON         NULL COMMENT '不含密码等秘密',
  `Fcreate_time`  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `Fexpire_time`  DATETIME     NOT NULL,
  PRIMARY KEY (`Frequest_id`),
  KEY `idx_expire` (`Fexpire_time`),
  KEY `idx_kind_target` (`Fkind`,`Ftarget_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- ---------------------------------------------------------------------------
-- 非敏感设置
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `tadm_setting` (
  `Fkey`        VARCHAR(64) NOT NULL,
  `Fvalue`      TEXT        NOT NULL,
  `Fmodify_time` DATETIME   NOT NULL DEFAULT CURRENT_TIMESTAMP
                ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`Fkey`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- ---------------------------------------------------------------------------
-- 异步导出（P1）
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `tadm_export_job` (
  `Fid`          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `Frequest_id`  CHAR(26)        NOT NULL,
  `Fkind`        VARCHAR(32)     NOT NULL COMMENT 'usage | audit | runs',
  `Fquery`       JSON            NOT NULL,
  `Fstatus`      TINYINT         NOT NULL DEFAULT 0 COMMENT '0 queued 1 running 2 done 3 failed',
  `Frows`        BIGINT          NOT NULL DEFAULT 0,
  `Ffile_path`   VARCHAR(255)    NOT NULL DEFAULT '',
  `Ferror`       VARCHAR(512)    NOT NULL DEFAULT '',
  `Fcreated_by`  VARCHAR(64)     NOT NULL DEFAULT '',
  `Fcreate_time` DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `Fexpire_time` DATETIME        NOT NULL,
  PRIMARY KEY (`Fid`),
  UNIQUE KEY `uk_request` (`Frequest_id`),
  KEY `idx_status` (`Fstatus`,`Fcreate_time`),
  KEY `idx_expire` (`Fexpire_time`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- ---------------------------------------------------------------------------
-- 默认设置（重复执行安全）
-- ---------------------------------------------------------------------------
INSERT IGNORE INTO `tadm_setting` (`Fkey`,`Fvalue`) VALUES
  ('ui.timezone', 'Asia/Shanghai'),
  ('ui.refresh_default_seconds', '30'),
  ('presence.default_window', '15m'),
  ('usage.cache_seconds', '60'),
  ('audit.retention_days', '365'),
  ('export.retention_hours', '24'),
  ('export.max_rows', '50000');

INSERT IGNORE INTO `tadm_schema_migration` (`version`)
VALUES ('000001_init.mysql.sql');

-- ---------------------------------------------------------------------------
-- 授权模板（请替换密码；不要直接照抄到生产）
--
-- 原则：
--   1. Tenon 库只读 + 只允许写 t_user 的身份列 + 只允许更新会话过期时间；
--   2. 管理库逐表授权，审计表只有 INSERT/SELECT；
--   3. 不授予 DDL，不授予 t_user 的 DELETE。
-- ---------------------------------------------------------------------------
-- CREATE USER 'tenon_ops_app'@'localhost' IDENTIFIED BY '<strong-random>';
-- -- 如通过 TCP 127.0.0.1 连接（含 SSH 隧道 bootstrap），再建对应 host：
-- CREATE USER 'tenon_ops_app'@'127.0.0.1' IDENTIFIED BY '<strong-random>';
-- -- 两个 host 的账号需要分别执行下面的 GRANT（MySQL 不跨 host 复制权限）。
-- -- 不要使用 @'%'，除非防火墙/私网已严格限制。
--
-- -- Tenon：只读全库
-- GRANT SELECT ON `tenon_prod`.* TO 'tenon_ops_app'@'localhost';
-- -- Tenon：只允许插入身份列
-- GRANT INSERT (`Fuser_id`,`Fname`,`Fpwd_hash`,`Fstatus`,`Fmax_conv`,`Fcreate_time`,`Fmodify_time`)
--   ON `tenon_prod`.`t_user` TO 'tenon_ops_app'@'localhost';
-- -- Tenon：只允许改身份列（P1 改显示名/max_conv 也在此列）
-- GRANT UPDATE (`Fname`,`Fpwd_hash`,`Fstatus`,`Fmax_conv`,`Fmodify_time`)
--   ON `tenon_prod`.`t_user` TO 'tenon_ops_app'@'localhost';
-- -- Tenon：只允许提前会话过期时间
-- GRANT UPDATE (`Fexpire_time`) ON `tenon_prod`.`t_auth_session`
--   TO 'tenon_ops_app'@'localhost';
--
-- -- 管理库：逐表授权；只给真正需要的动作
-- GRANT SELECT, INSERT, UPDATE ON `tenon_admin`.`tadm_admin` TO 'tenon_ops_app'@'localhost';
-- GRANT SELECT, INSERT, UPDATE, DELETE ON `tenon_admin`.`tadm_admin_session` TO 'tenon_ops_app'@'localhost';
-- GRANT SELECT, INSERT, UPDATE, DELETE ON `tenon_admin`.`tadm_admin_credential` TO 'tenon_ops_app'@'localhost';
-- GRANT SELECT, INSERT, UPDATE ON `tenon_admin`.`tadm_tenon_user` TO 'tenon_ops_app'@'localhost';
-- GRANT SELECT, INSERT, UPDATE, DELETE ON `tenon_admin`.`tadm_user_secret` TO 'tenon_ops_app'@'localhost';
-- GRANT SELECT, INSERT, UPDATE ON `tenon_admin`.`tadm_model` TO 'tenon_ops_app'@'localhost';
-- GRANT SELECT, INSERT ON `tenon_admin`.`tadm_model_price` TO 'tenon_ops_app'@'localhost';
-- GRANT SELECT, INSERT, UPDATE, DELETE ON `tenon_admin`.`tadm_usage_daily` TO 'tenon_ops_app'@'localhost';
-- GRANT SELECT, INSERT, UPDATE, DELETE ON `tenon_admin`.`tadm_operation` TO 'tenon_ops_app'@'localhost';
-- GRANT SELECT, INSERT, UPDATE ON `tenon_admin`.`tadm_setting` TO 'tenon_ops_app'@'localhost';
-- GRANT SELECT, INSERT, UPDATE, DELETE ON `tenon_admin`.`tadm_export_job` TO 'tenon_ops_app'@'localhost';
-- GRANT SELECT, INSERT ON `tenon_admin`.`tadm_schema_migration` TO 'tenon_ops_app'@'localhost';
-- GRANT SELECT, INSERT ON `tenon_admin`.`tadm_audit_log` TO 'tenon_ops_app'@'localhost';
--
-- -- 可选只读账号（分析查询用；不参与写事务）
-- CREATE USER 'tenon_admin_ro'@'localhost' IDENTIFIED BY '<strong-random>';
-- GRANT SELECT ON `tenon_prod`.* TO 'tenon_admin_ro'@'localhost';
-- GRANT SELECT ON `tenon_admin`.* TO 'tenon_admin_ro'@'localhost';
