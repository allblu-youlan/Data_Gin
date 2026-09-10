package data_ctrl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"gin-biz-web-api/constant"
	"gin-biz-web-api/internal/requestbody"
	"gin-biz-web-api/internal/service/data_svc"
	"gin-biz-web-api/model"
)

func TestReportControllerCreateUsesActorAndStrictJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &fakeReportControllerService{createResult: &data_svc.ReportDraftDTO{ID: 9}}
	controller := NewReportControllerWithService(service)
	router := reportControllerRouter()
	router.POST("/reports", controller.Create)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/reports", strings.NewReader(`{"code":"sales","unknown":true}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnprocessableEntity || service.createCalls != 0 {
		t.Fatalf("unknown field response = %d %s, calls=%d", recorder.Code, recorder.Body, service.createCalls)
	}

	recorder = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/reports", strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated || service.actor != 17 || service.createCalls != 1 {
		t.Fatalf("create response = %d %s actor=%d calls=%d", recorder.Code, recorder.Body, service.actor, service.createCalls)
	}
}

func TestReportControllerErrorContract(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{name: "not found", err: data_svc.ErrReportNotFound, wantStatus: http.StatusNotFound},
		{name: "conflict", err: data_svc.ErrReportConflict, wantStatus: http.StatusConflict},
		{name: "delete conflict", err: data_svc.ErrReportDeleteConflict, wantStatus: http.StatusConflict},
		{name: "invalid", err: data_svc.ErrReportInvalid, wantStatus: http.StatusUnprocessableEntity},
		{name: "publication invalid", err: data_svc.ErrReportPublicationInvalid, wantStatus: http.StatusUnprocessableEntity},
		{name: "temporary result table", err: fmt.Errorf("%w: %w", data_svc.ErrReportPublicationInvalid, data_svc.ErrReportPublicationTemporaryTable), wantStatus: http.StatusUnprocessableEntity},
		{name: "parameter keyring unavailable", err: data_svc.ErrReportRunCredentialUnavailable, wantStatus: http.StatusServiceUnavailable},
		{name: "report run busy", err: data_svc.ErrReportRunBusy, wantStatus: http.StatusConflict},
		{name: "input query unavailable", err: data_svc.ErrReportInputQueryUnavailable, wantStatus: http.StatusServiceUnavailable},
		{name: "internal", err: errors.New("database password=secret"), wantStatus: http.StatusInternalServerError},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			controller := NewReportControllerWithService(&fakeReportControllerService{getErr: test.err})
			router := reportControllerRouter()
			router.GET("/reports/:id", controller.Get)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/reports/9", nil))
			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body)
			}
			if strings.Contains(recorder.Body.String(), "password") || strings.Contains(recorder.Body.String(), "secret") {
				t.Fatalf("internal error leaked: %s", recorder.Body)
			}
		})
	}
}

func TestReportControllerExplainsUnsupportedTemporaryResultTable(t *testing.T) {
	controller := NewReportControllerWithService(&fakeReportControllerService{
		getErr: fmt.Errorf("%w: %w", data_svc.ErrReportPublicationInvalid, data_svc.ErrReportPublicationTemporaryTable),
	})
	router := reportControllerRouter()
	router.GET("/reports/:id", controller.Get)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/reports/9", nil))

	if recorder.Code != http.StatusUnprocessableEntity || !strings.Contains(recorder.Body.String(), "不能使用临时表") || !strings.Contains(recorder.Body.String(), "导出成功后清空") {
		t.Fatalf("response = %d %s", recorder.Code, recorder.Body)
	}
}

func TestReportControllerInvalidDraftReturnsSafeSpecificReason(t *testing.T) {
	controller := NewReportControllerWithService(&fakeReportControllerService{
		getErr: fmt.Errorf("%w: invalid Oracle result table", data_svc.ErrReportInvalid),
	})
	router := reportControllerRouter()
	router.GET("/reports/:id", controller.Get)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/reports/9", nil))

	if recorder.Code != http.StatusUnprocessableEntity || !strings.Contains(recorder.Body.String(), "Oracle 结果表") {
		t.Fatalf("response = %d %s", recorder.Code, recorder.Body)
	}
}

