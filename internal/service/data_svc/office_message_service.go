package data_svc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	webdavconnector "gin-biz-web-api/connector/webdav"
	"gin-biz-web-api/global"
	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/internal/reportsecret"
	"gin-biz-web-api/job"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/credential"
	"gin-biz-web-api/pkg/database"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrOfficeMessageInvalid  = errors.New("office message: invalid input")
	ErrOfficeMessageNotFound = errors.New("office message: not found")
	ErrOfficeMessageConflict = errors.New("office message: version conflict")
)

type OfficeMessageInput struct {
	Name                string                 `json:"name"`
	SourceType          string                 `json:"sourceType"`
	Content             string                 `json:"content"`
	ProcedureOwner      string                 `json:"procedureOwner"`
	PackageName         string                 `json:"packageName"`
	ProcedureName       string                 `json:"procedureName"`
	ProcedureOverload   string                 `json:"procedureOverload"`
	ResultTableOwner    string                 `json:"resultTableOwner"`
	ResultTableName     string                 `json:"resultTableName"`
	SelectSQL           string                 `json:"selectSql"`
	FileNameTemplate    string                 `json:"fileNameTemplate"`
	Parameters          []OfficeQueryParameter `json:"parameters"`
	ColumnMapping       []OfficeColumnMapping  `json:"columnMapping"`
	Enabled             *bool                  `json:"enabled"`
	ExpectedLockVersion uint64                 `json:"expectedLockVersion"`
}

type OfficePushTargetInput struct {
	Name                string `json:"name"`
	MessageID           uint   `json:"messageId"`
	Channel             string `json:"channel"`
	BotAppID            string `json:"botAppId"`
	ReceiveIDType       string `json:"receiveIdType"`
	ReceiveID           string `json:"receiveId"`
	WebDAVURL           string `json:"webdavUrl"`
	WebDAVUsername      string `json:"webdavUsername"`
	WebDAVPassword      string `json:"webdavPassword"`
	WebDAVPath          string `json:"webdavPath"`
	Enabled             *bool  `json:"enabled"`
	ExpectedLockVersion uint64 `json:"expectedLockVersion"`
}

type OfficePushRunInput struct {
	Parameters map[string]string `json:"parameters"`
	RequestID  string            `json:"requestId"`
}

const officeFeishuBotSourceEnvironment = "ENVIRONMENT"

type OfficeFeishuBotOption struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Source string `json:"source"`
}

type officeFeishuBotConfig struct {
	appID      string
	configured bool
}

const officeWebDAVCredentialPurpose = "office-webdav"

type officePushCredentialCipher interface {
	EncryptScoped(purpose, plaintext string) (string, string, error)
	DecryptScoped(purpose, version, ciphertext string) (string, error)
}

type OfficeMessageService struct {
	db         *gorm.DB
	now        func() time.Time
	feishuBot  officeFeishuBotConfig
	credential officePushCredentialCipher
}

func NewOfficeMessageService() *OfficeMessageService {
	return newOfficeMessageService(database.DB, officeFeishuBotConfig{
		appID: strings.TrimSpace(global.Credentials.FeishuAppID()),
		configured: global.Credentials.Configured(credential.EnvFeishuAppID) &&
			global.Credentials.Configured(credential.EnvFeishuAppSecret),
	})
}

func newOfficeMessageService(db *gorm.DB, botConfigs ...officeFeishuBotConfig) *OfficeMessageService {
	if db == nil {
		panic("office message service: database is required")
	}
	var botConfig officeFeishuBotConfig
	if len(botConfigs) > 0 {
		botConfig = botConfigs[0]
	}
	return &OfficeMessageService{
		db: db, now: func() time.Time { return time.Now().UTC() }, feishuBot: botConfig,
		credential: reportsecret.EnvironmentKeyring{},
	}
}

func (service *OfficeMessageService) ListFeishuBots(_ context.Context) []OfficeFeishuBotOption {
	if service == nil || !service.feishuBot.configured || strings.TrimSpace(service.feishuBot.appID) == "" {
		return []OfficeFeishuBotOption{}
	}
	return []OfficeFeishuBotOption{{
		ID: service.feishuBot.appID, Name: "默认飞书机器人", Source: officeFeishuBotSourceEnvironment,
	}}
}

