package auth_svc

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"gin-biz-web-api/constant"
	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/internal/requests/auth_request"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/database"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	dataAuthorizationCreateScope  = "data_authorization.account.create"
	dataAuthorizationGrantScope   = "data_authorization.permission.grant"
	dataAuthorizationRevokeScope  = "data_authorization.permission.revoke"
	dataAuthorizationReissueScope = "data_authorization.token.reissue"
	dataAuthorizationMaxPageSize  = 100
)

var (
	ErrDataAuthorizationForbidden           = errors.New("data authorization: forbidden")
	ErrDataAuthorizationInvalidInput        = errors.New("data authorization: invalid input")
	ErrDataAuthorizationNotFound            = errors.New("data authorization: not found")
	ErrDataAuthorizationConflict            = errors.New("data authorization: conflict")
	ErrDataAuthorizationIdempotencyConflict = errors.New("data authorization: idempotency conflict")
	ErrDataAuthorizationIdempotencyPending  = errors.New("data authorization: idempotency pending")

	dataAuthorizationAccountPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{2,39}$`)
	dataAuthorizationKeyPattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{7,254}$`)
)

type DataAuthorizationPermissionDTO struct {
	Permission string     `json:"permission"`
	Label      string     `json:"label"`
	Scope      string     `json:"scope"`
	Status     string     `json:"status"`
	Permanent  bool       `json:"permanent"`
	ExpiresAt  *time.Time `json:"expiresAt,omitempty"`
}

type DataAuthorizationAccountDTO struct {
	ID               uint                             `json:"id"`
	Account          string                           `json:"account"`
	Nickname         string                           `json:"nickname"`
	CredentialStatus string                           `json:"credentialStatus"`
	TokenPrefix      string                           `json:"tokenPrefix,omitempty"`
	IssuedAt         *time.Time                       `json:"issuedAt,omitempty"`
	Permissions      []DataAuthorizationPermissionDTO `json:"permissions"`
	CreatedAt        time.Time                        `json:"createdAt"`
}

type DataAuthorizationPagination struct {
	PageSize     int  `json:"pageSize"`
	NextBeforeID uint `json:"nextBeforeId,omitempty"`
	HasMore      bool `json:"hasMore"`
}

type DataAuthorizationAccountQueryResult struct {
	Accounts   []DataAuthorizationAccountDTO `json:"accounts"`
	Pagination DataAuthorizationPagination   `json:"pagination"`
}

type DataAuthorizationAccountCreateResult struct {
	Account               DataAuthorizationAccountDTO `json:"account"`
	Token                 string                      `json:"token,omitempty"`
	OneTimeTokenAvailable bool                        `json:"oneTimeTokenAvailable"`
	Replayed              bool                        `json:"replayed"`
}

type DataAuthorizationMutationResult struct {
	TargetUserID uint                           `json:"targetUserId"`
	Permission   DataAuthorizationPermissionDTO `json:"permission"`
	Action       string                         `json:"action"`
	Changed      bool                           `json:"changed"`
	Replayed     bool                           `json:"replayed"`
}

type DataAuthorizationTokenReissueResult struct {
	TargetUserID          uint      `json:"targetUserId"`
	Token                 string    `json:"token,omitempty"`
	TokenPrefix           string    `json:"tokenPrefix"`
	IssuedAt              time.Time `json:"issuedAt"`
	OneTimeTokenAvailable bool      `json:"oneTimeTokenAvailable"`
	Replayed              bool      `json:"replayed"`
}

type DataAuthorizationAuditDTO struct {
	ID            uint       `json:"id"`
	TargetUserID  uint       `json:"targetUserId"`
	TargetAccount string     `json:"targetAccount"`
	Permission    string     `json:"permission"`
	Action        string     `json:"action"`
	OldExpiresAt  *time.Time `json:"oldExpiresAt,omitempty"`
	NewExpiresAt  *time.Time `json:"newExpiresAt,omitempty"`
	ActorUserID   uint       `json:"actorUserId"`
	Reason        string     `json:"reason"`
	CreatedAt     time.Time  `json:"createdAt"`
}

type DataAuthorizationAuditQueryResult struct {
	Audits     []DataAuthorizationAuditDTO `json:"audits"`
	Pagination DataAuthorizationPagination `json:"pagination"`
}

type DataAuthorizationService struct {
	db            *gorm.DB
	now           func() time.Time
	generateToken func() (string, error)
}

func NewDataAuthorizationService() *DataAuthorizationService {
	return &DataAuthorizationService{db: database.DB, now: time.Now, generateToken: generateOpenAPIToken}
}

