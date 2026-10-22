package etc

import _ "embed"

// OracleReportSnapshotSQL is the shared snapshot schema shipped with the service.
//
//go:embed oracle_report_snapshot.sql
var OracleReportSnapshotSQL string
