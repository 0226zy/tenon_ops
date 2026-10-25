-- Pricing v2: cache-hit input and weekday peak multiplier.
-- Apply once to an existing tenon_admin schema; new installs get these columns
-- from 000001_init.mysql.sql.
ALTER TABLE `tadm_model_price`
  ADD COLUMN `Fcache_hit_per_mtok` DECIMAL(18,6) NOT NULL DEFAULT 0 AFTER `Finput_per_mtok`,
  ADD COLUMN `Fpeak_multiplier` DECIMAL(8,4) NOT NULL DEFAULT 2.0000 AFTER `Foutput_per_mtok`;

INSERT IGNORE INTO `tadm_schema_migration` (`version`)
VALUES ('000002_pricing_peak.mysql.sql');
