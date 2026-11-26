package data_ctrl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/bits"
	"net/http"
	"strconv"
	"strings"

	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/internal/requestbody"
	"gin-biz-web-api/internal/service/data_svc"
	"gin-biz-web-api/pkg/auth"
	"gin-biz-web-api/pkg/errcode"
	"gin-biz-web-api/pkg/responses"

	"github.com/gin-gonic/gin"
)

const maxMallRequestBodyBytes int64 = 1 << 20

type MallService interface {
	Create(context.Context, uint, string, requestbody.MallCreateRequest) (*data_svc.MallCreateResult, bool, error)
	Import(context.Context, uint, string, []requestbody.MallCreateRequest) (*data_svc.MallImportResult, error)
	Get(context.Context, uint, uint) (*data_svc.MallDTO, error)
	List(context.Context, uint, requestbody.MallListRequest) (*data_svc.MallListResult, error)
	Update(context.Context, uint, uint, requestbody.MallPatchRequest) (*data_svc.MallDTO, error)
	Delete(context.Context, uint, uint, uint64) error
	TriggerGeocode(context.Context, uint, uint, uint64) (*data_svc.MallGeocodeTriggerResult, error)
	ListGeocodeCandidates(context.Context, uint, uint) (*data_svc.MallGeocodeCandidatesResult, error)
	ConfirmGeocode(context.Context, uint, uint, requestbody.MallGeocodeConfirmRequest) (*data_svc.MallDTO, error)
}

type MallController struct {
	service MallService
}

func NewMallController() *MallController {
	return NewMallControllerWithService(data_svc.NewMallService())
}

func NewMallControllerWithService(service MallService) *MallController {
	if service == nil {
		panic("mall controller: nil service")
	}
	return &MallController{service: service}
}

func (controller *MallController) Create(c *gin.Context) {
	var request requestbody.MallCreateRequest
	if err := decodeMallJSON(c, &request); err != nil {
		writeMallError(c, fmt.Errorf("%w: invalid JSON body", data_svc.ErrMallInvalidInput))
		return
	}
	result, replayed, err := controller.service.Create(
		c.Request.Context(),
		auth.CurrentUserID(c),
		c.GetHeader("Idempotency-Key"),
		request,
	)
	if err != nil {
		writeMallError(c, err)
		return
	}
	if replayed {
		c.Header("Idempotency-Replayed", "true")
	}
	responses.New(c).ToResponseWithStatus(http.StatusCreated, result)
}

func (controller *MallController) Import(c *gin.Context) {
	var request requestbody.MallImportRequest
	if err := decodeMallJSON(c, &request); err != nil {
		writeMallError(c, fmt.Errorf("%w: invalid JSON body", data_svc.ErrMallInvalidInput))
		return
	}
	result, err := controller.service.Import(
		c.Request.Context(),
		auth.CurrentUserID(c),
		c.GetHeader("Idempotency-Key"),
		request.Items,
	)
	if err != nil {
		writeMallError(c, err)
		return
	}
	responses.New(c).ToResponseWithStatus(http.StatusOK, result)
}

func (controller *MallController) Get(c *gin.Context) {
	mallID, err := parseMallUint(c.Param("id"), "mall id")
	if err != nil {
		writeMallError(c, err)
		return
	}
	result, err := controller.service.Get(c.Request.Context(), auth.CurrentUserID(c), mallID)
	if err != nil {
		writeMallError(c, err)
		return
	}
	responses.New(c).ToResponseWithStatus(http.StatusOK, result)
}

func (controller *MallController) List(c *gin.Context) {
	request, err := parseMallListRequest(c)
	if err != nil {
		writeMallError(c, err)
		return
	}
	result, err := controller.service.List(c.Request.Context(), auth.CurrentUserID(c), request)
	if err != nil {
		writeMallError(c, err)
		return
	}
	responses.New(c).ToResponseWithStatus(http.StatusOK, result)
}

func (controller *MallController) Update(c *gin.Context) {
	mallID, err := parseMallUint(c.Param("id"), "mall id")
	if err != nil {
		writeMallError(c, err)
		return
	}
	var request requestbody.MallPatchRequest
	if err := decodeMallJSON(c, &request); err != nil {
		writeMallError(c, fmt.Errorf("%w: invalid JSON body", data_svc.ErrMallInvalidInput))
		return
	}
	result, err := controller.service.Update(c.Request.Context(), auth.CurrentUserID(c), mallID, request)
	if err != nil {
		writeMallError(c, err)
		return
	}
	responses.New(c).ToResponseWithStatus(http.StatusOK, result)
}

