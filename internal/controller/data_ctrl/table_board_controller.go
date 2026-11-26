package data_ctrl

import (
	"errors"
	"net/http"
	"strconv"

	"gin-biz-web-api/internal/tableboard"
	"gin-biz-web-api/pkg/auth"
	"gin-biz-web-api/pkg/database"
	"gin-biz-web-api/pkg/errcode"
	"gin-biz-web-api/pkg/responses"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func boardStore(c *gin.Context) tableboard.Store {
	return tableboard.NewStore(database.DB, auth.CurrentUserID(c))
}

func ListTableBoards(c *gin.Context) {
	var after uint
	if cursor := c.Query("afterId"); cursor != "" {
		id, err := strconv.ParseUint(cursor, 10, strconv.IntSize)
		if err != nil {
			responses.New(c).ToSafeErrorResponse(errcode.BadRequest, "看板列表游标无效")
			return
		}
		after = uint(id)
	}
	page, err := boardStore(c).List(c.Request.Context(), after)
	boardResponse(c, http.StatusOK, page, err)
}

func boardID(c *gin.Context) (uint, bool) {
	id, err := parseReportUint(c.Param("id"), "id")
	if err != nil {
		responses.New(c).ToSafeErrorResponse(errcode.BadRequest, "看板编号无效")
	}
	return id, err == nil
}

func GetTableBoard(c *gin.Context) {
	if id, ok := boardID(c); ok {
		board, err := boardStore(c).Get(c.Request.Context(), id)
		boardResponse(c, http.StatusOK, board, err)
	}
}

func CreateTableBoard(c *gin.Context) { saveTableBoard(c, 0, http.StatusCreated) }

func UpdateTableBoard(c *gin.Context) {
	if id, ok := boardID(c); ok {
		saveTableBoard(c, id, http.StatusOK)
	}
}

func saveTableBoard(c *gin.Context, id uint, status int) {
	var input tableboard.Input
	if err := decodeLimitedJSON(c, &input, 2<<20); err != nil {
		responses.New(c).ToSafeErrorResponse(errcode.BadRequest, "看板配置格式无效或大小超过 2 MiB")
		return
	}
	if err := input.Validate(id == 0); err != nil {
		responses.New(c).ToSafeErrorResponse(errcode.UnprocessableEntity, err.Error())
		return
	}
	board, err := boardStore(c).Save(c.Request.Context(), id, input)
	boardResponse(c, status, board, err)
}

func DeleteTableBoard(c *gin.Context) {
	id, ok := boardID(c)
	if !ok {
		return
	}
	revision, err := strconv.ParseUint(c.Query("revision"), 10, 64)
	if err != nil || revision == 0 {
		responses.New(c).ToSafeErrorResponse(errcode.BadRequest, "删除看板需要当前版本号")
		return
	}
	err = boardStore(c).Delete(c.Request.Context(), id, revision)
	boardResponse(c, http.StatusOK, gin.H{"id": id}, err)
}

func boardResponse(c *gin.Context, status int, data any, err error) {
	c.Header("Cache-Control", "no-store")
	database.RecordResult(err)
	if err == nil {
		responses.New(c).ToResponseWithStatus(status, data)
		return
	}
	_ = c.Error(err).SetType(gin.ErrorTypePrivate)
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		responses.New(c).ToSafeErrorResponse(errcode.NotFound, "看板不存在")
	case errors.Is(err, tableboard.ErrConflict):
		responses.New(c).ToSafeErrorResponse(errcode.Conflict, tableboard.ErrConflict.Error())
	default:
		responses.New(c).ToSafeErrorResponse(errcode.ServiceUnavailable, "暂时无法读写看板，请稍后重试")
	}
}
