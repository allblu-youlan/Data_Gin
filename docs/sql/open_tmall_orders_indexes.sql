-- Optional index for the confirmed NUM_IID + CREATED open-API order query.
-- Run these sections manually; this script is not applied by the application.
-- Review the checks before executing DDL. Do not run the whole file unattended.

-- 1. Check existing indexes, including any collision with the proposed name.
-- Skip creation if a usable visible index already begins with
-- (NUM_IID, CREATED, ID) in compatible order, even under another name.
-- If the proposed name already exists with another definition, stop for review;
-- do not drop or replace an existing index automatically.
SELECT i.owner, i.index_name, i.index_type, i.status, i.visibility,
       c.column_name, c.column_position, c.descend
  FROM all_indexes i
  JOIN all_ind_columns c
    ON c.index_owner = i.owner AND c.index_name = i.index_name
 WHERE (i.table_owner = 'YL_DBS' AND i.table_name = 'DWD_TF_TM_ORDERS')
    OR (i.owner = 'YL_DBS' AND i.index_name = 'IX_TM_ORD_NUM_CREATED_ID')
 ORDER BY i.owner, i.index_name, c.column_position;

-- 2. Create only after the checks above. Production is Oracle 19c Enterprise.
-- ONLINE permits concurrent DML; build during low DML activity and confirm
-- available space first. Parallel DML is not supported during online creation.
-- No NOLOGGING or forced parallelism; existing MODIFIED indexes are unchanged.
CREATE INDEX YL_DBS.IX_TM_ORD_NUM_CREATED_ID
    ON YL_DBS.DWD_TF_TM_ORDERS (NUM_IID, CREATED, ID) ONLINE;

-- 3. Verify VALID / VISIBLE and the three column positions after creation.
SELECT i.index_name, i.status, i.visibility, i.last_analyzed,
       c.column_name, c.column_position, c.descend
  FROM all_indexes i
  JOIN all_ind_columns c
    ON c.index_owner = i.owner AND c.index_name = i.index_name
 WHERE i.owner = 'YL_DBS' AND i.index_name = 'IX_TM_ORD_NUM_CREATED_ID'
 ORDER BY c.column_position;

-- Replay the original API query: timeField=created, the same NUM_IID filter,
-- 2026-11-16 00:00:00 <= CREATED < 2026-11-17 00:00:00, page=1, pageSize=100.
-- Verify a successful response, unchanged ordering/pagination and elapsed time
-- below the 10-second query budget. Inspect its new cursor plan and I/O;
-- the pre-change cursor was 4jxm88n2jrjps with a full table scan.
-- Queries without NUM_IID have a different shape; no second index is added here.

-- Manual rollback only if this change created the index and removal is needed.
-- DROP INDEX YL_DBS.IX_TM_ORD_NUM_CREATED_ID;