func (service *OfficeMessageService) resolveOfficeFeishuBot(requested string) (string, error) {
	if service == nil {
		return "", fmt.Errorf("%w: feishu bot is not configured", ErrOfficeMessageInvalid)
	}
	requested = strings.TrimSpace(requested)
	configured := strings.TrimSpace(service.feishuBot.appID)
	if !service.feishuBot.configured || configured == "" {
		return "", fmt.Errorf("%w: feishu bot is not configured", ErrOfficeMessageInvalid)
	}
	if requested == "" {
		return configured, nil
	}
	if requested != configured {
		return "", fmt.Errorf("%w: feishu bot is unavailable", ErrOfficeMessageInvalid)
	}
	return configured, nil
}

func (service *OfficeMessageService) ListMessages(ctx context.Context) ([]model.OfficeMessage, error) {
	var messages []model.OfficeMessage
	if err := service.db.WithContext(ctx).Order("id DESC").Limit(500).Find(&messages).Error; err != nil {
		return nil, fmt.Errorf("office message: list messages: %w", err)
	}
	return messages, nil
}

func (service *OfficeMessageService) CreateMessage(ctx context.Context, actorID uint, input OfficeMessageInput) (*model.OfficeMessage, error) {
	message, err := normalizeOfficeMessageInput(input)
	if err != nil {
		return nil, err
	}
	message.CreatedBy = actorID
	message.UpdatedBy = actorID
	if err := service.db.WithContext(ctx).Create(&message).Error; err != nil {
		return nil, fmt.Errorf("office message: create message: %w", err)
	}
	return &message, nil
}

func (service *OfficeMessageService) UpdateMessage(ctx context.Context, actorID, messageID uint, input OfficeMessageInput) (*model.OfficeMessage, error) {
	if messageID == 0 || input.ExpectedLockVersion == 0 {
		return nil, fmt.Errorf("%w: message id and lock version are required", ErrOfficeMessageInvalid)
	}
	normalized, err := normalizeOfficeMessageInput(input)
	if err != nil {
		return nil, err
	}
	updates := map[string]interface{}{
		"name": normalized.Name, "source_type": normalized.SourceType, "content": normalized.Content,
		"procedure_owner": normalized.ProcedureOwner, "package_name": normalized.PackageName,
		"procedure_name": normalized.ProcedureName, "procedure_overload": normalized.ProcedureOverload,
		"result_table_owner": normalized.ResultTableOwner, "result_table_name": normalized.ResultTableName,
		"select_sql": normalized.SelectSQL, "file_name_template": normalized.FileNameTemplate,
		"parameter_schema_json": normalized.ParameterSchemaJSON,
		"column_mapping_json":   normalized.ColumnMappingJSON, "enabled": normalized.Enabled,
		"updated_by": actorID, "lock_version": gorm.Expr("lock_version + 1"), "updated_at": service.now().UTC(),
	}
	result := service.db.WithContext(ctx).Model(&model.OfficeMessage{}).
		Where("id = ? AND lock_version = ?", messageID, input.ExpectedLockVersion).Updates(updates)
	if result.Error != nil {
		return nil, fmt.Errorf("office message: update message: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return nil, ErrOfficeMessageConflict
	}
	return service.getMessage(ctx, messageID)
}

func (service *OfficeMessageService) DeleteMessage(ctx context.Context, messageID uint, expectedLockVersion uint64) error {
	if messageID == 0 || expectedLockVersion == 0 {
		return fmt.Errorf("%w: message id and lock version are required", ErrOfficeMessageInvalid)
	}
	return service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var message model.OfficeMessage
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", messageID).First(&message).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrOfficeMessageNotFound
		} else if err != nil {
			return fmt.Errorf("office message: lock message for delete: %w", err)
		}
		var targets int64
		if err := tx.Model(&model.OfficePushTarget{}).Where("message_id = ?", messageID).Count(&targets).Error; err != nil {
			return fmt.Errorf("office message: count message targets: %w", err)
		}
		if targets > 0 {
			return fmt.Errorf("%w: message is used by push targets", ErrOfficeMessageConflict)
		}
		result := tx.Where("id = ? AND lock_version = ?", messageID, expectedLockVersion).Delete(&model.OfficeMessage{})
		if result.Error != nil {
			return fmt.Errorf("office message: delete message: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrOfficeMessageConflict
		}
		return nil
	})
}

