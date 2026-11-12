package routers

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"gin-biz-web-api/constant"
	"gin-biz-web-api/internal/controller/data_ctrl"
	"gin-biz-web-api/internal/requestbody"
	"gin-biz-web-api/internal/service/data_svc"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/database"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type monitoringRouteService struct{ state *bojunRouteTestState }

func (service monitoringRouteService) record(actor uint, operation string) {
	if actor != 17 {
		service.state.t.Fatalf("monitoring service actor = %d", actor)
	}
	service.state.events = append(service.state.events, "query:"+operation)
}

func (service monitoringRouteService) QueryHourly(_ context.Context, actor uint, _ requestbody.OpenMonitoringHourlyQueryRequest) (*data_svc.OpenMonitoringHourlyResult, error) {
	service.record(actor, "hourly")
	return &data_svc.OpenMonitoringHourlyResult{}, nil
}

func (service monitoringRouteService) QueryDaily(_ context.Context, actor uint, _ requestbody.OpenMonitoringHourlyQueryRequest) (*data_svc.OpenMonitoringDailyResult, error) {
	service.record(actor, "daily")
	return &data_svc.OpenMonitoringDailyResult{}, nil
}

func (service monitoringRouteService) QueryAreas(_ context.Context, actor uint, _ requestbody.OpenMonitoringAreaQueryRequest) (*data_svc.OpenMonitoringAreaResult, error) {
	service.record(actor, "areas")
	return &data_svc.OpenMonitoringAreaResult{}, nil
}

func TestOpenMonitoringRoutesEnforcePermissionAndLimiterOrder(t *testing.T) {
	for _, route := range []struct{ path, permission, otherPermission string }{
		{"hourly", model.PermissionMonitoringHourlyRead, model.PermissionMonitoringDailyRead},
		{"daily", model.PermissionMonitoringDailyRead, model.PermissionMonitoringAreaRead},
		{"areas", model.PermissionMonitoringAreaRead, model.PermissionMonitoringHourlyRead},
	} {
		t.Run(route.path, func(t *testing.T) {
			for _, test := range []struct {
				name, token, grant string
				status             int
				wantEvents         []string
			}{
				{"missing token", "", "", http.StatusUnauthorized, []string{"ip"}},
				{"other child", bojunRouteTestToken, route.otherPermission, http.StatusForbidden, []string{"ip", "auth", "permission:" + route.permission}},
				{"matching child", bojunRouteTestToken, route.permission, http.StatusOK, []string{"ip", "auth", "permission:" + route.permission, "user", "query:" + route.path}},
				{"parent grant", bojunRouteTestToken, model.PermissionMonitoringAPIRead, http.StatusOK, []string{"ip", "auth", "permission:" + route.permission, "user", "query:" + route.path}},
			} {
				t.Run(test.name, func(t *testing.T) {
					router, state := newMonitoringRouteTestRouter(t)
					if test.grant != "" {
						state.grants[test.grant] = nil
					}
					request := httptest.NewRequest(http.MethodPost, "/api/open/monitoring/"+route.path+"/query", strings.NewReader(`{}`))
					request.Header.Set("Content-Type", "application/json")
					if test.token != "" {
						request.Header.Set("token", test.token)
					}
					response := httptest.NewRecorder()
					router.ServeHTTP(response, request)
					if response.Code != test.status || !reflect.DeepEqual(state.events, test.wantEvents) {
						t.Fatalf("status=%d events=%v want=%v body=%s", response.Code, state.events, test.wantEvents, response.Body)
					}
				})
			}
		})
	}
}

func newMonitoringRouteTestRouter(t *testing.T) (*gin.Engine, *bojunRouteTestState) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	state := &bojunRouteTestState{t: t, grants: make(map[string]*time.Time)}
	sqlDB := sql.OpenDB(bojunRouteTestConnector{state: state})
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{
		DisableAutomaticPing: true,
		Logger:               logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	previousDB := database.DB
	database.DB = db
	t.Cleanup(func() {
		database.DB = previousDB
		_ = sqlDB.Close()
	})
	router := gin.New()
	registerOpenMonitoringRoutesWithLimiters(router.Group("/api"),
		data_ctrl.NewOpenMonitoringControllerWithService(monitoringRouteService{state: state}),
		func(c *gin.Context) {
			state.events = append(state.events, "ip")
			if _, authenticated := c.Get(constant.CurrentUserID); authenticated {
				t.Fatal("IP limiter ran after authentication")
			}
			c.Next()
		},
		func(module, limit string) gin.HandlerFunc {
			if module != "monitoring" || limit != "30-M" {
				t.Fatalf("unexpected user limiter: %s %s", module, limit)
			}
			return func(c *gin.Context) {
				state.events = append(state.events, "user")
				if c.GetString(constant.CurrentUserID) != "17" {
					t.Fatal("user limiter ran before authentication")
				}
				c.Next()
			}
		})
	return router, state
}
