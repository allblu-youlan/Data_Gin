package reportrepo

import (
	"context"
	"gin-biz-web-api/model"
)

// Include inactive and pending entries so that a revoked table cannot be
// reopened as an ordinary Oracle table by omitting its catalog ID.
func (r *Repository) BoardManagedTables(ctx context.Context, datasource uint, owner, name string) ([]model.ReportBojunTable, error) {
	var rows []model.ReportBojunTable
	err := r.db.WithContext(ctx).Where("datasource_id = ? AND resolved_owner = ? AND (resolved_table = ? OR logical_name = ?)", datasource, owner, name, name).Order("id").Find(&rows).Error
	return rows, err
}
