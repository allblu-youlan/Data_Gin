package apidoc

import (
	"encoding/json"
	"os"
	"testing"

	"gin-biz-web-api/internal/reportoracle"
)

func TestTmallDocumentationMatchesQueryColumns(t *testing.T) {
	var catalog struct {
		Operations []Operation `json:"operations"`
	}
	if err := json.Unmarshal(catalogJSON, &catalog); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile("../../docs/open_tmall_api.openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	var openAPI struct {
		Components struct {
			Schemas map[string]struct {
				Required   []string `json:"required"`
				Properties map[string]struct {
					Type     string `json:"type"`
					Nullable bool   `json:"nullable"`
				} `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(content, &openAPI); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		id      string
		schema  string
		dataset reportoracle.TmallDataset
	}{
		{"tmall-sycm", "Sycm", reportoracle.TmallSycm},
		{"tmall-sycm-hourly", "SycmHourly", reportoracle.TmallSycmHourly},
		{"tmall-sycm-overview-hourly", "SycmOverviewHourly", reportoracle.TmallDataset("sycm_overview_hourly")},
		{"tmall-qztg", "Qztg", reportoracle.TmallQztg},
		{"tmall-qztg-hourly", "QztgHourly", reportoracle.TmallQztgHourly},
	} {
		t.Run(test.id, func(t *testing.T) {
			var operation *Operation
			for i := range catalog.Operations {
				if catalog.Operations[i].ID == test.id {
					operation = &catalog.Operations[i]
					break
				}
			}
			if operation == nil {
				t.Fatal("missing catalog operation")
			}
			items := findField(operation.ResponseFields, "data", "items")
			filters := findField(operation.RequestFields, "filters")
			if items == nil || filters == nil {
				t.Fatal("missing items or filters")
			}
			var example struct {
				Data struct {
					Items []map[string]string `json:"items"`
				} `json:"data"`
			}
			if err := json.Unmarshal(operation.ResponseExample, &example); err != nil {
				t.Fatal(err)
			}
			if len(example.Data.Items) != 1 {
				t.Fatal("expected one complete example row")
			}
			columns := reportoracle.TmallColumns(test.dataset)
			responseCount := 0
			for _, column := range columns {
				if column.Name != "ITEM_NO" {
					responseCount++
				}
			}
			row := openAPI.Components.Schemas[test.schema+"Row"]
			filterSchema := openAPI.Components.Schemas[test.schema+"Filters"]
			if len(items.Children) != responseCount || len(row.Properties) != responseCount || len(row.Required) != responseCount || len(example.Data.Items[0]) != responseCount {
				t.Fatalf("source columns=%d, catalog=%d, OpenAPI=%d, required=%d, example=%d", responseCount, len(items.Children), len(row.Properties), len(row.Required), len(example.Data.Items[0]))
			}
			required := make(map[string]bool, len(row.Required))
			for _, name := range row.Required {
				required[name] = true
			}
			filterCount := 0
			for _, column := range columns {
				field := findField(items.Children, column.Name)
				property, exists := row.Properties[column.Name]
				_, exampleExists := example.Data.Items[0][column.Name]
				if column.Name == "ITEM_NO" {
					if field != nil || exists || required[column.Name] || exampleExists {
						t.Errorf("hidden field %s is exposed", column.Name)
					}
				} else if field == nil || field.Type != "string" || !field.Required || field.Nullable || !exists || property.Type != "string" || property.Nullable || !required[column.Name] || !exampleExists {
					t.Errorf("missing or invalid response field %s", column.Name)
				}
				if column.Filterable {
					filterCount++
					if findField(filters.Children, column.Name) == nil {
						t.Errorf("catalog missing filter %s", column.Name)
					}
					if _, ok := filterSchema.Properties[column.Name]; !ok {
						t.Errorf("OpenAPI missing filter %s", column.Name)
					}
				}
			}
			if len(filters.Children) != filterCount || len(filterSchema.Properties) != filterCount {
				t.Errorf("source filters=%d, catalog=%d, OpenAPI=%d", filterCount, len(filters.Children), len(filterSchema.Properties))
			}
		})
	}
}
