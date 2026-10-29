package data_svc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/internal/reportrepo"
	"gin-biz-web-api/job"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/database"
	"gin-biz-web-api/pkg/email"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type TableEmailService struct {
	db   *gorm.DB
	open tableEmailOracleOpener
	send func([]string, string, string) error
	now  func() time.Time
}

func NewTableEmailService() *TableEmailService {
	return &TableEmailService{db: database.DB, open: newTableEmailOracleOpener(database.DB), send: func(to []string, subject, body string) error { return email.NewMailer().SendMail(to, subject, body) }, now: func() time.Time { return time.Now().UTC() }}
}

type TableEmailDatasource struct {
	ID   uint   `json:"id"`
	Name string `json:"name"`
}

func (service *TableEmailService) Datasources(ctx context.Context) ([]TableEmailDatasource, error) {
	result := make([]TableEmailDatasource, 0)
	if _, _, err := officeOracleConfigFromEnvironment(); err == nil {
		result = append(result, TableEmailDatasource{Name: "系统 Oracle 配置"})
	}
	sources, err := reportrepo.New(service.db).ListReportDatasources(ctx)
	if err != nil {
		return nil, err
	}
	for _, source := range sources {
		if source.Enabled {
			result = append(result, TableEmailDatasource{ID: source.ID, Name: source.Name})
		}
	}
	return result, nil
}

func (service *TableEmailService) Tables(ctx context.Context, datasourceID uint, owner, search string) ([]OfficeResultTableSummary, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	connection, err := service.open(ctx, datasourceID)
	if err != nil {
		return nil, err
	}
	defer connection.Close()
	tables, err := connection.ListResultTables(ctx, reportoracle.ResultTableCatalogQuery{Owner: strings.TrimSpace(owner), Search: strings.TrimSpace(search), Limit: 100})
	if err != nil {
		return nil, err
	}
	result := make([]OfficeResultTableSummary, 0, len(tables))
	for _, table := range tables {
		result = append(result, OfficeResultTableSummary{Owner: table.Owner, Name: table.Name, ColumnCount: table.ColumnCount})
	}
	return result, nil
}

func (service *TableEmailService) Columns(ctx context.Context, input TableEmailRuleInput) ([]OfficeResultColumn, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	connection, err := service.open(ctx, input.DatasourceID)
	if err != nil {
		return nil, err
	}
	defer connection.Close()
	return tableEmailColumns(ctx, connection, input)
}

func (service *TableEmailService) List(ctx context.Context) ([]model.TableEmailRule, error) {
	result := make([]model.TableEmailRule, 0)
	err := service.db.WithContext(ctx).Order("id DESC").Find(&result).Error
	return result, err
}

func tableEmailInput(rule model.TableEmailRule) TableEmailRuleInput {
	return TableEmailRuleInput{Name: rule.Name, DatasourceID: rule.DatasourceID, Owner: rule.Owner, TableName: rule.TableNameValue, CursorColumn: rule.CursorColumn, RecipientsTemplate: rule.RecipientsTemplate, SubjectTemplate: rule.SubjectTemplate, BodyTemplate: rule.BodyTemplate, Enabled: rule.Enabled, PollIntervalSeconds: rule.PollIntervalSeconds, LockVersion: rule.LockVersion}
}

func tableEmailNeedsBaseline(current model.TableEmailRule, input TableEmailRuleInput) bool {
	return current.ID == 0 || current.DatasourceID != input.DatasourceID || current.Owner != input.Owner || current.TableNameValue != input.TableName || current.CursorColumn != input.CursorColumn || (!current.Enabled && input.Enabled)
}

