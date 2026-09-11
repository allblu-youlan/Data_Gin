package data_dao

import (
	"context"
	"time"

	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/database"

	"gorm.io/gorm"
)

type BojunRetailOrderDAO struct {
	db *gorm.DB
}

type OpenBojunOrderQuery struct {
	StartCompletedAt  time.Time
	EndCompletedAt    time.Time
	BeforeCompletedAt *time.Time
	StartBillDate     int
	EndBillDate       int
	BeforeBillDate    int
	StoreCodes        []string
	OrderTypes        []string
	BeforeID          uint
	Limit             int
}

func NewBojunRetailOrderDAO(databases ...*gorm.DB) *BojunRetailOrderDAO {
	db := database.DB
	if len(databases) > 0 && databases[0] != nil {
		db = databases[0]
	}
	return &BojunRetailOrderDAO{db: db}
}

func (dao *BojunRetailOrderDAO) Create(ctx context.Context, order *model.BojunRetailOrder) (uint, error) {
	now := int(time.Now().Unix())
	order.CreatedAt = now
	order.UpdatedAt = now

	err := dao.db.WithContext(ctx).Create(order).Error
	return order.ID, err
}

func (dao *BojunRetailOrderDAO) FindByDocNo(ctx context.Context, docNo string) (*model.BojunRetailOrder, error) {
	var order model.BojunRetailOrder
	err := dao.db.WithContext(ctx).
		Where("docno = ?", docNo).
		First(&order).
		Error
	return &order, err
}

func (dao *BojunRetailOrderDAO) FindByID(ctx context.Context, id uint) (*model.BojunRetailOrder, error) {
	var order model.BojunRetailOrder
	err := dao.db.WithContext(ctx).First(&order, id).Error
	return &order, err
}

func (dao *BojunRetailOrderDAO) Update(ctx context.Context, order *model.BojunRetailOrder) error {
	order.UpdatedAt = int(time.Now().Unix())
	return dao.db.WithContext(ctx).Save(order).Error
}

func (dao *BojunRetailOrderDAO) CreateOrUpdate(ctx context.Context, order *model.BojunRetailOrder) error {
	_, err := dao.CreateOrUpdateWithCreated(ctx, order)
	return err
}

func (dao *BojunRetailOrderDAO) ExistsByDocNo(ctx context.Context, docNo string) (bool, error) {
	var count int64
	err := dao.db.WithContext(ctx).
		Model(&model.BojunRetailOrder{}).
		Where("docno = ?", docNo).
		Count(&count).
		Error
	return count > 0, err
}

func (dao *BojunRetailOrderDAO) CreateIfNotExists(ctx context.Context, order *model.BojunRetailOrder) (bool, error) {
	exists, err := dao.ExistsByDocNo(ctx, order.DocNo)
	if err != nil {
		return false, err
	}
	if exists {
		return false, nil
	}

	_, err = dao.Create(ctx, order)
	return err == nil, err
}

func (dao *BojunRetailOrderDAO) CreateOrUpdateWithCreated(ctx context.Context, order *model.BojunRetailOrder) (bool, error) {
	existing, err := dao.FindByDocNo(ctx, order.DocNo)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			_, err = dao.Create(ctx, order)
			return err == nil, err
		}
		return false, err
	}

	order.ID = existing.ID
	order.CreatedAt = existing.CreatedAt
	return false, dao.Update(ctx, order)
}

func (dao *BojunRetailOrderDAO) UpdateSyncStatus(ctx context.Context, id uint, synced int) error {
	return dao.db.WithContext(ctx).
		Model(&model.BojunRetailOrder{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"synced":     synced,
			"updated_at": time.Now().Unix(),
		}).
		Error
}

func (dao *BojunRetailOrderDAO) SupplementOracleFieldsIfMissing(
	ctx context.Context,
	localOrderID uint,
	order *model.BojunRetailOrder,
) (bool, error) {
	if dao == nil || dao.db == nil || ctx == nil || localOrderID == 0 || order == nil ||
		order.DocNo == "" || order.OracleRetailID == nil || *order.OracleRetailID == 0 {
		return false, gorm.ErrInvalidData
	}
	result := dao.db.WithContext(ctx).
		Model(&model.BojunRetailOrder{}).
		Where("id = ? AND docno = ? AND oracle_retail_id IS NULL", localOrderID, order.DocNo).
		Updates(map[string]interface{}{
			"oracle_retail_id": order.OracleRetailID,
			"retailbilltype":   order.RetailBillType,
			"c_store_name":     order.StoreName,
			"order_phone":      order.OrderPhone,
			"paid_amount":      order.PaidAmount,
			"push_amount":      order.PushAmount,
			"is_to_shop":       order.IsToShop,
			"tot_amt_list":     order.TotalAmtList,
			"tot_amt_actual":   order.TotalAmtActual,
			"tot_amt_acc":      order.TotalAmtAcc,
			"tot_amt_acc1":     order.TotalAmtAcc1,
			"updated_at":       time.Now().Unix(),
		})
	return result.RowsAffected > 0, result.Error
}

