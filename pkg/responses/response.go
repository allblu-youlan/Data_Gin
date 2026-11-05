// 数据返回封装
package responses

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"gin-biz-web-api/pkg/errcode"
	"gin-biz-web-api/pkg/helper/mapx"
	"gin-biz-web-api/pkg/paginator"
	"gin-biz-web-api/pkg/providerhttp"
)

type Response struct {
	Ctx *gin.Context
}

type ResponseData struct {
	Code int         `json:"code"` // 状态码
	Msg  string      `json:"msg"`  // 错误信息
	Data interface{} `json:"data"` // 数据
}

// New 实例化返回类
func New(ctx *gin.Context) *Response {
	return &Response{Ctx: ctx}
}

// ToResponse 正确数据返回
func (r *Response) ToResponse(data interface{}) {
	code := errcode.Success
	response := ResponseData{
		Code: code.Code(),
		Msg:  code.Msg(),
		Data: nil,
	}
	if data == nil {
		response.Data = gin.H{}
	} else {
		response.Data = data
	}

	r.Ctx.JSON(http.StatusOK, response)
}

// ToResponseWithStatus returns a successful response with the real HTTP status
// while preserving the existing application-level success code.
func (r *Response) ToResponseWithStatus(status int, data interface{}) {
	if data == nil {
		data = gin.H{}
	}
	r.Ctx.JSON(status, ResponseData{
		Code: errcode.Success.Code(),
		Msg:  errcode.Success.Msg(),
		Data: data,
	})
}

// ToSafeErrorResponse returns a stable public message and records private diagnostic context.
func (r *Response) ToSafeErrorResponse(err *errcode.Error, message string) {
	if err == nil {
		err = errcode.InternalServerError
	}
	if message == "" {
		message = err.Msg()
	}
	r.logErrorResponse(err, err, message, err.HttpStatusCode())
	r.Ctx.AbortWithStatusJSON(err.HttpStatusCode(), ResponseData{
		Code: err.Code(),
		Msg:  message,
		Data: nil,
	})
}

// ToResponseWithPagination 返回分页数据
func (r *Response) ToResponseWithPagination(result interface{}, pagination paginator.Pagination) {
	r.ToResponse(gin.H{
		"result":     result,
		"pagination": pagination,
	})
}

// ToErrorResponse 错误返回
func (r *Response) ToErrorResponse(err error, messages ...string) {
	var e *errcode.Error
	if !errors.As(err, &e) {
		e = errcode.InternalServerError.WithError(err)
	}

	response := ResponseData{
		Code: e.Code(),
		Msg:  e.Msg(),
		Data: nil,
	}

	if len(messages) > 0 {
		response.Msg = messages[0]
	}

	r.logErrorResponse(e, err, response.Msg, http.StatusOK)

	// r.Ctx.JSON(err.HttpStatusCode(), response)
	// r.Ctx.AbortWithStatusJSON(err.HttpStatusCode(), response)
	r.Ctx.AbortWithStatusJSON(http.StatusOK, response)
}

// ToErrorValidateResponse 验证器验证不通过时，错误返回
func (r *Response) ToErrorValidateResponse(err *errcode.Error, errors map[string][]string) {
	response := ResponseData{
		Code: err.Code(),
		Msg:  err.Msg(),
		Data: nil,
	}

	if len(errors) > 0 {
		ks := mapx.SortAscKeyString(errors)

		for _, k := range ks {
			response.Msg = errors[k][0]
			break
		}
	}
	LogRequestFailure(r.Ctx, zap.WarnLevel, response.Msg,
		zap.Int("business_code", err.Code()),
		zap.Int("response_status", http.StatusOK),
		zap.String("validation_errors", providerhttp.RedactText(fmt.Sprint(errors))),
	)

	// r.Ctx.AbortWithStatusJSON(err.HttpStatusCode(), response)
	r.Ctx.AbortWithStatusJSON(http.StatusOK, response)
}

func (r *Response) logErrorResponse(code *errcode.Error, err error, message string, status int) {
	fields := []zap.Field{
		zap.Int("business_code", code.Code()),
		zap.Int("response_status", status),
	}
	if err != nil {
		fields = append(fields, zap.String("error", providerhttp.RedactText(err.Error())))
	}
	if code.Err() != nil {
		fields = append(fields, zap.String("error_cause", providerhttp.RedactText(code.Err().Error())))
	}
	if len(code.Details()) > 0 {
		details := make([]string, len(code.Details()))
		for index, detail := range code.Details() {
			details[index] = providerhttp.RedactText(detail)
		}
		fields = append(fields, zap.Strings("error_details", details))
	}
	level := zap.WarnLevel
	if code.HttpStatusCode() >= http.StatusInternalServerError {
		level = zap.ErrorLevel
	}
	LogRequestFailure(r.Ctx, level, message, fields...)
}
