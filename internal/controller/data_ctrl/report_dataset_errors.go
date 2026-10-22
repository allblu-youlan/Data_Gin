package data_ctrl

import (
	"errors"
	"gin-biz-web-api/internal/service/data_svc"
	"gin-biz-web-api/pkg/errcode"
	"gin-biz-web-api/pkg/responses"
	"github.com/gin-gonic/gin"
	"net/http"
	"strings"
)

func writeKnownDatasetError(c *gin.Context, e error) bool {
	if e == nil {
		return false
	}
	status, code, message := 0, "", ""
	switch {
	case errors.Is(e, data_svc.ErrReportSelectResultExpired):
		status, code, message = http.StatusGone, "REPORT_RESULT_EXPIRED", "结果已过期，请重新生成报表"
	case strings.Contains(e.Error(), "REPORT_RELATION_NOT_UNIQUE"):
		status, code, message = http.StatusBadRequest, "REPORT_RELATION_NOT_UNIQUE", "关联目标必须绑定完整且已验证的唯一键"
	case strings.Contains(e.Error(), "REPORT_METADATA_UNSUPPORTED"):
		status, code, message = http.StatusBadRequest, "REPORT_METADATA_UNSUPPORTED", "字段或逻辑表过滤条件尚不支持执行"
	case strings.Contains(e.Error(), "REPORT_METADATA_CHANGED"):
		status, code, message = http.StatusConflict, "REPORT_METADATA_CHANGED", "依赖字段或授权范围已变化，请更新并重新发布"
	case strings.Contains(e.Error(), "REPORT_QUERY_LIMIT_EXCEEDED"):
		status, code, message = http.StatusUnprocessableEntity, "REPORT_QUERY_LIMIT_EXCEEDED", "结果超出查询限制，请缩小范围"
	case strings.Contains(e.Error(), "REPORT_QUERY_INVALID"):
		status, code, message = http.StatusBadRequest, "REPORT_QUERY_INVALID", "查询配置或条件无效，请核对字段、操作符与参数"
	}
	if status == 0 {
		return false
	}
	_ = c.Error(e).SetType(gin.ErrorTypePrivate)
	c.AbortWithStatusJSON(status, responses.ResponseData{Code: errcode.BadRequest.Code(), Msg: message, Data: gin.H{"errorCode": code}})
	return true
}