func IsTrustedConsoleAdmin(user *model.User) bool { return isTrustedConsoleAdmin(user) }

func (service *DataAuthorizationService) QueryAccounts(ctx context.Context, actorUserID uint, request auth_request.DataAuthorizationAccountQueryRequest) (*DataAuthorizationAccountQueryResult, error) {
	if err := service.authorizeAdmin(ctx, service.db, actorUserID, false); err != nil {
		return nil, err
	}
	pageSize := normalizeDataAuthorizationPageSize(request.PageSize)
	query := service.db.WithContext(ctx).Model(&model.User{}).
		Select("users.id", "users.account", "users.nickname", "users.created_at").
		Joins("JOIN open_api_credentials ON open_api_credentials.user_id = users.id").
		Where("users.console_managed = ?", false).
		Where("users.account_type = ?", model.AccountTypeOpenAPI).
		Group("users.id, users.account, users.nickname, users.created_at")
	if request.BeforeID > 0 {
		query = query.Where("users.id < ?", request.BeforeID)
	}
	keyword := strings.TrimSpace(request.Keyword)
	if keyword != "" {
		if utf8.RuneCountInString(keyword) > 80 {
			return nil, ErrDataAuthorizationInvalidInput
		}
		like := "%" + strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(keyword) + "%"
		query = query.Where("(users.account LIKE ? ESCAPE '\\\\' OR users.nickname LIKE ? ESCAPE '\\\\')", like, like)
	}
	var users []model.User
	if err := query.Order("users.id DESC").Limit(pageSize + 1).Find(&users).Error; err != nil {
		return nil, fmt.Errorf("data authorization: query accounts: %w", err)
	}
	hasMore := len(users) > pageSize
	if hasMore {
		users = users[:pageSize]
	}
	result, err := service.buildAccountResult(ctx, users)
	if err != nil {
		return nil, err
	}
	result.Pagination = DataAuthorizationPagination{PageSize: pageSize, HasMore: hasMore}
	if hasMore && len(users) > 0 {
		result.Pagination.NextBeforeID = users[len(users)-1].ID
	}
	return result, nil
}

func (service *DataAuthorizationService) CreateAccount(ctx context.Context, actorUserID uint, idempotencyKey string, request auth_request.DataAuthorizationAccountCreateRequest) (*DataAuthorizationAccountCreateResult, error) {
	normalized, grants, err := service.normalizeCreate(request)
	if err != nil || !validDataAuthorizationKey(idempotencyKey) {
		return nil, ErrDataAuthorizationInvalidInput
	}
	requestHash, keyHash, err := dataAuthorizationHashes(normalized, idempotencyKey)
	if err != nil {
		return nil, err
	}
	var result *DataAuthorizationAccountCreateResult
	err = service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := service.authorizeAdmin(ctx, tx, actorUserID, true); err != nil {
			return err
		}
		record, replayed, err := reserveDataAuthorization(ctx, tx, dataAuthorizationCreateScope, actorUserID, keyHash, requestHash)
		if err != nil {
			return err
		}
		if replayed {
			if err := json.Unmarshal([]byte(record.ResponseJSON), &result); err != nil {
				return fmt.Errorf("data authorization: decode account replay: %w", err)
			}
			result.Replayed = true
			return nil
		}
		password, err := randomSecret(32)
		if err != nil {
			return err
		}
		now := service.now().UTC().Truncate(time.Millisecond)
		user := model.User{BaseModel: &model.BaseModel{}, CommonTimestampsField: &model.CommonTimestampsField{CreatedAt: int(now.Unix()), UpdatedAt: int(now.Unix())}, Account: normalized.Account, Nickname: normalized.Nickname, Password: password, AccountType: model.AccountTypeOpenAPI, Status: model.AccountStatusActive, AuthVersion: 1, MallScopeMode: model.MallScopeAll}
		if err := createDataAuthorizationUser(ctx, tx, &user); err != nil {
			if isDuplicateEntry(err) {
				return ErrDataAuthorizationConflict
			}
			return fmt.Errorf("data authorization: create user: %w", err)
		}
		token, err := service.generateToken()
		if err != nil {
			return err
		}
		credential := model.OpenAPICredential{UserID: user.ID, TokenHash: tokenDigest(token), TokenPrefix: tokenDisplayPrefix(token), Status: model.OpenAPICredentialStatusActive, IssuedBy: actorUserID, IssuedAt: now}
		if err := tx.WithContext(ctx).Create(&credential).Error; err != nil {
			return fmt.Errorf("data authorization: create credential: %w", err)
		}
		if err := createDataAuthorizationAudit(ctx, tx, user.ID, "open_api.account", model.DataAuthorizationActionAccountCreate, nil, nil, actorUserID, normalized.Reason, keyHash); err != nil {
			return err
		}
		for _, grant := range grants {
			if err := upsertDataPermission(ctx, tx, user.ID, actorUserID, grant.Permission, grant.ExpiresAt); err != nil {
				return err
			}
			if err := createDataAuthorizationAudit(ctx, tx, user.ID, grant.Permission, model.DataAuthorizationActionGrant, nil, grant.ExpiresAt, actorUserID, normalized.Reason, keyHash); err != nil {
				return err
			}
		}
		account := accountDTO(user, credential, permissionDTOsFromGrants(grants, now))
		result = &DataAuthorizationAccountCreateResult{Account: account, Token: token, OneTimeTokenAvailable: true}
		snapshot := *result
		snapshot.Token, snapshot.OneTimeTokenAvailable = "", false
		if err := completeDataAuthorization(ctx, tx, record.ID, user.ID, &snapshot); err != nil {
			return err
		}
		return nil
	})
	return result, err
}