func TestWriteReportErrorRecordsPrivateCauseWithoutLeakingIt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	cause := errors.New("database password=secret")

	writeReportError(context, cause)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body)
	}
	privateErrors := context.Errors.ByType(gin.ErrorTypePrivate)
	if len(privateErrors) != 1 || !errors.Is(privateErrors[0].Err, cause) {
		t.Fatalf("private errors = %#v", privateErrors)
	}
	if strings.Contains(recorder.Body.String(), "password") || strings.Contains(recorder.Body.String(), "secret") {
		t.Fatalf("internal error leaked: %s", recorder.Body)
	}
}

func TestReportControllerListAndUpdateParseBoundaries(t *testing.T) {
	service := &fakeReportControllerService{listResult: &data_svc.ReportDraftListDTO{}, updateResult: &data_svc.ReportDraftDTO{ID: 7}}
	controller := NewReportControllerWithService(service)
	router := reportControllerRouter()
	router.GET("/reports", controller.List)
	router.PUT("/reports/:id", controller.Update)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/reports?afterId=4&limit=20&category=finance&search=sales", nil))
	if recorder.Code != http.StatusOK || service.afterID != 4 || service.limit != 20 || service.category != "finance" || service.search != "sales" {
		t.Fatalf("list response = %d %s service=%#v", recorder.Code, recorder.Body, service)
	}

	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/reports?limit=bad", nil))
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("bad list status = %d body=%s", recorder.Code, recorder.Body)
	}

	recorder = httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/reports/7", strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || service.reportID != 7 || service.updateCalls != 1 {
		t.Fatalf("update response = %d %s id=%d calls=%d", recorder.Code, recorder.Body, service.reportID, service.updateCalls)
	}
}

func TestReportControllerDeleteUsesActorAndExpectedLockVersion(t *testing.T) {
	service := &fakeReportControllerService{deleteResult: &data_svc.ReportDraftDeleteDTO{ID: 7}}
	controller := NewReportControllerWithService(service)
	router := reportControllerRouter()
	router.DELETE("/reports/:id", controller.Delete)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodDelete, "/reports/7?expectedLockVersion=3", nil))
	if recorder.Code != http.StatusOK || service.actor != 17 || service.reportID != 7 || service.deleteLockVersion != 3 || service.deleteCalls != 1 || !strings.Contains(recorder.Body.String(), `"id":7`) {
		t.Fatalf("delete response = %d %s service=%#v", recorder.Code, recorder.Body, service)
	}

	for _, path := range []string{"/reports/7", "/reports/7?expectedLockVersion=0", "/reports/7?expectedLockVersion=bad", "/reports/0?expectedLockVersion=3"} {
		recorder = httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodDelete, path, nil))
		if recorder.Code != http.StatusUnprocessableEntity || service.deleteCalls != 1 {
			t.Fatalf("invalid delete %q response = %d %s calls=%d", path, recorder.Code, recorder.Body, service.deleteCalls)
		}
	}
}

func TestReportControllerListReturnsSharedPublishedSummaryWithoutConfigurationMetadata(t *testing.T) {
	service := &fakeReportControllerService{listResult: &data_svc.ReportDraftListDTO{Items: []data_svc.ReportDraftSummaryDTO{{
		ID: 12, Code: "shared_sales", Name: "共享销售报表", Status: "ACTIVE",
	}}}}
	controller := NewReportControllerWithService(service)
	router := reportControllerRouter()
	router.GET("/reports", controller.List)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/reports", nil))
	if recorder.Code != http.StatusOK || service.actor != 17 {
		t.Fatalf("list response = %d %s actor=%d", recorder.Code, recorder.Body, service.actor)
	}
	body := recorder.Body.String()
	for _, expected := range []string{`"code":"shared_sales"`, `"status":"ACTIVE"`, `"datasourceId":0`, `"lockVersion":0`, `"isOwner":false`} {
		if !strings.Contains(body, expected) {
			t.Fatalf("list response %s does not contain %s", body, expected)
		}
	}
	for _, forbidden := range []string{"ownerUserId", "currentDraftVersionId", "grants", "callTemplate"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("list response leaked %q: %s", forbidden, body)
		}
	}
}

