package data_ctrl

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"gin-biz-web-api/internal/service/data_svc"
	"gin-biz-web-api/internal/tableboard"
	"gin-biz-web-api/pkg/auth"
	"gin-biz-web-api/pkg/errcode"
	"gin-biz-web-api/pkg/responses"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func EvaluateTableBoard(c *gin.Context) {
	id, ok := boardID(c)
	if !ok {
		return
	}
	var input struct {
		Revision uint64 `json:"revision"`
	}
	if err := decodeLimitedJSON(c, &input, 4096); err != nil || input.Revision == 0 {
		responses.New(c).ToSafeErrorResponse(errcode.BadRequest, "请提供当前布局版本")
		return
	}
	board, err := boardStore(c).Get(c.Request.Context(), id)
	if err != nil {
		boardResponse(c, http.StatusOK, nil, err)
		return
	}
	if board.Revision != input.Revision {
		boardResponse(c, http.StatusOK, nil, tableboard.ErrConflict)
		return
	}
	var layout tableboard.Layout
	if err = json.Unmarshal([]byte(board.Layout), &layout); err != nil {
		boardResponse(c, http.StatusOK, nil, err)
		return
	}
	ctx := c.Request.Context()
	stamp, err := data_svc.CurrentBoardAccessStamp(ctx, layout)
	if err != nil {
		boardResponse(c, http.StatusOK, nil, err)
		return
	}
	location, _ := time.LoadLocation("Asia/Shanghai")
	asOf := time.Now().In(location)
	results := data_svc.EvaluateBoard(ctx, auth.CurrentUserID(c), layout, asOf)
	export, err := data_svc.BuildBoardSnapshot(layout, results)
	if err != nil {
		boardResponse(c, http.StatusOK, nil, err)
		return
	}
	// A policy or layout change while Oracle is running invalidates this result.
	current, err := boardStore(c).Get(ctx, id)
	if err != nil {
		boardResponse(c, http.StatusOK, nil, err)
		return
	}
	if current.Revision != input.Revision {
		boardResponse(c, http.StatusOK, nil, tableboard.ErrConflict)
		return
	}
	after, err := data_svc.CurrentBoardAccessStamp(ctx, layout)
	if err != nil {
		boardResponse(c, http.StatusOK, nil, err)
		return
	}
	if after != stamp {
		responses.New(c).ToSafeErrorResponse(errcode.Forbidden, "数据权限已变化，请刷新看板")
		return
	}
	calculation := data_svc.BoardCalculation{Revision: board.Revision, CalculationID: uuid.NewString(), AsOf: asOf, Cells: results, Export: export}
	if err = data_svc.StoreBoardCalculation(ctx, auth.CurrentUserID(c), id, stamp, calculation); err != nil {
		boardResponse(c, http.StatusOK, nil, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	responses.New(c).ToResponse(calculation)
}

func ExportBoardCalculation(c *gin.Context) {
	id, ok := boardID(c)
	if !ok {
		return
	}
	var input struct {
		Revision      uint64 `json:"revision"`
		CalculationID string `json:"calculationId"`
		Format        string `json:"format"`
	}
	if err := decodeLimitedJSON(c, &input, 4096); err != nil || input.Revision == 0 || (input.Format != "xlsx" && input.Format != "csv") {
		responses.New(c).ToSafeErrorResponse(errcode.BadRequest, "请选择当前计算快照和文件格式")
		return
	}
	ctx := c.Request.Context()
	board, err := boardStore(c).Get(ctx, id)
	if err != nil {
		boardResponse(c, http.StatusOK, nil, err)
		return
	}
	if board.Revision != input.Revision {
		boardResponse(c, http.StatusOK, nil, tableboard.ErrConflict)
		return
	}
	snapshot, stamp, err := data_svc.LoadBoardCalculation(ctx, auth.CurrentUserID(c), id, input.CalculationID)
	c.Header("Cache-Control", "no-store")
	if errors.Is(err, data_svc.ErrBoardSnapshotExpired) {
		c.JSON(http.StatusGone, responses.ResponseData{Code: 100410, Msg: err.Error()})
		return
	}
	if err != nil {
		boardResponse(c, http.StatusOK, nil, err)
		return
	}
	if snapshot.Revision != board.Revision {
		boardResponse(c, http.StatusOK, nil, tableboard.ErrConflict)
		return
	}
	var layout tableboard.Layout
	if err = json.Unmarshal([]byte(board.Layout), &layout); err != nil {
		boardResponse(c, http.StatusOK, nil, err)
		return
	}
	current, err := data_svc.CurrentBoardAccessStamp(ctx, layout)
	if err != nil {
		boardResponse(c, http.StatusOK, nil, err)
		return
	}
	if stamp != current {
		responses.New(c).ToSafeErrorResponse(errcode.Forbidden, "数据权限已变化，请重新计算")
		return
	}
	var content []byte
	if input.Format == "xlsx" {
		content, err = data_svc.RenderBoardXLSX(snapshot.Export)
	} else {
		content, err = data_svc.RenderBoardCSV(snapshot.Export)
	}
	if err != nil {
		boardResponse(c, http.StatusOK, nil, err)
		return
	}
	responses.New(c).ToResponse(gin.H{"content": content})
}
