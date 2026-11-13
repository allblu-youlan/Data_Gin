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
	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/internal/requestbody"
	"gin-biz-web-api/internal/service/data_svc"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/database"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type youzanRouteService struct{ state *bojunRouteTestState }

func (service youzanRouteService) Query(_ context.Context, actor uint, dataset reportoracle.YouzanDataset, _ requestbody.OpenYouzanQueryRequest) (*data_svc.OpenYouzanQueryDTO, error) {
	if actor != 17 {
		service.state.t.Fatalf("youzan service actor = %d", actor)
	}
	service.state.events = append(service.state.events, "query:"+string(dataset))
	return &data_svc.OpenYouzanQueryDTO{Items: []reportoracle.YouzanRow{}}, nil
}

func TestOpenYouzanRoutesRequireTheirOwnPermission(t *testing.T) {
	for _, route := range []struct {
		path, permission, sibling string
		dataset                   reportoracle.YouzanDataset
	}{
		{"orders", model.PermissionYouzanOrdersRead, model.PermissionYouzanOrderItemsRead, reportoracle.YouzanOrders},
		{"orders/items", model.PermissionYouzanOrderItemsRead, model.PermissionYouzanOrdersRead, reportoracle.YouzanOrderItems},
	} {
		t.Run(route.path, func(t *testing.T) {
			for _, test := range []struct {
				name, token string
				grants      []string
				status      int
				wantEvents  []string
			}{
				{"missing token", "", nil, http.StatusUnauthorized, []string{"ip"}},
				{"unrelated permission", bojunRouteTestToken, []string{model.PermissionBojunOrderRead}, http.StatusForbidden, []string{"ip", "auth", "permission:" + route.permission}},
				{"sibling permission", bojunRouteTestToken, []string{route.sibling}, http.StatusForbidden, []string{"ip", "auth", "permission:" + route.permission}},
				{"parent permission", bojunRouteTestToken, []string{"youzan.read"}, http.StatusOK, []string{"ip", "auth", "permission:" + route.permission, "user", "query:" + string(route.dataset)}},
				{"matching permission", bojunRouteTestToken, []string{route.permission}, http.StatusOK, []string{"ip", "auth", "permission:" + route.permission, "user", "query:" + string(route.dataset)}},
			} {
				t.Run(test.name, func(t *testing.T) {
					router, state := newYouzanRouteTestRouter(t)
					for _, code := range test.grants {
						state.grants[code] = nil
					}
					request := httptest.NewRequest(http.MethodPost, "/api/open/youzan/"+route.path+"/query", strings.NewReader(`{}`))
					request.Header.Set("Content-Type", "application/json")
					if test.token != "" {
						request.Header.Set("token", test.token)
					}
					recorder := httptest.NewRecorder()
					router.ServeHTTP(recorder, request)
					if recorder.Code != test.status || !reflect.DeepEqual(state.events, test.wantEvents) {
						t.Fatalf("status=%d events=%v want=%v body=%s", recorder.Code, state.events, test.wantEvents, recorder.Body.String())
					}
				})
			}
		})
	}
}

func newYouzanRouteTestRouter(t *testing.T) (*gin.Engine, *bojunRouteTestState) {
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
	registerOpenYouzanRoutesWithLimiters(router.Group("/api"),
		data_ctrl.NewOpenYouzanControllerWithService(youzanRouteService{state: state}),
		func(c *gin.Context) {
			state.events = append(state.events, "ip")
			if _, authenticated := c.Get(constant.CurrentUserID); authenticated {
				t.Fatal("IP limiter ran after authentication")
			}
			c.Next()
		},
		func(module, limit string) gin.HandlerFunc {
			if module != "youzan" || limit != "30-M" {
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
