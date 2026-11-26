package tableboard

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"gin-biz-web-api/model"
	"gorm.io/gorm"
)

var ErrConflict = errors.New("看板已被修改，请重新载入后再试")

type Input struct {
	Name     string `json:"name"`
	Layout   Layout `json:"layout"`
	Revision uint64 `json:"revision"`
}

func (in Input) Validate(creating bool) error {
	if !validName(in.Name) {
		return errors.New("看板名称需要 1 至 100 个字符")
	}
	if creating != (in.Revision == 0) {
		return errors.New("请提供正确的看板版本号")
	}
	return in.Layout.Validate()
}

type Summary struct {
	ID       uint   `json:"id"`
	Name     string `json:"name"`
	Revision uint64 `json:"revision"`
}

type Page struct {
	Items       []Summary `json:"items"`
	NextAfterID uint      `json:"nextAfterId"`
}

// Store is scoped to the authenticated account. Even the existence checks used
// to distinguish a stale revision from a missing board use the same scope.
type Store struct {
	db    *gorm.DB
	owner uint
}

func NewStore(db *gorm.DB, owner uint) Store { return Store{db: db, owner: owner} }

func (s Store) boards(ctx context.Context) *gorm.DB {
	return s.db.WithContext(ctx).Model(&model.TableBoard{}).Where("owner_id = ?", s.owner)
}

func (s Store) List(ctx context.Context, afterID uint) (Page, error) {
	const size = 50
	page := Page{Items: []Summary{}}
	query := s.boards(ctx).Select("id", "name", "revision").Order("id DESC").Limit(size + 1)
	if afterID > 0 {
		query = query.Where("id < ?", afterID)
	}
	if err := query.Find(&page.Items).Error; err != nil {
		return page, err
	}
	if len(page.Items) > size {
		page.Items = page.Items[:size]
		page.NextAfterID = page.Items[size-1].ID
	}
	return page, nil
}

func (s Store) Get(ctx context.Context, id uint) (*model.TableBoard, error) {
	var board model.TableBoard
	err := s.boards(ctx).First(&board, id).Error
	return &board, err
}

// The caller validates the input before Save. A successful CAS returns the
// exact committed revision, independent of later writes from another replica.
func (s Store) Save(ctx context.Context, id uint, input Input) (*model.TableBoard, error) {
	layout, err := json.Marshal(input.Layout)
	if err != nil {
		return nil, err
	}
	board := &model.TableBoard{
		BaseModel: model.BaseModel{ID: id}, OwnerID: s.owner,
		Name: strings.TrimSpace(input.Name), Layout: model.JSONText(layout), Revision: input.Revision + 1,
	}
	if id == 0 {
		return board, s.db.WithContext(ctx).Create(board).Error
	}
	result := s.boards(ctx).Where("id = ? AND revision = ?", id, input.Revision).
		Updates(map[string]any{"name": board.Name, "layout": board.Layout, "revision": board.Revision})
	return board, s.writeResult(ctx, id, result)
}

func (s Store) Delete(ctx context.Context, id uint, revision uint64) error {
	result := s.boards(ctx).Where("id = ? AND revision = ?", id, revision).Delete(&model.TableBoard{})
	return s.writeResult(ctx, id, result)
}

func (s Store) writeResult(ctx context.Context, id uint, result *gorm.DB) error {
	if result.Error != nil || result.RowsAffected > 0 {
		return result.Error
	}
	var count int64
	if err := s.boards(ctx).Where("id = ?", id).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return gorm.ErrRecordNotFound
	}
	return ErrConflict
}
