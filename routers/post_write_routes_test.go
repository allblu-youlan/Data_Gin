package routers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gin-biz-web-api/constant"
	"gin-biz-web-api/global"
	"gin-biz-web-api/internal/controller/data_ctrl"
	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/internal/middleware"
	"gin-biz-web-api/internal/requestbody"
	"gin-biz-web-api/internal/service/data_svc"
	"gin-biz-web-api/model"
	pkgconfig "gin-biz-web-api/pkg/config"
	"gin-biz-web-api/pkg/database"

	"github.com/gin-gonic/gin"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func TestAllWriteRoutesHaveAuthenticatedPOSTAliases(t *testing.T) {
	gin.SetMode(gin.TestMode)
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "config.yaml"), []byte("JWT:\n  Key: post-route-test-key\n"), 0600); err != nil {
		t.Fatal(err)
	}
	pkgconfig.NewConfig("", directory+string(os.PathSeparator))
	previousDB := database.DB
	db, err := gorm.Open(gormmysql.New(gormmysql.Config{
		DSN: "route_test:route_test@tcp(127.0.0.1:1)/route_test", SkipInitializeWithVersion: true,
	}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	database.DB = db
	t.Cleanup(func() {
		database.DB = previousDB
		sqlDB, err := db.DB()
		if err == nil {
			sqlDB.Close()
		}
	})
	previous := global.ReportCenterEnabledAtStartup
	global.ReportCenterEnabledAtStartup = true
	t.Cleanup(func() { global.ReportCenterEnabledAtStartup = previous })
	router := gin.New()
	apiData(router.Group("/api"))
	registerAccessRoutes(router.Group("/api"))
	routes := make(map[string]gin.RouteInfo)
	for _, route := range router.Routes() {
		routes[route.Method+" "+route.Path] = route
	}
	writeCount := 0
	for _, route := range router.Routes() {
		suffix, exists := map[string]string{http.MethodPut: "/update", http.MethodPatch: "/patch", http.MethodDelete: "/delete"}[route.Method]
		if !exists {
			continue
		}
		writeCount++
		t.Run(route.Method+" "+route.Path, func(t *testing.T) {
			aliasPath := route.Path + suffix
			alias, exists := routes[http.MethodPost+" "+aliasPath]
			if !exists {
				t.Fatalf("missing POST alias %s", aliasPath)
			}
			if alias.Handler != route.Handler {
				t.Fatalf("POST alias handler %s differs from %s", alias.Handler, route.Handler)
			}
			for _, endpoint := range []struct{ method, path string }{{route.Method, route.Path}, {http.MethodPost, aliasPath}} {
				segments := strings.Split(endpoint.path, "/")
				for i := range segments {
					if strings.HasPrefix(segments[i], ":") {
						segments[i] = "9"
					}
				}
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, httptest.NewRequest(endpoint.method, strings.Join(segments, "/"), strings.NewReader(`{}`)))
				if recorder.Code != http.StatusUnauthorized {
					t.Fatalf("%s %s status = %d, want 401", endpoint.method, endpoint.path, recorder.Code)
				}
			}
		})
	}
	if writeCount != 45 {
		t.Fatalf("registered write route count = %d, want 45", writeCount)
	}
}

func TestPOSTWriteAliasPreservesPermissionAndPathParameters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, allowed := range []bool{false, true} {
		name := "permission denied"
		if allowed {
			name = "permission allowed"
		}
		t.Run(name, func(t *testing.T) {
			router := gin.New()
			group := router.Group("/api/v1/pipeline-stages")
			group.Use(func(c *gin.Context) { c.Set(constant.CurrentUserInfo, model.User{}) })
			calls := 0
			permission := middleware.RequirePermissionWithAuthorizer(model.PermissionPipelineManage, postRoutePermissionChecker(allowed))
			registerPOSTWriteRoute(group, http.MethodPut, "/:stage_id/steps/:step_id", permission, func(c *gin.Context) {
				calls++
				if c.Param("stage_id") != "3" || c.Param("step_id") != "7" {
					t.Errorf("path parameters changed: %v", c.Params)
				}
				c.Status(http.StatusNoContent)
			})
			for _, endpoint := range []struct{ method, path string }{
				{http.MethodPut, "/api/v1/pipeline-stages/3/steps/7"},
				{http.MethodPost, "/api/v1/pipeline-stages/3/steps/7/update"},
			} {
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, httptest.NewRequest(endpoint.method, endpoint.path, nil))
				want := http.StatusForbidden
				if allowed {
					want = http.StatusNoContent
				}
				if recorder.Code != want {
					t.Fatalf("status = %d, want %d", recorder.Code, want)
				}
			}
			wantCalls := 0
			if allowed {
				wantCalls = 2
			}
			if calls != wantCalls {
				t.Fatalf("handler calls = %d, want %d", calls, wantCalls)
			}
		})
	}
}

func TestPOSTMallPatchPreservesKaiyuIDAndVersionContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name, body, shopID string
		version            uint64
		serviceError       error
		wantStatus         int
	}{
		{"set binding", `{"expectedMallVersion":4,"kaiyuShopId":"abc123"}`, "abc123", 4, nil, 200},
		{"clear binding", `{"expectedMallVersion":4,"kaiyuShopId":""}`, "", 4, nil, 200},
		{"stale version", `{"expectedMallVersion":3,"kaiyuShopId":"abc123"}`, "abc123", 3, data_dao.ErrMallVersionConflict, 409},
		{"missing version", `{"kaiyuShopId":"abc123"}`, "abc123", 0, data_svc.ErrMallInvalidInput, 422},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			service := &postRouteMallService{update: func(_ context.Context, actor, id uint, request requestbody.MallPatchRequest) (*data_svc.MallDTO, error) {
				calls++
				if actor != 17 || id != 9 || request.KaiyuShopID == nil || *request.KaiyuShopID != tc.shopID || request.ExpectedMallVersion != tc.version {
					t.Fatalf("request changed: actor=%d id=%d request=%+v", actor, id, request)
				}
				return &data_svc.MallDTO{ID: id, KaiyuShopID: tc.shopID, Version: 5}, tc.serviceError
			}}
			router := gin.New()
			router.Use(func(c *gin.Context) { c.Set(constant.CurrentUserID, "17") })
			controller := data_ctrl.NewMallControllerWithService(service)
			registerPOSTWriteRoute(router.Group("/api/v1/malls"), http.MethodPatch, "/:id", controller.Update)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/api/v1/malls/9/patch", strings.NewReader(tc.body))
			request.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(recorder, request)
			if recorder.Code != tc.wantStatus || calls != 1 {
				t.Fatalf("status=%d calls=%d body=%s", recorder.Code, calls, recorder.Body.String())
			}
			if tc.wantStatus == http.StatusOK {
				var response struct{ Data data_svc.MallDTO }
				if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
					t.Fatal(err)
				}
				if response.Data.KaiyuShopID != tc.shopID || response.Data.Version != 5 {
					t.Fatalf("response data changed: %+v", response.Data)
				}
			}
		})
	}
}

type postRoutePermissionChecker bool

func (checker postRoutePermissionChecker) HasPermission(context.Context, model.User, string) (bool, error) {
	return bool(checker), nil
}

type postRouteMallService struct {
	data_ctrl.MallService
	update func(context.Context, uint, uint, requestbody.MallPatchRequest) (*data_svc.MallDTO, error)
}

func (service *postRouteMallService) Update(ctx context.Context, actor, id uint, request requestbody.MallPatchRequest) (*data_svc.MallDTO, error) {
	return service.update(ctx, actor, id, request)
}
