package data_svc

import (
	"reflect"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestExcelOrderItemReleaseCompletedOrdersKeepsFutureReservationsAcrossSteps(t *testing.T) {
	doneItem := excelOrderItemIdentity{no: "DONE_ONE"}
	otherDoneItem := excelOrderItemIdentity{no: "DONE_TWO"}
	pendingItem := excelOrderItemIdentity{no: "PENDING"}
	unknownItem := excelOrderItemIdentity{no: "UNKNOWN"}
	reservations := excelOrderItemReservations{
		0: {"done": {"ABC": {{count: 0}}}, "pending": {"ABC": {{count: 0}}}},
		1: {"pending": {"ABC": {{priceCents: 1000, qty: 1, count: 1}}}},
	}
	state := newExcelOrderItemMatchState(reservations)
	state.used = map[string]excelOrderItemUseCounts{"done": {doneItem: 2, otherDoneItem: 1}, "pending": {pendingItem: 1}, "unknown": {unknownItem: 1}}
	state.usedCount = 5
	state.releaseCompletedOrders()
	if _, exists := state.used["done"]; exists {
		t.Fatal("completed order still retained")
	}
	if state.usedCount != 2 || state.used["pending"][pendingItem] != 1 || state.used["unknown"][unknownItem] != 1 {
		t.Fatalf("active/unknown orders or occurrence counts changed: state=%+v", state)
	}
	reservations.consume(1, "pending", "ABC", 1000, 1)
	state.releaseCompletedOrders()
	if state.usedCount != 1 || len(state.used) != 1 || state.used["unknown"][unknownItem] != 1 {
		t.Fatalf("completed later-step order was not released correctly: state=%+v", state)
	}
}

func TestExcelOrderItemReleaseDoesNotReuseSKUInLaterFile(t *testing.T) {
	reservations := excelOrderItemReservations{0: {"ORDER": {"ABC": {{priceCents: 1000, qty: 1, count: 2}}}}}
	state := newExcelOrderItemMatchState(reservations)
	detail := excelOrderItemDetail{items: []excelOrderItem{
		{no: "SKU_ONE", productName: "ABC", priceCents: 1000, qty: 1, valid: true},
		{no: "SKU_TWO", productName: "ABC", priceCents: 1000, qty: 1, valid: true},
	}}
	// The first file's row is consumed, but the second file still reserves a
	// matching row of this order. Keep its first SKU unavailable to that row.
	reservations.consume(0, "ORDER", "ABC", 1000, 1)
	first, _, err := state.match(detail, "ORDER", "ABC", 1000, 1)
	if err != nil || first != "SKU_ONE" {
		t.Fatalf("first=%q error=%v", first, err)
	}
	state.releaseCompletedOrders()
	if state.usedCount != 1 || len(state.used) != 1 {
		t.Fatalf("future file's order was released: %+v", state)
	}
	reservations.consume(0, "ORDER", "ABC", 1000, 1)
	second, _, err := state.match(detail, "ORDER", "ABC", 1000, 1)
	if err != nil || second != "SKU_TWO" {
		t.Fatalf("later file reused the first SKU: first=%q second=%q error=%v", first, second, err)
	}
	state.releaseCompletedOrders()
	if state.usedCount != 0 || len(state.used) != 0 {
		t.Fatalf("fully processed order still counts toward the active-item limit: %+v", state)
	}
}

func TestExcelOrderItemReleaseWithoutCompleteReservationsPreservesLegacyUsage(t *testing.T) {
	for _, test := range []struct {
		name         string
		reservations excelOrderItemReservations
	}{
		{name: "nil reservation scan"},
		{name: "empty reservation scan", reservations: excelOrderItemReservations{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := newExcelOrderItemMatchState(test.reservations)
			item := excelOrderItemIdentity{no: "USED"}
			state.used["ORDER"] = excelOrderItemUseCounts{item: 3}
			state.usedCount = 3
			state.releaseCompletedOrders()
			if state.usedCount != 3 || state.used["ORDER"][item] != 3 {
				t.Fatalf("unscanned order was released: %+v", state)
			}
		})
	}
}

func TestExcelOrderItemCompleteScanReleasesUniqueOrdersNotRetainedInReservations(t *testing.T) {
	for _, test := range []struct {
		name         string
		reservations excelOrderItemReservations
	}{
		{name: "all unique orders", reservations: excelOrderItemReservations{}},
		{name: "unique order among repeated orders", reservations: excelOrderItemReservations{0: {"REPEATED": {"ABC": {{priceCents: 1000, qty: 1, count: 1}}}}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := newExcelOrderItemMatchState(test.reservations)
			state.reservationsComplete = true
			detail := excelOrderItemDetail{items: []excelOrderItem{{no: "SKU_ONE", productName: "ABC", priceCents: 1000, qty: 1, valid: true}}}
			pending := 0
			if len(test.reservations) > 0 {
				// One prior file has already used this SKU, while one reservation
				// remains for a later row/file of the same order.
				if _, _, err := state.match(detail, "REPEATED", "ABC", 1000, 1); err != nil {
					t.Fatal(err)
				}
				pending = 1
			}
			if _, _, err := state.match(detail, "UNIQUE_FIRST", "ABC", 1000, 1); err != nil {
				t.Fatal(err)
			}
			if _, _, err := state.match(detail, "UNIQUE_SECOND", "ABC", 1000, 1); err != nil {
				t.Fatal(err)
			}
			if state.usedCount != 2+pending {
				t.Fatalf("unique orders not matched: %+v", state)
			}
			state.releaseCompletedOrders()
			if state.usedCount != pending || len(state.used) != pending {
				t.Fatalf("complete scan kept unique order usage: %+v", state)
			}
			if pending > 0 && len(state.used["REPEATED"]) != 1 {
				t.Fatal("complete flag released a pending repeated order")
			}
		})
	}
}

func TestExcelOrderItemFilteredReservationScanCountsEligibleRowsAndRetainsRepeatedOrdersOnly(t *testing.T) {
	file := excelize.NewFile()
	defer file.Close()
	rows := [][]interface{}{
		{"订单号", "规格", "金额", "数量"},
		{"REPEATED", "ABC", "10", "1"},
		{"UNIQUE", "XYZ", "20", "1"},
		{"REPEATED", "ABC12", "10", "1"},
		{"INVALID_AMOUNT", "ABC", "not a number", "1"},
		{"COMPLETE_SKU", "123456789012345", "10", "1"},
	}
	for index, row := range rows {
		cell, err := excelize.CoordinatesToCellName(1, index+1)
		if err != nil {
			t.Fatal(err)
		}
		if err := file.SetSheetRow(defaultExcelSheetName, cell, &row); err != nil {
			t.Fatal(err)
		}
	}
	config, err := normalizeExcelMatchConfig(ExcelMatchConfig{SheetName: defaultExcelSheetName, Steps: []ExcelMatchStep{{MatchMode: excelMatchModeOrderItemSKU, TableName: bojunRetailOrdersTable, MatchExcelColumn: "订单号", SpecExcelColumn: "规格", PriceExcelColumn: "金额", QtyExcelColumn: "数量", DBMatchField: "docno", DBValueField: "items_json", OutputColumnName: "完整SKU"}}})
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	counted, err := collectExcelOrderItemReservationsFiltered(t.Context(), file, config, 0, func(order string) bool { counts[order]++; return false })
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(counts, map[string]int{"REPEATED": 2, "UNIQUE": 1}) || len(counted[0]) != 0 {
		t.Fatalf("counting pass retained reservations or counted ineligible rows: counts=%v reservations=%v", counts, counted)
	}
	filtered, err := collectExcelOrderItemReservationsFiltered(t.Context(), file, config, 0, func(order string) bool { return counts[order] > 1 })
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered[0]) != 1 || len(filtered[0]["REPEATED"]) != 2 {
		t.Fatalf("unique or invalid orders retained: %v", filtered)
	}
	all, err := collectExcelOrderItemReservations(t.Context(), file, config, 0)
	if err != nil || len(all[0]) != 2 {
		t.Fatalf("unfiltered wrapper compatibility changed: reservations=%v error=%v", all, err)
	}
}