func createDataAuthorizationUser(ctx context.Context, db *gorm.DB, user *model.User) error {
	// API-only accounts have neither an email nor a phone. Both columns are
	// nullable and unique, so NULL avoids synthetic identities and collisions.
	return db.WithContext(ctx).Omit("Email", "Phone").Create(user).Error
}

func (service *DataAuthorizationService) Grant(ctx context.Context, actorUserID, targetUserID uint, idempotencyKey string, request auth_request.DataAuthorizationGrantRequest) (*DataAuthorizationMutationResult, error) {
	permission, expiresAt, reason, err := normalizeDataAuthorizationGrant(request.Permission, request.ExpiresAt, request.Permanent, request.Reason, service.now())
	if err != nil || targetUserID == 0 || !validDataAuthorizationKey(idempotencyKey) {
		return nil, ErrDataAuthorizationInvalidInput
	}
	normalized := struct {
		TargetUserID uint       `json:"targetUserId"`
		Permission   string     `json:"permission"`
		ExpiresAt    *time.Time `json:"expiresAt"`
		Reason       string     `json:"reason"`
	}{targetUserID, permission, expiresAt, reason}
	requestHash, keyHash, err := dataAuthorizationHashes(normalized, idempotencyKey)
	if err != nil {
		return nil, err
	}
	var result *DataAuthorizationMutationResult
	err = service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := service.authorizeAdmin(ctx, tx, actorUserID, true); err != nil {
			return err
		}
		record, replayed, err := reserveDataAuthorization(ctx, tx, dataAuthorizationGrantScope, actorUserID, keyHash, requestHash)
		if err != nil {
			return err
		}
		if replayed {
			if err := json.Unmarshal([]byte(record.ResponseJSON), &result); err != nil {
				return err
			}
			result.Replayed = true
			return nil
		}
		if err := lockOpenAccount(ctx, tx, targetUserID); err != nil {
			return err
		}
		var existing model.MallWeatherUserPermission
		findErr := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND permission = ?", targetUserID, permission).First(&existing).Error
		if findErr != nil && !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return findErr
		}
		action := model.DataAuthorizationActionGrant
		var old *time.Time
		if findErr == nil {
			action = model.DataAuthorizationActionRenew
			old = existing.ExpiresAt
		}
		if err := upsertDataPermission(ctx, tx, targetUserID, actorUserID, permission, expiresAt); err != nil {
			return err
		}
		audit, err := createDataAuthorizationAuditRecord(ctx, tx, targetUserID, permission, action, old, expiresAt, actorUserID, reason, keyHash)
		if err != nil {
			return err
		}
		result = &DataAuthorizationMutationResult{TargetUserID: targetUserID, Permission: permissionDTO(permission, expiresAt, true, service.now()), Action: action, Changed: true}
		return completeDataAuthorization(ctx, tx, record.ID, audit.ID, result)
	})
	return result, err
}

