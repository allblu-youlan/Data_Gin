package data_ctrl

import (
	"context"
	"net/http"
	"strconv"

	appconfig "gin-biz-web-api/config"
	"gin-biz-web-api/global"
	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/internal/service/data_svc"
	"gin-biz-web-api/pkg/auth"
	"gin-biz-web-api/pkg/database"
	"gin-biz-web-api/pkg/errcode"
	"gin-biz-web-api/pkg/logger"
	"gin-biz-web-api/pkg/paginator"
	"gin-biz-web-api/pkg/responses"
	"github.com/gin-gonic/gin"
)

type FeishuMentionController struct {
	service     *data_svc.FeishuMentionAdminService
	configError error
}

func NewFeishuMentionController() *FeishuMentionController {
	cfg, err := appconfig.LoadFeishuMentionConfig()
	return &FeishuMentionController{service: data_svc.NewFeishuMentionAdminService(database.DB, global.Credentials.FeishuAppID(), cfg), configError: err}
}
func (c *FeishuMentionController) check(ctx *gin.Context) bool {
	if c.configError != nil {
		writeFeishuMentionError(ctx, c.configError)
		return false
	}
	return true
}
func writeFeishuMentionError(c *gin.Context, err error) {
	status, msg := data_svc.FeishuMentionAdminSafeError(err)
	code := errcode.InternalServerError
	switch status {
	case 400:
		code = errcode.BadRequest
	case 404:
		code = errcode.NotFound
	case 409:
		code = errcode.Conflict
	}
	if c.Request.Method != "GET" && logger.Logger != nil {
		logger.WarnJSON("feishu_mention", "admin_request_failed", gin.H{"actorUserId": auth.CurrentUserID(c), "path": c.FullPath(), "objectId": c.Param("id"), "code": code.Code()})
	}
	responses.New(c).ToSafeErrorResponse(code, msg)
}
func mentionID(c *gin.Context, param string) (uint, bool) {
	n, e := strconv.ParseUint(c.Param(param), 10, 64)
	if e != nil || n == 0 || uint64(uint(n)) != n {
		writeFeishuMentionError(c, data_svc.ErrFeishuMentionAdminInvalid)
		return 0, false
	}
	return uint(n), true
}
func mentionFilter(c *gin.Context, kind string) (data_dao.FeishuMentionAdminFilter, bool) {
	f, e := data_svc.ParseFeishuMentionAdminFilter(c.Request.URL.Query(), kind)
	if e != nil {
		writeFeishuMentionError(c, e)
		return f, false
	}
	return f, true
}
func mentionList[T any](ctx *gin.Context, c *FeishuMentionController, kind string, fn func(context.Context, data_dao.FeishuMentionAdminFilter) ([]T, paginator.Pagination, error)) {
	if !c.check(ctx) {
		return
	}
	f, ok := mentionFilter(ctx, kind)
	if !ok {
		return
	}
	rows, p, err := fn(ctx.Request.Context(), f)
	if err != nil {
		writeFeishuMentionError(ctx, err)
		return
	}
	responses.New(ctx).ToResponseWithPagination(rows, p)
}
func (c *FeishuMentionController) Chats(ctx *gin.Context) {
	mentionList(ctx, c, "chats", c.service.Chats)
}
func (c *FeishuMentionController) Recipients(ctx *gin.Context) {
	mentionList(ctx, c, "recipients", c.service.Recipients)
}
func (c *FeishuMentionController) Reports(ctx *gin.Context) {
	mentionList(ctx, c, "reports", c.service.Reports)
}
func (c *FeishuMentionController) Jobs(ctx *gin.Context)       { mentionList(ctx, c, "jobs", c.service.Jobs) }
func (c *FeishuMentionController) CreateChat(ctx *gin.Context) { c.saveChat(ctx, 0) }
func (c *FeishuMentionController) SaveDigestSettings(ctx *gin.Context) {
	if !c.check(ctx) {
		return
	}
	var input data_svc.FeishuMentionDigestSettingsInput
	if err := bindOfficeJSON(ctx, &input); err != nil {
		writeFeishuMentionError(ctx, data_svc.ErrFeishuMentionAdminInvalid)
		return
	}
	out, err := c.service.SaveDigestSettings(ctx.Request.Context(), auth.CurrentUserID(ctx), input)
	if err != nil {
		writeFeishuMentionError(ctx, err)
		return
	}
	responses.New(ctx).ToResponse(out)
}
func (c *FeishuMentionController) UpdateChat(ctx *gin.Context) {
	id, ok := mentionID(ctx, "id")
	if ok {
		c.saveChat(ctx, id)
	}
}
func (c *FeishuMentionController) saveChat(ctx *gin.Context, id uint) {
	if !c.check(ctx) {
		return
	}
	var input data_svc.FeishuMentionChatInput
	if e := bindOfficeJSON(ctx, &input); e != nil {
		writeFeishuMentionError(ctx, data_svc.ErrFeishuMentionAdminInvalid)
		return
	}
	out, e := c.service.SaveChat(ctx.Request.Context(), auth.CurrentUserID(ctx), id, input)
	if e != nil {
		writeFeishuMentionError(ctx, e)
		return
	}
	status := http.StatusOK
	if id == 0 {
		status = http.StatusCreated
	}
	responses.New(ctx).ToResponseWithStatus(status, out)
}
func (c *FeishuMentionController) Recipient(ctx *gin.Context) {
	if !c.check(ctx) {
		return
	}
	id, ok := mentionID(ctx, "id")
	if !ok {
		return
	}
	out, e := c.service.Recipient(ctx.Request.Context(), id)
	if e != nil {
		writeFeishuMentionError(ctx, e)
		return
	}
	responses.New(ctx).ToResponse(out)
}
func (c *FeishuMentionController) Report(ctx *gin.Context) {
	if !c.check(ctx) {
		return
	}
	id, ok := mentionID(ctx, "id")
	if !ok {
		return
	}
	out, e := c.service.Report(ctx.Request.Context(), id)
	if e != nil {
		writeFeishuMentionError(ctx, e)
		return
	}
	responses.New(ctx).ToResponse(out)
}
func (c *FeishuMentionController) ReportMessages(ctx *gin.Context) {
	if !c.check(ctx) {
		return
	}
	id, ok := mentionID(ctx, "id")
	if !ok {
		return
	}
	f, ok := mentionFilter(ctx, "messages")
	if !ok {
		return
	}
	out, p, e := c.service.ReportMessages(ctx.Request.Context(), id, f)
	if e != nil {
		writeFeishuMentionError(ctx, e)
		return
	}
	responses.New(ctx).ToResponseWithPagination(out, p)
}
func (c *FeishuMentionController) ReportPart(ctx *gin.Context) {
	if !c.check(ctx) {
		return
	}
	id, ok := mentionID(ctx, "id")
	if !ok {
		return
	}
	partNo, ok := mentionID(ctx, "partNo")
	if !ok {
		return
	}
	out, e := c.service.ReportPart(ctx.Request.Context(), id, partNo)
	if e != nil {
		writeFeishuMentionError(ctx, e)
		return
	}
	responses.New(ctx).ToResponse(out)
}
func (c *FeishuMentionController) Retry(ctx *gin.Context) {
	if !c.check(ctx) {
		return
	}
	id, ok := mentionID(ctx, "id")
	if !ok {
		return
	}
	var body map[string]interface{}
	if e := bindOfficeJSON(ctx, &body); e != nil || body == nil || len(body) != 0 {
		writeFeishuMentionError(ctx, data_svc.ErrFeishuMentionAdminInvalid)
		return
	}
	out, e := c.service.Retry(ctx.Request.Context(), auth.CurrentUserID(ctx), id)
	if e != nil {
		writeFeishuMentionError(ctx, e)
		return
	}
	responses.New(ctx).ToResponseWithStatus(http.StatusAccepted, out)
}
func (c *FeishuMentionController) Status(ctx *gin.Context) {
	if !c.check(ctx) {
		return
	}
	out, e := c.service.Status(ctx.Request.Context())
	if e != nil {
		writeFeishuMentionError(ctx, e)
		return
	}
	responses.New(ctx).ToResponse(out)
}
