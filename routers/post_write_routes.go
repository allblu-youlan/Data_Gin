package routers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// registerPOSTWriteRoute exposes writes through POST for gateways that reject
// PUT, PATCH, and DELETE, while retaining the original route for existing clients.
// Both routes share the same group middleware and handler chain.
func registerPOSTWriteRoute(group *gin.RouterGroup, method, path string, handlers ...gin.HandlerFunc) {
	var suffix string
	switch method {
	case http.MethodPut:
		suffix = "/update"
	case http.MethodPatch:
		suffix = "/patch"
	case http.MethodDelete:
		suffix = "/delete"
	default:
		panic("unsupported POST-compatible write method: " + method)
	}
	group.Handle(method, path, handlers...)
	group.POST(path+suffix, handlers...)
}
