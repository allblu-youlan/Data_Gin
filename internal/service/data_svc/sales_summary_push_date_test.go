package data_svc

import (
	"database/sql"
	"os/exec"
	"strings"
	"testing"

	"gin-biz-web-api/model"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// Execute the generated relational query against a real local SQL engine,
// without adding a database driver or connecting to application databases.
func TestSalesSummaryPushDatesDeduplicateSuccessfulOrders(t *testing.T) {
	sqlite, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Skip("sqlite3 is required for the local SQL behavior test")
	}
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: &sql.DB{}, SkipInitializeWithVersion: true}), &gorm.Config{
		DryRun: true, DisableAutomaticPing: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	const fixture = `
CREATE TABLE bojun_retail_orders (
  id INTEGER PRIMARY KEY, docno TEXT, c_store_code TEXT, billdate INTEGER,
  oracle_retail_id INTEGER, raw_content_json TEXT
);
CREATE TABLE delivery_logs (
  id INTEGER PRIMARY KEY, clean_record_id INTEGER, source_code TEXT,
  destination_id INTEGER, dataset_kind TEXT, success BOOLEAN, response_body TEXT, sent_at DATETIME
);
CREATE INDEX idx_delivery_logs_sent_at ON delivery_logs(sent_at);
CREATE INDEX idx_delivery_logs_clean_record_id ON delivery_logs(clean_record_id);
INSERT INTO bojun_retail_orders VALUES
  (1, 'CROSS_DAY', 'ABCN002A001', 20261015, NULL, '{}'),
  (2, 'FAILED', 'ABCN002A001', 20261016, NULL, '{}'),
  (3, 'SKIPPED', 'ABCN002A001', 20261016, 3, '{"STATUS":1}'),
  (4, 'END_BOUNDARY', 'ABCN002A001', 20261016, NULL, '{}'),
  (5, 'BEFORE_START', 'ABCN002A001', 20261016, NULL, '{}'),
  (6, 'LATE_DAY', 'ABCN002A001', 20261015, NULL, '{}'),
  (7, 'LEGACY', 'ABCN002A001', 20261015, NULL, '{}'),
  (8, 'OTHER_SOURCE', 'ABCN002A001', 20261016, NULL, '{}'),
  (9, 'NO_TIME', 'ABCN002A001', 20261016, NULL, '{}'),
  (10, 'ORACLE_ONLY', 'ABCN002A001', 20261016, 10, '{"STATUS":1}'),
  (11, 'SKIP_THEN_REAL', 'ABCN002A001', 20261015, NULL, '{}'),
  (12, 'OTHER_STORE', 'ABCN001A003', 20261016, NULL, '{}');
INSERT INTO delivery_logs VALUES
  (1, 1, 'bojun_order', 0, 'retail_order', FALSE, 'failed', '2026-10-15 10:00:00'),
  (2, 1, 'bojun_order', 0, 'retail_order', TRUE, 'ok', '2026-10-16 00:00:00'),
  (3, 1, 'bojun_order', 0, 'retail_order', TRUE, 'ok', '2026-10-16 12:00:00'),
  (4, 1, 'bojun_order', 0, 'retail_order', TRUE, 'ok', '2026-10-17 12:00:00'),
  (5, 2, 'bojun_order', 0, 'retail_order', FALSE, 'failed', '2026-10-16 12:00:00'),
  (6, 3, 'bojun_order', 0, 'retail_order', TRUE, 'skipped_by_order_push_policy', '2026-10-16 12:00:00'),
  (7, 4, 'bojun_order', 0, 'retail_order', TRUE, 'ok', '2026-10-17 00:00:00'),
  (8, 5, 'bojun_order', 0, 'retail_order', TRUE, 'ok', '2026-10-15 23:59:59'),
  (9, 5, 'bojun_order', 0, 'retail_order', TRUE, 'ok', '2026-10-16 12:00:00'),
  (10, 6, 'bojun_order', 0, 'retail_order', TRUE, 'ok', '2026-10-16 23:59:59'),
  (11, 7, NULL, 0, 'retail_order', TRUE, NULL, '2026-10-16 12:00:00'),
  (12, 8, 'other', 1, 'other', TRUE, 'ok', '2026-10-16 12:00:00'),
  (13, 9, 'bojun_order', 0, 'retail_order', TRUE, 'ok', NULL),
  (14, 11, 'bojun_order', 0, 'retail_order', TRUE, 'skipped_by_order_push_policy', '2026-10-15 12:00:00'),
  (15, 11, 'bojun_order', 0, 'retail_order', TRUE, 'ok', '2026-10-16 12:00:00'),
  (16, 12, 'bojun_order', 0, 'retail_order', TRUE, 'ok', '2026-10-16 12:00:00');
`
	for _, test := range []struct {
		name       string
		start, end int
		want       string
	}{
		{"before success", 20261015, 20261015, "BEFORE_START"},
		{"success day includes both boundaries and ignores billdate", 20261016, 20261016, "CROSS_DAY\nLATE_DAY\nLEGACY\nSKIP_THEN_REAL"},
		{"later retries stay on first success day", 20261017, 20261017, "END_BOUNDARY"},
		{"whole range counts each successful order once", 20261015, 20261017, "BEFORE_START\nCROSS_DAY\nEND_BOUNDARY\nLATE_DAY\nLEGACY\nSKIP_THEN_REAL"},
	} {
		t.Run(test.name, func(t *testing.T) {
			statement := salesSummaryPushedOrdersQuery(db, "ABCN002A001", test.start, test.end).
				Select("docno").Order("docno").Find(&[]model.BojunRetailOrder{}).Statement
			query := db.Dialector.Explain(statement.SQL.String(), statement.Vars...)
			command := exec.CommandContext(t.Context(), sqlite, "-batch", "-bail", ":memory:")
			command.Stdin = strings.NewReader(fixture + query + ";\n")
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("execute pushed-order query: %v\n%s", err, output)
			}
			if got := strings.TrimSpace(string(output)); got != test.want {
				t.Fatalf("pushed orders = %q, want %q", got, test.want)
			}
		})
	}
}