func (controller *MallController) Delete(c *gin.Context) {
	mallID, err := parseMallUint(c.Param("id"), "mall id")
	if err != nil {
		writeMallError(c, err)
		return
	}
	version, err := parsePositiveUint64(c.Query("expectedMallVersion"), "expectedMallVersion")
	if err != nil {
		writeMallError(c, err)
		return
	}
	if err := controller.service.Delete(c.Request.Context(), auth.CurrentUserID(c), mallID, version); err != nil {
		writeMallError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (controller *MallController) TriggerGeocode(c *gin.Context) {
	mallID, err := parseMallUint(c.Param("id"), "mall id")
	if err != nil {
		writeMallError(c, err)
		return
	}
	var request requestbody.MallGeocodeTriggerRequest
	if err := decodeMallJSON(c, &request); err != nil {
		writeMallError(c, fmt.Errorf("%w: invalid JSON body", data_svc.ErrMallInvalidInput))
		return
	}
	result, err := controller.service.TriggerGeocode(
		c.Request.Context(), auth.CurrentUserID(c), mallID, request.ExpectedMallVersion,
	)
	if err != nil {
		writeMallError(c, err)
		return
	}
	responses.New(c).ToResponseWithStatus(http.StatusAccepted, result)
}

func (controller *MallController) ListGeocodeCandidates(c *gin.Context) {
	mallID, err := parseMallUint(c.Param("id"), "mall id")
	if err != nil {
		writeMallError(c, err)
		return
	}
	result, err := controller.service.ListGeocodeCandidates(c.Request.Context(), auth.CurrentUserID(c), mallID)
	if err != nil {
		writeMallError(c, err)
		return
	}
	responses.New(c).ToResponseWithStatus(http.StatusOK, result)
}

func (controller *MallController) ConfirmGeocode(c *gin.Context) {
	mallID, err := parseMallUint(c.Param("id"), "mall id")
	if err != nil {
		writeMallError(c, err)
		return
	}
	var request requestbody.MallGeocodeConfirmRequest
	if err := decodeMallJSON(c, &request); err != nil {
		writeMallError(c, fmt.Errorf("%w: invalid JSON body", data_svc.ErrMallInvalidInput))
		return
	}
	result, err := controller.service.ConfirmGeocode(
		c.Request.Context(), auth.CurrentUserID(c), mallID, request,
	)
	if err != nil {
		writeMallError(c, err)
		return
	}
	responses.New(c).ToResponseWithStatus(http.StatusOK, result)
}

func decodeMallJSON(c *gin.Context, destination interface{}) error {
	return decodeLimitedJSON(c, destination, maxMallRequestBodyBytes)
}

func decodeLimitedJSON(c *gin.Context, destination interface{}, maxBytes int64) error {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
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

func parseMallListRequest(c *gin.Context) (requestbody.MallListRequest, error) {
	var request requestbody.MallListRequest
	var err error
	if value := strings.TrimSpace(c.Query("afterId")); value != "" {
		request.AfterID, err = parseMallUint(value, "afterId")
		if err != nil {
			return request, err
		}
	}
	if value := strings.TrimSpace(c.Query("limit")); value != "" {
		request.Limit, err = strconv.Atoi(value)
		if err != nil || request.Limit <= 0 {
			return request, fmt.Errorf("%w: invalid limit", data_svc.ErrMallInvalidInput)
		}
	}
	if value := strings.TrimSpace(c.Query("weatherEnabled")); value != "" {
		enabled, parseErr := strconv.ParseBool(value)
		if parseErr != nil {
			return request, fmt.Errorf("%w: invalid weatherEnabled", data_svc.ErrMallInvalidInput)
		}
		request.WeatherEnabled = &enabled
	}
	request.City = c.Query("city")
	request.Status = c.Query("status")
	request.GeocodeStatus = c.Query("geocodeStatus")
	return request, nil
}

func parseMallUint(value, field string) (uint, error) {
	parsed, err := parsePositiveUint64(value, field)
	if err != nil || bits.UintSize == 32 && parsed > uint64(^uint(0)) {
		return 0, fmt.Errorf("%w: invalid %s", data_svc.ErrMallInvalidInput, field)
	}
	return uint(parsed), nil
}

func parsePositiveUint64(value, field string) (uint64, error) {
	parsed, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
	if err != nil || parsed == 0 {
		return 0, fmt.Errorf("%w: invalid %s", data_svc.ErrMallInvalidInput, field)
	}
	return parsed, nil
}

func writeMallError(c *gin.Context, err error) {
	code, message := classifyMallError(err)
	responses.New(c).ToSafeErrorResponse(code, message)
}

func classifyMallError(err error) (*errcode.Error, string) {
	switch {
	case errors.Is(err, data_svc.ErrMallForbidden):
		return errcode.Forbidden, "无权执行此商场操作"
	case errors.Is(err, data_dao.ErrMallNotFound):
		return errcode.NotFound, "商场不存在"
	case errors.Is(err, data_dao.ErrMallGeocodeRunNotFound),
		errors.Is(err, data_dao.ErrMallGeocodeCandidateNotFound):
		return errcode.NotFound, "地理编码候选不存在"
	case errors.Is(err, data_svc.ErrMallConflict),
		errors.Is(err, data_svc.ErrMallIdempotencyConflict),
		errors.Is(err, data_svc.ErrMallIdempotencyPending),
		errors.Is(err, data_dao.ErrMallVersionConflict):
		return errcode.Conflict, "商场请求冲突，请刷新后重试"
	case errors.Is(err, data_svc.ErrMallInvalidInput):
		return errcode.UnprocessableEntity, "商场请求参数校验失败"
	case errors.Is(err, data_svc.ErrMallWeatherDisabled):
		return errcode.ServiceUnavailable, "商场天气服务未启用，请联系管理员完成配置后重试"
	default:
		return errcode.InternalServerError, "商场服务暂时不可用"
	}
}
