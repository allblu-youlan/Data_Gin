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

func validateMetric(ctx context.Context, actor uint, m tableboard.Metric, filters []tableboard.Filter) error {
	r := aggregateRequest{metric: m, filters: filters}
	return withBoardConnection(ctx, m.DatasourceID, func(ctx context.Context, conn *reportoracle.Adapter) error {
		rel, err := r.relation(ctx, conn, actor)
		if err != nil {
			return err
		}
		if err = validateMetricFields(m, rel); err != nil {
			return err
		}
		p, err := boardFilters(rel, append(slices.Clone(m.Filters), filters...))
		if err != nil {
			return err
		}
		return checkBoardDaySamples(ctx, conn, m, rel, p)
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
	seen := map[string]bool{}
	validate := func(m tableboard.Metric, filters []tableboard.Filter) error {
		key := reportdataset.Hash([]any{m, filters})
		if seen[key] {
			return nil
		}
		seen[key] = true
		return validateMetric(ctx, actor, m, filters)
	}
	metrics := map[string]tableboard.Metric{}
	for _, m := range layout.Metrics {
		metrics[m.ID] = m
		if err := validate(m, nil); err != nil {
			return err
		}
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
				if err = validate(m, c.Filters); err != nil {
					return err
				}
			}
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
		rel, err := resolveBoardRelation(ctx, conn, actor, m, m.Fields(), restricted, false)
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