func TestReportControllerPublishUsesActorAndLockVersion(t *testing.T) {
	service := &fakeReportControllerService{}
	hash := strings.Repeat("a", 64)
	publishService := &fakeReportPublishService{result: &data_svc.ReportPublicationDTO{DefinitionID: 7, VersionID: 23, Version: 3, Status: "PUBLISHED", ContractHash: hash, Validation: &data_svc.ReportPublicationValidationDTO{
		Procedure: data_svc.ReportPublicationProcedureDTO{Owner: "REPORT", Name: "BUILD", ArgumentCount: 1, SignatureHash: hash},
		Result:    data_svc.ReportPublicationResultDTO{TableOwner: "REPORT", TableName: "RESULT", ColumnCount: 3, SchemaHash: hash},
		Snapshot:  data_svc.ReportPublicationSnapshotDTO{ResultTableValidated: true},
		Export:    data_svc.ReportPublicationExportDTO{ExportableColumnCount: 1, SchemaHash: hash},
	}}}
	controller := NewReportControllerWithServices(service, publishService)
	router := reportControllerRouter()
	router.POST("/reports/:id/publish", controller.Publish)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/reports/7/publish", strings.NewReader(`{"expectedLockVersion":3}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || publishService.actor != 17 || publishService.reportID != 7 || publishService.lockVersion != 3 {
		t.Fatalf("publish response = %d %s service=%#v", recorder.Code, recorder.Body, publishService)
	}
	var envelope map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode publish response: %v", err)
	}
	encoded := recorder.Body.String()
	for _, expected := range []string{`"definitionId":7`, `"versionId":23`, `"contractHash":"` + hash + `"`, `"argumentCount":1`, `"resultTableValidated":true`, `"exportableColumnCount":1`} {
		if !strings.Contains(encoded, expected) {
			t.Fatalf("publish response %s does not contain %s", encoded, expected)
		}
	}
	for _, forbidden := range []string{"compiledSpec", "schemaProbeToken", "password", "credential", "dsn"} {
		if strings.Contains(strings.ToLower(encoded), strings.ToLower(forbidden)) {
			t.Fatalf("publish response leaked %q: %s", forbidden, encoded)
		}
	}

	recorder = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/reports/7/publish", strings.NewReader(`{"expectedLockVersion":0}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnprocessableEntity || publishService.calls != 1 {
		t.Fatalf("invalid publish response = %d %s calls=%d", recorder.Code, recorder.Body, publishService.calls)
	}
}

func TestReportControllerActivatesSelectedVersion(t *testing.T) {
	publishService := &fakeReportPublishService{result: &data_svc.ReportPublicationDTO{DefinitionID: 7, VersionID: 18, Version: 2, Status: model.ReportVersionStatusPublished}}
	controller := NewReportControllerWithServices(&fakeReportControllerService{}, publishService)
	router := reportControllerRouter()
	router.PUT("/reports/:id/active-version", controller.ActivateVersion)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/reports/7/active-version", strings.NewReader(`{"versionId":18,"expectedLockVersion":3}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || publishService.actor != 17 || publishService.reportID != 7 || publishService.versionID != 18 || publishService.lockVersion != 3 {
		t.Fatalf("activation response = %d %s service=%#v", recorder.Code, recorder.Body, publishService)
	}

	recorder = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPut, "/reports/7/active-version", strings.NewReader(`{"versionId":0,"expectedLockVersion":3}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnprocessableEntity || publishService.calls != 1 {
		t.Fatalf("invalid activation response = %d %s calls=%d", recorder.Code, recorder.Body, publishService.calls)
	}
}

func TestReportControllerCreateRunUsesActorParametersAndStrictJSON(t *testing.T) {
	draftService := &fakeReportControllerService{}
	runService := &fakeReportRunService{result: &data_svc.ReportRunDTO{ID: 31}}
	controller := NewReportControllerWithAllServices(draftService, nil, runService)
	router := reportControllerRouter()
	router.POST("/reports/:id/runs", controller.CreateRun)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/reports/9/runs", strings.NewReader(`{"parameters":{"storeCode":"S001"},"refreshNonce":"refresh-1"}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted || runService.actor != 17 || runService.reportID != 9 ||
		string(runService.request.Parameters["storeCode"]) != `"S001"` || runService.request.RefreshNonce != "refresh-1" {
		t.Fatalf("create run response = %d %s service=%#v", recorder.Code, recorder.Body, runService)
	}

	recorder = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/reports/9/runs", strings.NewReader(`{"parameters":{},"unknown":true}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnprocessableEntity || runService.calls != 1 {
		t.Fatalf("invalid run response = %d %s calls=%d", recorder.Code, recorder.Body, runService.calls)
	}
}