func (service *DataAuthorizationService) Revoke(ctx context.Context, actorUserID, targetUserID uint, idempotencyKey string, request auth_request.DataAuthorizationRevokeRequest) (*DataAuthorizationMutationResult, error) {
	permission, reason, err := normalizeDataAuthorizationRevoke(request.Permission, request.Reason)
	if err != nil || targetUserID == 0 || !validDataAuthorizationKey(idempotencyKey) {
		return nil, ErrDataAuthorizationInvalidInput
	}
	normalized := struct {
		TargetUserID uint   `json:"targetUserId"`
		Permission   string `json:"permission"`
		Reason       string `json:"reason"`
	}{targetUserID, permission, reason}
	requestHash, keyHash, err := dataAuthorizationHashes(normalized, idempotencyKey)
	if err != nil {
		return nil, err
	}
	var result *DataAuthorizationMutationResult
	err = service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := service.authorizeAdmin(ctx, tx, actorUserID, true); err != nil {
			return err
		}
		record, replayed, err := reserveDataAuthorization(ctx, tx, dataAuthorizationRevokeScope, actorUserID, keyHash, requestHash)
		if err != nil {
			return err
		}
		if replayed {
			if err := json.Unmarshal([]byte(record.ResponseJSON), &result); err != nil {
				return err
			}
			result.Replayed = true
			return nil
		}
		if err := lockOpenAccount(ctx, tx, targetUserID); err != nil {
			return err
		}
		var existing model.MallWeatherUserPermission
		findErr := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND permission = ?", targetUserID, permission).First(&existing).Error
		if findErr != nil && !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return findErr
		}
		changed := findErr == nil
		var old *time.Time
		if changed {
			old = existing.ExpiresAt
			if err := tx.WithContext(ctx).Delete(&existing).Error; err != nil {
				return err
			}
		}
		audit, err := createDataAuthorizationAuditRecord(ctx, tx, targetUserID, permission, model.DataAuthorizationActionRevoke, old, nil, actorUserID, reason, keyHash)
		if err != nil {
			return err
		}
		result = &DataAuthorizationMutationResult{TargetUserID: targetUserID, Permission: permissionDTO(permission, nil, false, service.now()), Action: model.DataAuthorizationActionRevoke, Changed: changed}
		return completeDataAuthorization(ctx, tx, record.ID, audit.ID, result)
	})
	return result, err
}

func (service *DataAuthorizationService) ReissueToken(ctx context.Context, actorUserID, targetUserID uint, idempotencyKey string, request auth_request.DataAuthorizationTokenReissueRequest) (*DataAuthorizationTokenReissueResult, error) {
	reason, err := normalizeDataAuthorizationReason(request.Reason)
	if err != nil || targetUserID == 0 || !validDataAuthorizationKey(idempotencyKey) {
		return nil, ErrDataAuthorizationInvalidInput
	}
	normalized := struct {
		TargetUserID uint   `json:"targetUserId"`
		Reason       string `json:"reason"`
	}{targetUserID, reason}
	requestHash, keyHash, err := dataAuthorizationHashes(normalized, idempotencyKey)
	if err != nil {
		return nil, err
	}
	var result *DataAuthorizationTokenReissueResult
	err = service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := service.authorizeAdmin(ctx, tx, actorUserID, true); err != nil {
			return err
		}
		record, replayed, err := reserveDataAuthorization(ctx, tx, dataAuthorizationReissueScope, actorUserID, keyHash, requestHash)
		if err != nil {
			return err
		}
		if replayed {
			if err := json.Unmarshal([]byte(record.ResponseJSON), &result); err != nil {
				return err
			}
			result.Replayed = true
			return nil
		}
		if err := lockOpenAccount(ctx, tx, targetUserID); err != nil {
			return err
		}
		now := service.now().UTC().Truncate(time.Millisecond)
		if err := tx.WithContext(ctx).Model(&model.OpenAPICredential{}).Where("user_id = ? AND status = ?", targetUserID, model.OpenAPICredentialStatusActive).Updates(map[string]interface{}{"status": model.OpenAPICredentialStatusRevoked, "revoked_at": now}).Error; err != nil {
			return err
		}
		token, err := service.generateToken()
		if err != nil {
			return err
		}
		credential := model.OpenAPICredential{UserID: targetUserID, TokenHash: tokenDigest(token), TokenPrefix: tokenDisplayPrefix(token), Status: model.OpenAPICredentialStatusActive, IssuedBy: actorUserID, IssuedAt: now}
		if err := tx.WithContext(ctx).Create(&credential).Error; err != nil {
			return err
		}
		audit, err := createDataAuthorizationAuditRecord(ctx, tx, targetUserID, "open_api.credential", model.DataAuthorizationActionTokenReissue, nil, nil, actorUserID, reason, keyHash)
		if err != nil {
			return err
		}
		result = &DataAuthorizationTokenReissueResult{TargetUserID: targetUserID, Token: token, TokenPrefix: credential.TokenPrefix, IssuedAt: now, OneTimeTokenAvailable: true}
		snapshot := *result
		snapshot.Token, snapshot.OneTimeTokenAvailable = "", false
		return completeDataAuthorization(ctx, tx, record.ID, audit.ID, &snapshot)
	})
	return result, err
}

