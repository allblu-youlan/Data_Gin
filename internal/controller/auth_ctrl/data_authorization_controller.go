package auth_ctrl

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math/bits"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gin-biz-web-api/internal/requests/auth_request"
	"gin-biz-web-api/internal/service/auth_svc"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/auth"
	"gin-biz-web-api/pkg/errcode"
	"gin-biz-web-api/pkg/responses"

	"github.com/gin-gonic/gin"
)

const maxDataAuthorizationBodyBytes int64 = 64 << 10

type DataAuthorizationService interface {
	QueryAccounts(context.Context, uint, auth_request.DataAuthorizationAccountQueryRequest) (*auth_svc.DataAuthorizationAccountQueryResult, error)
	CreateAccount(context.Context, uint, string, auth_request.DataAuthorizationAccountCreateRequest) (*auth_svc.DataAuthorizationAccountCreateResult, error)
	Grant(context.Context, uint, uint, string, auth_request.DataAuthorizationGrantRequest) (*auth_svc.DataAuthorizationMutationResult, error)
	Revoke(context.Context, uint, uint, string, auth_request.DataAuthorizationRevokeRequest) (*auth_svc.DataAuthorizationMutationResult, error)
	ReissueToken(context.Context, uint, uint, string, auth_request.DataAuthorizationTokenReissueRequest) (*auth_svc.DataAuthorizationTokenReissueResult, error)
	QueryAudits(context.Context, uint, auth_request.DataAuthorizationAuditQueryRequest) (*auth_svc.DataAuthorizationAuditQueryResult, error)
}

type DataAuthorizationController struct{ service DataAuthorizationService }

func NewDataAuthorizationController() *DataAuthorizationController {
	return NewDataAuthorizationControllerWithService(auth_svc.NewDataAuthorizationService())
}

func NewDataAuthorizationControllerWithService(service DataAuthorizationService) *DataAuthorizationController {
	if service == nil {
		panic("data authorization controller: nil service")
	}
	return &DataAuthorizationController{service: service}
}

func (controller *DataAuthorizationController) PermissionCatalog(c *gin.Context) {
	writeDataAuthorizationResult(c, gin.H{"groups": model.OpenAPIPermissionGroups()}, nil, http.StatusOK)
}

func (controller *DataAuthorizationController) QueryAccounts(c *gin.Context) {
	var request auth_request.DataAuthorizationAccountQueryRequest
	if decodeDataAuthorizationJSON(c, &request) != nil {
		writeDataAuthorizationError(c, auth_svc.ErrDataAuthorizationInvalidInput)
		return
	}
	ctx, cancel := dataAuthorizationContext(c)
	defer cancel()
	result, err := controller.service.QueryAccounts(ctx, auth.CurrentUserID(c), request)
	writeDataAuthorizationResult(c, result, err, http.StatusOK)
}

func (controller *DataAuthorizationController) CreateAccount(c *gin.Context) {
	var request auth_request.DataAuthorizationAccountCreateRequest
	if decodeDataAuthorizationJSON(c, &request) != nil {
		writeDataAuthorizationError(c, auth_svc.ErrDataAuthorizationInvalidInput)
		return
	}
	ctx, cancel := dataAuthorizationContext(c)
	defer cancel()
	result, err := controller.service.CreateAccount(ctx, auth.CurrentUserID(c), c.GetHeader("Idempotency-Key"), request)
	if result != nil && result.Replayed {
		c.Header("Idempotency-Replayed", "true")
	}
	writeDataAuthorizationResult(c, result, err, http.StatusCreated)
}

func (controller *DataAuthorizationController) Grant(c *gin.Context) {
	targetID, err := parseDataAuthorizationID(c.Param("id"))
	if err != nil {
		writeDataAuthorizationError(c, err)
		return
	}
	var request auth_request.DataAuthorizationGrantRequest
	if decodeDataAuthorizationJSON(c, &request) != nil {
		writeDataAuthorizationError(c, auth_svc.ErrDataAuthorizationInvalidInput)
		return
	}
	ctx, cancel := dataAuthorizationContext(c)
	defer cancel()
	result, err := controller.service.Grant(ctx, auth.CurrentUserID(c), targetID, c.GetHeader("Idempotency-Key"), request)
	if result != nil && result.Replayed {
		c.Header("Idempotency-Replayed", "true")
	}
	writeDataAuthorizationResult(c, result, err, http.StatusOK)
}

