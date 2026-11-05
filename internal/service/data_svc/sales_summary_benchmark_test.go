package data_svc

import (
	"fmt"
	"strings"
	"testing"

	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/shanghaimall"
)

var salesSummaryBenchmarkMetrics SalesSummaryMetrics
var salesSummaryBenchmarkRetail shanghaimall.RetailOrder

func BenchmarkSalesSummaryAggregation(b *testing.B) {
	for _, itemCount := range []int{0, 1, 20} {
		b.Run(fmt.Sprintf("items=%d", itemCount), func(b *testing.B) {
			id := uint64(35)
			order := model.BojunRetailOrder{
				DocNo: "ORDER-1", OracleRetailID: &id, BillDate: 20261105,
				OrderTypeCode: "CMR", PushAmount: 288.35, TotalQty: itemCount,
			}
			if itemCount > 0 {
				item := `{"qty":2,"no":"SKU-001","mProductName":"cotton shirt","mColor":"blue","mSize":"M","priceActual":144.175,"amtActual":288.35,"discount":0.8}`
				order.ItemsJSON = "[" + strings.Repeat(item+",", itemCount-1) + item + "]"
			}
			b.ReportAllocs()
			for b.Loop() {
				var metrics SalesSummaryMetrics
				if err := addSalesSummaryOrder(&metrics, order); err != nil {
					b.Fatal(err)
				}
				salesSummaryBenchmarkMetrics = metrics
			}
		})
	}
}

func BenchmarkSalesSummaryRetailConversion(b *testing.B) {
	id := uint64(35)
	order := model.BojunRetailOrder{
		DocNo: "ORDER-1", OracleRetailID: &id, BillDate: 20261105,
		OrderTypeCode: "CMR", PushAmount: 288.35, TotalQty: 2,
	}
	b.ReportAllocs()
	for b.Loop() {
		salesSummaryBenchmarkRetail = shanghaimall.RetailOrderFromBojun(order)
	}
}

func BenchmarkSalesSummaryReadPushedOrders(b *testing.B) {
	store := &salesSummaryDBStore{db: salesSummaryTestDB(b, salesSummaryBenchmarkFixture(1000))}
	ctx := b.Context()
	b.ReportAllocs()
	for b.Loop() {
		var metrics SalesSummaryMetrics
		err := store.ReadPushedOrders(ctx, "ABCN001A003", 20261001, 20261105, func(order model.BojunRetailOrder) error {
			return addSalesSummaryOrder(&metrics, order)
		})
		if err != nil {
			b.Fatal(err)
		}
		salesSummaryBenchmarkMetrics = metrics
	}
}