func (service *DataAuthorizationService) QueryAudits(ctx context.Context, actorUserID uint, request auth_request.DataAuthorizationAuditQueryRequest) (*DataAuthorizationAuditQueryResult, error) {
	startTime, endTime, err := normalizeDataAuthorizationAuditTimeRange(request.StartTime, request.EndTime)
	if err != nil {
		return nil, err
	}
	if err := service.authorizeAdmin(ctx, service.db, actorUserID, false); err != nil {
		return nil, err
	}
	pageSize := normalizeDataAuthorizationPageSize(request.PageSize)
	query, err := buildDataAuthorizationAuditQuery(service.db.WithContext(ctx), request, startTime, endTime)
	if err != nil {
		return nil, err
	}
	var audits []model.DataAuthorizationAudit
	if err := query.Order("id DESC").Limit(pageSize + 1).Find(&audits).Error; err != nil {
		return nil, err
	}
	hasMore := len(audits) > pageSize
	if hasMore {
		audits = audits[:pageSize]
	}
	ids := make([]uint, 0, len(audits))
	for _, audit := range audits {
		ids = append(ids, audit.TargetUserID)
	}
	accounts := map[uint]string{}
	if len(ids) > 0 {
		var users []model.User
		if err := service.db.WithContext(ctx).Select("id", "account").Where("id IN ?", ids).Find(&users).Error; err != nil {
			return nil, err
		}
		for _, user := range users {
			accounts[user.ID] = user.Account
		}
	}
	items := make([]DataAuthorizationAuditDTO, 0, len(audits))
	for _, audit := range audits {
		items = append(items, DataAuthorizationAuditDTO{ID: audit.ID, TargetUserID: audit.TargetUserID, TargetAccount: accounts[audit.TargetUserID], Permission: audit.Permission, Action: audit.Action, OldExpiresAt: audit.OldExpiresAt, NewExpiresAt: audit.NewExpiresAt, ActorUserID: audit.ActorUserID, Reason: audit.Reason, CreatedAt: audit.CreatedAt})
	}
	result := &DataAuthorizationAuditQueryResult{Audits: items, Pagination: DataAuthorizationPagination{PageSize: pageSize, HasMore: hasMore}}
	if hasMore && len(audits) > 0 {
		result.Pagination.NextBeforeID = audits[len(audits)-1].ID
	}
	return result, nil
}

func buildDataAuthorizationAuditQuery(query *gorm.DB, request auth_request.DataAuthorizationAuditQueryRequest, startTime, endTime *time.Time) (*gorm.DB, error) {
	query = query.Model(&model.DataAuthorizationAudit{})
	if request.BeforeID > 0 {
		query = query.Where("id < ?", request.BeforeID)
	}
	if request.TargetUserID > 0 {
		query = query.Where("target_user_id = ?", request.TargetUserID)
	}
	if p := strings.TrimSpace(request.Permission); p != "" {
		if !grantableDataPermission(p) && p != "open_api.account" && p != "open_api.credential" {
			return nil, ErrDataAuthorizationInvalidInput
		}
		query = query.Where("permission = ?", p)
	}
	if a := strings.TrimSpace(request.Action); a != "" {
		if !validDataAuthorizationAuditAction(a) {
			return nil, ErrDataAuthorizationInvalidInput
		}
		query = query.Where("action = ?", a)
	}
	if startTime != nil {
		query = query.Where("created_at >= ?", *startTime)
	}
	if endTime != nil {
		query = query.Where("created_at <= ?", *endTime)
	}
	return query, nil
}

type normalizedDataAuthorizationCreate struct {
	Account     string                                          `json:"account"`
	Nickname    string                                          `json:"nickname"`
	Permissions []auth_request.DataAuthorizationPermissionInput `json:"permissions"`
	Reason      string                                          `json:"reason"`
}
type normalizedDataGrant struct {
	Permission string
	ExpiresAt  *time.Time
}