func (service *OfficeMessageService) ListTargets(ctx context.Context) ([]model.OfficePushTarget, error) {
	var targets []model.OfficePushTarget
	if err := service.db.WithContext(ctx).Order("id DESC").Limit(500).Find(&targets).Error; err != nil {
		return nil, fmt.Errorf("office message: list push targets: %w", err)
	}
	for index := range targets {
		if effectiveOfficePushChannel(targets[index].Channel) == model.OfficePushChannelFeishu && targets[index].BotAppID == "" && service.feishuBot.configured {
			targets[index].BotAppID = service.feishuBot.appID
		}
		sanitizeOfficePushTarget(&targets[index])
	}
	return targets, nil
}

func (service *OfficeMessageService) CreateTarget(ctx context.Context, actorID uint, input OfficePushTargetInput) (*model.OfficePushTarget, error) {
	target, err := normalizeOfficePushTargetInput(input)
	if err != nil {
		return nil, err
	}
	if err := service.prepareOfficePushTargetCredential(&target, input, nil); err != nil {
		return nil, err
	}
	target.CreatedBy = actorID
	target.UpdatedBy = actorID
	if err := service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var message model.OfficeMessage
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", target.MessageID).First(&message).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrOfficeMessageNotFound
		} else if err != nil {
			return fmt.Errorf("office message: lock target message: %w", err)
		}
		if err := validateOfficePushTargetMessage(target, message); err != nil {
			return err
		}
		return tx.Create(&target).Error
	}); err != nil {
		return nil, fmt.Errorf("office message: create push target: %w", err)
	}
	sanitizeOfficePushTarget(&target)
	return &target, nil
}

func (service *OfficeMessageService) UpdateTarget(ctx context.Context, actorID, targetID uint, input OfficePushTargetInput) (*model.OfficePushTarget, error) {
	if targetID == 0 || input.ExpectedLockVersion == 0 {
		return nil, fmt.Errorf("%w: target id and lock version are required", ErrOfficeMessageInvalid)
	}
	target, err := normalizeOfficePushTargetInput(input)
	if err != nil {
		return nil, err
	}
	err = service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var message model.OfficeMessage
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", target.MessageID).First(&message).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrOfficeMessageNotFound
		} else if err != nil {
			return fmt.Errorf("office message: lock target message: %w", err)
		}
		var existing model.OfficePushTarget
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", targetID).First(&existing).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrOfficeMessageNotFound
		} else if err != nil {
			return fmt.Errorf("office message: lock push target: %w", err)
		}
		if err := service.prepareOfficePushTargetCredential(&target, input, &existing); err != nil {
			return err
		}
		if err := validateOfficePushTargetMessage(target, message); err != nil {
			return err
		}
		result := tx.Model(&model.OfficePushTarget{}).Where("id = ? AND lock_version = ?", targetID, input.ExpectedLockVersion).Updates(map[string]interface{}{
			"name": target.Name, "message_id": target.MessageID, "channel": target.Channel,
			"bot_app_id": target.BotAppID, "receive_id_type": target.ReceiveIDType, "receive_id": target.ReceiveID,
			"webdav_url": target.WebDAVURL, "webdav_username": target.WebDAVUsername, "webdav_path": target.WebDAVPath,
			"webdav_password_ciphertext": target.WebDAVPasswordCiphertext, "credential_key_version": target.CredentialKeyVersion,
			"enabled":    target.Enabled,
			"updated_by": actorID, "lock_version": gorm.Expr("lock_version + 1"), "updated_at": service.now().UTC(),
		})
		if result.Error != nil {
			return fmt.Errorf("office message: update push target: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrOfficeMessageConflict
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return service.getTarget(ctx, targetID)
}

func (service *OfficeMessageService) DeleteTarget(ctx context.Context, targetID uint, expectedLockVersion uint64) error {
	if targetID == 0 || expectedLockVersion == 0 {
		return fmt.Errorf("%w: target id and lock version are required", ErrOfficeMessageInvalid)
	}
	return service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var target model.OfficePushTarget
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", targetID).First(&target).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrOfficeMessageNotFound
		} else if err != nil {
			return fmt.Errorf("office message: lock push target for delete: %w", err)
		}
		var schedules int64
		if err := tx.Model(&model.OfficePushSchedule{}).Where("target_id = ?", targetID).Count(&schedules).Error; err != nil {
			return fmt.Errorf("office message: count target schedules: %w", err)
		}
		if schedules > 0 {
			return fmt.Errorf("%w: push target is used by schedules", ErrOfficeMessageConflict)
		}
		result := tx.Where("id = ? AND lock_version = ?", targetID, expectedLockVersion).Delete(&model.OfficePushTarget{})
		if result.Error != nil {
			return fmt.Errorf("office message: delete push target: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrOfficeMessageConflict
		}
		return nil
	})
}

