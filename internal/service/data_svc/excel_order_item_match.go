package data_svc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

const (
	excelOrderItemQtyTolerance     = 1e-9
	maxExcelOrderItemsJSONBytes    = 16 * 1024 * 1024
	maxExcelOrderReservationGroups = 250000
	maxExcelOrderUsedItems         = 1000000
)

type excelOrderItemJSON struct {
	No           string          `json:"no"`
	Qty          json.RawMessage `json:"qty"`
	TotAmtActual json.RawMessage `json:"totAmtActual"`
	ProductName  string          `json:"mProductName"`
}

type excelOrderItem struct {
	no          string
	productName string
	qty         float64
	priceCents  int64
	valid       bool
}

type excelOrderItemDetail struct {
	items  []excelOrderItem
	reason string
}

type excelOrderItemReservation struct {
	priceCents int64
	qty        float64
	count      int
}

type excelOrderItemReservations map[int]map[string]map[string][]excelOrderItemReservation

type excelOrderItemIdentity struct {
	no          string
	productName string
	qty         float64
	priceCents  int64
}

type excelOrderItemUseCounts map[excelOrderItemIdentity]int

type excelOrderItemMatchState struct {
	reservations excelOrderItemReservations
	used         map[string]excelOrderItemUseCounts
	usedCount    int
}

func newExcelOrderItemMatchState(reservations excelOrderItemReservations) *excelOrderItemMatchState {
	return &excelOrderItemMatchState{
		reservations: reservations,
		used:         make(map[string]excelOrderItemUseCounts),
	}
}

func collectExcelOrderItemReservations(ctx context.Context, input excelRowsSource, config ExcelMatchConfig, scanLimit int) (excelOrderItemReservations, error) {
	reservations := make(excelOrderItemReservations)
	hasOrderItemStep := false
	for stepIndex, step := range config.Steps {
		if step.isEnabled() && step.MatchMode == excelMatchModeOrderItemSKU {
			reservations[stepIndex] = make(map[string]map[string][]excelOrderItemReservation)
			hasOrderItemStep = true
		}
	}
	if !hasOrderItemStep {
		return reservations, nil
	}
	rows, err := input.Rows(config.SheetName)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var headers []string
	var columnIndexes map[string]int
	rowCount := 0
	reservationGroups := 0
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		columns, err := rows.Columns()
		if err != nil {
			return nil, err
		}
		if headers == nil {
			headers = normalizeHeaders(columns)
			if len(headers) == 0 {
				return nil, fmt.Errorf("Excel 表头不能为空")
			}
			if _, err := prepareExcelMatchPipeline(headers, config); err != nil {
				return nil, err
			}
			columnIndexes = make(map[string]int, len(headers))
			for index, header := range headers {
				columnIndexes[header] = index
			}
			continue
		}
		rowCount++
		row := normalizeExcelRow(columns, len(headers))
		for stepIndex, step := range config.Steps {
			if !step.isEnabled() || step.MatchMode != excelMatchModeOrderItemSKU || !excelRowMatchesFilters(row, columnIndexes, step.Filters) {
				continue
			}
			orderNo := excelMatchRowValue(row, columnIndexes[step.MatchExcelColumn])
			specCode := normalizeExcelSpecCode(excelMatchRowValue(row, columnIndexes[step.SpecExcelColumn]))
			priceCents, priceOK := parseExcelMatchPrice(excelMatchRowValue(row, columnIndexes[step.PriceExcelColumn]))
			qty, qtyOK := parseExcelMatchNumber(excelMatchRowValue(row, columnIndexes[step.QtyExcelColumn]))
			if orderNo == "" || specCode == "" || skipExcelOrderItemSpecCode(specCode) || !priceOK || !qtyOK {
				continue
			}
			if reservations.add(stepIndex, orderNo, specCode, priceCents, qty) {
				reservationGroups++
				if reservationGroups > maxExcelOrderReservationGroups {
					return nil, fmt.Errorf("规格前缀匹配组合超过上限 %d，请拆分Excel文件", maxExcelOrderReservationGroups)
				}
			}
		}
		if scanLimit > 0 && rowCount >= scanLimit {
			break
		}
	}
	if err := rows.Error(); err != nil {
		return nil, err
	}
	return reservations, nil
}

