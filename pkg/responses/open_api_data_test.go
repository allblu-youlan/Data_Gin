package responses

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestForOpenAPIReplacesNullWithTypedEmptyValues(t *testing.T) {
	type details struct {
		Name *string `json:"name"`
	}
	type payload struct {
		Text    *string            `json:"text"`
		Count   *int64             `json:"count"`
		Amount  *float64           `json:"amount"`
		Enabled *bool              `json:"enabled"`
		Time    *time.Time         `json:"time"`
		Items   []*details         `json:"items"`
		Object  *details           `json:"object"`
		Labels  map[string]*string `json:"labels"`
		Any     any                `json:"any"`
		Omitted *string            `json:"omitted,omitempty"`
	}
	for _, test := range []struct {
		name  string
		value any
		want  string
	}{
		{
			name:  "typed fields",
			value: payload{},
			want: `{"text":"","count":0,"amount":0,"enabled":false,"time":"",` +
				`"items":[],"object":{},"labels":{},"any":{}}`,
		},
		{
			name: "nested objects and rows",
			value: payload{
				Items:  []*details{nil, {}},
				Object: &details{},
				Labels: map[string]*string{"ADD_CART_COST": nil, "BOOST_CLICK": nil},
				Any:    map[string]any{"items": []string(nil), "text": (*string)(nil)},
			},
			want: `{"text":"","count":0,"amount":0,"enabled":false,"time":"",` +
				`"items":[{},{"name":""}],"object":{"name":""},` +
				`"labels":{"ADD_CART_COST":"","BOOST_CLICK":""},"any":{"items":[],"text":""}}`,
		},
		{name: "nil data", value: nil, want: `{}`},
		{name: "nil object", value: (*payload)(nil), want: `{}`},
		{name: "nil list", value: []string(nil), want: `[]`},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw, err := json.Marshal(ForOpenAPI(test.value))
			if err != nil {
				t.Fatal(err)
			}
			var got, want any
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(test.want), &want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("response = %s, want %s", raw, test.want)
			}
		})
	}
}

func TestForOpenAPIPreservesValuesAndSource(t *testing.T) {
	text := "null"
	value := map[string]any{
		"text":         &text,
		"amount":       "123456789012345678.1234567890",
		"id":           int64(9007199254740993),
		"empty":        (*string)(nil),
		"fetchedAtUtc": time.Date(2026, 11, 4, 14, 0, 0, 0, time.UTC),
	}
	raw, err := json.Marshal(ForOpenAPI(value))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"amount":"123456789012345678.1234567890","empty":"",` +
		`"fetchedAtUtc":"2026-11-04 14:00:00","id":9007199254740993,"text":"null"}`
	if string(raw) != want {
		t.Fatalf("response = %s, want %s", raw, want)
	}
	if value["empty"] != (*string)(nil) || *value["text"].(*string) != "null" {
		t.Fatal("serialization changed source values")
	}
}
