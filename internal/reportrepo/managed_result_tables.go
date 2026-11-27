package reportrepo

import (
	"context"
	"strings"

	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/model"
)

// The Oracle catalog resolves local synonyms to physical identities before this
// lookup. Include inactive and pending catalog entries so disabling a table
// never makes it appear in the unrestricted picker.
func (r *Repository) ManagedResultTableRefs(ctx context.Context, ds uint, refs []reportoracle.ResultTableRef) (map[reportoracle.ResultTableRef]bool, error) {
	managed := make(map[reportoracle.ResultTableRef]bool)
	if len(refs) == 0 {
		return managed, nil
	}
	owners := make([]string, 0, len(refs))
	names := make([]string, 0, len(refs))
	for _, ref := range refs {
		owners = append(owners, ref.Owner)
		names = append(names, ref.Name)
	}
	var matches []model.ReportBojunTable
	err := r.db.WithContext(ctx).Select("resolved_owner", "resolved_table", "logical_name").
		Where("datasource_id = ? AND resolved_owner IN ? AND (resolved_table IN ? OR logical_name IN ?)", ds, owners, names, names).
		Find(&matches).Error
	for _, m := range matches {
		for _, name := range []string{m.ResolvedTable, m.LogicalName} {
			managed[reportoracle.ResultTableRef{Owner: strings.ToUpper(m.ResolvedOwner), Name: strings.ToUpper(name)}] = true
		}
	}
	return managed, err
}
