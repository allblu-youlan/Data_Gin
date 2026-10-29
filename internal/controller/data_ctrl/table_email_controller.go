package data_ctrl

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"gin-biz-web-api/internal/service/data_svc"
	"gin-biz-web-api/pkg/auth"
	"gin-biz-web-api/pkg/errcode"
	"gin-biz-web-api/pkg/responses"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type TableEmailController struct{ service *data_svc.TableEmailService }

func NewTableEmailController() *TableEmailController {
	return &TableEmailController{service: data_svc.NewTableEmailService()}
}

func tableEmailResponse(c *gin.Context, value any, err error) {
	if err == nil {
		responses.New(c).ToResponse(value)
		return
	}
	_ = c.Error(err).SetType(gin.ErrorTypePrivate)
	switch {
	case errors.Is(err, data_svc.ErrTableEmailInvalid):
		responses.New(c).ToSafeErrorResponse(errcode.BadRequest, err.Error())
	case errors.Is(err, data_svc.ErrTableEmailConflict):
		responses.New(c).ToSafeErrorResponse(errcode.Conflict, err.Error())
	case errors.Is(err, gorm.ErrRecordNotFound):
		responses.New(c).ToSafeErrorResponse(errcode.NotFound, "提醒规则或发送记录不存在")
	default:
		responses.New(c).ToSafeErrorResponse(errcode.ServiceUnavailable, "邮件提醒服务暂时不可用，请检查数据库和 Oracle 配置")
	}
}
func bindTableEmailJSON(c *gin.Context, value any) error {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return data_svc.ErrTableEmailInvalid
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return data_svc.ErrTableEmailInvalid
	}
	return nil
}
func tableEmailID(c *gin.Context) (uint, error) {
	value, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || value == 0 {
		return 0, data_svc.ErrTableEmailInvalid
	}
	return uint(value), nil
}
func tableEmailQueryID(c *gin.Context, key string) (uint, error) {
	raw := c.Query(key)
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, data_svc.ErrTableEmailInvalid
	}
	return uint(value), nil
}
func (controller *TableEmailController) List(c *gin.Context) {
	items, err := controller.service.List(c.Request.Context())
	tableEmailResponse(c, gin.H{"items": items}, err)
}
func (controller *TableEmailController) Datasources(c *gin.Context) {
	items, err := controller.service.Datasources(c.Request.Context())
	tableEmailResponse(c, gin.H{"items": items}, err)
}
func (controller *TableEmailController) Tables(c *gin.Context) {
	id, err := tableEmailQueryID(c, "datasourceId")
	if err != nil {
		tableEmailResponse(c, nil, err)
		return
	}
	items, err := controller.service.Tables(c.Request.Context(), id, c.Query("owner"), c.Query("search"))
	tableEmailResponse(c, gin.H{"items": items}, err)
}
func (controller *TableEmailController) Columns(c *gin.Context) {
	id, err := tableEmailQueryID(c, "datasourceId")
	if err != nil {
		tableEmailResponse(c, nil, err)
		return
	}
	items, err := controller.service.Columns(c.Request.Context(), data_svc.TableEmailRuleInput{DatasourceID: id, Owner: c.Query("owner"), TableName: c.Query("tableName")})
	tableEmailResponse(c, gin.H{"items": items}, err)
}
func (controller *TableEmailController) Create(c *gin.Context) { controller.save(c, 0) }
func (controller *TableEmailController) Update(c *gin.Context) {
	id, err := tableEmailID(c)
	if err != nil {
		tableEmailResponse(c, nil, err)
		return
	}
	controller.save(c, id)
}
func (controller *TableEmailController) save(c *gin.Context, id uint) {
	var input data_svc.TableEmailRuleInput
	if err := bindTableEmailJSON(c, &input); err != nil {
		tableEmailResponse(c, nil, err)
		return
	}
	item, err := controller.service.Save(c.Request.Context(), auth.CurrentUserID(c), id, input)
	tableEmailResponse(c, item, err)
}
func (controller *TableEmailController) Delete(c *gin.Context) {
	id, err := tableEmailID(c)
	if err != nil {
		tableEmailResponse(c, nil, err)
		return
	}
	version, err := strconv.ParseUint(c.Query("expectedLockVersion"), 10, 64)
	if err != nil || version == 0 {
		tableEmailResponse(c, nil, data_svc.ErrTableEmailInvalid)
		return
	}
	tableEmailResponse(c, gin.H{"id": id}, controller.service.Delete(c.Request.Context(), id, version))
}
func (controller *TableEmailController) Preview(c *gin.Context) {
	var input struct {
		data_svc.TableEmailRuleInput
		Sample map[string]any `json:"sample"`
	}
	if err := bindTableEmailJSON(c, &input); err != nil {
		tableEmailResponse(c, nil, err)
		return
	}
	preview, err := controller.service.Preview(c.Request.Context(), input.TableEmailRuleInput, input.Sample)
	tableEmailResponse(c, preview, err)
}
func (controller *TableEmailController) Deliveries(c *gin.Context) {
	id, err := tableEmailQueryID(c, "ruleId")
	if err != nil {
		tableEmailResponse(c, nil, err)
		return
	}
	items, err := controller.service.Deliveries(c.Request.Context(), id)
	tableEmailResponse(c, gin.H{"items": items}, err)
}
func (controller *TableEmailController) Retry(c *gin.Context) {
	id, err := tableEmailID(c)
	if err != nil {
		tableEmailResponse(c, nil, err)
		return
	}
	tableEmailResponse(c, gin.H{"id": id}, controller.service.Retry(c.Request.Context(), id))
}