func (dao *BojunRetailOrderDAO) UpdateCompletedAtIfEmpty(
	ctx context.Context,
	docNo string,
	completedAt time.Time,
) (bool, error) {
	if dao == nil || dao.db == nil || ctx == nil || docNo == "" || completedAt.IsZero() {
		return false, gorm.ErrInvalidData
	}
	result := dao.db.WithContext(ctx).
		Model(&model.BojunRetailOrder{}).
		Where("docno = ? AND completed_at IS NULL", docNo).
		Updates(map[string]interface{}{
			"completed_at": completedAt,
			"updated_at":   time.Now().Unix(),
		})
	return result.RowsAffected > 0, result.Error
}

func (dao *BojunRetailOrderDAO) ListOpenOrders(
	ctx context.Context,
	query OpenBojunOrderQuery,
) ([]model.BojunRetailOrder, error) {
	if query.Limit <= 0 {
		return nil, gorm.ErrInvalidData
	}
	dbQuery, err := dao.openOrdersQuery(ctx, query)
	if err != nil {
		return nil, err
	}
	dbQuery = dbQuery.
		Select([]string{
			"id", "otherdocno", "docno", "order_phone", "billdate", "completed_at", "c_store_code", "c_store_name",
			"order_type_code", "order_type_name", "tot_lines", "tot_qty", "tot_amt_list",
			"tot_amt_actual", "avg_discount", "related_normal_docno", "items_json",
		})
	completedAtMode := !query.StartCompletedAt.IsZero()
	if completedAtMode && query.BeforeCompletedAt != nil && query.BeforeID > 0 {
		dbQuery = dbQuery.Where(
			"completed_at < ? OR (completed_at = ? AND id < ?)",
			*query.BeforeCompletedAt,
			*query.BeforeCompletedAt,
			query.BeforeID,
		)
	} else if !completedAtMode && query.BeforeBillDate > 0 && query.BeforeID > 0 {
		dbQuery = dbQuery.Where(
			"billdate < ? OR (billdate = ? AND id < ?)",
			query.BeforeBillDate,
			query.BeforeBillDate,
			query.BeforeID,
		)
	}

	orders := make([]model.BojunRetailOrder, 0)
	if completedAtMode {
		dbQuery = dbQuery.Order("completed_at DESC")
	} else {
		dbQuery = dbQuery.Order("billdate DESC")
	}
	err = dbQuery.Order("id DESC").Limit(query.Limit).Find(&orders).Error
	return orders, err
}

func (dao *BojunRetailOrderDAO) CountOpenOrders(ctx context.Context, query OpenBojunOrderQuery) (int64, error) {
	dbQuery, err := dao.openOrdersQuery(ctx, query)
	if err != nil {
		return 0, err
	}
	var total int64
	if err := dbQuery.Count(&total).Error; err != nil {
		return 0, err
	}
	return total, nil
}

func (dao *BojunRetailOrderDAO) openOrdersQuery(ctx context.Context, query OpenBojunOrderQuery) (*gorm.DB, error) {
	if dao == nil || dao.db == nil || ctx == nil {
		return nil, gorm.ErrInvalidData
	}
	completedAtMode := !query.StartCompletedAt.IsZero() || !query.EndCompletedAt.IsZero()
	billDateMode := query.StartBillDate > 0 || query.EndBillDate > 0
	if completedAtMode == billDateMode {
		return nil, gorm.ErrInvalidData
	}
	dbQuery := dao.db.WithContext(ctx).Model(&model.BojunRetailOrder{})
	if completedAtMode {
		if query.StartCompletedAt.IsZero() || !query.EndCompletedAt.After(query.StartCompletedAt) {
			return nil, gorm.ErrInvalidData
		}
		dbQuery = dbQuery.Where(
			"completed_at >= ? AND completed_at < ?",
			query.StartCompletedAt,
			query.EndCompletedAt,
		)
	} else {
		if query.StartBillDate <= 0 || query.EndBillDate < query.StartBillDate {
			return nil, gorm.ErrInvalidData
		}
		dbQuery = dbQuery.Where("billdate BETWEEN ? AND ?", query.StartBillDate, query.EndBillDate)
	}
	if len(query.StoreCodes) > 0 {
		dbQuery = dbQuery.Where("c_store_code IN ?", query.StoreCodes)
	}
	if len(query.OrderTypes) > 0 {
		dbQuery = dbQuery.Where("order_type_code IN ?", query.OrderTypes)
	}
	return dbQuery, nil
}