type excelRowsSource interface {
	Rows(sheet string) (*excelize.Rows, error)
}

func runExcelOrderItemMatchStep(
	ctx context.Context,
	stepIndex int,
	step ExcelMatchStep,
	lookup ExcelMatchLookup,
	layout excelMatchPipelineLayout,
	state *excelOrderItemMatchState,
	rows []*excelMatchPipelineRow,
	eligibleRows []bool,
	orderKeys []string,
) error {
	details := make(map[string]excelOrderItemDetail, len(orderKeys))
	if len(orderKeys) > 0 {
		values, err := lookup.Lookup(ctx, step, orderKeys)
		if err != nil {
			return fmt.Errorf("执行第 %d 个订单商品SKU匹配步骤失败: %w", stepIndex+1, err)
		}
		for _, key := range orderKeys {
			if err := ctx.Err(); err != nil {
				return err
			}
			raw, ok := values[key]
			if !ok || strings.TrimSpace(raw) == "" {
				details[key] = excelOrderItemDetail{reason: "数据库无订单购物明细"}
				continue
			}
			if len(raw) > maxExcelOrderItemsJSONBytes {
				details[key] = excelOrderItemDetail{reason: "订单购物明细JSON过大"}
				continue
			}
			items, err := parseExcelOrderItems(raw)
			if err != nil {
				details[key] = excelOrderItemDetail{reason: "订单购物明细JSON无效"}
				continue
			}
			details[key] = excelOrderItemDetail{items: items}
		}
	}

	specIndex := layout.columnIndexes[step.SpecExcelColumn]
	priceIndex := layout.columnIndexes[step.PriceExcelColumn]
	qtyIndex := layout.columnIndexes[step.QtyExcelColumn]
	orderIndex := layout.stepInputIndexes[stepIndex]
	for rowIndex, row := range rows {
		if err := ctx.Err(); err != nil {
			return err
		}
		result := ExcelMatchPreviewStepResult{StepIndex: stepIndex + 1, StepName: step.Name, Status: "skipped", Reason: "未命中本步骤筛选"}
		value := ""
		if eligibleRows[rowIndex] {
			orderNo := excelMatchRowValue(row.values, orderIndex)
			result.MatchKey = orderNo
			specCode := excelMatchRowValue(row.values, specIndex)
			priceCents, priceOK := parseExcelMatchPrice(excelMatchRowValue(row.values, priceIndex))
			qty, qtyOK := parseExcelMatchNumber(excelMatchRowValue(row.values, qtyIndex))
			switch {
			case skipExcelOrderItemSpecCode(specCode):
				value = specCode
				result.MatchedValue = value
				result.Status, result.Reason = "matched", "规格编码已是完整SKU，已直接写入"
			case orderNo == "":
				result.Status, result.Reason = "unmatched", "订单号为空"
			case !validExcelSpecCode(specCode):
				result.Status, result.Reason = "unmatched", "规格编码为空"
			case !priceOK:
				result.Status, result.Reason = "unmatched", "销售金额不是有效数字"
			case !qtyOK:
				result.Status, result.Reason = "unmatched", "销售数量不是有效数字"
			default:
				state.reservations.consume(stepIndex, orderNo, specCode, priceCents, qty)
				var err error
				value, result.Reason, err = state.match(details[orderNo], orderNo, specCode, priceCents, qty)
				if err != nil {
					return err
				}
				result.Status = "unmatched"
				if value != "" {
					result.MatchedValue = value
					result.Status = "matched"
					if result.Reason == "" {
						result.Reason = "已按订单商品明细匹配"
					}
				}
			}
		}
		row.values = append(row.values, value)
		row.stepResults = append(row.stepResults, result)
	}
	return nil
}

