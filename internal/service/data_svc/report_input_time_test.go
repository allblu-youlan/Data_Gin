package data_svc

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"gin-biz-web-api/internal/requestbody"
	"gin-biz-web-api/model"
)

func TestReportTimeDefaultsUseBeijingCalendar(t *testing.T) {
	now := time.Date(2026, 10, 31, 16, 30, 45, 0, time.UTC) // November 1 in Beijing.
	for _, test := range []struct{ source, valueType, control, format, want string }{
		{"CURRENT_MONTH", "str", "TEXT", "", `"202611"`},
		{"PREVIOUS_MONTH", "number", "NUMBER", "", `202610`},
		{"NEXT_MONTH", "str", "TEXT", "", `"202612"`},
		{"CURRENT_YEAR", "str", "TEXT", "", `"2026"`},
		{"TODAY", "str", "DATE", "YYYYMMDD", `"20261101"`},
		{"YESTERDAY", "str", "DATE", "YYYY-MM-DD", `"2026-10-31"`},
		{"MONTH_START", "str", "TEXT", "", `"20261101"`},
		{"MONTH_END", "str", "DATE", "YYYYMMDD", `"20261130"`},
		{"YEAR_START", "str", "TEXT", "", `"20260101"`},
		{"YEAR_END", "str", "TEXT", "", `"20261231"`},
		{"NOW", "str", "DATETIME", "YYYY-MM-DD HH:mm:ss", `"2026-11-01 00:30:45"`},
		{"TODAY", "str", "DATETIME", "YYYYMMDDHHmmss", `"20261101000000"`},
	} {
		t.Run(test.source+test.control, func(t *testing.T) {
			got, err := reportTimeDefaultValue(test.source, test.valueType, test.control, test.format, now)
			if err != nil || string(got) != test.want {
				t.Fatalf("default = %s, %v; want %s", got, err, test.want)
			}
		})
	}
	for _, test := range []struct{ source, date, want string }{
		{"PREVIOUS_MONTH", "2026-01-31", `"202512"`},
		{"NEXT_MONTH", "2026-12-31", `"202701"`},
		{"MONTH_END", "2028-02-15", `"20280229"`},
	} {
		date, _ := time.Parse("2006-01-02", test.date)
		got, err := reportTimeDefaultValue(test.source, "str", "TEXT", "", date)
		if err != nil || string(got) != test.want {
			t.Fatalf("%s at %s = %s, %v", test.source, test.date, got, err)
		}
	}
}

func TestReportDynamicDefaultsValidateAndPreserveConfiguration(t *testing.T) {
	schema := json.RawMessage(`{"month":{"type":"str","displayName":"月份","defaultTime":"current_month"}}`)
	canonical, err := canonicalReportInputSchema(schema)
	if err != nil || !strings.Contains(string(canonical), `"defaultTime":"CURRENT_MONTH"`) || strings.Contains(string(canonical), `"default":`) {
		t.Fatalf("canonical schema = %s, %v", canonical, err)
	}
	for _, field := range []string{
		`"type":"bool","displayName":"无效","defaultTime":"TODAY"`,
		`"type":"str","displayName":"无效","defaultTime":"UNKNOWN"`,
		`"type":"str","displayName":"无效","defaultTime":"TODAY","default":"20261023"`,
		`"type":"str","displayName":"无效","defaultTime":"CURRENT_MONTH","control":"DATE","format":"YYYYMMDD"`,
		`"type":"str","displayName":"无效","defaultTime":"TODAY","allowedValues":["20261023"]`,
	} {
		if _, err := canonicalReportInputSchema(json.RawMessage(`{"value":{` + field + `}}`)); !errors.Is(err, ErrReportInvalid) {
			t.Fatalf("accepted invalid dynamic default %s: %v", field, err)
		}
	}
}

func TestReportRunResolvesDynamicDefaultsAtCreationAndAllowsOverrides(t *testing.T) {
	published := publishedRunFixture()
	published.Version.ExecutionMode = model.ReportExecutionModeRefCursor
	published.Version.InputSchemaJSON = model.JSONText(`{"month":{"type":"str","displayName":"月份","required":true,"defaultTime":"CURRENT_MONTH"}}`)
	published.Parameters = nil
	store := &fakeReportRunStore{published: published}
	service := NewReportRunServiceWithDependencies(store, &fakeReportParameterCipher{})
	now := time.Date(2026, 10, 23, 3, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	for _, test := range []struct {
		value json.RawMessage
		month int
		want  string
	}{
		{nil, 10, `{"month":"202610"}`},
		{json.RawMessage(`""`), 11, `{"month":"202611"}`},
		{json.RawMessage(`"202601"`), 11, `{"month":"202601"}`},
	} {
		now = time.Date(2026, time.Month(test.month), 23, 3, 0, 0, 0, time.UTC)
		input := map[string]json.RawMessage{}
		if test.value != nil {
			input["month"] = test.value
		}
		if _, err := service.Create(t.Context(), 17, 9, requestbody.ReportRunCreateRequest{Conditions: input}); err != nil {
			t.Fatal(err)
		}
		if got := string(store.command.Run.NormalizedParametersJSON); got != test.want {
			t.Fatalf("snapshot = %s, want %s", got, test.want)
		}
	}
	values, err := normalizeSelectConditionsAt(published.Version.InputSchemaJSON, nil, now)
	if err != nil || string(values) != `{"month":"202611"}` {
		t.Fatalf("SELECT dynamic conditions = %s, %v", values, err)
	}
}
