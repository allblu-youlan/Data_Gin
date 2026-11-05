package data_svc

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"strings"
	"time"

	send "gin-biz-web-api/Trigger/Send_Data"
	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/app"
	"gin-biz-web-api/pkg/orderpush"
	"gin-biz-web-api/pkg/shanghaimall"

	"github.com/google/uuid"
)

const (
	bojunOrderPushSource  = "bojun_order"
	bojunOrderDatasetKind = "retail_order"

	bojunPushTargetHangzhouHenglong = orderpush.TargetBojunHangzhouHenglong
	bojunHangzhouHenglongStoreCode  = "416201"
	bojunHangzhouHenglongItemCode   = "E6600000099"
)

type bojunRetailOrderSyncUpdater interface {
	UpdateSyncStatus(ctx context.Context, id uint, synced int) error
}

type deliveryLogCreator interface {
	Create(ctx context.Context, log *model.DeliveryLog) (uint, error)
}

type BojunOrderPushService struct {
	retailOrderDAO bojunRetailOrderSyncUpdater
	logDAO         deliveryLogCreator
	pipelineRunDAO pipelineRunRecorder
}

type bojunOrderPushTarget struct {
	Code  string
	Name  string
	Store string
}

type bojunOrderPushResult struct {
	Target          bojunOrderPushTarget
	Success         bool
	Skipped         bool
	SyncStatusSaved bool // The local status corresponding to Success was persisted.
	Error           error
}

func NewBojunOrderPushService() *BojunOrderPushService {
	return &BojunOrderPushService{
		retailOrderDAO: data_dao.NewBojunRetailOrderDAO(),
		logDAO:         data_dao.NewDeliveryLogDAO(),
		pipelineRunDAO: data_dao.NewPipelineRunDAO(),
	}
}

func (s *BojunOrderPushService) PushNewOrder(ctx context.Context, order *model.BojunRetailOrder) bojunOrderPushResult {
	return s.PushNewOrderWithPolicy(ctx, order, 0, OrderPushSkipPolicy{})
}

func (s *BojunOrderPushService) PushNewOrderWithPolicy(ctx context.Context, order *model.BojunRetailOrder, position int, policy OrderPushSkipPolicy) bojunOrderPushResult {
	target, ok := bojunTargetForStore(order.StoreCode)
	if !ok {
		err := fmt.Errorf("bojun store %q has no configured push target", order.StoreCode)
		return bojunOrderPushResult{Skipped: true, Error: err}
	}
	if policy.ShouldSkip(position) {
		s.writePolicySkippedLog(ctx, order, target, policy, position)
		statusErr := s.retailOrderDAO.UpdateSyncStatus(ctx, order.ID, 1)
		return bojunOrderPushResult{
			Target:          target,
			Success:         true,
			Skipped:         true,
			SyncStatusSaved: statusErr == nil,
			Error:           statusErr,
		}
	}

	traceID := uuid.NewString()
	startedAt := time.Now()
	runID, err := s.pipelineRunDAO.Create(ctx, &model.PipelineRun{
		TraceID:      traceID,
		RunType:      "delivery",
		TriggerType:  "event",
		Status:       "running",
		TotalCount:   1,
		StartedAt:    &model.TimeNormal{Time: startedAt},
		ErrorMessage: fmt.Sprintf("%s -> %s docno=%s", bojunOrderPushSource, target.Name, order.DocNo),
	})
	if err != nil {
		return bojunOrderPushResult{Target: target, Error: err}
	}

	success, pushErr := s.pushToTarget(ctx, traceID, runID, order, target)
	status := "success"
	successCount := 1
	failedCount := 0
	errorMessage := ""
	synced := 1
	if !success {
		status = "failed"
		successCount = 0
		failedCount = 1
		synced = 2
		if pushErr != nil {
			errorMessage = pushErr.Error()
		}
	}

	if err := s.pipelineRunDAO.Finish(ctx, runID, status, successCount, failedCount, errorMessage); err != nil && pushErr == nil {
		pushErr = err
	}
	statusErr := s.retailOrderDAO.UpdateSyncStatus(ctx, order.ID, synced)
	if statusErr != nil && pushErr == nil {
		pushErr = statusErr
	}

	return bojunOrderPushResult{
		Target:          target,
		Success:         success,
		SyncStatusSaved: statusErr == nil,
		Error:           pushErr,
	}
}

