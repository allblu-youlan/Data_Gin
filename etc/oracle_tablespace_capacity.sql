-- YL_DBS_TS 容量检查（只读）。在 ORCLPDB1 以有数据字典权限的账号执行。
-- 2026-11-27 取证：两个文件均已达到 MAXBYTES，总空闲 3.5 MiB。
-- 此文件不会自动扩容；先重新检查当前容量，再由管理员确认是否执行末尾示例。

SELECT SYS_CONTEXT('USERENV', 'CON_NAME') AS container_name FROM dual;

SELECT d.file_id, d.file_name, d.tablespace_name,
       ROUND(d.bytes / 1024 / 1024, 2) AS size_mib,
       d.autoextensible, ROUND(d.maxbytes / 1024 / 1024, 2) AS max_mib,
       d.increment_by * t.block_size / 1024 / 1024 AS increment_mib,
       NVL(f.free_bytes, 0) / 1024 / 1024 AS free_mib,
       NVL(f.max_free, 0) / 1024 / 1024 AS largest_free_mib,
       t.bigfile, t.status
FROM dba_data_files d
JOIN dba_tablespaces t ON t.tablespace_name = d.tablespace_name
LEFT JOIN (
    SELECT file_id, SUM(bytes) AS free_bytes, MAX(bytes) AS max_free
    FROM dba_free_space
    WHERE tablespace_name = 'YL_DBS_TS'
    GROUP BY file_id
) f ON f.file_id = d.file_id
WHERE d.tablespace_name = 'YL_DBS_TS'
ORDER BY d.file_id;

-- 定位 ETL 日志中的 LOB 属于哪张表、哪个字段。
SELECT owner, table_name, column_name, segment_name, tablespace_name
FROM dba_lobs
WHERE owner = 'YL_DBS' AND segment_name = 'SYS_LOB0000074840C00003$$';

-- 扩容示例（保持注释，确认后单独执行）：
-- 1. 在 Oracle 服务器/容器中检查该目录对应卷的磁盘余量，预留其他服务所需空间。
--    初始需 8 GiB，自动扩展最多使用 31 GiB；按实际磁盘预算调整。
-- 2. 确认当前连接为 ORCLPDB1、BIGFILE=NO，且下列新文件名未被使用。
-- 3. 不使用 REUSE；不要覆盖或删除已有 .dbf 文件。
-- ALTER TABLESPACE YL_DBS_TS
--   ADD DATAFILE '/opt/oracle/oradata/ORCLCDB/ORCLPDB1/YL_DBS_TS03.dbf'
--   SIZE 8G AUTOEXTEND ON NEXT 1G MAXSIZE 31G;
-- 4. 再次执行上方容量查询确认有新增空闲空间，再重提失败导入并核查 ETL 恢复。
--    已失败的 Excel 工作流不会自动重放；不要把执行进度当作已提交记录数。
