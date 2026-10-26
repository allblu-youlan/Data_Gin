package routers

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

const bojunRouteTestToken = "dg_open_bojun_route_test"

type bojunRouteTestState struct {
	t                 *testing.T
	grants            map[string]*time.Time
	events            []string
	userRateLimit     string
	credentialRevoked bool
	authErr           error
	permissionErr     error
}

func (state *bojunRouteTestState) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	state.t.Helper()
	switch {
	case strings.Contains(query, "users AS open_user"):
		state.events = append(state.events, "auth")
		sum := sha256.Sum256([]byte(bojunRouteTestToken))
		if !strings.Contains(query, "credential.token_hash = ? AND credential.status = ? AND open_user.account_type = ? AND open_user.status = ?") ||
			len(args) != 4 || args[0].Value != hex.EncodeToString(sum[:]) ||
			args[1].Value != model.OpenAPICredentialStatusActive || args[2].Value != model.AccountTypeOpenAPI || args[3].Value != model.AccountStatusActive {
			state.t.Fatalf("unexpected credential query: %s %#v", query, args)
		}
		if state.authErr != nil {
			return nil, state.authErr
		}
		rows := &bojunRouteTestRows{columns: []string{"id", "account_type", "status"}}
		if !state.credentialRevoked {
			rows.values = []driver.Value{int64(17), model.AccountTypeOpenAPI, model.AccountStatusActive}
		}
		return rows, nil
	case strings.Contains(query, "mall_weather_user_permissions"):
		if !strings.Contains(query, "user_id = ? AND permission = ? AND (expires_at IS NULL OR expires_at > ?)") ||
			len(args) != 3 || fmt.Sprint(args[0].Value) != "17" {
			state.t.Fatalf("unexpected permission query: %s %#v", query, args)
		}
		permission, ok := args[1].Value.(string)
		if !ok {
			state.t.Fatalf("permission is not a string: %#v", args[1])
		}
		now, ok := args[2].Value.(time.Time)
		if !ok || now.IsZero() {
			state.t.Fatalf("permission expiry check is missing: %#v", args[2])
		}
		state.events = append(state.events, "permission:"+permission)
		if state.permissionErr != nil {
			return nil, state.permissionErr
		}
		rows := &bojunRouteTestRows{columns: []string{"1"}}
		if expiresAt, granted := state.grants[permission]; granted && (expiresAt == nil || expiresAt.After(now)) {
			rows.values = []driver.Value{int64(1)}
		}
		return rows, nil
	default:
		state.t.Fatalf("unexpected database query: %s", query)
		return nil, errors.New("unexpected query")
	}
}

type bojunRouteTestConnector struct{ state *bojunRouteTestState }

func (connector bojunRouteTestConnector) Connect(context.Context) (driver.Conn, error) {
	return &bojunRouteTestConnection{state: connector.state}, nil
}
func (connector bojunRouteTestConnector) Driver() driver.Driver {
	return bojunRouteTestDriver{state: connector.state}
}

type bojunRouteTestDriver struct{ state *bojunRouteTestState }

func (testDriver bojunRouteTestDriver) Open(string) (driver.Conn, error) {
	return &bojunRouteTestConnection{state: testDriver.state}, nil
}

type bojunRouteTestConnection struct{ state *bojunRouteTestState }

func (*bojunRouteTestConnection) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (*bojunRouteTestConnection) Close() error                        { return nil }
func (*bojunRouteTestConnection) Begin() (driver.Tx, error)           { return nil, driver.ErrSkip }
func (connection *bojunRouteTestConnection) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	return connection.state.QueryContext(ctx, query, args)
}

type bojunRouteTestRows struct {
	columns []string
	values  []driver.Value
	read    bool
}

func (rows *bojunRouteTestRows) Columns() []string { return rows.columns }
func (*bojunRouteTestRows) Close() error           { return nil }
func (rows *bojunRouteTestRows) Next(values []driver.Value) error {
	if rows.read || len(rows.values) == 0 {
		return io.EOF
	}
	rows.read = true
	copy(values, rows.values)
	return nil
}

type bojunRouteStoreService struct{ state *bojunRouteTestState }

func (service bojunRouteStoreService) Query(_ context.Context, actor uint, _ requestbody.OpenBojunStoreQueryRequest) (*data_svc.OpenBojunStoreQueryDTO, error) {
	if actor != 17 {
		service.state.t.Fatalf("store service actor = %d", actor)
	}
	service.state.events = append(service.state.events, "stores")
	return &data_svc.OpenBojunStoreQueryDTO{Items: []data_svc.OpenBojunStoreDTO{}}, nil
}

type bojunRouteStockService struct{ state *bojunRouteTestState }

type bojunRouteProductInfoService struct{ state *bojunRouteTestState }