func (service *OfficeMessageService) CreateRun(ctx context.Context, actorID, targetID uint, input OfficePushRunInput) (*model.OfficePushRun, error) {
	requestID, err := canonicalOfficeUUID(input.RequestID)
	if actorID == 0 || targetID == 0 || err != nil {
		return nil, fmt.Errorf("%w: actor and target are required", ErrOfficeMessageInvalid)
	}
	input.RequestID = requestID
	var created model.OfficePushRun
	err = service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing model.OfficePushRun
		if err := tx.Where("run_uuid = ?", input.RequestID).First(&existing).Error; err == nil {
			if err := validateOfficeRunReplay(existing, actorID, targetID, input.Parameters); err != nil {
				return err
			}
			created = existing
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("office message: read idempotent push run: %w", err)
		}
		var target model.OfficePushTarget
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND enabled = ?", targetID, true).First(&target).Error; err != nil {
			return fmt.Errorf("%w: push target is unavailable", ErrOfficeMessageNotFound)
		}
		var message model.OfficeMessage
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND enabled = ?", target.MessageID, true).First(&message).Error; err != nil {
			return fmt.Errorf("%w: message is unavailable", ErrOfficeMessageNotFound)
		}
		if err := service.preparePersistedOfficePushTarget(&target, message); err != nil {
			return err
		}
		parameters, err := normalizeOfficeRunParameters(message, input.Parameters)
		if err != nil {
			return err
		}
		snapshot, err := newOfficePushSnapshot(target, message)
		if err != nil {
			return err
		}
		created = model.OfficePushRun{
			RunUUID: input.RequestID, TargetID: target.ID, MessageID: message.ID,
			Status: model.OfficePushRunStatusQueued, TriggerType: model.OfficePushTriggerManual,
			RequestedBy: actorID, ParametersJSON: parameters, SnapshotJSON: snapshot,
		}
		if err := tx.Create(&created).Error; err != nil {
			return fmt.Errorf("office message: create push run: %w", err)
		}
		outbox, err := newOfficePushOutbox(created.ID, created.RunUUID, service.now().UTC())
		if err != nil {
			return err
		}
		if err := data_dao.NewAsyncJobOutboxDAO(tx).Create(ctx, &outbox); err != nil {
			return fmt.Errorf("office message: create push outbox: %w", err)
		}
		return nil
	})
	if err != nil {
		var existing model.OfficePushRun
		if lookupErr := service.db.WithContext(ctx).Where("run_uuid = ?", input.RequestID).First(&existing).Error; lookupErr == nil {
			if replayErr := validateOfficeRunReplay(existing, actorID, targetID, input.Parameters); replayErr != nil {
				return nil, replayErr
			}
			return &existing, nil
		}
		return nil, err
	}
	return &created, nil
}

