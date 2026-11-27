package data_svc

import (
	"context"
	"gin-biz-web-api/internal/reportdataset"
	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/internal/tableboard"
	"github.com/godror/godror"
	"slices"
	"time"
)

type metricValidation struct {
	metric  tableboard.Metric
	period  string
	filters [][]tableboard.Filter
}

func (v metricValidation) validate(ctx context.Context, actor uint) error {
	m := v.metric
	r := aggregateRequest{metric: m, period: v.period, filters: v.filters[0]}
	return withBoardConnection(ctx, m.DatasourceID, func(ctx context.Context, conn *reportoracle.Adapter) error {
		rel, err := r.relation(ctx, conn, actor)
		if err != nil {
			return err
		}
		if err = validateMetricFields(m, rel); err != nil {
			return err
		}
		for _, conditions := range v.filters {
			p, err := boardFilters(rel, append(slices.Clone(m.Filters), conditions...))
			if err != nil {
				return err
			}
			if v.period == tableboard.PeriodFilterOnly {
				continue
			}
			if err = checkBoardDaySamples(ctx, conn, m, rel, p); err != nil {
				return err
			}
		}
		return nil
	})
}

func ValidateBoardMappings(ctx context.Context, actor uint, layout tableboard.Layout) error {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	for _, source := range layout.Sources {
		if err := withBoardConnection(ctx, source.DatasourceID, func(ctx context.Context, conn *reportoracle.Adapter) error {
			_, _, err := detailPlan(ctx, conn, actor, source)
			return err
		}); err != nil {
			return err
		}
	}
	// All stores with the same mapped fields share one live schema and policy lookup.
	groups := map[string]*metricValidation{}
	seen := map[string]bool{}
	queue := func(m tableboard.Metric, filters []tableboard.Filter, period string) {
		// All calendar periods impose a predicate on the same time field.
		if period != tableboard.PeriodFilterOnly {
			period = ""
		}
		key := reportdataset.Hash([]any{m, filters, period})
		if seen[key] {
			return
		}
		seen[key] = true
		r := aggregateRequest{metric: m, filters: filters, period: period}
		group := reportdataset.Hash([]any{m, r.restrictionFields()})
		if groups[group] == nil {
			groups[group] = &metricValidation{metric: m, period: period}
		}
		groups[group].filters = append(groups[group].filters, filters)
	}
	metrics := map[string]tableboard.Metric{}
	for _, m := range layout.Metrics {
		metrics[m.ID] = m
		queue(m, nil, "")
	}
	for _, s := range layout.Sections {
		for _, row := range s.Cells {
			for _, c := range row {
				if c.Kind != "formula" {
					continue
				}
				m, err := metrics[c.MetricID].ForCell(c)
				if err != nil {
					return err
				}
				queue(m, c.Filters, c.Period)
			}
		}
	}
	for _, group := range groups {
		if err := group.validate(ctx, actor); err != nil {
			return err
		}
	}
	return nil
}
func BoardDimensionValues(ctx context.Context, actor uint, m tableboard.Metric, field, search string) ([]string, error) {
	if err := m.Validate(); err != nil {
		return nil, boardInvalid("%s", err)
	}
	if !slices.Contains(m.Dimensions, field) {
		return nil, boardInvalid("请选择指标维度")
	}
	values := []string{}
	err := withBoardConnection(ctx, m.DatasourceID, func(ctx context.Context, conn *reportoracle.Adapter) error {
		restricted := []string{}
		for _, f := range m.Filters {
			restricted = append(restricted, f.Field)
		}
		rel, err := NewReportBojunCatalogService().boardRelation(ctx, conn, actor, m, m.Fields(), restricted, false)
		if err != nil {
			return err
		}
		if err = validateMetricFields(m, rel); err != nil {
			return err
		}
		p, err := boardFilters(rel, m.Filters)
		if err != nil {
			return err
		}
		col := quotedBoardField(field)
		p.parts = append(p.parts, col+" IS NOT NULL")
		if search != "" {
			p.parts = append(p.parts, "INSTR("+col+", "+p.bind(search)+") > 0")
		}
		rows, err := conn.QuerySelect(ctx, "SELECT * FROM (SELECT DISTINCT "+col+" FROM "+rel.from+p.where()+" ORDER BY "+col+") WHERE ROWNUM <= 200", append(p.args, godror.NumberAsString())...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var v string
			if err = rows.Scan(&v); err != nil {
				return err
			}
			values = append(values, v)
		}
		return rows.Err()
	})
	return values, err
}
