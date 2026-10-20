package data_svc

import "gin-biz-web-api/internal/reportdataset"

type ReportAllowedFilterDTO struct {
	reportdataset.AllowedFilter
	LogicalType string `json:"logicalType"`
}
type ReportAllowedSortDTO struct {
	AliasID     string `json:"aliasId"`
	ColumnID    string `json:"columnId"`
	Label       string `json:"label"`
	LogicalType string `json:"logicalType"`
}

func selectRuntimeCapabilities(c reportdataset.Compiled) ([]ReportAllowedFilterDTO, []ReportAllowedSortDTO) {
	aliases := map[string]reportdataset.Table{"base": c.Tables[c.Query.Base.TableID]}
	for _, r := range c.Query.Relations {
		aliases[r.ID] = c.Tables[r.TargetTableID]
	}
	column := func(alias, id string) reportdataset.Column {
		for _, col := range aliases[alias].Columns {
			if col.ID == id {
				return col
			}
		}
		return reportdataset.Column{}
	}
	label := func(alias string, col reportdataset.Column) string {
		table := aliases[alias].Description
		if table == "" {
			table = aliases[alias].LogicalName
		}
		name := col.Description
		if name == "" {
			name = col.DBName
		}
		return table + " · " + name
	}
	filters := []ReportAllowedFilterDTO{}
	sorts := []ReportAllowedSortDTO{}
	for _, field := range c.Query.AllowedFilters {
		col := column(field.AliasID, field.ColumnID)
		if field.Label == "" {
			field.Label = label(field.AliasID, col)
		}
		filters = append(filters, ReportAllowedFilterDTO{AllowedFilter: field, LogicalType: col.LogicalType})
	}
	for _, field := range c.Query.AllowedSortFields {
		col := column(field.AliasID, field.ColumnID)
		sorts = append(sorts, ReportAllowedSortDTO{AliasID: field.AliasID, ColumnID: field.ColumnID, Label: label(field.AliasID, col), LogicalType: col.LogicalType})
	}
	return filters, sorts
}