func (state *excelOrderItemMatchState) match(detail excelOrderItemDetail, orderNo, specCode string, priceCents int64, qty float64) (string, string, error) {
	if detail.reason != "" {
		return "", detail.reason, nil
	}
	used := state.used[orderNo]
	if used == nil {
		used = make(excelOrderItemUseCounts)
		state.used[orderNo] = used
	}
	matchedButUsed := false
	matchedButReserved := false
	hasSpecMatch := false
	seenOccurrences := make(map[excelOrderItemIdentity]int)
	for _, item := range detail.items {
		if !item.valid || !excelSpecCodesMatch(specCode, item.productName) {
			continue
		}
		hasSpecMatch = true
		if priceCents != item.priceCents || math.Abs(qty-item.qty) >= excelOrderItemQtyTolerance {
			continue
		}
		identity := item.identity()
		occurrence := seenOccurrences[identity]
		seenOccurrences[identity] = occurrence + 1
		if occurrence < used[identity] {
			matchedButUsed = true
			continue
		}
		if !state.canConsumeItem(detail.items, used, orderNo, specCode, item) {
			matchedButReserved = true
			continue
		}
		if state.usedCount >= maxExcelOrderUsedItems {
			return "", "", fmt.Errorf("订单商品明细使用数量超过上限 %d，请拆分Excel文件", maxExcelOrderUsedItems)
		}
		used[identity]++
		state.usedCount++
		return item.no, "", nil
	}
	if matchedButUsed {
		return "", "符合条件的SKU已被当前订单其他Excel行使用", nil
	}
	if matchedButReserved {
		return "", "符合条件的SKU已为更长规格编码行保留", nil
	}
	if hasSpecMatch {
		return "", "订单购物明细中无规格编码、金额和数量同时相符的SKU", nil
	}
	return state.matchByAmountAndQty(detail.items, used, orderNo, priceCents, qty)
}

func (state *excelOrderItemMatchState) matchByAmountAndQty(items []excelOrderItem, used excelOrderItemUseCounts, orderNo string, priceCents int64, qty float64) (string, string, error) {
	matchedButUsed := false
	matchedButReserved := false
	seenOccurrences := make(map[excelOrderItemIdentity]int)
	for _, item := range items {
		if !item.valid || priceCents != item.priceCents || !excelOrderItemQtyEqual(qty, item.qty) {
			continue
		}
		identity := item.identity()
		occurrence := seenOccurrences[identity]
		seenOccurrences[identity] = occurrence + 1
		if occurrence < used[identity] {
			matchedButUsed = true
			continue
		}
		if !state.canConsumeItem(items, used, orderNo, "", item) {
			matchedButReserved = true
			continue
		}
		if state.usedCount >= maxExcelOrderUsedItems {
			return "", "", fmt.Errorf("订单商品明细使用数量超过上限 %d，请拆分Excel文件", maxExcelOrderUsedItems)
		}
		used[identity]++
		state.usedCount++
		return item.no, "规格编码未匹配，已按金额和数量匹配", nil
	}
	if matchedButUsed {
		return "", "符合金额和数量的SKU已被当前订单其他Excel行使用", nil
	}
	if matchedButReserved {
		return "", "符合金额和数量的SKU已为其他规格编码行保留", nil
	}
	return "", "订单购物明细中无金额和数量同时相符的SKU", nil
}

func (item excelOrderItem) identity() excelOrderItemIdentity {
	return excelOrderItemIdentity{
		no:          item.no,
		productName: item.productName,
		qty:         item.qty,
		priceCents:  item.priceCents,
	}
}