func (service bojunRouteProductInfoService) Query(_ context.Context, actor uint, _ requestbody.OpenBojunProductInfoQueryRequest) (*data_svc.OpenBojunProductInfoQueryDTO, error) {
	if actor != 17 {
		service.state.t.Fatalf("product info service actor = %d", actor)
	}
	service.state.events = append(service.state.events, "product-infos")
	return &data_svc.OpenBojunProductInfoQueryDTO{Items: []data_svc.OpenBojunProductInfoDTO{}}, nil
}

func (service bojunRouteStockService) Query(_ context.Context, actor uint, _ requestbody.OpenBojunStockQueryRequest) (*data_svc.OpenBojunStockQueryDTO, error) {
	if actor != 17 {
		service.state.t.Fatalf("stock service actor = %d", actor)
	}
	service.state.events = append(service.state.events, "stocks")
	return &data_svc.OpenBojunStockQueryDTO{Items: []data_svc.OpenBojunStockDTO{}}, nil
}

func newBojunRouteTestRouter(t *testing.T) (*gin.Engine, *bojunRouteTestState) {
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
	// These tests intentionally run serially because the production auth middleware
	// resolves its database through the existing application-level connection.
	previousDB := database.DB
	database.DB = db
	t.Cleanup(func() {
		database.DB = previousDB
		_ = sqlDB.Close()
	})
	router := gin.New()
	registerOpenBojunRoutesWithLimiters(
		router.Group("/api"),
		&data_ctrl.OpenBojunOrderController{}, &data_ctrl.OpenBojunOrderDetailController{},
		&data_ctrl.OpenBojunProductController{}, &data_ctrl.OpenBojunSyncStatusController{},
		data_ctrl.NewOpenBojunStoreControllerWithService(bojunRouteStoreService{state: state}),
		data_ctrl.NewOpenBojunStockControllerWithService(bojunRouteStockService{state: state}),
		data_ctrl.NewOpenBojunProductInfoControllerWithService(bojunRouteProductInfoService{state: state}),
		func(c *gin.Context) {
			state.events = append(state.events, "ip")
			if _, authenticated := c.Get(constant.CurrentUserID); authenticated {
				t.Fatal("IP limiter ran after authentication")
			}
			c.Next()
		},
		func(module, limit string) gin.HandlerFunc {
			if module != "bojun" {
				t.Fatalf("user limiter module = %q", module)
			}
			return func(c *gin.Context) {
				state.events = append(state.events, "user")
				state.userRateLimit = limit
				if c.GetString(constant.CurrentUserID) != "17" {
					t.Fatal("user limiter ran before authentication")
				}
				c.Next()
			}
		},
	)
	return router, state
}

func TestOpenBojunStockRouteUses60PerMinuteWithoutChangingStoreQuota(t *testing.T) {
	router, state := newBojunRouteTestRouter(t)
	state.grants[model.PermissionBojunStockRead] = nil
	state.grants[model.PermissionBojunStoreRead] = nil
	for _, route := range []struct{ path, limit string }{
		{"stocks", "60-M"},
		{"stores", "30-M"},
	} {
		t.Run(route.path, func(t *testing.T) {
			requestBojunRoute(t, router, route.path, bojunRouteTestToken, "{}", http.StatusOK)
			if state.userRateLimit != route.limit {
				t.Fatalf("%s limit=%s want=%s", route.path, state.userRateLimit, route.limit)
			}
		})
	}
}

func requestBojunRoute(t *testing.T, router *gin.Engine, path, token, body string, wantStatus int) {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/open/bojun/"+path+"/query", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("token", token)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != wantStatus {
		t.Fatalf("%s: status=%d want=%d body=%s", path, recorder.Code, wantStatus, recorder.Body.String())
	}
	var response struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	wantCode := 0
	if wantStatus != http.StatusOK {
		wantCode = 100000 + wantStatus
	}
	if response.Code != wantCode {
		t.Fatalf("%s: response code=%d want=%d", path, response.Code, wantCode)
	}
	if strings.Contains(recorder.Body.String(), "private database detail") || strings.Contains(recorder.Body.String(), bojunRouteTestToken) {
		t.Fatalf("sensitive response: %s", recorder.Body.String())
	}
}

func TestOpenBojunRoutesRegisterSevenPOSTEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerOpenBojunRoutes(router.Group("/api"),
		&data_ctrl.OpenBojunOrderController{}, &data_ctrl.OpenBojunOrderDetailController{},
		&data_ctrl.OpenBojunProductController{}, &data_ctrl.OpenBojunSyncStatusController{},
		&data_ctrl.OpenBojunStoreController{}, &data_ctrl.OpenBojunStockController{},
		&data_ctrl.OpenBojunProductInfoController{},
	)
	want := map[string]bool{"orders": true, "orders/details": true, "products": true, "product-infos": true, "sync-status": true, "stores": true, "stocks": true}
	for _, route := range router.Routes() {
		path := strings.TrimSuffix(strings.TrimPrefix(route.Path, "/api/open/bojun/"), "/query")
		if route.Method != http.MethodPost || !want[path] {
			t.Fatalf("unexpected route: %#v", route)
		}
		delete(want, path)
	}
	if len(want) != 0 {
		t.Fatalf("missing routes: %v", want)
	}
}

