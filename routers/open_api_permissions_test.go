package routers

import (
	"net/http"
	"testing"
	"time"

	"gin-biz-web-api/model"
)

func TestOpenBojunRoutesInheritModuleGrantAndKeepIndependentChild(t *testing.T) {
	router, state := newBojunRouteTestRouter(t)
	state.grants[model.PermissionBojunRead] = nil
	for _, path := range []string{"stores", "stocks", "product-infos"} {
		requestBojunRoute(t, router, path, bojunRouteTestToken, "{}", http.StatusOK)
	}

	// An expired or removed module grant cannot authorize the child routes.
	expired := time.Now().Add(-time.Hour)
	state.grants[model.PermissionBojunRead] = &expired
	requestBojunRoute(t, router, "stocks", bojunRouteTestToken, "{}", http.StatusForbidden)
	delete(state.grants, model.PermissionBojunRead)
	requestBojunRoute(t, router, "stocks", bojunRouteTestToken, "{}", http.StatusForbidden)

	// An independently granted child remains usable without its module grant.
	state.grants[model.PermissionBojunStockRead] = nil
	requestBojunRoute(t, router, "stocks", bojunRouteTestToken, "{}", http.StatusOK)
	requestBojunRoute(t, router, "stores", bojunRouteTestToken, "{}", http.StatusForbidden)
}