func (service *DataAuthorizationService) normalizeCreate(request auth_request.DataAuthorizationAccountCreateRequest) (normalizedDataAuthorizationCreate, []normalizedDataGrant, error) {
	account := strings.ToLower(strings.TrimSpace(request.Account))
	nickname := strings.TrimSpace(request.Nickname)
	reason, err := normalizeDataAuthorizationReason(request.Reason)
	if err != nil || !dataAuthorizationAccountPattern.MatchString(account) || constant.IsConsoleAdminAccount(account) || utf8.RuneCountInString(nickname) < 1 || utf8.RuneCountInString(nickname) > 64 || len(request.Permissions) > len(model.GrantableDataPermissions()) {
		return normalizedDataAuthorizationCreate{}, nil, ErrDataAuthorizationInvalidInput
	}
	grants := make([]normalizedDataGrant, 0, len(request.Permissions))
	seen := map[string]struct{}{}
	normalizedInputs := make([]auth_request.DataAuthorizationPermissionInput, 0, len(request.Permissions))
	for _, input := range request.Permissions {
		p, expiry, _, err := normalizeDataAuthorizationGrant(input.Permission, input.ExpiresAt, input.Permanent, reason, service.now())
		if err != nil {
			return normalizedDataAuthorizationCreate{}, nil, err
		}
		if _, ok := seen[p]; ok {
			return normalizedDataAuthorizationCreate{}, nil, ErrDataAuthorizationInvalidInput
		}
		seen[p] = struct{}{}
		grants = append(grants, normalizedDataGrant{Permission: p, ExpiresAt: expiry})
		normalizedInput := auth_request.DataAuthorizationPermissionInput{Permission: p, Permanent: expiry == nil}
		if expiry != nil {
			normalizedInput.ExpiresAt = expiry.Format(time.RFC3339Nano)
		}
		normalizedInputs = append(normalizedInputs, normalizedInput)
	}
	return normalizedDataAuthorizationCreate{account, nickname, normalizedInputs, reason}, grants, nil
}

func (service *DataAuthorizationService) authorizeAdmin(ctx context.Context, db *gorm.DB, actorUserID uint, lock bool) error {
	if ctx == nil || db == nil || actorUserID == 0 {
		return ErrDataAuthorizationForbidden
	}
	var actor model.User
	query := db.WithContext(ctx)
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := query.First(&actor, actorUserID).Error; err != nil {
		return ErrDataAuthorizationForbidden
	}
	allowed, err := NewAuthorizer(db).HasPermission(ctx, actor, model.PermissionSystemAccountManage)
	if err != nil || !allowed {
		return ErrDataAuthorizationForbidden
	}
	return nil
}

func (service *DataAuthorizationService) buildAccountResult(ctx context.Context, users []model.User) (*DataAuthorizationAccountQueryResult, error) {
	result := &DataAuthorizationAccountQueryResult{Accounts: make([]DataAuthorizationAccountDTO, 0, len(users))}
	if len(users) == 0 {
		return result, nil
	}
	ids := make([]uint, 0, len(users))
	for _, user := range users {
		ids = append(ids, user.ID)
	}
	var credentials []model.OpenAPICredential
	if err := service.db.WithContext(ctx).Where("user_id IN ? AND status = ?", ids, model.OpenAPICredentialStatusActive).Order("issued_at DESC").Find(&credentials).Error; err != nil {
		return nil, err
	}
	credentialByUser := map[uint]model.OpenAPICredential{}
	for _, credential := range credentials {
		if _, ok := credentialByUser[credential.UserID]; !ok {
			credentialByUser[credential.UserID] = credential
		}
	}
	var permissions []model.MallWeatherUserPermission
	if err := service.db.WithContext(ctx).Where("user_id IN ? AND permission IN ?", ids, model.GrantableDataPermissions()).Find(&permissions).Error; err != nil {
		return nil, err
	}
	permissionByUser := map[uint]map[string]*time.Time{}
	for _, p := range permissions {
		if permissionByUser[p.UserID] == nil {
			permissionByUser[p.UserID] = map[string]*time.Time{}
		}
		permissionByUser[p.UserID][p.Permission] = p.ExpiresAt
	}
	now := service.now()
	for _, user := range users {
		credential := credentialByUser[user.ID]
		dto := accountDTO(user, credential, permissionDTOs(permissionByUser[user.ID], now))
		result.Accounts = append(result.Accounts, dto)
	}
	return result, nil
}

func accountDTO(user model.User, credential model.OpenAPICredential, permissions []DataAuthorizationPermissionDTO) DataAuthorizationAccountDTO {
	status := model.OpenAPICredentialStatusRevoked
	var issuedAt *time.Time
	if credential.ID > 0 {
		status = credential.Status
		issued := credential.IssuedAt
		issuedAt = &issued
	}
	return DataAuthorizationAccountDTO{ID: user.ID, Account: user.Account, Nickname: user.Nickname, CredentialStatus: status, TokenPrefix: credential.TokenPrefix, IssuedAt: issuedAt, Permissions: permissions, CreatedAt: time.Unix(int64(user.CreatedAt), 0).UTC()}
}

