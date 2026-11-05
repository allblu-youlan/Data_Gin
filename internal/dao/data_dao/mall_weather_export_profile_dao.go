package data_dao

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/database"

	mysqlDriver "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrMallWeatherExportProfileNotFound = errors.New("mall weather export profile: not found")
	ErrMallWeatherExportProfileConflict = errors.New("mall weather export profile: version conflict")
)

const maxMallWeatherExportProfileDAOPageSize = 101

type MallWeatherExportProfileListQuery struct {
	Enabled   *bool
	AfterCode string
	Limit     int
}

type MallWeatherExportProfileDAO struct {
	db *gorm.DB
}

func NewMallWeatherExportProfileDAO(databases ...*gorm.DB) *MallWeatherExportProfileDAO {
	db := database.DB
	if len(databases) > 0 && databases[0] != nil {
		db = databases[0]
	}
	return &MallWeatherExportProfileDAO{db: db}
}

func (dao *MallWeatherExportProfileDAO) Save(
	ctx context.Context,
	row *model.MallWeatherExportProfile,
	expectedVersion *uint64,
) (bool, error) {
	if dao == nil || dao.db == nil || ctx == nil || row == nil || strings.TrimSpace(row.Code) == "" ||
		strings.TrimSpace(row.Name) == "" || row.ProfileJSON == "" || row.UpdatedBy == 0 {
		return false, fmt.Errorf("mall weather export profile: invalid save")
	}
	created := false
	err := dao.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing model.MallWeatherExportProfile
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("code = ?", row.Code).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if expectedVersion != nil && *expectedVersion != 0 {
				return ErrMallWeatherExportProfileConflict
			}
			row.Version = 1
			row.CreatedBy = row.UpdatedBy
			if err := tx.Create(row).Error; err != nil {
				if isMySQLDuplicateKeyError(err) {
					return ErrMallWeatherExportProfileConflict
				}
				return fmt.Errorf("mall weather export profile: create: %w", err)
			}
			created = true
			return nil
		}
		if err != nil {
			return fmt.Errorf("mall weather export profile: find for update: %w", err)
		}
		if expectedVersion == nil || *expectedVersion != existing.Version {
			return ErrMallWeatherExportProfileConflict
		}
		if existing.Version == ^uint64(0) {
			return fmt.Errorf("mall weather export profile: version exhausted")
		}
		nextVersion := existing.Version + 1
		result := tx.Model(&model.MallWeatherExportProfile{}).
			Where("id = ? AND version = ?", existing.ID, existing.Version).
			Updates(map[string]interface{}{
				"name": row.Name, "profile_json": row.ProfileJSON, "enabled": row.Enabled,
				"updated_by": row.UpdatedBy, "version": nextVersion,
			})
		if result.Error != nil {
			return fmt.Errorf("mall weather export profile: update: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrMallWeatherExportProfileConflict
		}
		if err := tx.First(row, existing.ID).Error; err != nil {
			return fmt.Errorf("mall weather export profile: reload updated profile: %w", err)
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	return created, nil
}

func (dao *MallWeatherExportProfileDAO) List(
	ctx context.Context,
	filter MallWeatherExportProfileListQuery,
) ([]model.MallWeatherExportProfile, error) {
	invalidLimit := filter.Limit < 1 || filter.Limit > maxMallWeatherExportProfileDAOPageSize
	if dao == nil || dao.db == nil || ctx == nil || invalidLimit {
		return nil, fmt.Errorf("mall weather export profile: invalid list")
	}
	query := dao.db.WithContext(ctx).Model(&model.MallWeatherExportProfile{})
	if filter.Enabled != nil {
		query = query.Where("enabled = ?", *filter.Enabled)
	}
	if filter.AfterCode != "" {
		query = query.Where("code > ?", filter.AfterCode)
	}
	var rows []model.MallWeatherExportProfile
	if err := query.Order("code ASC").Limit(filter.Limit).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("mall weather export profile: list: %w", err)
	}
	return rows, nil
}

func (dao *MallWeatherExportProfileDAO) FindByID(
	ctx context.Context,
	profileID uint,
) (*model.MallWeatherExportProfile, error) {
	if dao == nil || dao.db == nil || ctx == nil || profileID == 0 {
		return nil, fmt.Errorf("mall weather export profile: invalid lookup")
	}
	var row model.MallWeatherExportProfile
	err := dao.db.WithContext(ctx).Where("id = ?", profileID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrMallWeatherExportProfileNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("mall weather export profile: find by id: %w", err)
	}
	return &row, nil
}

func (dao *MallWeatherExportProfileDAO) EnsureSystemProfile(
	ctx context.Context,
	desired *model.MallWeatherExportProfile,
) (*model.MallWeatherExportProfile, error) {
	if dao == nil || dao.db == nil || ctx == nil || desired == nil || strings.TrimSpace(desired.Code) == "" ||
		strings.TrimSpace(desired.Name) == "" || !json.Valid([]byte(desired.ProfileJSON)) || !desired.Enabled {
		return nil, fmt.Errorf("mall weather export profile: invalid system profile")
	}
	candidate := *desired
	candidate.BaseModel = model.BaseModel{}
	candidate.Version = 1
	candidate.CreatedBy = 0
	candidate.UpdatedBy = 0
	candidate.WeatherTimestamps = model.WeatherTimestamps{}
	var resolved model.MallWeatherExportProfile
	err := dao.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&candidate).Error; err != nil {
			return fmt.Errorf("mall weather export profile: create system profile: %w", err)
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("code = ?", desired.Code).First(&resolved).Error; err != nil {
			return fmt.Errorf("mall weather export profile: lock system profile: %w", err)
		}
		if resolved.Name == desired.Name && sameMallWeatherExportProfileJSON(resolved.ProfileJSON, desired.ProfileJSON) &&
			resolved.Enabled {
			return nil
		}
		if resolved.Version == ^uint64(0) {
			return fmt.Errorf("mall weather export profile: system profile version exhausted")
		}
		result := tx.Model(&model.MallWeatherExportProfile{}).
			Where("id = ? AND version = ?", resolved.ID, resolved.Version).
			Updates(map[string]interface{}{
				"name": desired.Name, "profile_json": desired.ProfileJSON, "enabled": true,
				"updated_by": 0, "version": resolved.Version + 1,
			})
		if result.Error != nil {
			return fmt.Errorf("mall weather export profile: update system profile: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrMallWeatherExportProfileConflict
		}
		if err := tx.First(&resolved, resolved.ID).Error; err != nil {
			return fmt.Errorf("mall weather export profile: reload system profile: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &resolved, nil
}

func sameMallWeatherExportProfileJSON(left, right model.JSONText) bool {
	var leftValue interface{}
	var rightValue interface{}
	if json.Unmarshal([]byte(left), &leftValue) != nil || json.Unmarshal([]byte(right), &rightValue) != nil {
		return false
	}
	return reflect.DeepEqual(leftValue, rightValue)
}

func isMySQLDuplicateKeyError(err error) bool {
	var mysqlError *mysqlDriver.MySQLError
	return errors.As(err, &mysqlError) && mysqlError.Number == 1062
}