func (s *BojunOrderPushService) pushToTarget(
	ctx context.Context,
	traceID string,
	runID uint,
	order *model.BojunRetailOrder,
	target bojunOrderPushTarget,
) (bool, error) {
	if target.Code == bojunPushTargetHangzhouHenglong {
		return s.pushHangzhouHenglong(ctx, traceID, runID, order, target)
	}

	retailOrder := retailOrderForTarget(order, target.Code)
	result, err := shanghaimall.Push(ctx, shanghaimall.Target(target.Code), retailOrder)
	requestBody := marshalLogJSON(resultRequestBody(result))
	responseBody := ""
	httpStatus := 0
	success := false
	if result != nil {
		responseBody = result.ResponseBody
		httpStatus = result.HTTPStatus
		success = result.Success
	}
	s.writeDeliveryLog(ctx, deliveryLogPayload{
		TraceID:       traceID,
		RunID:         runID,
		Target:        target,
		Order:         order,
		RequestBody:   requestBody,
		ResponseBody:  responseBody,
		HTTPStatus:    httpStatus,
		Success:       success,
		DeliveryError: err,
	})
	return success, err
}

func (s *BojunOrderPushService) pushHangzhouHenglong(
	ctx context.Context,
	traceID string,
	runID uint,
	order *model.BojunRetailOrder,
	target bojunOrderPushTarget,
) (bool, error) {
	retailOrder := shanghaimall.RetailOrderFromBojun(*order)
	salesType := "SA"
	storeCode := bojunHangzhouHenglongStoreCode
	mallItemCode := bojunHangzhouHenglongItemCode
	if retailOrder.IsRefund() {
		salesType = "SR"
	}
	issuedAt := app.TimeNowInTimezone()

	result, err := send.SendSalesDataWithResultContext(
		ctx,
		retailOrder.Amount,
		order.DocNo,
		&issuedAt,
		storeCode,
		mallItemCode,
		salesType,
	)
	requestBody := ""
	responseBody := ""
	httpStatus := 0
	success := false
	if result != nil {
		requestBody = result.RequestBody
		responseBody = result.ResponseBody
		httpStatus = result.HTTPStatus
		success = result.Success
	}
	if err == nil && !success {
		err = fmt.Errorf("hangzhou henglong push failed: %s", responseBody)
	}

	s.writeDeliveryLog(ctx, deliveryLogPayload{
		TraceID:       traceID,
		RunID:         runID,
		Target:        target,
		Order:         order,
		RequestBody:   requestBody,
		ResponseBody:  responseBody,
		HTTPStatus:    httpStatus,
		Success:       success,
		DeliveryError: err,
	})
	return success, err
}

type deliveryLogPayload struct {
	TraceID       string
	RunID         uint
	Target        bojunOrderPushTarget
	Order         *model.BojunRetailOrder
	RequestBody   string
	ResponseBody  string
	HTTPStatus    int
	Success       bool
	DeliveryError error
}

func (s *BojunOrderPushService) writeDeliveryLog(ctx context.Context, payload deliveryLogPayload) {
	log := newBojunOrderDeliveryLog(payload, app.TimeNowInTimezone())
	_, _ = s.logDAO.Create(ctx, log)
}

func newBojunOrderDeliveryLog(payload deliveryLogPayload, sentAt time.Time) *model.DeliveryLog {
	log := &model.DeliveryLog{
		TraceID:         payload.TraceID,
		RunID:           payload.RunID,
		CleanRecordID:   payload.Order.ID,
		DestinationID:   0,
		DestinationCode: payload.Target.Code,
		DestinationName: payload.Target.Name,
		BusinessKey:     payload.Order.DocNo,
		DatasetKind:     bojunOrderDatasetKind,
		RequestBody:     payload.RequestBody,
		ResponseBody:    payload.ResponseBody,
		HTTPStatus:      payload.HTTPStatus,
		Success:         payload.Success,
		SentAt:          &model.TimeNormal{Time: sentAt},
	}
	if payload.DeliveryError != nil {
		log.ErrorMessage = payload.DeliveryError.Error()
	}
	return log
}

