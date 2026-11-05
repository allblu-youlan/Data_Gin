package data_svc

import (
	"context"

	"gin-biz-web-api/model"
)

type bojunOrderReader interface {
	FindByDocNo(context.Context, string) (*model.BojunRetailOrder, error)
	FindByDocNos(context.Context, []string) ([]model.BojunRetailOrder, error)
}

type bojunOrderPageLookup struct {
	reader  bojunOrderReader
	orders  map[string]model.BojunRetailOrder
	loadErr error
}

func newBojunOrderPageLookup(ctx context.Context, reader bojunOrderReader, docNos []string) *bojunOrderPageLookup {
	lookup := &bojunOrderPageLookup{reader: reader, orders: make(map[string]model.BojunRetailOrder)}
	requested := make(map[string]struct{}, len(docNos))
	for _, docNo := range docNos {
		if docNo != "" {
			requested[docNo] = struct{}{}
		}
	}
	if len(requested) == 0 {
		return lookup
	}
	orders, err := reader.FindByDocNos(ctx, docNos)
	lookup.loadErr = err
	for _, order := range orders {
		if _, exactMatch := requested[order.DocNo]; exactMatch {
			lookup.orders[order.DocNo] = order
		}
	}
	return lookup
}

func (lookup *bojunOrderPageLookup) FindByDocNo(ctx context.Context, docNo string) (*model.BojunRetailOrder, error) {
	if lookup.loadErr != nil {
		return nil, lookup.loadErr
	}
	if order, exists := lookup.orders[docNo]; exists {
		// Consume the snapshot once so later duplicates observe earlier writes.
		delete(lookup.orders, docNo)
		return &order, nil
	}
	order, err := lookup.reader.FindByDocNo(ctx, docNo)
	if order != nil {
		// A database collation alias can refer to a differently spelled cached key.
		delete(lookup.orders, order.DocNo)
	}
	return order, err
}

type bojunAPIPageOrderWriter struct {
	bojunRetailOrderWriter
	lookup *bojunOrderPageLookup
}

func (writer bojunAPIPageOrderWriter) FindByDocNo(ctx context.Context, docNo string) (*model.BojunRetailOrder, error) {
	return writer.lookup.FindByDocNo(ctx, docNo)
}