func TestOpenBojunRoutesRejectMissingTokenBeforePermissionLookup(t *testing.T) {
	for _, path := range []string{"stores", "stocks", "product-infos"} {
		t.Run(path, func(t *testing.T) {
			router, state := newBojunRouteTestRouter(t)
			requestBojunRoute(t, router, path, "", "{}", http.StatusUnauthorized)
			if !reflect.DeepEqual(state.events, []string{"ip"}) {
				t.Fatalf("authentication order: %v", state.events)
			}
		})
	}
}

func TestOpenBojunRoutesKeepOldAndNewPermissionsIndependent(t *testing.T) {
	for _, granted := range []string{model.PermissionBojunOrderRead, model.PermissionBojunStoreRead, model.PermissionBojunStockRead} {
		t.Run(granted, func(t *testing.T) {
			router, state := newBojunRouteTestRouter(t)
			state.grants[granted] = nil
			for _, route := range []struct{ path, permission string }{
				{"orders", model.PermissionBojunOrderRead}, {"orders/details", model.PermissionBojunOrderRead},
				{"products", model.PermissionBojunOrderRead}, {"sync-status", model.PermissionBojunOrderRead},
				{"product-infos", model.PermissionBojunOrderRead},
				{"stores", model.PermissionBojunStoreRead}, {"stocks", model.PermissionBojunStockRead},
			} {
				state.events = nil
				wantStatus, body := http.StatusForbidden, "{}"
				wantEvents := []string{"ip", "auth", "permission:" + route.permission}
				if granted == route.permission {
					wantEvents = append(wantEvents, "user")
					if granted == model.PermissionBojunOrderRead {
						// Malformed JSON proves the legacy controller was reached without
						// requiring order services or Oracle in this routing test.
						wantStatus, body = http.StatusUnprocessableEntity, "{"
					} else {
						wantStatus = http.StatusOK
						wantEvents = append(wantEvents, route.path)
					}
				}
				requestBojunRoute(t, router, route.path, bojunRouteTestToken, body, wantStatus)
				if !reflect.DeepEqual(state.events, wantEvents) {
					t.Fatalf("%s events=%v want=%v", route.path, state.events, wantEvents)
				}
			}
		})
	}
}

func TestOpenBojunRoutesObserveRevokedAndExpiredGrants(t *testing.T) {
	for _, route := range []struct{ path, permission string }{
		{"stores", model.PermissionBojunStoreRead}, {"stocks", model.PermissionBojunStockRead},
		{"product-infos", model.PermissionBojunOrderRead},
	} {
		t.Run(route.path, func(t *testing.T) {
			router, state := newBojunRouteTestRouter(t)
			state.grants[route.permission] = nil
			requestBojunRoute(t, router, route.path, bojunRouteTestToken, "{}", http.StatusOK)
			delete(state.grants, route.permission)
			requestBojunRoute(t, router, route.path, bojunRouteTestToken, "{}", http.StatusForbidden)
			future := time.Now().Add(time.Hour)
			state.grants[route.permission] = &future
			requestBojunRoute(t, router, route.path, bojunRouteTestToken, "{}", http.StatusOK)
			past := time.Now().Add(-time.Hour)
			state.grants[route.permission] = &past
			state.events = nil
			requestBojunRoute(t, router, route.path, bojunRouteTestToken, "{}", http.StatusForbidden)
			if !reflect.DeepEqual(state.events, []string{"ip", "auth", "permission:" + route.permission}) {
				t.Fatalf("expired permission reached service or limiter: %v", state.events)
			}
		})
	}
}

func TestOpenBojunRoutesFailSafelyWhenCredentialsOrDependenciesFail(t *testing.T) {
	for _, path := range []string{"stores", "stocks"} {
		t.Run(path, func(t *testing.T) {
			router, state := newBojunRouteTestRouter(t)
			state.credentialRevoked = true
			requestBojunRoute(t, router, path, bojunRouteTestToken, "{}", http.StatusUnauthorized)
			if !reflect.DeepEqual(state.events, []string{"ip", "auth"}) {
				t.Fatalf("revoked credential reached permission check: %v", state.events)
			}
			state.credentialRevoked = false
			state.authErr = errors.New("private database detail")
			requestBojunRoute(t, router, path, bojunRouteTestToken, "{}", http.StatusServiceUnavailable)
			state.authErr = nil
			state.permissionErr = errors.New("private database detail")
			requestBojunRoute(t, router, path, bojunRouteTestToken, "{}", http.StatusServiceUnavailable)
		})
	}
}
