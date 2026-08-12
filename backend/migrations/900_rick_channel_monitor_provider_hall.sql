-- Migration: 900_rick_channel_monitor_provider_hall
-- 供应商大厅（Provider Hall）所需的渠道监控扩展字段。
--
-- 编号使用 900 段而非紧接上游序列，避免上游新增迁移时撞号；
-- 迁移按文件名字典序执行，900_ 始终排在上游三位数迁移之后。
--
-- group_id：历史上监控只有 group_name 字符串，无法与 V2 被动统计
--           （按 group_id 聚合）和分组倍率对齐。刻意不加外键约束：
--           分组删除后留悬空 ID，展示层回退到 group_name 即可。
-- public_visible：默认 false，避免升级后把既有监控项一次性曝光给用户。

ALTER TABLE channel_monitors
    ADD COLUMN IF NOT EXISTS group_id BIGINT,
    ADD COLUMN IF NOT EXISTS public_visible BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS public_note VARCHAR(200) DEFAULT '',
    ADD COLUMN IF NOT EXISTS report_url VARCHAR(500) DEFAULT '',
    ADD COLUMN IF NOT EXISTS expected_input_tokens INTEGER;

-- 用户端大厅只查「已展示且启用」的监控项。
CREATE INDEX IF NOT EXISTS channelmonitor_public_visible_enabled
    ON channel_monitors (public_visible, enabled);

CREATE INDEX IF NOT EXISTS channelmonitor_group_id
    ON channel_monitors (group_id);
