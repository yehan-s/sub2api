-- 分销站「从生产同步账号」功能：账号来源标识 + 同步幂等键。
-- source: manual=后台手动添加, synced=主站(生产)同步而来
-- sync_source_id: synced 时记录生产库原始账号 id，用于同步幂等匹配（避免重复导入）
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS source VARCHAR(20) NOT NULL DEFAULT 'manual';
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS sync_source_id BIGINT;

CREATE INDEX IF NOT EXISTS idx_accounts_source ON accounts(source);
CREATE INDEX IF NOT EXISTS idx_accounts_sync_source_id ON accounts(sync_source_id);

-- 注意：此处不做任何回填。同一镜像的迁移会在生产与分销两边都执行，
-- 若在此回填 source='synced' 会污染生产账号语义（生产账号本就是 manual）。
-- 新增列默认即 'manual'，符合生产语义；分销站若由生产整库导入而来需把历史账号
-- 标记为 synced，请在部署时只对【分销库】单独执行回填（见部署手册 T15）。
