package data_svc

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	appConfig "gin-biz-web-api/config"
	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/internal/requestbody"
	"gin-biz-web-api/model"
)

type fakeOpenBojunProductOracle struct {
	row         *reportoracle.BojunProductRow
	err         error
	productCode string
	deadline    bool
	calls       int
}

func (oracle *fakeOpenBojunProductOracle) QueryBojunProductByCode(
	ctx context.Context,
	productCode string,
) (*reportoracle.BojunProductRow, error) {
	oracle.calls++
	oracle.productCode = productCode
	_, oracle.deadline = ctx.Deadline()
	return oracle.row, oracle.err
}

type fakeOpenBojunProductPermissionReader struct {
	allowed    bool
	err        error
	actor      uint
	permission string
	at         time.Time
}

func (reader *fakeOpenBojunProductPermissionReader) HasPermission(
	_ context.Context,
	actor uint,
	permission string,
	at time.Time,
) (bool, error) {
	reader.actor, reader.permission, reader.at = actor, permission, at
	return reader.allowed, reader.err
}

func openBojunProductOracleConfig() appConfig.ReportInputOracleConfig {
	return appConfig.ReportInputOracleConfig{
		Host: "oracle", Port: 1521, ServiceName: "REPORT", Username: "user", Password: "secret",
		QueryTimeout: time.Second, PrefetchRows: 100, ArraySize: 100,
	}
}

func TestOpenBojunProductQueryServiceQueriesNormalizedCodeAndCachesOracle(t *testing.T) {
	now := time.Date(2026, time.September, 15, 10, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	oracle := &fakeOpenBojunProductOracle{row: &reportoracle.BojunProductRow{
		PriceList: "179", ProductName: "C17F2021", Value: "儿童|半裙",
		ProductCode: "C17F2021H063090", ProdColor: "C17F2021H063", Value1: "甜蜜薰衣草", Value2: "090",
	}}
	permissions := &fakeOpenBojunProductPermissionReader{allowed: true}
	openCalls := 0
	service := newOpenBojunProductQueryService(
		openBojunProductOracleConfig(),
		func(_ context.Context, config reportoracle.Config) (openBojunProductOracle, error) {
			openCalls++
			if config.Host != "oracle" || config.ServiceName != "REPORT" || config.Password != "secret" {
				t.Fatalf("Oracle config = %#v", config)
			}
			return oracle, nil
		},
		permissions,
		func() time.Time { return now },
	)

	for range 2 {
		result, err := service.Query(t.Context(), 17, requestbody.OpenBojunProductQueryRequest{
			ProductCode: " C17F2021H063090 ",
		})
		if err != nil {
			t.Fatalf("Query() error = %v", err)
		}
		if result.PriceList != "179" || result.ProductName != "C17F2021" || result.Value != "儿童|半裙" ||
			result.ProductCode != "C17F2021H063090" || result.ProductColor != "C17F2021H063" ||
			result.Value1 != "甜蜜薰衣草" || result.Value2 != "090" {
			t.Fatalf("result = %#v", result)
		}
	}
	if openCalls != 1 || oracle.calls != 2 || oracle.productCode != "C17F2021H063090" || !oracle.deadline {
		t.Fatalf("openCalls=%d oracle=%#v", openCalls, oracle)
	}
	if permissions.actor != 17 || permissions.permission != model.PermissionBojunOrderRead || !permissions.at.Equal(now.UTC()) {
		t.Fatalf("permissions = %#v", permissions)
	}
}

func TestOpenBojunProductQueryServiceRejectsInvalidOrUnauthorizedQuery(t *testing.T) {
	tests := []struct {
		name        string
		actor       uint
		allowed     bool
		productCode string
		wantErr     error
	}{
		{name: "missing actor", actor: 0, allowed: true, productCode: "C17F2021H063090", wantErr: ErrOpenBojunProductForbidden},
		{name: "permission denied", actor: 17, allowed: false, productCode: "C17F2021H063090", wantErr: ErrOpenBojunProductForbidden},
		{name: "empty code", actor: 17, allowed: true, productCode: "", wantErr: ErrOpenBojunProductInvalidQuery},
		{name: "code too long", actor: 17, allowed: true, productCode: strings.Repeat("A", 101), wantErr: ErrOpenBojunProductInvalidQuery},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			openCalls := 0
			service := newOpenBojunProductQueryService(
				openBojunProductOracleConfig(),
				func(context.Context, reportoracle.Config) (openBojunProductOracle, error) {
					openCalls++
					return nil, errors.New("must not open")
				},
				&fakeOpenBojunProductPermissionReader{allowed: tt.allowed},
				time.Now,
			)
			_, err := service.Query(t.Context(), tt.actor, requestbody.OpenBojunProductQueryRequest{ProductCode: tt.productCode})
			if !errors.Is(err, tt.wantErr) || openCalls != 0 {
				t.Fatalf("Query() error=%v openCalls=%d", err, openCalls)
			}
		})
	}
}

func TestOpenBojunProductQueryServiceMapsNotFoundAndUnavailable(t *testing.T) {
	tests := []struct {
		name     string
		row      *reportoracle.BojunProductRow
		queryErr error
		wantErr  error
	}{
		{name: "not found", queryErr: sql.ErrNoRows, wantErr: ErrOpenBojunProductNotFound},
		{name: "query failure", queryErr: errors.New("connection lost"), wantErr: ErrOpenBojunProductUnavailable},
		{name: "nil row", wantErr: ErrOpenBojunProductUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := newOpenBojunProductQueryService(
				openBojunProductOracleConfig(),
				func(context.Context, reportoracle.Config) (openBojunProductOracle, error) {
					return &fakeOpenBojunProductOracle{row: tt.row, err: tt.queryErr}, nil
				},
				&fakeOpenBojunProductPermissionReader{allowed: true},
				time.Now,
			)
			_, err := service.Query(t.Context(), 17, requestbody.OpenBojunProductQueryRequest{ProductCode: "C17F2021H063090"})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Query() error=%v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestOpenBojunProductQueryTimeoutCapsConfiguredDuration(t *testing.T) {
	for _, tt := range []struct {
		name       string
		configured time.Duration
		want       time.Duration
	}{
		{name: "configured shorter timeout", configured: time.Second, want: time.Second},
		{name: "configured longer timeout", configured: 30 * time.Second, want: openBojunProductMaxQueryTimeout},
		{name: "missing timeout", configured: 0, want: openBojunProductMaxQueryTimeout},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := openBojunProductQueryTimeout(tt.configured); got != tt.want {
				t.Fatalf("openBojunProductQueryTimeout()=%s, want %s", got, tt.want)
			}
		})
	}
}
