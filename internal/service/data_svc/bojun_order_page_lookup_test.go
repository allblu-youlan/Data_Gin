package data_svc

import (
	"context"
	"testing"

	"gin-biz-web-api/model"
)

type bojunAliasLookupReader struct {
	order      model.BojunRetailOrder
	findCalls  int
	batchCalls int
}

func (reader *bojunAliasLookupReader) FindByDocNo(context.Context, string) (*model.BojunRetailOrder, error) {
	reader.findCalls++
	order := reader.order
	return &order, nil
}

func (reader *bojunAliasLookupReader) FindByDocNos(context.Context, []string) ([]model.BojunRetailOrder, error) {
	reader.batchCalls++
	return []model.BojunRetailOrder{reader.order}, nil
}

func TestBojunOrderPageLookupAliasInvalidatesCanonicalSnapshot(t *testing.T) {
	reader := &bojunAliasLookupReader{order: model.BojunRetailOrder{DocNo: "ORDER-1", StoreName: "old"}}
	lookup := newBojunOrderPageLookup(t.Context(), reader, []string{"order-1", "ORDER-1"})
	if _, err := lookup.FindByDocNo(t.Context(), "order-1"); err != nil {
		t.Fatal(err)
	}
	reader.order.StoreName = "updated"

	order, err := lookup.FindByDocNo(t.Context(), "ORDER-1")
	if err != nil || order.StoreName != "updated" || reader.findCalls != 2 {
		t.Fatalf("order=%+v error=%v single reads=%d, want the updated database order", order, err, reader.findCalls)
	}
}

func TestBojunOrderPageLookupCachesOnlyExactRequestedDocNos(t *testing.T) {
	reader := &bojunAliasLookupReader{order: model.BojunRetailOrder{DocNo: "ORDER-1", StoreName: "old"}}
	lookup := newBojunOrderPageLookup(t.Context(), reader, []string{"order-1"})
	if len(lookup.orders) != 0 {
		t.Fatalf("cached unrequested spelling: %+v", lookup.orders)
	}
	reader.order.StoreName = "updated"
	order, err := lookup.FindByDocNo(t.Context(), "order-1")
	if err != nil || order.StoreName != "updated" || reader.findCalls != 1 {
		t.Fatalf("order=%+v error=%v single reads=%d", order, err, reader.findCalls)
	}
}

func TestBojunOrderPageLookupConsumesSnapshotOnce(t *testing.T) {
	reader := &bojunAliasLookupReader{order: model.BojunRetailOrder{DocNo: "ORDER-1", StoreName: "old"}}
	lookup := newBojunOrderPageLookup(t.Context(), reader, []string{"ORDER-1", "ORDER-1"})
	order, err := lookup.FindByDocNo(t.Context(), "ORDER-1")
	if err != nil || order.StoreName != "old" || reader.findCalls != 0 {
		t.Fatalf("first lookup order=%+v error=%v single reads=%d", order, err, reader.findCalls)
	}
	reader.order.StoreName = "updated"
	order, err = lookup.FindByDocNo(t.Context(), "ORDER-1")
	if err != nil || order.StoreName != "updated" || reader.findCalls != 1 {
		t.Fatalf("duplicate lookup order=%+v error=%v single reads=%d", order, err, reader.findCalls)
	}
}

func TestBojunOrderPageLookupEmptyDocNosSkipsBatchQuery(t *testing.T) {
	reader := &bojunAliasLookupReader{}
	lookup := newBojunOrderPageLookup(t.Context(), reader, []string{"", ""})
	if reader.batchCalls != 0 || len(lookup.orders) != 0 || lookup.loadErr != nil {
		t.Fatalf("batch reads=%d lookup=%+v", reader.batchCalls, lookup)
	}
}
