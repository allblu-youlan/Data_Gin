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

type tmallRouteService struct{ state *bojunRouteTestState }

func (service tmallRouteService) Query(_ context.Context, actor uint, dataset reportoracle.TmallDataset, _ requestbody.OpenTmallQueryRequest) (*data_svc.OpenTmallQueryDTO, error) {
	if actor != 17 {
		service.state.t.Fatalf("tmall service actor = %d", actor)
	}
	service.state.events = append(service.state.events, "query:"+string(dataset))
	return &data_svc.OpenTmallQueryDTO{Items: []reportoracle.TmallRow{}}, nil
}

func TestOpenTmallRoutesRequireTheirOwnPermission(t *testing.T) {
	for _, route := range []struct {
		path, permission string
		dataset          reportoracle.TmallDataset
	}{
		{"sycm", model.PermissionTmallSycmRead, reportoracle.TmallSycm},
		{"qztg", model.PermissionTmallQztgRead, reportoracle.TmallQztg},
		{"tot", model.PermissionTmallTotRead, reportoracle.TmallTot},
		{"sycm/hourly", model.PermissionTmallSycmRead, reportoracle.TmallDataset("sycm_hourly")},
		{"qztg/hourly", model.PermissionTmallQztgRead, reportoracle.TmallDataset("qztg_hourly")},
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
				{"matching permission", bojunRouteTestToken, []string{route.permission}, http.StatusOK, []string{"ip", "auth", "permission:" + route.permission, "user", "query:" + string(route.dataset)}},
			} {
				t.Run(test.name, func(t *testing.T) {
					router, state := newTmallRouteTestRouter(t)
					for _, code := range test.grants {
						state.grants[code] = nil
					}
					request := httptest.NewRequest(http.MethodPost, "/api/open/tmall/"+route.path+"/query", strings.NewReader(`{}`))
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

func newTmallRouteTestRouter(t *testing.T) (*gin.Engine, *bojunRouteTestState) {
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
	registerOpenTmallRoutesWithLimiters(router.Group("/api"),
		data_ctrl.NewOpenTmallControllerWithService(tmallRouteService{state: state}),
		func(c *gin.Context) {
			state.events = append(state.events, "ip")
			if _, authenticated := c.Get(constant.CurrentUserID); authenticated {
				t.Fatal("IP limiter ran after authentication")
			}
			c.Next()
		},
		func(module, limit string) gin.HandlerFunc {
			if module != "tmall" || limit != "30-M" {
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
