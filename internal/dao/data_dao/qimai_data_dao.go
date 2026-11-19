package data_dao

import (
	"context"
	"errors"

	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/database"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// QimaiDataDAO qimai_order_data表的DAO
type QimaiDataDAO struct {
	db *gorm.DB
}

// NewQimaiDataDAO 创建QimaiDataDAO实例
func NewQimaiDataDAO() *QimaiDataDAO {
	return &QimaiDataDAO{
		db: database.DB,
	}
}

// Create creates at most one order for each raw record. The raw row lock also
// serializes retries on other workers; provider requests run before this transaction.
func (dao *QimaiDataDAO) Create(ctx context.Context, qimaiData *model.QIMAI_ORDER_DATA) (uint, error) {
	var id uint
	err := dao.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rawData model.RawData
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").
			Where("id = ?", qimaiData.RawDataID).First(&rawData).Error; err != nil {
			return err
		}
		var existing model.QIMAI_ORDER_DATA
		err := tx.Select("id").Where("raw_data_id = ?", qimaiData.RawDataID).First(&existing).Error
		if err == nil {
			id = existing.ID
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := tx.Create(qimaiData).Error; err != nil {
			return err
		}
		id = qimaiData.ID
		return nil
	})
	if err != nil {
		return 0, err
	}
	qimaiData.ID = id
	return id, nil
}

// Update 更新企迈订单数据
func (dao *QimaiDataDAO) Update(ctx context.Context, qimaiData *model.QIMAI_ORDER_DATA) error {
	return dao.db.WithContext(ctx).Save(qimaiData).Error
}

// FindByID 根据ID查询企迈订单数据
func (dao *QimaiDataDAO) FindByID(ctx context.Context, id uint) (*model.QIMAI_ORDER_DATA, error) {
	var qimaiData model.QIMAI_ORDER_DATA
	err := dao.db.WithContext(ctx).First(&qimaiData, id).Error
	if err != nil {
		return nil, err
	}
	return &qimaiData, nil
}

// FindByOrderNo 根据订单号查询企迈订单数据
func (dao *QimaiDataDAO) FindByOrderNo(ctx context.Context, orderNo string) (*model.QIMAI_ORDER_DATA, error) {
	var qimaiData model.QIMAI_ORDER_DATA
	err := dao.db.WithContext(ctx).Where("order_no = ?", orderNo).First(&qimaiData).Error
	if err != nil {
		return nil, err
	}
	return &qimaiData, nil
}

// FindAll 查询所有企迈订单数据
func (dao *QimaiDataDAO) FindAll(ctx context.Context) ([]model.QIMAI_ORDER_DATA, error) {
	var qimaiDataList []model.QIMAI_ORDER_DATA
	err := dao.db.WithContext(ctx).Find(&qimaiDataList).Error
	return qimaiDataList, err
}

// Delete 删除企迈订单数据
func (dao *QimaiDataDAO) Delete(ctx context.Context, id uint) error {
	return dao.db.WithContext(ctx).Delete(&model.QIMAI_ORDER_DATA{}, id).Error
}

// FindByShopCodeAndStatus 根据门店编码和状态查询订单
func (dao *QimaiDataDAO) FindByShopCodeAndStatus(ctx context.Context, shopCode, status string) ([]model.QIMAI_ORDER_DATA, error) {
	var qimaiDataList []model.QIMAI_ORDER_DATA
	err := dao.db.WithContext(ctx).Where("shop_code = ? AND status = ? AND synced = 0", shopCode, status).Find(&qimaiDataList).Error
	return qimaiDataList, err
}

// MarkAsSynced 标记订单已同步
func (dao *QimaiDataDAO) MarkAsSynced(ctx context.Context, id uint) error {
	return dao.db.WithContext(ctx).Model(&model.QIMAI_ORDER_DATA{}).Where("id = ?", id).Update("synced", 1).Error
}
