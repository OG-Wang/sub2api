-- Migration: 902_rick_channel_monitor_cached_input_tokens
-- 探测记录补一列：输入 token 里命中缓存的部分。
--
-- 背景：上游报的 prompt_tokens 是「含缓存命中的总输入」。
-- 直接扣掉缓存存净输入是行不通的——探测反复发送完全相同的内容，
-- 缓存命中率会一路走高，净输入随之趋近 0，指标失去意义。
--
-- 所以总输入与缓存分开存：
--   input_tokens          模型实际处理的总输入（稳定，用于展示与注水检测）
--   cached_input_tokens   其中命中缓存的部分
--   两者相减 = 网关用量记录里的 input，可用于对账。

ALTER TABLE channel_monitor_histories
    ADD COLUMN IF NOT EXISTS cached_input_tokens INTEGER;
