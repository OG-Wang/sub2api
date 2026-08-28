-- Migration: 903_rick_unbounded_group_rate_multiplier
-- 分组倍率不应被 DECIMAL(10,4) 限制为最多 4 位小数和 6 位整数。
-- 业务层只要求倍率是有限且大于 0 的数；PostgreSQL NUMERIC（不指定 precision/scale）
-- 保留这个业务边界之外的合法精度和数量级。

ALTER TABLE groups
    ALTER COLUMN rate_multiplier TYPE NUMERIC
    USING rate_multiplier::NUMERIC;
