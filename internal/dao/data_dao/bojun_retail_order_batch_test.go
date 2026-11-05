package data_dao

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"gin-biz-web-api/model"

	"gorm.io/gorm"
)

func TestBojunRetailOrderFindByDocNosBoundsAndDeduplicatesQueries(t *testing.T) {
	for _, count := range []int{0, 100, 500, 501} {
		t.Run(fmt.Sprintf("orders_%d", count), func(t *testing.T) {
			db := dryRunWeatherDAOTestDB(t)
			queries := 0
			if err := db.Callback().Query().After("gorm:query").Register("test:batch_orders", func(tx *gorm.DB) {
				queries++
				if !strings.Contains(tx.Statement.SQL.String(), "WHERE docno IN (") || len(tx.Statement.Vars) > 500 {
					t.Fatalf("unexpected batch query SQL=%s parameters=%d", tx.Statement.SQL.String(), len(tx.Statement.Vars))
				}
				rows := tx.Statement.Dest.(*[]model.BojunRetailOrder)
				for _, value := range tx.Statement.Vars {
					*rows = append(*rows, model.BojunRetailOrder{DocNo: value.(string)})
				}
			}); err != nil {
				t.Fatal(err)
			}
			docNos := []string{""}
			for i := range count {
				docNo := fmt.Sprintf("ORDER-%03d", i)
				docNos = append(docNos, docNo, docNo)
			}

			orders, err := NewBojunRetailOrderDAO(db).FindByDocNos(t.Context(), docNos)
			if err != nil || len(orders) != count || queries != (count+499)/500 {
				t.Fatalf("error=%v orders=%d queries=%d, want %d orders in %d queries", err, len(orders), queries, count, (count+499)/500)
			}
		})
	}
}

func TestBojunRetailOrderFindByDocNosReturnsQueryError(t *testing.T) {
	db := dryRunWeatherDAOTestDB(t)
	queryFailure := errors.New("batch query failed")
	if err := db.Callback().Query().After("gorm:query").Register("test:batch_error", func(tx *gorm.DB) {
		tx.AddError(queryFailure)
	}); err != nil {
		t.Fatal(err)
	}
	orders, err := NewBojunRetailOrderDAO(db).FindByDocNos(t.Context(), []string{"ORDER-1"})
	if !errors.Is(err, queryFailure) || orders != nil {
		t.Fatalf("orders=%v error=%v, want batch query failure", orders, err)
	}
}
