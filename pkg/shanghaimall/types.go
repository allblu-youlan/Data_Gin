package shanghaimall

import (
	"fmt"
	"time"

	"gin-biz-web-api/model"
)

type Target string

const (
	TargetJialiCheng   Target = "jialicheng"
	TargetPanlong      Target = "panlong"
	TargetQiantan      Target = "qiantan"
	TargetShangsheng   Target = "shangsheng"
	TargetXinjiaCenter Target = "xinjia_center"
	TargetXintiandi    Target = "xintiandi"
)

type RetailOrder struct {
	DocNo         string
	OrderTypeCode string
	SaleTime      string
	Amount        float64
	ListAmount    float64
	Quantity      int
}

type PushResult struct {
	Target       Target                 `json:"target"`
	Success      bool                   `json:"success"`
	HTTPStatus   int                    `json:"http_status"`
	RequestBody  map[string]interface{} `json:"request_body,omitempty"`
	ResponseBody string                 `json:"response_body,omitempty"`
	ResponseJSON map[string]interface{} `json:"response_json,omitempty"`
}

func RetailOrderFromBojun(order model.BojunRetailOrder) RetailOrder {
	saleTime := billDateToSaleTime(order.BillDate)
	amount, listAmount := AmountsFromBojun(&order)
	return RetailOrder{
		DocNo:         order.DocNo,
		OrderTypeCode: order.OrderTypeCode,
		SaleTime:      saleTime,
		Amount:        amount,
		ListAmount:    listAmount,
		Quantity:      order.TotalQty,
	}
}

// AmountsFromBojun returns the signed sales and list amounts used when pushing a Bojun order.
func AmountsFromBojun(order *model.BojunRetailOrder) (amount, listAmount float64) {
	amount = order.TotalAmtActual
	listAmount = order.TotalAmtList
	if order.OracleRetailID != nil {
		amount = order.PushAmount
		listAmount = order.PushAmount
	}
	return signedAmount(order.OrderTypeCode, amount), signedAmount(order.OrderTypeCode, listAmount)
}

func (order RetailOrder) IsRefund() bool {
	return order.OrderTypeCode == "RET" || order.Amount < 0
}

func (order RetailOrder) normalizedQuantity() int {
	if order.IsRefund() {
		return -1
	}
	if order.Quantity == 0 {
		return 1
	}
	return 1
}

func (order RetailOrder) validate() error {
	if order.DocNo == "" {
		return fmt.Errorf("retail order docno is required")
	}
	if order.SaleTime == "" {
		return fmt.Errorf("retail order sale_time is required")
	}
	return nil
}

func signedAmount(orderTypeCode string, amount float64) float64 {
	if orderTypeCode == "RET" && amount > 0 {
		return -amount
	}
	return amount
}

func billDateToSaleTime(billDate int) string {
	text := fmt.Sprintf("%08d", billDate)
	if len(text) != 8 {
		return time.Now().Format("2006-01-02 15:04:05")
	}
	parsed, err := time.Parse("20060102", text)
	if err != nil {
		return time.Now().Format("2006-01-02 15:04:05")
	}
	return parsed.Format("2006-01-02 15:04:05")
}
