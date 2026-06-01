-- 分销站「从生产同步账号」功能：账号来源标识 + 同步幂等键。
-- source: manual=后台手动添加, synced=主站(生产)同步而来
-- sync_source_id: synced 时记录生产库原始账号 id，用于同步幂等匹配（避免重复导入）
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS source VARCHAR(20) NOT NULL DEFAULT 'manual';
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS sync_source_id BIGINT;

CREATE INDEX IF NOT EXISTS idx_accounts_source ON accounts(source);
CREATE INDEX IF NOT EXISTS idx_accounts_sync_source_id ON accounts(sync_source_id);

-- 一次性回填：分销库当前账号全部来自生产整库导入(id 与生产一致)，标记为 synced 并自映射。
-- 仅对未软删除、尚未标记的行生效；若本环境已有本地手动新增账号，先人工甄别后再执行本段。
UPDATE accounts
SET source = 'synced', sync_source_id = id
WHERE source = 'manual' AND sync_source_id IS NULL AND deleted_at IS NULL;
