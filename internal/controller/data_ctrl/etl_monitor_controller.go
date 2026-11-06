package data_ctrl

import (
	"gin-biz-web-api/internal/etlmonitor"
	"gin-biz-web-api/pkg/responses"

	"github.com/gin-gonic/gin"
)

type ETLMonitorController struct {
	snapshot func() etlmonitor.Snapshot
}

func NewETLMonitorController(snapshot func() etlmonitor.Snapshot) *ETLMonitorController {
	return &ETLMonitorController{snapshot: snapshot}
}

func (controller *ETLMonitorController) Snapshot(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	responses.New(c).ToResponse(controller.snapshot())
}