func (controller *DataAuthorizationController) Revoke(c *gin.Context) {
	targetID, err := parseDataAuthorizationID(c.Param("id"))
	if err != nil {
		writeDataAuthorizationError(c, err)
		return
	}
	var request auth_request.DataAuthorizationRevokeRequest
	if decodeDataAuthorizationJSON(c, &request) != nil {
		writeDataAuthorizationError(c, auth_svc.ErrDataAuthorizationInvalidInput)
		return
	}
	ctx, cancel := dataAuthorizationContext(c)
	defer cancel()
	result, err := controller.service.Revoke(ctx, auth.CurrentUserID(c), targetID, c.GetHeader("Idempotency-Key"), request)
	if result != nil && result.Replayed {
		c.Header("Idempotency-Replayed", "true")
	}
	writeDataAuthorizationResult(c, result, err, http.StatusOK)
}

func (controller *DataAuthorizationController) ReissueToken(c *gin.Context) {
	targetID, err := parseDataAuthorizationID(c.Param("id"))
	if err != nil {
		writeDataAuthorizationError(c, err)
		return
	}
	var request auth_request.DataAuthorizationTokenReissueRequest
	if decodeDataAuthorizationJSON(c, &request) != nil {
		writeDataAuthorizationError(c, auth_svc.ErrDataAuthorizationInvalidInput)
		return
	}
	ctx, cancel := dataAuthorizationContext(c)
	defer cancel()
	result, err := controller.service.ReissueToken(ctx, auth.CurrentUserID(c), targetID, c.GetHeader("Idempotency-Key"), request)
	if result != nil && result.Replayed {
		c.Header("Idempotency-Replayed", "true")
	}
	writeDataAuthorizationResult(c, result, err, http.StatusOK)
}

func (controller *DataAuthorizationController) QueryAudits(c *gin.Context) {
	var request auth_request.DataAuthorizationAuditQueryRequest
	if decodeDataAuthorizationJSON(c, &request) != nil {
		writeDataAuthorizationError(c, auth_svc.ErrDataAuthorizationInvalidInput)
		return
	}
	ctx, cancel := dataAuthorizationContext(c)
	defer cancel()
	result, err := controller.service.QueryAudits(ctx, auth.CurrentUserID(c), request)
	writeDataAuthorizationResult(c, result, err, http.StatusOK)
}

func dataAuthorizationContext(c *gin.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(c.Request.Context(), 5*time.Second)
}

func decodeDataAuthorizationJSON(c *gin.Context, destination interface{}) error {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxDataAuthorizationBodyBytes)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func parseDataAuthorizationID(value string) (uint, error) {
	parsed, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
	if err != nil || parsed == 0 || bits.UintSize == 32 && parsed > uint64(^uint(0)) {
		return 0, auth_svc.ErrDataAuthorizationInvalidInput
	}
	return uint(parsed), nil
}

func writeDataAuthorizationResult(c *gin.Context, result interface{}, err error, successStatus int) {
	if err != nil {
		writeDataAuthorizationError(c, err)
		return
	}
	responses.New(c).ToResponseWithStatus(successStatus, result)
}

func writeDataAuthorizationError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, auth_svc.ErrDataAuthorizationForbidden):
		responses.New(c).ToSafeErrorResponse(errcode.Forbidden, "仅可信管理员可管理数据授权")
	case errors.Is(err, auth_svc.ErrDataAuthorizationNotFound):
		responses.New(c).ToSafeErrorResponse(errcode.NotFound, "开放接口账号不存在")
	case errors.Is(err, auth_svc.ErrDataAuthorizationConflict), errors.Is(err, auth_svc.ErrDataAuthorizationIdempotencyConflict), errors.Is(err, auth_svc.ErrDataAuthorizationIdempotencyPending):
		responses.New(c).ToSafeErrorResponse(errcode.Conflict, "数据授权请求冲突，请刷新后重试")
	case errors.Is(err, auth_svc.ErrDataAuthorizationInvalidInput):
		responses.New(c).ToSafeErrorResponse(errcode.UnprocessableEntity, "数据授权请求参数校验失败")
	default:
		responses.New(c).ToSafeErrorResponse(errcode.InternalServerError, "数据授权服务暂时不可用")
	}
}
