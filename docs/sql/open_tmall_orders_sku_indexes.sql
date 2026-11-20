-- 天猫实时订单 SKU_8 筛选的增量索引；由运维手动执行，应用不会自动运行。
-- 复用 open_tmall_orders_indexes.sql 的检查、逐项创建、验收流程。
-- 已有 CREATED / NUM_IID 索引保留，不需要重新执行上一份脚本。
-- 按节执行，每创建一个索引就验收一次，不要整份无人值守执行。

-- 1. 只读预检：列出目标表的索引，并检查拟用名称是否被其他表占用。
-- 如果已有 VALID、VISIBLE 的普通升序索引，且前导列及顺序符合
-- (SKU_8, CREATED, ID) 或 (SKU_8, MODIFIED, ID)，跳过对应创建步骤。
-- 同名但不同表、不同列或不可用的索引，需要先核对，不能直接删除重建。
SELECT i.owner, i.index_name, i.table_owner, i.table_name,
       i.index_type, i.status, i.visibility,
       c.column_name, c.column_position, c.descend
  FROM all_indexes i
  JOIN all_ind_columns c
    ON c.index_owner = i.owner AND c.index_name = i.index_name
 WHERE (i.table_owner = 'YL_DBS' AND i.table_name = 'DWD_TF_TM_ORDERS')
    OR (i.owner = 'YL_DBS' AND i.index_name IN
        ('IX_TM_ORD_SKU_CREATED_ID', 'IX_TM_ORD_SKU_MODIFIED_ID'))
 ORDER BY i.owner, i.index_name, c.column_position;

-- 线上为 Oracle 19c Enterprise；确认空间后，在写入较少时逐条 ONLINE 创建。
-- ONLINE 允许并发 DML，但建索引仍消耗 I/O，且开始或结束时可能等待锁。
-- 不设置 NOLOGGING 或强制并行，也不调整应用的 10 秒查询超时。

-- 2. SKU_8 等值 + CREATED 范围，按 CREATED、ID 升序分页。
-- 确认第 1 节无等效索引及名称冲突后执行，然后运行第 4 节验收。
CREATE INDEX YL_DBS.IX_TM_ORD_SKU_CREATED_ID
    ON YL_DBS.DWD_TF_TM_ORDERS (SKU_8, CREATED, ID) ONLINE;

-- 3. SKU_8 等值 + MODIFIED 范围，按 MODIFIED、ID 升序分页。
-- 确认第 1 节无等效索引及名称冲突后执行，然后运行第 4 节验收。
CREATE INDEX YL_DBS.IX_TM_ORD_SKU_MODIFIED_ID
    ON YL_DBS.DWD_TF_TM_ORDERS (SKU_8, MODIFIED, ID) ONLINE;

-- 4. 每条创建完成后确认目标表、VALID / VISIBLE、三列顺序及 ASC。
-- 若复用了其他名称的等效索引，重新运行第 1 节检查其状态。
SELECT i.index_name, i.table_owner, i.table_name,
       i.status, i.visibility, i.last_analyzed,
       c.column_name, c.column_position, c.descend
  FROM all_indexes i
  JOIN all_ind_columns c
    ON c.index_owner = i.owner AND c.index_name = i.index_name
 WHERE i.owner = 'YL_DBS' AND i.index_name IN
       ('IX_TM_ORD_SKU_CREATED_ID', 'IX_TM_ORD_SKU_MODIFIED_ID')
 ORDER BY i.index_name, c.column_position;

-- 5. 重放原失败请求，保留原 SKU_8 值及其字符串绑定，不在脚本写入业务数据。
-- POST /api/open/tmall/orders/realtime/query
-- startTime=2026-09-01 00:00:00，endTime=2026-10-01 00:00:00
-- filters 仅包含 SKU_8；page=1；分别验证：
--   timeField=created，pageSize=100；
--   timeField=modified，pageSize=100 和 10。
-- 确认接口成功、耗时低于 10 秒，并核对排序及分页结果。
-- 已确认原 SQL ID：57q8114bfbrwa (created)、avkyd7df0294a (modified)。
-- 创建索引会使相关游标失效；复测后检查新执行计划、耗时和逻辑/物理读，
-- 不要把建索引前的累计执行统计当成复测结果。
-- 预期采用对应联合索引的范围扫描，避免全表扫描及全量排序；
-- 仅索引 VALID / VISIBLE 不能证明实际请求已经恢复。

-- 仅在确需回滚时，人工删除本次新建的索引；不删除复用的已有索引。
-- DROP INDEX YL_DBS.IX_TM_ORD_SKU_CREATED_ID;
-- DROP INDEX YL_DBS.IX_TM_ORD_SKU_MODIFIED_ID;