func validateOfficeRunReplay(existing model.OfficePushRun, actorID, targetID uint, input map[string]string) error {
	if existing.TargetID != targetID || existing.RequestedBy != actorID {
		return ErrOfficeMessageConflict
	}
	snapshot, err := decodeOfficePushSnapshot(existing.SnapshotJSON)
	if err != nil {
		return fmt.Errorf("office message: validate idempotent push snapshot: %w", err)
	}
	message := snapshot.messageModel()
	parameters, err := normalizeOfficeRunParameters(message, input)
	if err != nil {
		return ErrOfficeMessageConflict
	}
	storedInput := make(map[string]string)
	if err := decodeOfficeJSON(existing.ParametersJSON, &storedInput); err != nil {
		return fmt.Errorf("office message: validate idempotent push parameters: %w", err)
	}
	storedParameters, err := normalizeOfficeRunParameters(message, storedInput)
	if err != nil || string(parameters) != string(storedParameters) {
		return ErrOfficeMessageConflict
	}
	return nil
}

func normalizeOfficeRunParameters(message model.OfficeMessage, input map[string]string) (model.JSONText, error) {
	if message.SourceType != model.OfficeMessageSourceOracleQuery {
		if len(input) > 0 {
			return "", fmt.Errorf("%w: this message source does not accept parameters", ErrOfficeMessageInvalid)
		}
		return model.JSONText("{}"), nil
	}
	schema, _, err := normalizeOfficeQueryParameters(message.SelectSQL, message.ParameterSchemaJSON)
	if err != nil {
		return "", err
	}
	parameters, _, err := normalizeOfficeParameterValues(schema, input)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrOfficeMessageInvalid, err)
	}
	return parameters, nil
}

func canonicalOfficeUUID(value string) (string, error) {
	parsed, err := uuid.Parse(strings.TrimSpace(value))
	if err != nil {
		return "", err
	}
	return parsed.String(), nil
}

func (service *OfficeMessageService) ListRuns(ctx context.Context, limit int) ([]model.OfficePushRun, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var runs []model.OfficePushRun
	if err := service.db.WithContext(ctx).Order("id DESC").Limit(limit).Find(&runs).Error; err != nil {
		return nil, fmt.Errorf("office message: list push runs: %w", err)
	}
	return runs, nil
}

func (service *OfficeMessageService) getMessage(ctx context.Context, id uint) (*model.OfficeMessage, error) {
	var message model.OfficeMessage
	if err := service.db.WithContext(ctx).Where("id = ?", id).First(&message).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrOfficeMessageNotFound
	} else if err != nil {
		return nil, fmt.Errorf("office message: get message: %w", err)
	}
	return &message, nil
}

func (service *OfficeMessageService) getTarget(ctx context.Context, id uint) (*model.OfficePushTarget, error) {
	var target model.OfficePushTarget
	if err := service.db.WithContext(ctx).Where("id = ?", id).First(&target).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrOfficeMessageNotFound
	} else if err != nil {
		return nil, fmt.Errorf("office message: get push target: %w", err)
	}
	sanitizeOfficePushTarget(&target)
	return &target, nil
}