func (service *TableEmailService) Save(ctx context.Context, actor, id uint, input TableEmailRuleInput) (*model.TableEmailRule, error) {
	ctx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	var result model.TableEmailRule
	err := service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if id != 0 {
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&result, id).Error; err != nil {
				return err
			}
			if result.LockVersion != input.LockVersion {
				return ErrTableEmailConflict
			}
			paused := tableEmailInput(result)
			paused.Enabled = false
			// Pausing a rule must remain possible while Oracle is unavailable.
			if !input.Enabled && input == paused {
				result.Enabled = false
				result.LockVersion++
				result.UpdatedBy = actor
				return tx.Save(&result).Error
			}
		}
		connection, err := service.open(ctx, input.DatasourceID)
		if err != nil {
			return err
		}
		defer connection.Close()
		columns, err := tableEmailColumns(ctx, connection, input)
		if err != nil {
			return err
		}
		input, err = normalizeTableEmailRule(input, columns)
		if err != nil {
			return err
		}
		if err := validateTableEmailOracleCursor(ctx, connection, input); err != nil {
			return err
		}
		if tableEmailNeedsBaseline(result, input) {
			cursor, err := tableEmailHighWater(ctx, connection, input)
			if err != nil {
				return err
			}
			result.CursorValue = cursor
			result.Generation++
		}
		result.Name = input.Name
		result.DatasourceID = input.DatasourceID
		result.Owner = input.Owner
		result.TableNameValue = input.TableName
		result.CursorColumn = input.CursorColumn
		result.RecipientsTemplate = input.RecipientsTemplate
		result.SubjectTemplate = input.SubjectTemplate
		result.BodyTemplate = input.BodyTemplate
		result.Enabled = input.Enabled
		result.PollIntervalSeconds = input.PollIntervalSeconds
		result.LockVersion++
		result.UpdatedBy = actor
		result.LastError = ""
		result.NextCheckAt = service.now()
		if id == 0 {
			result.CreatedBy = actor
			return tx.Create(&result).Error
		}
		return tx.Save(&result).Error
	})
	return &result, err
}

func (service *TableEmailService) Delete(ctx context.Context, id uint, version uint64) error {
	result := service.db.WithContext(ctx).Where("id = ? AND lock_version = ?", id, version).Delete(&model.TableEmailRule{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrTableEmailConflict
	}
	return nil
}

func (service *TableEmailService) Preview(ctx context.Context, input TableEmailRuleInput, sample map[string]any) (TableEmailPreview, error) {
	columns, err := service.Columns(ctx, input)
	if err != nil {
		return TableEmailPreview{}, err
	}
	input, err = normalizeTableEmailRule(input, columns)
	if err != nil {
		return TableEmailPreview{}, err
	}
	return renderTableEmail(input, sample)
}

func (service *TableEmailService) Deliveries(ctx context.Context, ruleID uint) ([]model.TableEmailDelivery, error) {
	result := make([]model.TableEmailDelivery, 0)
	query := service.db.WithContext(ctx).Order("id DESC").Limit(100)
	if ruleID != 0 {
		query = query.Where("rule_id = ?", ruleID)
	}
	err := query.Find(&result).Error
	return result, err
}

func newTableEmailDelivery(rule model.TableEmailRule, row map[string]any) (model.TableEmailDelivery, error) {
	cursor, err := normalizeTableEmailCursor(fmt.Sprint(row[rule.CursorColumn]))
	if err != nil {
		return model.TableEmailDelivery{}, err
	}
	snapshot, err := json.Marshal(row)
	if err != nil {
		return model.TableEmailDelivery{}, fmt.Errorf("table email: encode record: %w", err)
	}
	delivery := model.TableEmailDelivery{RuleID: rule.ID, Generation: rule.Generation, SourceValue: cursor, RecipientsJSON: "[]", SnapshotJSON: model.JSONText(snapshot), Status: "queued"}
	preview, renderErr := renderTableEmail(tableEmailInput(rule), row)
	if renderErr != nil {
		delivery.Status = "failed"
		delivery.LastError = renderErr.Error()
		return delivery, nil
	}
	recipients, err := json.Marshal(preview.Recipients)
	if err != nil {
		return delivery, err
	}
	delivery.RecipientsJSON = model.JSONText(recipients)
	delivery.Subject = preview.Subject
	delivery.Body = preview.Body
	return delivery, nil
}

func tableEmailOutbox(delivery model.TableEmailDelivery, now time.Time) (model.AsyncJobOutbox, error) {
	payload, err := json.Marshal(job.TableEmailDeliveryPayload{DeliveryID: delivery.ID, RetryVersion: delivery.RetryVersion})
	if err != nil {
		return model.AsyncJobOutbox{}, err
	}
	return model.AsyncJobOutbox{TaskKey: fmt.Sprintf("table-email:%d:%d", delivery.ID, delivery.RetryVersion), TaskType: job.TypeTableEmailDelivery, PayloadJSON: model.JSONText(payload), QueueName: job.TableEmailQueue, AvailableAt: now}, nil
}

// Scan commits the record snapshots, outbox rows and scan position together.
// Row locks serialize the same rule across workers, while SKIP LOCKED permits
// independent rules to progress on other workers.
func (service *TableEmailService) Scan(ctx context.Context) error {
	for i := 0; i < 100; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		found := false
		err := service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var rule model.TableEmailRule
			err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("enabled = ? AND next_check_at <= ?", true, service.now()).Order("next_check_at ASC, id ASC").First(&rule).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			if err != nil {
				return err
			}
			found = true
			now := service.now()
			rule.LastCheckedAt = &now
			rule.NextCheckAt = now.Add(time.Duration(rule.PollIntervalSeconds) * time.Second)
			queryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			connection, err := service.open(queryCtx, rule.DatasourceID)
			if err != nil {
				rule.LastError = "Oracle 连接失败，请检查数据源配置"
				return tx.Save(&rule).Error
			}
			defer connection.Close()
			records, err := tableEmailReadRows(queryCtx, connection, tableEmailInput(rule), rule.CursorValue)
			if err != nil {
				rule.LastError = "Oracle 新增记录查询失败，请检查表、增量字段及权限"
				return tx.Save(&rule).Error
			}
			for _, record := range records {
				delivery, err := newTableEmailDelivery(rule, record)
				if err != nil {
					return err
				}
				if err := tx.Create(&delivery).Error; err != nil {
					return err
				}
				if delivery.Status == "queued" {
					outbox, err := tableEmailOutbox(delivery, now)
					if err != nil {
						return err
					}
					if err := tx.Create(&outbox).Error; err != nil {
						return err
					}
				}
				rule.CursorValue = delivery.SourceValue
			}
			rule.LastError = ""
			return tx.Save(&rule).Error
		})
		if err != nil {
			return fmt.Errorf("table email: scan: %w", err)
		}
		if !found {
			return nil
		}
	}
	return nil
}

