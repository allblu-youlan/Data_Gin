package middleware

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/errcode"
	"gin-biz-web-api/pkg/responses"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func TestAuthOpenTokenRejectsOtherCredentialSources(t *testing.T) {
	tests := []struct {
		name          string
		tokenHeader   string
		authorization string
		url           string
	}{
		{name: "missing"},
		{name: "authorization open token is ignored", authorization: "Bearer dg_open_value"},
		{name: "authorization internal jwt is ignored", authorization: "Bearer abc.def.ghi"},
		{name: "query token is ignored", url: "/api/open/test?token=dg_open_value"},
		{name: "non open token is rejected", tokenHeader: "abc.def.ghi"},
		{name: "empty open token is rejected", tokenHeader: "dg_open_"},
		{name: "surrounding whitespace is rejected", tokenHeader: " dg_open_value "},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			router := gin.New()
			calls := 0
			router.POST("/api/open/test", AuthOpenToken(), func(c *gin.Context) {
				calls++
				c.Status(http.StatusNoContent)
			})
			url := test.url
			if url == "" {
				url = "/api/open/test"
			}
			request := httptest.NewRequest(http.MethodPost, url, nil)
			if test.tokenHeader != "" {
				request.Header.Set("token", test.tokenHeader)
			}
			if test.authorization != "" {
				request.Header.Set("Authorization", test.authorization)
			}
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusUnauthorized || calls != 0 {
				t.Fatalf("status=%d calls=%d body=%s", recorder.Code, calls, recorder.Body.String())
			}
			var body responses.ResponseData
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if body.Code != errcode.Unauthorized.Code() {
				t.Fatalf("response=%+v", body)
			}
			if data, ok := body.Data.(map[string]any); !ok || len(data) != 0 {
				t.Fatalf("error data must be an empty object: %s", recorder.Body.String())
			}
		})
	}
}

func TestOpenAPIToken(t *testing.T) {
	token, ok := openAPIToken([]string{"dg_open_abc123"})
	if !ok || token != "dg_open_abc123" {
		t.Fatalf("token=%q ok=%t", token, ok)
	}
	for _, values := range [][]string{
		nil,
		{""},
		{"dg_open_"},
		{"internal-jwt"},
		{"dg_open_one", "dg_open_two"},
	} {
		if _, accepted := openAPIToken(values); accepted {
			t.Fatalf("accepted values %#v", values)
		}
	}
}

func TestOpenAPIUserLookupUsesSingleCredentialJoin(t *testing.T) {
	db, err := gorm.Open(mysql.New(mysql.Config{
		Conn:                      &sql.DB{},
		SkipInitializeWithVersion: true,
	}), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatalf("gorm.Open() error=%v", err)
	}
	var user model.User
	result := openAPIUserLookup(context.Background(), db, strings.Repeat("a", 64)).Take(&user)
	if result.Error != nil {
		t.Fatalf("openAPIUserLookup() error=%v", result.Error)
	}
	statement := result.Statement.SQL.String()
	for _, fragment := range []string{
		"FROM users AS open_user",
		"INNER JOIN open_api_credentials AS credential ON credential.user_id = open_user.id",
		"credential.token_hash = ? AND credential.status = ? AND open_user.account_type = ? AND open_user.status = ?",
	} {
		if !strings.Contains(statement, fragment) {
			t.Fatalf("statement does not contain %q: %s", fragment, statement)
		}
	}
	if len(result.Statement.Vars) != 4 {
		t.Fatalf("vars=%v", result.Statement.Vars)
	}
}

func TestValidConsoleSession(t *testing.T) {
	valid := &model.User{BaseModel: &model.BaseModel{ID: 7}, AccountType: model.AccountTypeConsole, Status: model.AccountStatusActive, AuthVersion: 3}
	if !validConsoleSession(valid, 3) {
		t.Fatal("active console user with matching version was rejected")
	}
	for _, user := range []*model.User{
		nil,
		{BaseModel: &model.BaseModel{ID: 7}, AccountType: model.AccountTypeOpenAPI, Status: model.AccountStatusActive, AuthVersion: 3},
		{BaseModel: &model.BaseModel{ID: 7}, AccountType: model.AccountTypeConsole, Status: model.AccountStatusDisabled, AuthVersion: 3},
		{BaseModel: &model.BaseModel{ID: 7}, AccountType: model.AccountTypeConsole, Status: model.AccountStatusActive, AuthVersion: 4},
	} {
		if validConsoleSession(user, 3) {
			t.Fatalf("invalid session was accepted: %#v", user)
		}
	}
}

func TestAuthInternalBearerJWTRejectsOpenToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	calls := 0
	router.POST("/internal", AuthInternalBearerJWT(), func(c *gin.Context) {
		calls++
		c.Status(http.StatusNoContent)
	})
	request := httptest.NewRequest(http.MethodPost, "/internal", nil)
	request.Header.Set("Authorization", "Bearer dg_open_value")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized || calls != 0 {
		t.Fatalf("status=%d calls=%d body=%s", recorder.Code, calls, recorder.Body.String())
	}
}

func TestBearerToken(t *testing.T) {
	token, ok := bearerToken("bearer abc.def")
	if !ok || token != "abc.def" {
		t.Fatalf("token=%q ok=%t", token, ok)
	}
	if _, ok := bearerToken("Bearer one two"); ok {
		t.Fatal("accepted malformed authorization header")
	}
}