func permissionDTOs(values map[string]*time.Time, now time.Time) []DataAuthorizationPermissionDTO {
	result := make([]DataAuthorizationPermissionDTO, 0, 2)
	for _, p := range model.GrantableDataPermissions() {
		expiresAt, granted := values[p]
		result = append(result, permissionDTO(p, expiresAt, granted, now))
	}
	return result
}
func permissionDTOsFromGrants(grants []normalizedDataGrant, now time.Time) []DataAuthorizationPermissionDTO {
	values := map[string]*time.Time{}
	for i := range grants {
		values[grants[i].Permission] = grants[i].ExpiresAt
	}
	return permissionDTOs(values, now)
}
func permissionDTO(permission string, expiresAt *time.Time, granted bool, now time.Time) DataAuthorizationPermissionDTO {
	label := "天气数据查询"
	if permission == model.PermissionBojunOrderRead {
		label = "Bojun 订单查询"
	}
	status := "NOT_GRANTED"
	if granted {
		status = "ACTIVE"
		if expiresAt != nil && !expiresAt.After(now.UTC()) {
			status = "EXPIRED"
		}
	}
	return DataAuthorizationPermissionDTO{
		Permission: permission, Label: label, Scope: "全模块数据", Status: status,
		Permanent: granted && expiresAt == nil, ExpiresAt: expiresAt,
	}
}

func lockOpenAccount(ctx context.Context, tx *gorm.DB, targetUserID uint) error {
	var user model.User
	if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, targetUserID).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrDataAuthorizationNotFound
	} else if err != nil {
		return err
	}
	if user.ConsoleManaged || constant.IsConsoleAdminAccount(user.Account) {
		return ErrDataAuthorizationForbidden
	}
	var count int64
	if err := tx.WithContext(ctx).Model(&model.OpenAPICredential{}).Where("user_id = ?", targetUserID).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return ErrDataAuthorizationNotFound
	}
	return nil
}
func upsertDataPermission(ctx context.Context, tx *gorm.DB, userID, actorID uint, permission string, expiresAt *time.Time) error {
	grant := model.MallWeatherUserPermission{UserID: userID, Permission: permission, GrantedBy: actorID, ExpiresAt: expiresAt}
	return tx.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}, {Name: "permission"}}, DoUpdates: clause.AssignmentColumns([]string{"granted_by", "expires_at", "updated_at"})}).Create(&grant).Error
}

func reserveDataAuthorization(ctx context.Context, tx *gorm.DB, scope string, actor uint, keyHash, requestHash string) (*model.APIIdempotencyRecord, bool, error) {
	dao := data_dao.NewAPIIdempotencyDAO(tx)
	record := &model.APIIdempotencyRecord{OperationScope: scope, ActorUserID: actor, KeyHash: keyHash, RequestHash: requestHash, ResourceType: "data_authorization", ResponseJSON: model.JSONText(`{}`)}
	reserved, err := dao.Reserve(ctx, record)
	if err != nil {
		return nil, false, err
	}
	if reserved {
		return record, false, nil
	}
	existing, err := dao.FindForUpdate(ctx, scope, actor, keyHash)
	if err != nil {
		return nil, false, err
	}
	if existing.RequestHash != requestHash {
		return nil, false, ErrDataAuthorizationIdempotencyConflict
	}
	if existing.ResourceID == 0 || existing.HTTPStatus == 0 || existing.ResponseJSON == "" || existing.ResponseJSON == model.JSONText(`{}`) {
		return nil, false, ErrDataAuthorizationIdempotencyPending
	}
	return existing, true, nil
}
func completeDataAuthorization(ctx context.Context, tx *gorm.DB, recordID, resourceID uint, response interface{}) error {
	data, err := json.Marshal(response)
	if err != nil {
		return err
	}
	return data_dao.NewAPIIdempotencyDAO(tx).Complete(ctx, recordID, resourceID, http.StatusOK, model.JSONText(data))
}
func createDataAuthorizationAudit(ctx context.Context, tx *gorm.DB, target uint, permission, action string, oldExpiry, newExpiry *time.Time, actor uint, reason, keyHash string) error {
	_, err := createDataAuthorizationAuditRecord(ctx, tx, target, permission, action, oldExpiry, newExpiry, actor, reason, keyHash)
	return err
}
func createDataAuthorizationAuditRecord(ctx context.Context, tx *gorm.DB, target uint, permission, action string, oldExpiry, newExpiry *time.Time, actor uint, reason, keyHash string) (*model.DataAuthorizationAudit, error) {
	audit := &model.DataAuthorizationAudit{TargetUserID: target, Permission: permission, Action: action, OldExpiresAt: oldExpiry, NewExpiresAt: newExpiry, ActorUserID: actor, Reason: reason, IdempotencyKeyHash: keyHash}
	if err := tx.WithContext(ctx).Create(audit).Error; err != nil {
		return nil, fmt.Errorf("data authorization: create audit: %w", err)
	}
	return audit, nil
}