func (service *TableEmailService) Deliver(ctx context.Context, payload job.TableEmailDeliveryPayload, retryAllowed bool) error {
	var sendErr error
	err := service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var delivery model.TableEmailDelivery
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&delivery, payload.DeliveryID).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		} else if err != nil {
			return err
		}
		if delivery.RetryVersion != payload.RetryVersion || (delivery.Status != "queued" && delivery.Status != "retrying") {
			return nil
		}
		var rule model.TableEmailRule
		err := tx.First(&rule, delivery.RuleID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) || err == nil && (!rule.Enabled || rule.Generation != delivery.Generation) {
			delivery.Status = "cancelled"
			delivery.LastError = "提醒已暂停、删除或更换监听起点"
			return tx.Save(&delivery).Error
		}
		if err != nil {
			return err
		}
		var recipients []string
		if err := json.Unmarshal([]byte(delivery.RecipientsJSON), &recipients); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		delivery.Attempts++
		sendErr = service.send(recipients, delivery.Subject, tableEmailHTML(delivery.Body))
		if sendErr != nil {
			delivery.Status = "failed"
			if retryAllowed {
				delivery.Status = "retrying"
			}
			delivery.LastError = "邮件发送失败，请检查 SMTP 配置和收件地址"
		} else {
			now := service.now()
			delivery.Status = "sent"
			delivery.SentAt = &now
			delivery.LastError = ""
		}
		return tx.Save(&delivery).Error
	})
	if err != nil {
		return fmt.Errorf("table email: update delivery: %w", err)
	}
	if sendErr != nil {
		return fmt.Errorf("table email: SMTP delivery failed")
	}
	return nil
}

func (service *TableEmailService) Retry(ctx context.Context, id uint) error {
	return service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var delivery model.TableEmailDelivery
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&delivery, id).Error; err != nil {
			return err
		}
		if delivery.Status != "failed" {
			return tableEmailInvalid("只能重试失败的发送记录")
		}
		var rule model.TableEmailRule
		if err := tx.First(&rule, delivery.RuleID).Error; err != nil {
			return err
		}
		if !rule.Enabled || rule.Generation != delivery.Generation {
			return tableEmailInvalid("请先启用原提醒规则；已更换监听起点的记录不能重试")
		}
		// Render failures have no mail snapshot; allow a corrected template to use
		// the saved trigger values. SMTP failures retain their original recipients.
		if string(delivery.RecipientsJSON) == "[]" {
			var row map[string]any
			decoder := json.NewDecoder(strings.NewReader(string(delivery.SnapshotJSON)))
			decoder.UseNumber()
			if err := decoder.Decode(&row); err != nil {
				return err
			}
			corrected, err := newTableEmailDelivery(rule, row)
			if err != nil {
				return err
			}
			if corrected.Status == "failed" {
				return tableEmailInvalid(corrected.LastError)
			}
			delivery.RecipientsJSON = corrected.RecipientsJSON
			delivery.Subject = corrected.Subject
			delivery.Body = corrected.Body
		}
		delivery.RetryVersion++
		delivery.Status = "queued"
		delivery.LastError = ""
		if err := tx.Save(&delivery).Error; err != nil {
			return err
		}
		outbox, err := tableEmailOutbox(delivery, service.now())
		if err != nil {
			return err
		}
		return tx.Create(&outbox).Error
	})
}
