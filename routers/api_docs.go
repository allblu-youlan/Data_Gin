package routers

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"gin-biz-web-api/internal/apidoc"

	"github.com/gin-gonic/gin"
)

func registerAPIDocsRoutes(root *gin.Engine, api *gin.RouterGroup) {
	// Resolve on the first request, after all business routes are registered.
	var once sync.Once
	var payload []byte
	var etag string
	var buildErr error
	api.GET("/v1/api-docs", func(c *gin.Context) {
		once.Do(func() {
			routes := make(map[string]bool)
			for _, route := range root.Routes() {
				routes[route.Method+" "+route.Path] = true
			}
			var document apidoc.Document
			document, buildErr = apidoc.Build(routes)
			if buildErr == nil {
				payload, buildErr = json.Marshal(gin.H{"code": 0, "msg": "请求成功", "data": gin.H{"document": document}})
				etag = fmt.Sprintf(`"%x"`, sha256.Sum256(payload))
			}
		})
		if buildErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 100500, "msg": "接口文档暂时不可用", "data": nil})
			return
		}
		c.Header("ETag", etag)
		c.Header("Cache-Control", "public, max-age=60")
		if c.GetHeader("If-None-Match") == etag {
			c.Status(http.StatusNotModified)
			return
		}
		c.Data(http.StatusOK, "application/json; charset=utf-8", payload)
	})
}