func (s *BojunOrderPushService) writePolicySkippedLog(ctx context.Context, order *model.BojunRetailOrder, target bojunOrderPushTarget, policy OrderPushSkipPolicy, position int) {
	requestBody := marshalLogJSON(map[string]interface{}{
		"push_skip_policy": policy,
		"position":         position,
		"reason":           policy.Reason(position),
	})
	_, _ = s.logDAO.Create(ctx, &model.DeliveryLog{
		TraceID:         uuid.NewString(),
		CleanRecordID:   order.ID,
		DestinationID:   0,
		DestinationCode: target.Code,
		DestinationName: target.Name,
		BusinessKey:     order.DocNo,
		DatasetKind:     bojunOrderDatasetKind,
		RequestBody:     requestBody,
		ResponseBody:    "skipped_by_order_push_policy",
		Success:         true,
		ErrorMessage:    policy.Reason(position),
		SentAt:          &model.TimeNormal{Time: time.Now()},
	})
}

func bojunTargetForStore(storeCode string) (bojunOrderPushTarget, bool) {
	normalizedStoreCode := strings.ToUpper(strings.TrimSpace(storeCode))
	targets := map[string]bojunOrderPushTarget{
		"ABCN001A001": {Code: string(shanghaimall.TargetShangsheng), Name: "上生新所", Store: "ABCN001A001"},
		"ABCN001A004": {Code: string(shanghaimall.TargetJialiCheng), Name: "嘉里城", Store: "ABCN001A004"},
		"ABCN001A005": {Code: string(shanghaimall.TargetPanlong), Name: "蟠龙", Store: "ABCN001A005"},
		"ABCN001A003": {Code: string(shanghaimall.TargetXintiandi), Name: "新天地", Store: "ABCN001A003"},
		"ABCN001P012": {Code: string(shanghaimall.TargetQiantan), Name: "前滩", Store: "ABCN001P012"},
		"ABCN002A001": {Code: bojunPushTargetHangzhouHenglong, Name: "杭州恒隆", Store: "ABCN002A001"},
	}
	if target, ok := targets[normalizedStoreCode]; ok {
		return target, true
	}
	if configuredStoreCode := xinjiaCenterBojunStoreCode(); configuredStoreCode != "" && normalizedStoreCode == configuredStoreCode {
		return bojunOrderPushTarget{
			Code:  string(shanghaimall.TargetXinjiaCenter),
			Name:  "新嘉中心",
			Store: configuredStoreCode,
		}, true
	}
	return bojunOrderPushTarget{}, false
}

func xinjiaCenterBojunStoreCode() string {
	return strings.ToUpper(strings.TrimSpace(os.Getenv("SHANGHAI_XINJIA_CENTER_BOJUN_STORE_CODE")))
}

func retailOrderForTarget(order *model.BojunRetailOrder, targetCode string) shanghaimall.RetailOrder {
	retailOrder := shanghaimall.RetailOrderFromBojun(*order)
	if targetCode == string(shanghaimall.TargetXinjiaCenter) && order.CompletedAt != nil {
		retailOrder.SaleTime = order.CompletedAt.Format("2006-01-02 15:04:05")
	}
	return retailOrder
}

func resultRequestBody(result *shanghaimall.PushResult) map[string]interface{} {
	if result == nil {
		return nil
	}
	return result.RequestBody
}

func marshalLogJSON(value interface{}) string {
	if value == nil {
		return ""
	}
	data, err := json.Marshal(redactLogValue(value))
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(data)
}

func redactLogValue(value interface{}) interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		redacted := make(map[string]interface{}, len(typed))
		maps.Copy(redacted, typed)
		for key, item := range redacted {
			if isSensitiveLogKey(key) {
				redacted[key] = "***"
				continue
			}
			redacted[key] = redactLogValue(item)
		}
		return redacted
	case []interface{}:
		redacted := make([]interface{}, 0, len(typed))
		for _, item := range typed {
			redacted = append(redacted, redactLogValue(item))
		}
		return redacted
	case []map[string]interface{}:
		redacted := make([]map[string]interface{}, 0, len(typed))
		for _, item := range typed {
			if mapped, ok := redactLogValue(item).(map[string]interface{}); ok {
				redacted = append(redacted, mapped)
			}
		}
		return redacted
	default:
		return value
	}
}

func isSensitiveLogKey(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(key), "_", ""))
	for _, marker := range []string{"password", "secret", "token", "apikey", "licensekey"} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func bojunBillDateTime(billDate int) *time.Time {
	text := fmt.Sprintf("%08d", billDate)
	parsed, err := time.Parse("20060102", text)
	if err != nil {
		now := time.Now()
		return &now
	}
	return &parsed
}
