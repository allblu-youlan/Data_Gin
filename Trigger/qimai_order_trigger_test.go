package Trigger

import (
	"bytes"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"testing"

	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/database"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func TestQimaiOrderTriggerDoesNotLogVerificationCredentials(t *testing.T) {
	db, err := gorm.Open(mysql.New(mysql.Config{SkipInitializeWithVersion: true}), &gorm.Config{
		DisableAutomaticPing: true, Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
		rows, ok := tx.Statement.Dest.(*[]model.TokenData)
		if !ok {
			t.Fatalf("unexpected query destination %T", tx.Statement.Dest)
		}
		*rows = []model.TokenData{{AccountName: "野选", VerificationInfo: map[string]interface{}{
			"openId": "private-open-id", "grantCode": "private-grant-code", "open_key": "private-open-key", "nonce": "private-nonce",
		}}}
		tx.RowsAffected = 1
	}); err != nil {
		t.Fatal(err)
	}
	previousDB, previousTransport, previousOutput := database.DB, http.DefaultTransport, log.Writer()
	var output bytes.Buffer
	database.DB = db
	log.SetOutput(&output)
	http.DefaultTransport = qimaiTriggerRoundTripper(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host != "openapi.qmai.cn" {
			return nil, errors.New("unexpected provider host")
		}
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("unavailable")), Request: request}, nil
	})
	t.Cleanup(func() {
		database.DB, http.DefaultTransport = previousDB, previousTransport
		log.SetOutput(previousOutput)
	})
	trigger := NewQimaiOrderTrigger()
	err = trigger.Trigger(t.Context(), &model.RawData{
		BaseModel: &model.BaseModel{ID: 240056}, Metadata: `{"remark":"qimai_order"}`,
		RawContent: `{"params":{"orderNo":"order-240056"}}`,
	})
	if err == nil {
		t.Fatal("provider failure must propagate")
	}
	for _, secret := range []string{"private-open-id", "private-grant-code", "private-open-key", "private-nonce"} {
		if strings.Contains(output.String(), secret) {
			t.Errorf("trigger log contains verification credential %q", secret)
		}
	}
}

type qimaiTriggerRoundTripper func(*http.Request) (*http.Response, error)

func (roundTrip qimaiTriggerRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}
