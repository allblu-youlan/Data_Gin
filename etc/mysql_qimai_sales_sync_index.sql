-- 企迈销售同步查询索引（MySQL 8 / InnoDB）。先选择应用数据库，再分段执行。
-- 与 migrate schema 的 2026-11-28-qimai-sales-sync-index-v1 及 migrate query-indexes 保持一致。
-- 本脚本供人工审阅执行，不由应用启动执行。

-- 1. 检查现有索引。若 idx_qimai_sales_sync 已存在，确认列序为 shop_code/status/synced 后跳过建索引。
SHOW INDEX FROM `qimai_order_data`;

-- 2. 低峰在线创建非唯一索引。遇到不支持在线操作或锁等待错误即停止，不改用锁表重建。
-- 临时限制元数据锁等待，并恢复本连接原设置。
SET @qimai_previous_lock_wait_timeout = @@SESSION.lock_wait_timeout;
SET SESSION lock_wait_timeout = 5;
ALTER TABLE `qimai_order_data` ADD INDEX `idx_qimai_sales_sync` (`shop_code` ASC, `status` ASC, `synced` ASC), ALGORITHM=INPLACE, LOCK=NONE;
SET SESSION lock_wait_timeout = @qimai_previous_lock_wait_timeout;

-- 3. 验证索引可见、非唯一、三列顺序正确。
SELECT INDEX_NAME, NON_UNIQUE, SEQ_IN_INDEX, COLUMN_NAME, COLLATION, INDEX_TYPE, IS_VISIBLE, SUB_PART
FROM information_schema.STATISTICS
WHERE TABLE_SCHEMA = DATABASE()
  AND TABLE_NAME = 'qimai_order_data'
  AND INDEX_NAME = 'idx_qimai_sales_sync'
ORDER BY SEQ_IN_INDEX;

-- 4. 只查看计划，不执行订单推送。门店/状态可替换为本次待核查参数。
-- 预期可选/使用 idx_qimai_sales_sync，避免扫描全表；id ASC 固定少推送规则的处理顺序。
EXPLAIN
SELECT id, order_no, actual_amount, completed_at
FROM qimai_order_data
WHERE shop_code = 'YXHZR001' AND status = '70' AND synced = 0
ORDER BY id ASC;