func normalizeDataAuthorizationGrant(permission, expiresAt string, permanent bool, reason string, now time.Time) (string, *time.Time, string, error) {
	permission = strings.TrimSpace(permission)
	expiresAt = strings.TrimSpace(expiresAt)
	reason, err := normalizeDataAuthorizationReason(reason)
	if err != nil || !grantableDataPermission(permission) {
		return "", nil, "", ErrDataAuthorizationInvalidInput
	}
	if permanent {
		if expiresAt != "" {
			return "", nil, "", ErrDataAuthorizationInvalidInput
		}
		return permission, nil, reason, nil
	}
	if expiresAt == "" {
		return "", nil, "", ErrDataAuthorizationInvalidInput
	}
	expiry, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil {
		return "", nil, "", ErrDataAuthorizationInvalidInput
	}
	expiry = expiry.UTC().Truncate(time.Millisecond)
	current := now.UTC()
	if expiry.Before(current.Add(5*time.Minute)) || expiry.After(current.Add(365*24*time.Hour)) {
		return "", nil, "", ErrDataAuthorizationInvalidInput
	}
	return permission, &expiry, reason, nil
}
func normalizeDataAuthorizationRevoke(permission, reason string) (string, string, error) {
	permission = strings.TrimSpace(permission)
	normalizedReason, err := normalizeDataAuthorizationReason(reason)
	if err != nil || !grantableDataPermission(permission) {
		return "", "", ErrDataAuthorizationInvalidInput
	}
	return permission, normalizedReason, nil
}
func normalizeDataAuthorizationReason(reason string) (string, error) {
	reason = strings.TrimSpace(reason)
	if utf8.RuneCountInString(reason) < 1 || utf8.RuneCountInString(reason) > 500 || strings.ContainsAny(reason, "\x00\r\n") {
		return "", ErrDataAuthorizationInvalidInput
	}
	return reason, nil
}
func grantableDataPermission(permission string) bool {
	for _, candidate := range model.GrantableDataPermissions() {
		if permission == candidate {
			return true
		}
	}
	return false
}
func validDataAuthorizationAuditAction(action string) bool {
	switch action {
	case model.DataAuthorizationActionAccountCreate,
		model.DataAuthorizationActionGrant,
		model.DataAuthorizationActionRenew,
		model.DataAuthorizationActionRevoke,
		model.DataAuthorizationActionTokenReissue:
		return true
	default:
		return false
	}
}

func normalizeDataAuthorizationAuditTimeRange(start, end string) (*time.Time, *time.Time, error) {
	parse := func(value string) (*time.Time, error) {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, nil
		}
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return nil, ErrDataAuthorizationInvalidInput
		}
		parsed = parsed.UTC()
		return &parsed, nil
	}
	startTime, err := parse(start)
	if err != nil {
		return nil, nil, err
	}
	endTime, err := parse(end)
	if err != nil {
		return nil, nil, err
	}
	if startTime != nil && endTime != nil && startTime.After(*endTime) {
		return nil, nil, ErrDataAuthorizationInvalidInput
	}
	return startTime, endTime, nil
}

func normalizeDataAuthorizationPageSize(pageSize int) int {
	if pageSize <= 0 {
		return 20
	}
	if pageSize > dataAuthorizationMaxPageSize {
		return dataAuthorizationMaxPageSize
	}
	return pageSize
}
func validDataAuthorizationKey(key string) bool { return dataAuthorizationKeyPattern.MatchString(key) }
func dataAuthorizationHashes(value interface{}, key string) (string, string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", "", err
	}
	requestSum := sha256.Sum256(data)
	keySum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(requestSum[:]), hex.EncodeToString(keySum[:]), nil
}
func generateOpenAPIToken() (string, error) {
	secret, err := randomSecret(32)
	if err != nil {
		return "", err
	}
	return "dg_open_" + secret, nil
}
func randomSecret(size int) (string, error) {
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("data authorization: generate secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}
func tokenDigest(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
func tokenDisplayPrefix(token string) string {
	if len(token) <= 16 {
		return token
	}
	return token[:16]
}