func (state *excelOrderItemMatchState) canConsumeItem(items []excelOrderItem, used excelOrderItemUseCounts, orderNo, excelCode string, target excelOrderItem) bool {
	excelCode = normalizeExcelSpecCode(excelCode)
	maxPrefixLength := state.reservations.maxPendingCodeLength(orderNo, len(excelCode), len(target.productName), target.priceCents, target.qty)
	if maxPrefixLength <= len(excelCode) {
		return true
	}
	for prefixLength := len(excelCode); prefixLength <= maxPrefixLength; prefixLength++ {
		prefix := target.productName[:prefixLength]
		remaining := state.reservations.remainingForPrefix(
			items,
			used,
			orderNo,
			prefix,
			len(excelCode),
			len(target.productName),
			target.priceCents,
			target.qty,
		)
		if remaining == 0 {
			continue
		}
		if availableExcelOrderItemsForPrefix(items, used, prefix, target.priceCents, target.qty) <= remaining {
			return false
		}
	}
	return true
}

func (reservations excelOrderItemReservations) maxPendingCodeLength(orderNo string, currentCodeLength, databaseCodeLength int, priceCents int64, qty float64) int {
	maxLength := currentCodeLength
	for _, byOrder := range reservations {
		for reservedCode, items := range byOrder[orderNo] {
			if len(reservedCode) <= currentCodeLength || len(reservedCode) > databaseCodeLength {
				continue
			}
			for _, item := range items {
				if item.count > 0 && item.priceCents == priceCents && excelOrderItemQtyEqual(item.qty, qty) {
					if len(reservedCode) > maxLength {
						maxLength = len(reservedCode)
					}
					break
				}
			}
		}
	}
	return maxLength
}

func availableExcelOrderItemsForPrefix(items []excelOrderItem, used excelOrderItemUseCounts, prefix string, priceCents int64, qty float64) int {
	count := 0
	seenOccurrences := make(map[excelOrderItemIdentity]int)
	for _, item := range items {
		if !item.valid || !strings.HasPrefix(item.productName, prefix) || item.priceCents != priceCents || !excelOrderItemQtyEqual(item.qty, qty) {
			continue
		}
		identity := item.identity()
		occurrence := seenOccurrences[identity]
		seenOccurrences[identity] = occurrence + 1
		if occurrence >= used[identity] {
			count++
		}
	}
	return count
}

func (reservations excelOrderItemReservations) add(stepIndex int, orderNo, specCode string, priceCents int64, qty float64) bool {
	byOrder := reservations[stepIndex]
	bySpec := byOrder[orderNo]
	if bySpec == nil {
		bySpec = make(map[string][]excelOrderItemReservation)
		byOrder[orderNo] = bySpec
	}
	items := bySpec[specCode]
	for index := range items {
		if items[index].priceCents == priceCents && excelOrderItemQtyEqual(items[index].qty, qty) {
			items[index].count++
			bySpec[specCode] = items
			return false
		}
	}
	bySpec[specCode] = append(items, excelOrderItemReservation{priceCents: priceCents, qty: qty, count: 1})
	return true
}

func (reservations excelOrderItemReservations) consume(stepIndex int, orderNo, specCode string, priceCents int64, qty float64) {
	items := reservations[stepIndex][orderNo][normalizeExcelSpecCode(specCode)]
	for index := range items {
		if items[index].count > 0 && items[index].priceCents == priceCents && excelOrderItemQtyEqual(items[index].qty, qty) {
			items[index].count--
			return
		}
	}
}

func (reservations excelOrderItemReservations) remainingForPrefix(
	databaseItems []excelOrderItem,
	used excelOrderItemUseCounts,
	orderNo, prefix string,
	currentCodeLength, databaseCodeLength int,
	priceCents int64,
	qty float64,
) int {
	pendingByCode := make(map[string]int)
	for _, byOrder := range reservations {
		for reservedCode, items := range byOrder[orderNo] {
			if len(reservedCode) <= currentCodeLength || len(reservedCode) > databaseCodeLength || !strings.HasPrefix(reservedCode, prefix) {
				continue
			}
			for _, item := range items {
				if item.count > 0 && item.priceCents == priceCents && excelOrderItemQtyEqual(item.qty, qty) {
					pendingByCode[reservedCode] += item.count
				}
			}
		}
	}
	count := 0
	for reservedCode, pending := range pendingByCode {
		available := availableExcelOrderItemsForPrefix(databaseItems, used, reservedCode, priceCents, qty)
		if available < pending {
			pending = available
		}
		count += pending
	}
	return count
}