func normalizeOfficeMessageInput(input OfficeMessageInput) (model.OfficeMessage, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.SourceType = strings.ToUpper(strings.TrimSpace(input.SourceType))
	if input.Name == "" || len(input.Name) > 128 {
		return model.OfficeMessage{}, fmt.Errorf("%w: message name is invalid", ErrOfficeMessageInvalid)
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	message := model.OfficeMessage{Name: input.Name, SourceType: input.SourceType, Enabled: enabled, LockVersion: 1}
	switch input.SourceType {
	case model.OfficeMessageSourceEdited:
		message.Content = strings.TrimSpace(input.Content)
		if message.Content == "" || len(message.Content) > 60_000 || len(input.Parameters) != 0 || len(input.ColumnMapping) != 0 || strings.TrimSpace(input.SelectSQL) != "" || hasOfficeProcedureInput(input) {
			return model.OfficeMessage{}, fmt.Errorf("%w: edited message content is invalid", ErrOfficeMessageInvalid)
		}
		message.ParameterSchemaJSON = model.JSONText("[]")
		message.ColumnMappingJSON = model.JSONText("[]")
	case model.OfficeMessageSourceOracleProcedure:
		if strings.TrimSpace(input.Content) != "" || strings.TrimSpace(input.SelectSQL) != "" || len(input.Parameters) != 0 {
			return model.OfficeMessage{}, fmt.Errorf("%w: procedure message contains unrelated fields", ErrOfficeMessageInvalid)
		}
		procedure, err := reportoracle.NormalizeProcedureRef(reportoracle.ProcedureRef{
			Owner: input.ProcedureOwner, Package: input.PackageName, Name: input.ProcedureName, Overload: input.ProcedureOverload,
		})
		if err != nil {
			return model.OfficeMessage{}, fmt.Errorf("%w: Oracle procedure is invalid", ErrOfficeMessageInvalid)
		}
		table, err := reportoracle.NormalizeResultTableRef(reportoracle.ResultTableRef{Owner: input.ResultTableOwner, Name: input.ResultTableName})
		if err != nil {
			return model.OfficeMessage{}, fmt.Errorf("%w: Oracle result table is invalid", ErrOfficeMessageInvalid)
		}
		_, mappingJSON, err := normalizeOfficeInputMappings(input.ColumnMapping, input.SourceType)
		if err != nil {
			return model.OfficeMessage{}, fmt.Errorf("%w: %v", ErrOfficeMessageInvalid, err)
		}
		message.ProcedureOwner, message.PackageName, message.ProcedureName, message.ProcedureOverload = procedure.Owner, procedure.Package, procedure.Name, procedure.Overload
		message.ResultTableOwner, message.ResultTableName = table.Owner, table.Name
		message.FileNameTemplate, err = normalizeOfficeWorkbookFileNameTemplate(input.FileNameTemplate, input.Name)
		if err != nil {
			return model.OfficeMessage{}, fmt.Errorf("%w: %v", ErrOfficeMessageInvalid, err)
		}
		message.ParameterSchemaJSON, message.ColumnMappingJSON = model.JSONText("[]"), mappingJSON
	case model.OfficeMessageSourceOracleQuery:
		message.SelectSQL = strings.TrimSpace(input.SelectSQL)
		if len(message.SelectSQL) == 0 || len(message.SelectSQL) > 60_000 || strings.TrimSpace(input.Content) != "" || hasOfficeProcedureInput(input) {
			return model.OfficeMessage{}, fmt.Errorf("%w: SELECT message contains invalid fields", ErrOfficeMessageInvalid)
		}
		parameterJSON, err := json.Marshal(input.Parameters)
		if err != nil {
			return model.OfficeMessage{}, fmt.Errorf("%w: query parameters are invalid", ErrOfficeMessageInvalid)
		}
		_, canonicalParameters, err := normalizeOfficeQueryParameters(message.SelectSQL, model.JSONText(parameterJSON))
		if err != nil {
			return model.OfficeMessage{}, fmt.Errorf("%w: %v", ErrOfficeMessageInvalid, err)
		}
		_, mappingJSON, err := normalizeOfficeInputMappings(input.ColumnMapping, input.SourceType)
		if err != nil {
			return model.OfficeMessage{}, fmt.Errorf("%w: %v", ErrOfficeMessageInvalid, err)
		}
		message.ParameterSchemaJSON, message.ColumnMappingJSON = canonicalParameters, mappingJSON
		message.FileNameTemplate, err = normalizeOfficeWorkbookFileNameTemplate(input.FileNameTemplate, input.Name)
		if err != nil {
			return model.OfficeMessage{}, fmt.Errorf("%w: %v", ErrOfficeMessageInvalid, err)
		}
	default:
		return model.OfficeMessage{}, fmt.Errorf("%w: message source type is unsupported", ErrOfficeMessageInvalid)
	}
	return message, nil
}

func hasOfficeProcedureInput(input OfficeMessageInput) bool {
	return strings.TrimSpace(input.ProcedureOwner) != "" || strings.TrimSpace(input.PackageName) != "" || strings.TrimSpace(input.ProcedureName) != "" ||
		strings.TrimSpace(input.ProcedureOverload) != "" || strings.TrimSpace(input.ResultTableOwner) != "" || strings.TrimSpace(input.ResultTableName) != ""
}

func normalizeOfficeInputMappings(mappings []OfficeColumnMapping, sourceType string) ([]OfficeColumnMapping, model.JSONText, error) {
	raw, err := json.Marshal(mappings)
	if err != nil {
		return nil, "", err
	}
	return normalizeOfficeColumnMappings(model.JSONText(raw), sourceType)
}

func normalizeOfficePushTargetInput(input OfficePushTargetInput) (model.OfficePushTarget, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Channel = strings.ToUpper(strings.TrimSpace(input.Channel))
	if input.Channel == "" {
		input.Channel = model.OfficePushChannelFeishu
	}
	input.BotAppID = strings.TrimSpace(input.BotAppID)
	input.ReceiveIDType = strings.ToLower(strings.TrimSpace(input.ReceiveIDType))
	input.ReceiveID = strings.TrimSpace(input.ReceiveID)
	if input.Name == "" || len(input.Name) > 128 || input.MessageID == 0 {
		return model.OfficePushTarget{}, fmt.Errorf("%w: push target is invalid", ErrOfficeMessageInvalid)
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	target := model.OfficePushTarget{Name: input.Name, MessageID: input.MessageID, Channel: input.Channel, Enabled: enabled, LockVersion: 1}
	switch input.Channel {
	case model.OfficePushChannelFeishu:
		if !validOfficeReceiveIDType(input.ReceiveIDType) || input.ReceiveID == "" || len(input.ReceiveID) > 255 || strings.ContainsAny(input.ReceiveID, "\r\n\t ") {
			return model.OfficePushTarget{}, fmt.Errorf("%w: Feishu push target is invalid", ErrOfficeMessageInvalid)
		}
		target.BotAppID, target.ReceiveIDType, target.ReceiveID = input.BotAppID, input.ReceiveIDType, input.ReceiveID
	case model.OfficePushChannelWebDAV:
		baseURL, err := webdavconnector.NormalizeBaseURL(input.WebDAVURL)
		if err != nil {
			return model.OfficePushTarget{}, fmt.Errorf("%w: WebDAV URL is invalid", ErrOfficeMessageInvalid)
		}
		directory, err := webdavconnector.NormalizeDirectory(input.WebDAVPath)
		input.WebDAVUsername = strings.TrimSpace(input.WebDAVUsername)
		if err != nil || input.WebDAVUsername == "" || len(input.WebDAVUsername) > 255 || len(directory) > 1024 {
			return model.OfficePushTarget{}, fmt.Errorf("%w: WebDAV push target is invalid", ErrOfficeMessageInvalid)
		}
		target.WebDAVURL, target.WebDAVUsername, target.WebDAVPath = baseURL, input.WebDAVUsername, directory
	default:
		return model.OfficePushTarget{}, fmt.Errorf("%w: push target channel is unsupported", ErrOfficeMessageInvalid)
	}
	return target, nil
}

func (service *OfficeMessageService) prepareOfficePushTargetCredential(target *model.OfficePushTarget, input OfficePushTargetInput, existing *model.OfficePushTarget) error {
	if target == nil {
		return fmt.Errorf("%w: push target is invalid", ErrOfficeMessageInvalid)
	}
	if target.Channel == model.OfficePushChannelFeishu {
		appID, err := service.resolveOfficeFeishuBot(target.BotAppID)
		if err != nil {
			return err
		}
		target.BotAppID = appID
		return nil
	}
	if service == nil || service.credential == nil {
		return fmt.Errorf("%w: WebDAV credential encryption is unavailable", ErrOfficeMessageInvalid)
	}
	if input.WebDAVPassword == "" {
		if existing == nil || effectiveOfficePushChannel(existing.Channel) != model.OfficePushChannelWebDAV ||
			existing.WebDAVPasswordCiphertext == "" || existing.CredentialKeyVersion == "" {
			return fmt.Errorf("%w: WebDAV application password is required", ErrOfficeMessageInvalid)
		}
		target.WebDAVPasswordCiphertext = existing.WebDAVPasswordCiphertext
		target.CredentialKeyVersion = existing.CredentialKeyVersion
		return nil
	}
	if len(input.WebDAVPassword) > 512 {
		return fmt.Errorf("%w: WebDAV application password is invalid", ErrOfficeMessageInvalid)
	}
	version, ciphertext, err := service.credential.EncryptScoped(officeWebDAVCredentialPurpose, input.WebDAVPassword)
	if err != nil {
		return fmt.Errorf("office message: encrypt WebDAV credential: %w", err)
	}
	target.WebDAVPasswordCiphertext, target.CredentialKeyVersion = ciphertext, version
	return nil
}

func (service *OfficeMessageService) preparePersistedOfficePushTarget(target *model.OfficePushTarget, message model.OfficeMessage) error {
	if target == nil {
		return fmt.Errorf("%w: push target is unavailable", ErrOfficeMessageNotFound)
	}
	target.Channel = effectiveOfficePushChannel(target.Channel)
	if target.Channel == model.OfficePushChannelFeishu {
		appID, err := service.resolveOfficeFeishuBot(target.BotAppID)
		if err != nil {
			return err
		}
		target.BotAppID = appID
	}
	return validateOfficePushTargetMessage(*target, message)
}

func validateOfficePushTargetMessage(target model.OfficePushTarget, message model.OfficeMessage) error {
	switch effectiveOfficePushChannel(target.Channel) {
	case model.OfficePushChannelFeishu:
		return nil
	case model.OfficePushChannelWebDAV:
		if message.SourceType == model.OfficeMessageSourceEdited || !validOfficeWebDAVTarget(target) {
			return fmt.Errorf("%w: WebDAV push requires an Excel message and complete credentials", ErrOfficeMessageInvalid)
		}
		return nil
	default:
		return fmt.Errorf("%w: push target channel is unsupported", ErrOfficeMessageInvalid)
	}
}

func validOfficeWebDAVTarget(target model.OfficePushTarget) bool {
	_, urlErr := webdavconnector.NormalizeBaseURL(target.WebDAVURL)
	_, pathErr := webdavconnector.NormalizeDirectory(target.WebDAVPath)
	return urlErr == nil && pathErr == nil && strings.TrimSpace(target.WebDAVUsername) != "" &&
		target.WebDAVPasswordCiphertext != "" && strings.TrimSpace(target.CredentialKeyVersion) != ""
}

func effectiveOfficePushChannel(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "" {
		return model.OfficePushChannelFeishu
	}
	return value
}

func sanitizeOfficePushTarget(target *model.OfficePushTarget) {
	if target == nil {
		return
	}
	target.Channel = effectiveOfficePushChannel(target.Channel)
	target.HasWebDAVPassword = target.WebDAVPasswordCiphertext != ""
}

func validOfficeReceiveIDType(value string) bool {
	switch value {
	case "chat_id", "open_id", "user_id", "union_id", "email":
		return true
	default:
		return false
	}
}

func newOfficePushOutbox(runID uint, runUUID string, availableAt time.Time) (model.AsyncJobOutbox, error) {
	canonicalRunUUID, err := canonicalOfficeUUID(runUUID)
	if runID == 0 || err != nil || availableAt.IsZero() {
		return model.AsyncJobOutbox{}, fmt.Errorf("office message: invalid push outbox identity")
	}
	payload, err := json.Marshal(job.OfficePushTaskPayload{RunID: runID})
	if err != nil {
		return model.AsyncJobOutbox{}, fmt.Errorf("office message: encode push outbox: %w", err)
	}
	if _, err := job.DecodeOfficePushTaskPayload(payload); err != nil {
		return model.AsyncJobOutbox{}, err
	}
	return model.AsyncJobOutbox{
		TaskKey: "office:push:" + canonicalRunUUID, TaskType: job.TypeOfficePush,
		PayloadJSON: model.JSONText(payload), QueueName: job.OfficePushQueueName, AvailableAt: availableAt.UTC(),
	}, nil
}
