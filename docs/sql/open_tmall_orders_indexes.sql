-- Indexes for the confirmed CREATED queries, with and without NUM_IID.
-- Run these sections manually; this script is not applied by the application.
-- Read-only preflight first; create and verify each index separately after approval.
-- Do not run the whole file unattended or drop existing indexes automatically.

-- 1. Inspect the table's indexes and both proposed names across the YL_DBS schema.
-- For each index below, skip creation if a VALID, VISIBLE compatible index
-- already has its required leading columns, even under another name.
-- A proposed name on another table or with different columns requires review.
SELECT i.owner, i.index_name, i.table_owner, i.table_name,
       i.index_type, i.status, i.visibility,
       c.column_name, c.column_position, c.descend
  FROM all_indexes i
  JOIN all_ind_columns c
    ON c.index_owner = i.owner AND c.index_name = i.index_name
 WHERE (i.table_owner = 'YL_DBS' AND i.table_name = 'DWD_TF_TM_ORDERS')
    OR (i.owner = 'YL_DBS' AND i.index_name IN
        ('IX_TM_ORD_CREATED_ID', 'IX_TM_ORD_NUM_CREATED_ID'))
 ORDER BY i.owner, i.index_name, c.column_position;

-- Production is Oracle 19c Enterprise. Create only after the read-only checks.
-- ONLINE permits concurrent DML; build during low DML activity and confirm
-- available space first. Parallel DML is not supported during online creation.
-- No NOLOGGING or forced parallelism; existing MODIFIED indexes are unchanged.

-- 2. No NUM_IID filter: require leading (CREATED, ID), ordered by CREATED, ID.
-- Check its name/definition above, create if missing, then run section 4.
CREATE INDEX YL_DBS.IX_TM_ORD_CREATED_ID
    ON YL_DBS.DWD_TF_TM_ORDERS (CREATED, ID) ONLINE;

-- 3. NUM_IID equality filter: require leading (NUM_IID, CREATED, ID).
-- Recheck its name/definition above, create if missing, then run section 4.
CREATE INDEX YL_DBS.IX_TM_ORD_NUM_CREATED_ID
    ON YL_DBS.DWD_TF_TM_ORDERS (NUM_IID, CREATED, ID) ONLINE;

-- 4. After EACH creation, verify its table, VALID / VISIBLE and column order.
-- When reusing a differently named index, rerun section 1 to verify that index.
SELECT i.index_name, i.table_owner, i.table_name,
       i.status, i.visibility, i.last_analyzed,
       c.column_name, c.column_position, c.descend
  FROM all_indexes i
  JOIN all_ind_columns c
    ON c.index_owner = i.owner AND c.index_name = i.index_name
 WHERE i.owner = 'YL_DBS' AND i.index_name IN
       ('IX_TM_ORD_CREATED_ID', 'IX_TM_ORD_NUM_CREATED_ID')
 ORDER BY i.index_name, c.column_position;

-- Replay both failed CREATED queries with their original windows and page sizes:
-- no filters (page=1, pageSize=1 or 3), then NUM_IID (page=1, pageSize=3).
-- Verify success below the 10-second budget and unchanged ordering/pagination.
-- Inspect new cursor plans and I/O. Previous full-scan SQL IDs were
-- 2quvt0nn5xaug (no filter) and 4jxm88n2jrjps (NUM_IID).

-- Manual rollback only for indexes created by this change, if removal is needed.
-- DROP INDEX YL_DBS.IX_TM_ORD_CREATED_ID;
-- DROP INDEX YL_DBS.IX_TM_ORD_NUM_CREATED_ID;
