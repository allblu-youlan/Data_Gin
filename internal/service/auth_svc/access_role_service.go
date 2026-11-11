package auth_svc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/internal/requests/auth_request"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/database"

	"gorm.io/gorm"
)

const accessRoleMaxPageSize = 100

var (
	ErrAccessRoleForbidden    = errors.New("access role: forbidden")
	ErrAccessRoleInvalidInput = errors.New("access role: invalid input")
	ErrAccessRoleNotFound     = errors.New("access role: not found")
	ErrAccessRoleConflict     = errors.New("access role: conflict")
	accessRoleCodePattern     = regexp.MustCompile(`^[a-z][a-z0-9_]{2,63}$`)
	accessIdempotencyPattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{7,254}$`)
)

type AccessRoleDTO struct {
	ID          uint      `json:"id"`
	Code        string    `json:"code"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	IsSystem    bool      `json:"isSystem"`
	IsSuper     bool      `json:"isSuper"`
	Permissions []string  `json:"permissions"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type AccessRoleMutationResult struct {
	Role     *AccessRoleDTO `json:"role,omitempty"`
	Deleted  bool           `json:"deleted,omitempty"`
	Replayed bool           `json:"replayed,omitempty"`
}

type AccessAuditDTO struct {
	ID          uint           `json:"id"`
	ActorUserID uint           `json:"actorUserId"`
	Action      string         `json:"action"`
	TargetType  string         `json:"targetType"`
	TargetID    uint           `json:"targetId"`
	Before      model.JSONText `json:"before,omitempty"`
	After       model.JSONText `json:"after,omitempty"`
	Reason      string         `json:"reason"`
	RequestID   string         `json:"requestId"`
	CreatedAt   time.Time      `json:"createdAt"`
}

type AccessAuditQueryResult struct {
	Audits     []AccessAuditDTO            `json:"audits"`
	Pagination DataAuthorizationPagination `json:"pagination"`
}

type AccessRoleService struct{ db *gorm.DB }

func NewAccessRoleService() *AccessRoleService                  { return &AccessRoleService{db: database.DB} }
func NewAccessRoleServiceWithDB(db *gorm.DB) *AccessRoleService { return &AccessRoleService{db: db} }

func (s *AccessRoleService) PermissionCatalog(ctx context.Context, actorID uint) ([]model.Permission, error) {
	if err := s.authorize(ctx, actorID, model.PermissionSystemRoleRead); err != nil {
		return nil, err
	}
	var items []model.Permission
	if err := s.db.WithContext(ctx).Order("sort ASC, code ASC").Find(&items).Error; err != nil {
		return nil, err
	}
	for i := range items {
		items[i].ParentCode = model.OpenAPIPermissionParent(items[i].Code)
		items[i].Level = model.OpenAPIPermissionLevel(items[i].Code)
	}
	return items, nil
}

func (s *AccessRoleService) ListRoles(ctx context.Context, actorID uint) ([]AccessRoleDTO, error) {
	if err := s.authorize(ctx, actorID, model.PermissionSystemRoleRead); err != nil {
		return nil, err
	}
	var roles []model.Role
	if err := s.db.WithContext(ctx).Order("is_system DESC, id ASC").Find(&roles).Error; err != nil {
		return nil, err
	}
	return s.roleDTOs(ctx, roles)
}

func (s *AccessRoleService) CreateRole(ctx context.Context, actorID uint, key string, request auth_request.AccessRoleCreateRequest) (*AccessRoleMutationResult, error) {
	code, name, description, permissions, reason, err := normalizeRoleCreate(request)
	if err != nil || !accessIdempotencyPattern.MatchString(key) {
		return nil, ErrAccessRoleInvalidInput
	}
	if err := s.ensureGrantable(ctx, actorID, permissions); err != nil {
		return nil, err
	}
	result := new(AccessRoleMutationResult)
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		record, replay, err := reserveAccessMutation(ctx, tx, "access.role.create", actorID, key, request)
		if err != nil {
			return err
		}
		if replay {
			return decodeAccessReplay(record, result)
		}
		role := model.Role{Code: code, Name: name, Description: description, Status: model.RoleStatusActive}
		if err := tx.Create(&role).Error; err != nil {
			return ErrAccessRoleConflict
		}
		if err := replaceRolePermissions(tx, role.ID, permissions); err != nil {
			return err
		}
		dto := roleDTO(role, permissions)
		result.Role = &dto
		if err := createRoleAudit(tx, actorID, "role.create", role.ID, nil, dto, reason, accessKeyHash(key)); err != nil {
			return err
		}
		return completeAccessMutation(ctx, tx, record.ID, role.ID, result)
	})
	return result, err
}

func (s *AccessRoleService) UpdateRole(ctx context.Context, actorID, roleID uint, key string, request auth_request.AccessRoleUpdateRequest) (*AccessRoleMutationResult, error) {
	name, description, reason, err := normalizeRoleMeta(request.Name, request.Description, request.Reason)
	if err != nil || roleID == 0 || !accessIdempotencyPattern.MatchString(key) {
		return nil, ErrAccessRoleInvalidInput
	}
	if err := s.authorize(ctx, actorID, model.PermissionSystemRoleManage); err != nil {
		return nil, err
	}
	return s.mutateRole(ctx, actorID, roleID, key, "access.role.update", request, func(tx *gorm.DB, role *model.Role, before AccessRoleDTO) error {
		role.Name, role.Description = name, description
		if err := tx.Model(role).Updates(map[string]interface{}{"name": name, "description": description}).Error; err != nil {
			return err
		}
		return createRoleAudit(tx, actorID, "role.update", role.ID, before, roleDTO(*role, before.Permissions), reason, accessKeyHash(key))
	})
}

func (s *AccessRoleService) SetRoleStatus(ctx context.Context, actorID, roleID uint, key string, request auth_request.AccessRoleStatusRequest) (*AccessRoleMutationResult, error) {
	status, reason := strings.ToUpper(strings.TrimSpace(request.Status)), strings.TrimSpace(request.Reason)
	if (status != model.RoleStatusActive && status != model.RoleStatusDisabled) || !validAccessReason(reason) || roleID == 0 || !accessIdempotencyPattern.MatchString(key) {
		return nil, ErrAccessRoleInvalidInput
	}
	if err := s.authorize(ctx, actorID, model.PermissionSystemRoleManage); err != nil {
		return nil, err
	}
	return s.mutateRole(ctx, actorID, roleID, key, "access.role.status", request, func(tx *gorm.DB, role *model.Role, before AccessRoleDTO) error {
		role.Status = status
		if err := tx.Model(role).Update("status", status).Error; err != nil {
			return err
		}
		return createRoleAudit(tx, actorID, "role.status", role.ID, before, roleDTO(*role, before.Permissions), reason, accessKeyHash(key))
	})
}

func (s *AccessRoleService) ReplacePermissions(ctx context.Context, actorID, roleID uint, key string, request auth_request.AccessRolePermissionsRequest) (*AccessRoleMutationResult, error) {
	permissions, err := normalizePermissionCodes(request.Permissions)
	reason := strings.TrimSpace(request.Reason)
	if err != nil || !validAccessReason(reason) || roleID == 0 || !accessIdempotencyPattern.MatchString(key) {
		return nil, ErrAccessRoleInvalidInput
	}
	if err := s.ensureGrantable(ctx, actorID, permissions); err != nil {
		return nil, err
	}
	return s.mutateRole(ctx, actorID, roleID, key, "access.role.permissions", request, func(tx *gorm.DB, role *model.Role, before AccessRoleDTO) error {
		if err := replaceRolePermissions(tx, role.ID, permissions); err != nil {
			return err
		}
		return createRoleAudit(tx, actorID, "role.permissions.replace", role.ID, before, roleDTO(*role, permissions), reason, accessKeyHash(key))
	})
}

func (s *AccessRoleService) DeleteRole(ctx context.Context, actorID, roleID uint, key string, request auth_request.AccessRoleDeleteRequest) (*AccessRoleMutationResult, error) {
	reason := strings.TrimSpace(request.Reason)
	if !validAccessReason(reason) || roleID == 0 || !accessIdempotencyPattern.MatchString(key) {
		return nil, ErrAccessRoleInvalidInput
	}
	if err := s.authorize(ctx, actorID, model.PermissionSystemRoleManage); err != nil {
		return nil, err
	}
	result := new(AccessRoleMutationResult)
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		record, replay, err := reserveAccessMutation(ctx, tx, "access.role.delete", actorID, key, struct {
			ID      uint
			Request auth_request.AccessRoleDeleteRequest
		}{roleID, request})
		if err != nil {
			return err
		}
		if replay {
			return decodeAccessReplay(record, result)
		}
		role, before, err := loadMutableRole(tx, roleID)
		if err != nil {
			return err
		}
		var used int64
		if err := tx.Model(&model.UserRole{}).Where("role_id = ?", role.ID).Count(&used).Error; err != nil {
			return err
		}
		if used > 0 {
			return ErrAccessRoleConflict
		}
		if err := tx.Where("role_id = ?", role.ID).Delete(&model.RolePermission{}).Error; err != nil {
			return err
		}
		if err := tx.Delete(role).Error; err != nil {
			return err
		}
		result.Deleted = true
		if err := createRoleAudit(tx, actorID, "role.delete", role.ID, before, nil, reason, accessKeyHash(key)); err != nil {
			return err
		}
		return completeAccessMutation(ctx, tx, record.ID, role.ID, result)
	})
	return result, err
}

func (s *AccessRoleService) QueryAudits(ctx context.Context, actorID uint, request auth_request.AccessAuditQueryRequest) (*AccessAuditQueryResult, error) {
	if err := s.authorize(ctx, actorID, model.PermissionSystemAuditRead); err != nil {
		return nil, err
	}
	start, end, err := normalizeAccessTimes(request.StartTime, request.EndTime)
	if err != nil {
		return nil, err
	}
	pageSize := request.PageSize
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > accessRoleMaxPageSize {
		pageSize = accessRoleMaxPageSize
	}
	q := s.db.WithContext(ctx).Model(&model.AuthAudit{})
	if request.BeforeID > 0 {
		q = q.Where("id < ?", request.BeforeID)
	}
	if request.ActorID > 0 {
		q = q.Where("actor_user_id = ?", request.ActorID)
	}
	if request.TargetID > 0 {
		q = q.Where("target_id = ?", request.TargetID)
	}
	if v := strings.TrimSpace(request.Action); v != "" {
		q = q.Where("action = ?", v)
	}
	if v := strings.TrimSpace(request.TargetType); v != "" {
		q = q.Where("target_type = ?", v)
	}
	if start != nil {
		q = q.Where("created_at >= ?", *start)
	}
	if end != nil {
		q = q.Where("created_at <= ?", *end)
	}
	var rows []model.AuthAudit
	if err := q.Order("id DESC").Limit(pageSize + 1).Find(&rows).Error; err != nil {
		return nil, err
	}
	hasMore := len(rows) > pageSize
	if hasMore {
		rows = rows[:pageSize]
	}
	items := make([]AccessAuditDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, AccessAuditDTO{row.ID, row.ActorUserID, row.Action, row.TargetType, row.TargetID, row.BeforeJSON, row.AfterJSON, row.Reason, row.RequestID, row.CreatedAt})
	}
	result := &AccessAuditQueryResult{Audits: items, Pagination: DataAuthorizationPagination{PageSize: pageSize, HasMore: hasMore}}
	if hasMore && len(rows) > 0 {
		result.Pagination.NextBeforeID = rows[len(rows)-1].ID
	}
	return result, nil
}

func (s *AccessRoleService) mutateRole(ctx context.Context, actorID, roleID uint, key, scope string, request interface{}, change func(*gorm.DB, *model.Role, AccessRoleDTO) error) (*AccessRoleMutationResult, error) {
	result := new(AccessRoleMutationResult)
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		record, replay, err := reserveAccessMutation(ctx, tx, scope, actorID, key, struct {
			ID      uint
			Request interface{}
		}{roleID, request})
		if err != nil {
			return err
		}
		if replay {
			return decodeAccessReplay(record, result)
		}
		role, before, err := loadMutableRole(tx, roleID)
		if err != nil {
			return err
		}
		if err = change(tx, role, before); err != nil {
			return err
		}
		if err = bumpRoleUsers(tx, role.ID); err != nil {
			return err
		}
		dto, err := loadRoleDTO(tx, *role)
		if err != nil {
			return err
		}
		result.Role = &dto
		return completeAccessMutation(ctx, tx, record.ID, role.ID, result)
	})
	return result, err
}

func (s *AccessRoleService) authorize(ctx context.Context, actorID uint, permission string) error {
	super, grants, err := s.actorGrants(ctx, actorID)
	if err != nil {
		return err
	}
	if super {
		return nil
	}
	if _, ok := grants[permission]; !ok {
		return ErrAccessRoleForbidden
	}
	return nil
}
func (s *AccessRoleService) ensureGrantable(ctx context.Context, actorID uint, requested []string) error {
	super, grants, err := s.actorGrants(ctx, actorID)
	if err != nil {
		return err
	}
	if !super {
		if _, ok := grants[model.PermissionSystemRoleManage]; !ok {
			return ErrAccessRoleForbidden
		}
		for _, p := range requested {
			if _, ok := grants[p]; !ok {
				return ErrAccessRoleForbidden
			}
		}
	}
	var count int64
	if len(requested) > 0 {
		if err := s.db.WithContext(ctx).Model(&model.Permission{}).Where("code IN ?", requested).Count(&count).Error; err != nil {
			return err
		}
	}
	if int(count) != len(requested) {
		return ErrAccessRoleInvalidInput
	}
	return nil
}
func (s *AccessRoleService) actorGrants(ctx context.Context, actorID uint) (bool, map[string]struct{}, error) {
	if s == nil || s.db == nil || ctx == nil || actorID == 0 {
		return false, nil, ErrAccessRoleForbidden
	}
	var roles []authorizationRole
	if err := s.db.WithContext(ctx).Table("roles").Select("roles.code, roles.is_super").Joins("JOIN user_roles ON user_roles.role_id=roles.id").Joins("JOIN users ON users.id=user_roles.user_id").Where("users.id=? AND users.status=? AND users.account_type=? AND roles.status=?", actorID, model.AccountStatusActive, model.AccountTypeConsole, model.RoleStatusActive).Find(&roles).Error; err != nil {
		return false, nil, err
	}
	for _, r := range roles {
		if r.IsSuper || r.Code == model.RoleCodeSuperAdmin {
			return true, map[string]struct{}{}, nil
		}
	}
	var codes []string
	if err := s.db.WithContext(ctx).Table("role_permissions").Distinct("role_permissions.permission_code").Joins("JOIN roles ON roles.id=role_permissions.role_id").Joins("JOIN user_roles ON user_roles.role_id=roles.id").Where("user_roles.user_id=? AND roles.status=?", actorID, model.RoleStatusActive).Pluck("role_permissions.permission_code", &codes).Error; err != nil {
		return false, nil, err
	}
	set := map[string]struct{}{}
	for _, c := range codes {
		set[c] = struct{}{}
	}
	return false, set, nil
}

func (s *AccessRoleService) roleDTOs(ctx context.Context, roles []model.Role) ([]AccessRoleDTO, error) {
	out := make([]AccessRoleDTO, 0, len(roles))
	for _, r := range roles {
		dto, err := loadRoleDTO(s.db.WithContext(ctx), r)
		if err != nil {
			return nil, err
		}
		out = append(out, dto)
	}
	return out, nil
}
func loadRoleDTO(db *gorm.DB, role model.Role) (AccessRoleDTO, error) {
	var codes []string
	if err := db.Model(&model.RolePermission{}).Where("role_id=?", role.ID).Order("permission_code ASC").Pluck("permission_code", &codes).Error; err != nil {
		return AccessRoleDTO{}, err
	}
	return roleDTO(role, codes), nil
}
func roleDTO(r model.Role, p []string) AccessRoleDTO {
	if p == nil {
		p = []string{}
	}
	return AccessRoleDTO{r.ID, r.Code, r.Name, r.Description, r.Status, r.IsSystem, r.IsSuper, p, r.CreatedAt, r.UpdatedAt}
}
func loadMutableRole(tx *gorm.DB, id uint) (*model.Role, AccessRoleDTO, error) {
	var r model.Role
	if err := tx.First(&r, id).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, AccessRoleDTO{}, ErrAccessRoleNotFound
	} else if err != nil {
		return nil, AccessRoleDTO{}, err
	}
	if r.IsSystem || r.IsSuper {
		return nil, AccessRoleDTO{}, ErrAccessRoleForbidden
	}
	dto, err := loadRoleDTO(tx, r)
	return &r, dto, err
}
func replaceRolePermissions(tx *gorm.DB, id uint, codes []string) error {
	if err := tx.Where("role_id=?", id).Delete(&model.RolePermission{}).Error; err != nil {
		return err
	}
	rows := make([]model.RolePermission, 0, len(codes))
	for _, c := range codes {
		rows = append(rows, model.RolePermission{RoleID: id, PermissionCode: c})
	}
	if len(rows) > 0 {
		return tx.Create(&rows).Error
	}
	return nil
}
func bumpRoleUsers(tx *gorm.DB, id uint) error {
	return tx.Model(&model.User{}).Where("id IN (?)", tx.Model(&model.UserRole{}).Select("user_id").Where("role_id=?", id)).UpdateColumn("auth_version", gorm.Expr("auth_version + 1")).Error
}
func createRoleAudit(tx *gorm.DB, actor uint, action string, target uint, before, after interface{}, reason, requestID string) error {
	marshal := func(v interface{}) model.JSONText {
		if v == nil {
			return model.JSONText("")
		}
		b, _ := json.Marshal(v)
		return model.JSONText(b)
	}
	return tx.Create(&model.AuthAudit{ActorUserID: actor, Action: action, TargetType: "role", TargetID: target, BeforeJSON: marshal(before), AfterJSON: marshal(after), Reason: reason, RequestID: requestID}).Error
}
func normalizeRoleCreate(r auth_request.AccessRoleCreateRequest) (string, string, string, []string, string, error) {
	code := strings.ToLower(strings.TrimSpace(r.Code))
	name, desc, reason, err := normalizeRoleMeta(r.Name, r.Description, r.Reason)
	if err != nil || !accessRoleCodePattern.MatchString(code) || code == model.RoleCodeSuperAdmin || code == model.RoleCodeAdmin || code == model.RoleCodeOperator || code == model.RoleCodeViewer {
		return "", "", "", nil, "", ErrAccessRoleInvalidInput
	}
	p, err := normalizePermissionCodes(r.Permissions)
	return code, name, desc, p, reason, err
}
func normalizeRoleMeta(name, description, reason string) (string, string, string, error) {
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	reason = strings.TrimSpace(reason)
	if utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 128 || utf8.RuneCountInString(description) > 500 || !validAccessReason(reason) {
		return "", "", "", ErrAccessRoleInvalidInput
	}
	return name, description, reason, nil
}
func normalizePermissionCodes(codes []string) ([]string, error) {
	set := map[string]struct{}{}
	for _, c := range codes {
		c = strings.TrimSpace(c)
		if c == "" || len([]byte(c)) > 64 {
			return nil, ErrAccessRoleInvalidInput
		}
		set[c] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for c := range set {
		out = append(out, c)
	}
	sort.Strings(out)
	return out, nil
}
func validAccessReason(r string) bool {
	return utf8.RuneCountInString(r) >= 1 && utf8.RuneCountInString(r) <= 500 && !strings.ContainsAny(r, "\x00\r\n")
}
func normalizeAccessTimes(a, b string) (*time.Time, *time.Time, error) {
	parse := func(v string) (*time.Time, error) {
		if strings.TrimSpace(v) == "" {
			return nil, nil
		}
		t, e := time.Parse(time.RFC3339, strings.TrimSpace(v))
		if e != nil {
			return nil, ErrAccessRoleInvalidInput
		}
		u := t.UTC()
		return &u, nil
	}
	x, e := parse(a)
	if e != nil {
		return nil, nil, e
	}
	y, e := parse(b)
	if e != nil {
		return nil, nil, e
	}
	if x != nil && y != nil && x.After(*y) {
		return nil, nil, ErrAccessRoleInvalidInput
	}
	return x, y, nil
}
func accessKeyHash(k string) string { s := sha256.Sum256([]byte(k)); return hex.EncodeToString(s[:]) }
func reserveAccessMutation(ctx context.Context, tx *gorm.DB, scope string, actor uint, key string, payload interface{}) (*model.APIIdempotencyRecord, bool, error) {
	raw, _ := json.Marshal(payload)
	sum := sha256.Sum256(raw)
	record := &model.APIIdempotencyRecord{OperationScope: scope, ActorUserID: actor, KeyHash: accessKeyHash(key), RequestHash: hex.EncodeToString(sum[:]), ResourceType: "access_role", ResponseJSON: model.JSONText(`{}`)}
	dao := data_dao.NewAPIIdempotencyDAO(tx)
	ok, err := dao.Reserve(ctx, record)
	if err != nil {
		return nil, false, err
	}
	if ok {
		return record, false, nil
	}
	existing, err := dao.FindForUpdate(ctx, scope, actor, record.KeyHash)
	if err != nil {
		return nil, false, err
	}
	if existing.RequestHash != record.RequestHash || existing.HTTPStatus == 0 {
		return nil, false, ErrAccessRoleConflict
	}
	return existing, true, nil
}
func decodeAccessReplay(r *model.APIIdempotencyRecord, out *AccessRoleMutationResult) error {
	if err := json.Unmarshal([]byte(r.ResponseJSON), out); err != nil {
		return ErrAccessRoleConflict
	}
	out.Replayed = true
	return nil
}
func completeAccessMutation(ctx context.Context, tx *gorm.DB, id, resource uint, result interface{}) error {
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return data_dao.NewAPIIdempotencyDAO(tx).Complete(ctx, id, resource, http.StatusOK, model.JSONText(raw))
}
