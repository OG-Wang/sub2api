-- Migration: 901_rick_channel_monitor_probe_usage
-- 探测侧指标：首 token 耗时与上游报告的 token 用量。
--
-- 三列都可空：历史行没有这些数据，检测失败时也拿不到。
-- ttft_ms 需要流式探测才能测到，非流式探测下恒为 NULL（先建列，后填数）。
--
-- 明细表只保留 1 天并按批物理删，加三个可空 INTEGER 列对存储影响可忽略。

ALTER TABLE channel_monitor_histories
    ADD COLUMN IF NOT EXISTS ttft_ms INTEGER,
    ADD COLUMN IF NOT EXISTS input_tokens INTEGER,
    ADD COLUMN IF NOT EXISTS output_tokens INTEGER;