func TestReportControllerGetRunContractUsesActorAndReport(t *testing.T) {
	runService := &fakeReportRunService{contract: &data_svc.ReportRunContractDTO{DefinitionID: 9, VersionID: 23}}
	controller := NewReportControllerWithAllServices(&fakeReportControllerService{}, nil, runService)
	router := reportControllerRouter()
	router.GET("/reports/:id/run-contract", controller.GetRunContract)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/reports/9/run-contract", nil))
	if recorder.Code != http.StatusOK || runService.actor != 17 || runService.reportID != 9 || runService.contractCalls != 1 {
		t.Fatalf("contract response = %d %s service=%#v", recorder.Code, recorder.Body, runService)
	}
}

func TestReportControllerVersionEndpointsValidateAndScopeRequests(t *testing.T) {
	versionService := &fakeReportVersionService{page: &data_svc.ReportVersionPageDTO{}, diff: &data_svc.ReportVersionDiffDTO{}}
	controller := NewReportControllerWithVersionService(&fakeReportControllerService{}, nil, nil, versionService)
	router := reportControllerRouter()
	router.GET("/reports/:id/versions", controller.ListVersions)
	router.GET("/reports/:id/versions/:versionId", controller.GetVersion)
	router.GET("/reports/:id/version-diff", controller.VersionDiff)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/reports/9/versions?afterId=23&limit=50", nil))
	if recorder.Code != http.StatusOK || versionService.actor != 17 || versionService.reportID != 9 || versionService.afterID != 23 || versionService.limit != 50 {
		t.Fatalf("versions response=%d service=%#v", recorder.Code, versionService)
	}
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/reports/9/versions/12", nil))
	if recorder.Code != http.StatusOK || versionService.reportID != 9 || versionService.targetID != 12 {
		t.Fatalf("version detail response=%d service=%#v", recorder.Code, versionService)
	}
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/reports/9/version-diff?baseVersionId=11&targetVersionId=23", nil))
	if recorder.Code != http.StatusOK || versionService.baseID != 11 || versionService.targetID != 23 {
		t.Fatalf("diff response=%d service=%#v", recorder.Code, versionService)
	}
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/reports/9/version-diff?baseVersionId=11&targetVersionId=11", nil))
	if recorder.Code != http.StatusUnprocessableEntity || versionService.diffCalls != 1 {
		t.Fatalf("invalid diff response=%d calls=%d", recorder.Code, versionService.diffCalls)
	}
}

func TestReportControllerCreateRunMapsDeniedAndInvalid(t *testing.T) {
	for _, test := range []struct {
		name        string
		err         error
		wantStatus  int
		wantMessage string
	}{
		{name: "denied", err: data_svc.ErrReportRunDenied, wantStatus: http.StatusForbidden},
		{name: "invalid", err: data_svc.ErrReportRunInvalid, wantStatus: http.StatusUnprocessableEntity},
		{name: "busy", err: data_svc.ErrReportRunBusy, wantStatus: http.StatusConflict, wantMessage: "已有生成任务"},
	} {
		t.Run(test.name, func(t *testing.T) {
			runService := &fakeReportRunService{err: test.err}
			controller := NewReportControllerWithAllServices(&fakeReportControllerService{}, nil, runService)
			router := reportControllerRouter()
			router.POST("/reports/:id/runs", controller.CreateRun)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/reports/9/runs", strings.NewReader(`{"parameters":{}}`))
			request.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(recorder, request)
			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body)
			}
			if test.wantMessage != "" && !strings.Contains(recorder.Body.String(), test.wantMessage) {
				t.Fatalf("body = %s, want message %q", recorder.Body, test.wantMessage)
			}
		})
	}
}

func reportControllerRouter() *gin.Engine {
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(constant.CurrentUserID, "17")
		c.Next()
	})
	return router
}

type fakeReportControllerService struct {
	actor             uint
	reportID          uint
	afterID           uint
	limit             int
	category          string
	search            string
	createCalls       int
	updateCalls       int
	createResult      *data_svc.ReportDraftDTO
	listResult        *data_svc.ReportDraftListDTO
	updateResult      *data_svc.ReportDraftDTO
	deleteResult      *data_svc.ReportDraftDeleteDTO
	deleteCalls       int
	deleteLockVersion uint64
	getErr            error
}