func excelOrderItemQtyEqual(left, right float64) bool {
	return math.Abs(left-right) < excelOrderItemQtyTolerance
}

func parseExcelOrderItems(raw string) ([]excelOrderItem, error) {
	decoder := json.NewDecoder(bytes.NewBufferString(raw))
	decoder.UseNumber()
	var source []json.RawMessage
	if err := decoder.Decode(&source); err != nil {
		return nil, err
	}
	if err := ensureExcelOrderItemsJSONEOF(decoder); err != nil {
		return nil, err
	}
	items := make([]excelOrderItem, 0, len(source))
	for _, rawItem := range source {
		var item excelOrderItemJSON
		if err := json.Unmarshal(rawItem, &item); err != nil {
			continue
		}
		qty, qtyOK := parseJSONMatchNumber(item.Qty)
		priceCents, priceOK := parseJSONMatchPrice(item.TotAmtActual)
		no := strings.TrimSpace(item.No)
		productName := normalizeExcelSpecCode(item.ProductName)
		items = append(items, excelOrderItem{
			no:          no,
			productName: productName,
			qty:         qty,
			priceCents:  priceCents,
			valid:       no != "" && productName != "" && qtyOK && priceOK,
		})
	}
	return items, nil
}

func ensureExcelOrderItemsJSONEOF(decoder *json.Decoder) error {
	var trailing interface{}
	err := decoder.Decode(&trailing)
	if err == io.EOF {
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("订单购物明细JSON包含多余内容")
}

func parseJSONMatchNumber(raw json.RawMessage) (float64, bool) {
	value := strings.TrimSpace(string(raw))
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		unquoted, err := strconv.Unquote(value)
		if err != nil {
			return 0, false
		}
		value = unquoted
	}
	return parseExcelMatchNumber(value)
}

func parseJSONMatchPrice(raw json.RawMessage) (int64, bool) {
	value := strings.TrimSpace(string(raw))
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		unquoted, err := strconv.Unquote(value)
		if err != nil {
			return 0, false
		}
		value = unquoted
	}
	return parseExcelMatchPrice(value)
}

func parseExcelMatchPrice(raw string) (int64, bool) {
	value, ok := parseExcelMatchNumber(raw)
	if !ok {
		return 0, false
	}
	scaled := math.Round(value * 100)
	if scaled > math.MaxInt64 || scaled < math.MinInt64 {
		return 0, false
	}
	return int64(scaled), true
}

func parseExcelMatchNumber(raw string) (float64, bool) {
	value := strings.TrimSpace(raw)
	value = strings.NewReplacer(",", "", "￥", "", "¥", "", "$", "").Replace(value)
	if value == "" {
		return 0, false
	}
	parsed, err := strconv.ParseFloat(value, 64)
	return parsed, err == nil && !math.IsNaN(parsed) && !math.IsInf(parsed, 0)
}

func excelMatchRowValue(row []string, index int) string {
	if index < 0 || index >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[index])
}

func validExcelSpecCode(value string) bool {
	return normalizeExcelSpecCode(value) != ""
}

func skipExcelOrderItemSpecCode(value string) bool {
	length := len(normalizeExcelSpecCode(value))
	return length == 15 || length == 16
}

func excelSpecCodesMatch(excelCode, databaseCode string) bool {
	excelCode = normalizeExcelSpecCode(excelCode)
	databaseCode = normalizeExcelSpecCode(databaseCode)
	return excelCode != "" && len(excelCode) <= len(databaseCode) && strings.HasPrefix(databaseCode, excelCode)
}

func normalizeExcelSpecCode(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}
