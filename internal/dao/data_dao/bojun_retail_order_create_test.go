package data_dao

import (
	"errors"
	"reflect"
	"testing"

	"gin-biz-web-api/model"

	mysqlDriver "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

func TestBojunRetailOrderCreateIfNotExistsInsertsBeforeCheckingConflicts(t *testing.T) {
	duplicate := &mysqlDriver.MySQLError{Number: 1062, Message: "duplicate key"}
	writeFailure := errors.New("insert failed")
	queryFailure := errors.New("lookup failed")
	tests := []struct {
		name           string
		insertError    error
		lookupError    error
		existingCount  int64
		wantCreated    bool
		wantError      error
		wantOperations []string
	}{
		{name: "new order", wantCreated: true, wantOperations: []string{"insert"}},
		{name: "duplicate docno", insertError: duplicate, existingCount: 1, wantOperations: []string{"insert", "lookup"}},
		{name: "different unique key", insertError: duplicate, wantError: duplicate, wantOperations: []string{"insert", "lookup"}},
		{name: "insert failure", insertError: writeFailure, wantError: writeFailure, wantOperations: []string{"insert"}},
		{name: "duplicate lookup failure", insertError: duplicate, lookupError: queryFailure, wantError: queryFailure, wantOperations: []string{"insert", "lookup"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := dryRunWeatherDAOTestDB(t)
			var operations []string
			if err := db.Callback().Create().After("gorm:create").Register("test:bojun_insert", func(tx *gorm.DB) {
				operations = append(operations, "insert")
				if tt.insertError != nil {
					tx.AddError(tt.insertError)
					return
				}
				tx.Statement.Dest.(*model.BojunRetailOrder).ID = 42
			}); err != nil {
				t.Fatal(err)
			}
			if err := db.Callback().Query().After("gorm:query").Register("test:bojun_lookup", func(tx *gorm.DB) {
				operations = append(operations, "lookup")
				if !reflect.DeepEqual(tx.Statement.Vars, []interface{}{"B001"}) {
					t.Fatalf("duplicate lookup arguments = %v, want the inserted docno", tx.Statement.Vars)
				}
				if tt.lookupError != nil {
					tx.AddError(tt.lookupError)
					return
				}
				*tx.Statement.Dest.(*int64) = tt.existingCount
				tx.RowsAffected = 1
			}); err != nil {
				t.Fatal(err)
			}

			order := &model.BojunRetailOrder{DocNo: "B001"}
			created, err := NewBojunRetailOrderDAO(db).CreateIfNotExists(t.Context(), order)
			if created != tt.wantCreated || !errors.Is(err, tt.wantError) {
				t.Fatalf("created=%v error=%v, want created=%v error=%v", created, err, tt.wantCreated, tt.wantError)
			}
			if !reflect.DeepEqual(operations, tt.wantOperations) {
				t.Fatalf("operations=%v, want %v", operations, tt.wantOperations)
			}
			if created && (order.ID != 42 || order.CreatedAt <= 0 || order.UpdatedAt != order.CreatedAt) {
				t.Fatalf("created order = %+v, want generated ID and timestamps", order)
			}
		})
	}
}