type fakeReportPublishService struct {
	actor       uint
	reportID    uint
	versionID   uint
	lockVersion uint64
	calls       int
	result      *data_svc.ReportPublicationDTO
}

type fakeReportRunService struct {
	actor         uint
	reportID      uint
	calls         int
	request       requestbody.ReportRunCreateRequest
	result        *data_svc.ReportRunDTO
	contract      *data_svc.ReportRunContractDTO
	contractCalls int
	err           error
}

type fakeReportVersionService struct {
	actor, reportID, afterID uint
	limit                    int
	baseID, targetID         uint
	diffCalls                int
	page                     *data_svc.ReportVersionPageDTO
	diff                     *data_svc.ReportVersionDiffDTO
}

func (service *fakeReportVersionService) List(_ context.Context, actor, reportID, afterID uint, limit int) (*data_svc.ReportVersionPageDTO, error) {
	service.actor, service.reportID, service.afterID, service.limit = actor, reportID, afterID, limit
	return service.page, nil
}
func (service *fakeReportVersionService) Diff(_ context.Context, actor, reportID, baseID, targetID uint) (*data_svc.ReportVersionDiffDTO, error) {
	service.actor, service.reportID, service.baseID, service.targetID = actor, reportID, baseID, targetID
	service.diffCalls++
	return service.diff, nil
}
func (service *fakeReportVersionService) Get(_ context.Context, actor, reportID, versionID uint) (*data_svc.ReportVersionDetailDTO, error) {
	service.actor, service.reportID, service.targetID = actor, reportID, versionID
	return &data_svc.ReportVersionDetailDTO{}, nil
}

func (service *fakeReportRunService) Contract(_ context.Context, actor, reportID uint) (*data_svc.ReportRunContractDTO, error) {
	service.actor, service.reportID = actor, reportID
	service.contractCalls++
	return service.contract, service.err
}

func (service *fakeReportRunService) Create(_ context.Context, actor, reportID uint, request requestbody.ReportRunCreateRequest) (*data_svc.ReportRunDTO, error) {
	service.actor, service.reportID, service.request = actor, reportID, request
	service.calls++
	return service.result, service.err
}

func (service *fakeReportPublishService) Publish(_ context.Context, actor, reportID uint, lockVersion uint64) (*data_svc.ReportPublicationDTO, error) {
	service.actor, service.reportID, service.lockVersion = actor, reportID, lockVersion
	service.calls++
	return service.result, nil
}
func (service *fakeReportPublishService) Activate(_ context.Context, actor, reportID, versionID uint, lockVersion uint64) (*data_svc.ReportPublicationDTO, error) {
	service.actor, service.reportID, service.lockVersion = actor, reportID, lockVersion
	service.versionID = versionID
	service.calls++
	return service.result, nil
}

func (service *fakeReportControllerService) Create(_ context.Context, actor uint, _ requestbody.ReportDraftSaveRequest) (*data_svc.ReportDraftDTO, error) {
	service.actor = actor
	service.createCalls++
	return service.createResult, nil
}

func (service *fakeReportControllerService) Get(_ context.Context, actor, reportID uint) (*data_svc.ReportDraftDTO, error) {
	service.actor = actor
	service.reportID = reportID
	return nil, service.getErr
}

func (service *fakeReportControllerService) List(_ context.Context, actor, afterID uint, limit int, category, search string) (*data_svc.ReportDraftListDTO, error) {
	service.actor = actor
	service.afterID = afterID
	service.limit = limit
	service.category = category
	service.search = search
	return service.listResult, nil
}

func (service *fakeReportControllerService) Update(_ context.Context, actor, reportID uint, _ requestbody.ReportDraftSaveRequest) (*data_svc.ReportDraftDTO, error) {
	service.actor = actor
	service.reportID = reportID
	service.updateCalls++
	return service.updateResult, nil
}

func (service *fakeReportControllerService) Delete(_ context.Context, actor, reportID uint, expectedLockVersion uint64) (*data_svc.ReportDraftDeleteDTO, error) {
	service.actor = actor
	service.reportID = reportID
	service.deleteLockVersion = expectedLockVersion
	service.deleteCalls++
	return service.deleteResult, service.getErr
}
