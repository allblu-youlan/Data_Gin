package data_svc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gin-biz-web-api/internal/reportdataset"
	"gin-biz-web-api/internal/tableboard"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/database"
	projectredis "gin-biz-web-api/pkg/redis"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const BoardSnapshotTTL = 30 * time.Minute

var ErrBoardSnapshotExpired = errors.New("计算快照已过期，请刷新看板")

type BoardCalculation struct {
	Revision      uint64            `json:"revision"`
	CalculationID string            `json:"calculationId"`
	AsOf          time.Time         `json:"asOf"`
	Cells         []BoardCellResult `json:"cells"`
	Export        BoardExport       `json:"-"`
}

type storedBoardCalculation struct {
	BoardCalculation
	Export      BoardExport `json:"export"`
	AccessStamp string      `json:"accessStamp"`
	Owner       uint        `json:"owner"`
	BoardID     uint        `json:"boardId"`
}

// Capture the effective source policy from the configuration database. This
// includes revoked and disabled catalog entries and each user's data scope.
// Export rechecks this stamp without reading business rows again.
func BoardAccessStamp(ctx context.Context, db *gorm.DB, layout tableboard.Layout) (string, error) {
	ids := map[uint]bool{}
	for _, s := range layout.Sources {
		ids[s.DatasourceID] = true
	}
	for _, m := range layout.Metrics {
		ids[m.DatasourceID] = true
	}
	for _, section := range layout.Sections {
		for _, row := range section.Cells {
			for _, cell := range row {
				if cell.Kind != "formula" {
					continue
				}
				for _, m := range layout.Metrics {
					if m.ID != cell.MetricID {
						continue
					}
					effective, err := m.Effective(cell.Overrides)
					if err != nil {
						return "", err
					}
					ids[effective.DatasourceID] = true
				}
			}
		}
	}
	dsIDs := make([]uint, 0, len(ids))
	for id := range ids {
		dsIDs = append(dsIDs, id)
	}
	if len(dsIDs) == 0 {
		return reportdataset.Hash(layout), nil
	}
	var sources []model.ReportDatasource
	if err := db.WithContext(ctx).Where("id IN ?", dsIDs).Order("id").Find(&sources).Error; err != nil {
		return "", err
	}
	var tables []model.ReportBojunTable
	if err := db.WithContext(ctx).Where("datasource_id IN ?", dsIDs).Order("id").Find(&tables).Error; err != nil {
		return "", err
	}
	tableIDs := make([]uint, 0, len(tables))
	for _, t := range tables {
		tableIDs = append(tableIDs, t.ID)
	}
	var configs []model.ReportBojunTableConfig
	if len(tableIDs) > 0 {
		if err := db.WithContext(ctx).Where("table_id IN ?", tableIDs).Order("id").Find(&configs).Error; err != nil {
			return "", err
		}
	}
	return reportdataset.Hash(struct {
		Sources []model.ReportDatasource
		Tables  []model.ReportBojunTable
		Configs []model.ReportBojunTableConfig
	}{sources, tables, configs}), nil
}

func boardCalculationKey(owner, board uint, id string) string {
	return projectredis.GenNamespace(fmt.Sprintf("table-board:calculation:%d:%d:%s", owner, board, id))
}

func StoreBoardCalculation(ctx context.Context, owner, board uint, stamp string, calculation BoardCalculation) error {
	instance := projectredis.Instance()
	if instance == nil || instance.Client == nil {
		return errors.New("计算快照缓存暂不可用")
	}
	raw, err := json.Marshal(storedBoardCalculation{BoardCalculation: calculation, Export: calculation.Export, AccessStamp: stamp, Owner: owner, BoardID: board})
	if err != nil {
		return err
	}
	return instance.Client.Set(ctx, boardCalculationKey(owner, board, calculation.CalculationID), raw, BoardSnapshotTTL).Err()
}

func LoadBoardCalculation(ctx context.Context, owner, board uint, id string) (BoardCalculation, string, error) {
	if _, err := uuid.Parse(id); err != nil {
		return BoardCalculation{}, "", ErrBoardSnapshotExpired
	}
	instance := projectredis.Instance()
	if instance == nil || instance.Client == nil {
		return BoardCalculation{}, "", errors.New("计算快照缓存暂不可用")
	}
	raw, err := instance.Client.Get(ctx, boardCalculationKey(owner, board, id)).Bytes()
	if err != nil {
		// Missing snapshots and unavailable caches both require a new calculation.
		return BoardCalculation{}, "", ErrBoardSnapshotExpired
	}
	var stored storedBoardCalculation
	if err = json.Unmarshal(raw, &stored); err != nil {
		return BoardCalculation{}, "", err
	}
	if stored.Owner != owner || stored.BoardID != board || stored.CalculationID != id || time.Since(stored.AsOf) > BoardSnapshotTTL {
		return BoardCalculation{}, "", ErrBoardSnapshotExpired
	}
	stored.BoardCalculation.Export = stored.Export
	return stored.BoardCalculation, stored.AccessStamp, nil
}

func CurrentBoardAccessStamp(ctx context.Context, layout tableboard.Layout) (string, error) {
	return BoardAccessStamp(ctx, database.DB, layout)
}
