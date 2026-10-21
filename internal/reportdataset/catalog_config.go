package reportdataset

import "encoding/json"

// CatalogConfig inherits the source dictionary unless an existing administrator
// override supplies a narrower field or record scope. Report grants still govern access.
func CatalogConfig(raw []byte) (TableConfig, error) {
	config := TableConfig{Enabled: true, DataScope: DataScope{Mode: "GLOBAL_APPROVED"}}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &config); err != nil {
			return TableConfig{}, err
		}
	}
	if config.DataScope.Mode == "" {
		config.DataScope.Mode = "GLOBAL_APPROVED"
	}
	return config, nil
}
