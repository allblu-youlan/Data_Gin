package auth_svc

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"gin-biz-web-api/constant"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/database"
	"gin-biz-web-api/pkg/jwt"
	"gin-biz-web-api/pkg/phonecode"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrInvalidCredentials = errors.New("account auth: invalid credentials")
	ErrAccountUnavailable = errors.New("account auth: account unavailable")
	ErrPasswordTooWeak    = errors.New("account auth: password must contain 10 to 72 bytes")
	ErrPasswordUnchanged  = errors.New("account auth: new password must differ from current password")
)

var consolePhonePattern = regexp.MustCompile(`^1[3-9]\d{9}$`)

type phoneCodeVerifier interface {
	Issue(ctx context.Context, purpose phonecode.Purpose, phoneNumber string) error
	VerifyAndConsume(ctx context.Context, purpose phonecode.Purpose, phoneNumber, code string) error
}

type versionedTokenIssuer interface {
	GenerateVersionedToken(userID, tokenType string, authVersion uint64) string
}

type consoleAccount struct {
	ID            uint
	Account       string
	Phone         *string
	Password      string
	Nickname      string
	AccountType   string
	Status        string
	AuthVersion   uint64
	MallScopeMode string
	LastLoginAt   *time.Time
}

type consoleProfile struct {
	Account     consoleAccount
	Roles       []ConsoleRoleDTO
	Permissions []string
	MallIDs     []uint
}

type accountAuthRepository interface {
	FindActiveConsoleByAccount(ctx context.Context, account string) (*consoleAccount, error)
	FindActiveConsoleByPhone(ctx context.Context, phone string) (*consoleAccount, error)
	FindActiveConsoleByID(ctx context.Context, userID uint) (*consoleAccount, error)
	RecordLogin(ctx context.Context, userID uint, at time.Time) (*consoleAccount, error)
	UpdatePassword(ctx context.Context, userID uint, passwordHash string, at time.Time) error
	LoadProfile(ctx context.Context, userID uint) (*consoleProfile, error)
}

type consoleAdminAccessNormalizer interface {
	NormalizeConsoleAdminAccess(ctx context.Context, userID uint) error
}

type AccountAuthService struct {
	repository accountAuthRepository
	phoneCodes phoneCodeVerifier
	tokens     versionedTokenIssuer
	now        func() time.Time
}

type ConsoleSessionDTO struct {
	Token string            `json:"token"`
	User  ConsoleAccountDTO `json:"user"`
}

type ConsoleAccountDTO struct {
	ID            uint       `json:"id"`
	Account       string     `json:"account"`
	Phone         string     `json:"phone"`
	Nickname      string     `json:"nickname"`
	AccountType   string     `json:"accountType"`
	Status        string     `json:"status"`
	MallScopeMode string     `json:"mallScopeMode"`
	LastLoginAt   *time.Time `json:"lastLoginAt,omitempty"`
}

type ConsoleRoleDTO struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

type ConsoleProfileDTO struct {
	ConsoleAccountDTO
	Roles       []ConsoleRoleDTO `json:"roles"`
	Permissions []string         `json:"permissions"`
	MallIDs     []uint           `json:"mallIds"`
}

func NewAccountAuthService(repository accountAuthRepository, codes phoneCodeVerifier, tokens versionedTokenIssuer) *AccountAuthService {
	return &AccountAuthService{repository: repository, phoneCodes: codes, tokens: tokens, now: time.Now}
}

func NewDatabaseAccountAuthService(codes phoneCodeVerifier) *AccountAuthService {
	return NewAccountAuthService(&gormAccountAuthRepository{db: database.DB}, codes, jwt.NewJWT())
}

func (s *AccountAuthService) LoginPassword(ctx context.Context, account, password string) (*ConsoleSessionDTO, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	account = strings.TrimSpace(account)
	if account == "" || password == "" {
		return nil, ErrInvalidCredentials
	}
	var user *consoleAccount
	var err error
	if consolePhonePattern.MatchString(account) {
		user, err = s.repository.FindActiveConsoleByPhone(ctx, account)
	} else {
		user, err = s.repository.FindActiveConsoleByAccount(ctx, account)
	}
	if err != nil {
		return nil, normalizeCredentialLookupError(err)
	}
	if bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)) != nil {
		return nil, ErrInvalidCredentials
	}
	return s.completeLogin(ctx, user.ID)
}

// SendPhoneCode deliberately returns success for unknown, disabled, and
// non-console accounts so callers cannot enumerate accounts from the response.
func (s *AccountAuthService) SendPhoneCode(ctx context.Context, phone string, purpose phonecode.Purpose) error {
	if err := s.validate(); err != nil {
		return err
	}
	if err := s.requirePhoneCodes(); err != nil {
		return err
	}
	phone = strings.TrimSpace(phone)
	if !consolePhonePattern.MatchString(phone) {
		return phonecode.ErrInvalidPhone
	}
	if purpose != phonecode.PurposeLogin && purpose != phonecode.PurposePasswordReset {
		return phonecode.ErrInvalidPurpose
	}
	if _, err := s.repository.FindActiveConsoleByPhone(ctx, phone); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return fmt.Errorf("account auth: lookup phone-code account: %w", err)
	}
	if err := s.phoneCodes.Issue(ctx, purpose, phone); err != nil {
		return fmt.Errorf("account auth: issue phone code: %w", err)
	}
	return nil
}

func (s *AccountAuthService) LoginPhoneCode(ctx context.Context, phone, code string) (*ConsoleSessionDTO, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	if err := s.requirePhoneCodes(); err != nil {
		return nil, err
	}
	phone = strings.TrimSpace(phone)
	user, err := s.repository.FindActiveConsoleByPhone(ctx, phone)
	if err != nil {
		return nil, normalizeCredentialLookupError(err)
	}
	if err := s.phoneCodes.VerifyAndConsume(ctx, phonecode.PurposeLogin, phone, code); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidCredentials, err)
	}
	return s.completeLogin(ctx, user.ID)
}

func (s *AccountAuthService) ResetPassword(ctx context.Context, phone, code, password string) error {
	if err := s.validate(); err != nil {
		return err
	}
	if err := s.requirePhoneCodes(); err != nil {
		return err
	}
	if err := validateNewPassword(password); err != nil {
		return err
	}
	phone = strings.TrimSpace(phone)
	user, err := s.repository.FindActiveConsoleByPhone(ctx, phone)
	if err != nil {
		return normalizeCredentialLookupError(err)
	}
	if err := s.phoneCodes.VerifyAndConsume(ctx, phonecode.PurposePasswordReset, phone, code); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidCredentials, err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("account auth: hash password: %w", err)
	}
	if err := s.repository.UpdatePassword(ctx, user.ID, string(hash), s.now().UTC()); err != nil {
		return fmt.Errorf("account auth: reset password: %w", err)
	}
	return nil
}

func (s *AccountAuthService) ChangePassword(ctx context.Context, userID uint, currentPassword, newPassword string) error {
	if err := s.validate(); err != nil {
		return err
	}
	if err := validateNewPassword(newPassword); err != nil {
		return err
	}
	user, err := s.repository.FindActiveConsoleByID(ctx, userID)
	if err != nil {
		return normalizeCredentialLookupError(err)
	}
	if bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(currentPassword)) != nil {
		return ErrInvalidCredentials
	}
	if bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(newPassword)) == nil {
		return ErrPasswordUnchanged
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("account auth: hash password: %w", err)
	}
	if err := s.repository.UpdatePassword(ctx, user.ID, string(hash), s.now().UTC()); err != nil {
		return fmt.Errorf("account auth: change password: %w", err)
	}
	return nil
}

func (s *AccountAuthService) Profile(ctx context.Context, userID uint) (*ConsoleProfileDTO, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	profile, err := s.repository.LoadProfile(ctx, userID)
	if err != nil {
		return nil, normalizeCredentialLookupError(err)
	}
	roles := append([]ConsoleRoleDTO(nil), profile.Roles...)
	permissions := model.ExpandOpenAPIPermissions(profile.Permissions)
	mallIDs := append([]uint(nil), profile.MallIDs...)
	if roles == nil {
		roles = []ConsoleRoleDTO{}
	}
	if permissions == nil {
		permissions = []string{}
	}
	if mallIDs == nil {
		mallIDs = []uint{}
	}
	sort.Slice(roles, func(i, j int) bool { return roles[i].Code < roles[j].Code })
	sort.Strings(permissions)
	sort.Slice(mallIDs, func(i, j int) bool { return mallIDs[i] < mallIDs[j] })
	return &ConsoleProfileDTO{ConsoleAccountDTO: consoleAccountDTOFromModel(profile.Account), Roles: roles, Permissions: permissions, MallIDs: mallIDs}, nil
}

func (s *AccountAuthService) completeLogin(ctx context.Context, userID uint) (*ConsoleSessionDTO, error) {
	if normalizer, ok := s.repository.(consoleAdminAccessNormalizer); ok {
		if err := normalizer.NormalizeConsoleAdminAccess(ctx, userID); err != nil {
			return nil, fmt.Errorf("account auth: normalize console admin: %w", err)
		}
	}
	user, err := s.repository.RecordLogin(ctx, userID, s.now().UTC())
	if err != nil {
		return nil, normalizeCredentialLookupError(err)
	}
	token := s.tokens.GenerateVersionedToken(fmt.Sprint(user.ID), "refreshable", user.AuthVersion)
	if token == "" {
		return nil, fmt.Errorf("account auth: generate token")
	}
	return &ConsoleSessionDTO{Token: token, User: consoleAccountDTOFromModel(*user)}, nil
}

func (s *AccountAuthService) validate() error {
	if s == nil || s.repository == nil || s.tokens == nil || s.now == nil {
		return fmt.Errorf("account auth: invalid service")
	}
	return nil
}

func (s *AccountAuthService) requirePhoneCodes() error {
	if s == nil || s.phoneCodes == nil {
		return fmt.Errorf("account auth: phone-code service unavailable")
	}
	return nil
}

func validateNewPassword(password string) error {
	if len([]byte(password)) < 10 || len([]byte(password)) > 72 {
		return ErrPasswordTooWeak
	}
	return nil
}

func normalizeCredentialLookupError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrInvalidCredentials
	}
	return fmt.Errorf("account auth: lookup account: %w", err)
}

func consoleAccountDTOFromModel(user consoleAccount) ConsoleAccountDTO {
	phone := ""
	if user.Phone != nil {
		phone = maskPhone(*user.Phone)
	}
	return ConsoleAccountDTO{ID: user.ID, Account: user.Account, Phone: phone, Nickname: user.Nickname, AccountType: user.AccountType, Status: user.Status, MallScopeMode: user.MallScopeMode, LastLoginAt: user.LastLoginAt}
}

func maskPhone(phone string) string {
	if len(phone) != 11 {
		return ""
	}
	return phone[:3] + "****" + phone[7:]
}

type gormAccountAuthRepository struct{ db *gorm.DB }

func (r *gormAccountAuthRepository) NormalizeConsoleAdminAccess(ctx context.Context, userID uint) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user model.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, userID).Error; err != nil {
			return err
		}
		if user.Account != constant.ConsoleAdmin {
			return nil
		}
		if !isLegacyConsoleAdmin(&user) {
			return fmt.Errorf("reserved admin identity is not console managed")
		}
		if !normalizeConsoleAdmin(&user) {
			return nil
		}
		if user.CommonTimestampsField == nil {
			user.CommonTimestampsField = &model.CommonTimestampsField{}
		}
		user.UpdatedAt = int(time.Now().Unix())
		return tx.Save(&user).Error
	})
}

func (r *gormAccountAuthRepository) base(ctx context.Context) *gorm.DB {
	return r.db.WithContext(ctx).Table("users").Where("account_type = ? AND status = ?", model.AccountTypeConsole, model.AccountStatusActive)
}

func (r *gormAccountAuthRepository) FindActiveConsoleByAccount(ctx context.Context, account string) (*consoleAccount, error) {
	var user consoleAccount
	err := r.base(ctx).Where("account = ?", account).Take(&user).Error
	return &user, err
}

func (r *gormAccountAuthRepository) FindActiveConsoleByPhone(ctx context.Context, phone string) (*consoleAccount, error) {
	var user consoleAccount
	err := r.base(ctx).Where("phone = ?", phone).Take(&user).Error
	return &user, err
}

func (r *gormAccountAuthRepository) FindActiveConsoleByID(ctx context.Context, userID uint) (*consoleAccount, error) {
	var user consoleAccount
	err := r.base(ctx).Where("id = ?", userID).Take(&user).Error
	return &user, err
}

func (r *gormAccountAuthRepository) RecordLogin(ctx context.Context, userID uint, at time.Time) (*consoleAccount, error) {
	result := r.base(ctx).Where("id = ?", userID).Updates(map[string]interface{}{"last_login_at": at, "updated_at": at.Unix()})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected != 1 {
		return nil, gorm.ErrRecordNotFound
	}
	return r.FindActiveConsoleByID(ctx, userID)
}

func (r *gormAccountAuthRepository) UpdatePassword(ctx context.Context, userID uint, passwordHash string, at time.Time) error {
	result := r.base(ctx).Where("id = ?", userID).Updates(map[string]interface{}{"password": passwordHash, "password_changed_at": at, "auth_version": gorm.Expr("auth_version + 1"), "updated_at": at.Unix()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *gormAccountAuthRepository) LoadProfile(ctx context.Context, userID uint) (*consoleProfile, error) {
	account, err := r.FindActiveConsoleByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	var roles []ConsoleRoleDTO
	if err := r.db.WithContext(ctx).Table("roles").Select("roles.code, roles.name").Joins("JOIN user_roles ON user_roles.role_id = roles.id").Where("user_roles.user_id = ? AND roles.status = ?", userID, model.RoleStatusActive).Scan(&roles).Error; err != nil {
		return nil, err
	}
	permissions := []string{}
	isSuper := false
	for _, role := range roles {
		if role.Code == model.RoleCodeSuperAdmin {
			isSuper = true
			break
		}
	}
	if isSuper {
		if err := r.db.WithContext(ctx).Model(&model.Permission{}).Pluck("code", &permissions).Error; err != nil {
			return nil, err
		}
	} else if err := r.db.WithContext(ctx).Table("role_permissions").Distinct("role_permissions.permission_code").Joins("JOIN user_roles ON user_roles.role_id = role_permissions.role_id").Joins("JOIN roles ON roles.id = user_roles.role_id").Where("user_roles.user_id = ? AND roles.status = ?", userID, model.RoleStatusActive).Pluck("role_permissions.permission_code", &permissions).Error; err != nil {
		return nil, err
	}
	if !isSuper {
		actions, err := loadConsoleReportCategoryActions(ctx, r.db, userID)
		if err != nil {
			return nil, err
		}
		seen := make(map[string]struct{}, len(permissions)+3)
		for _, permission := range permissions {
			seen[permission] = struct{}{}
		}
		for _, permission := range reportRuntimePermissions(actions) {
			if _, exists := seen[permission]; exists {
				continue
			}
			permissions = append(permissions, permission)
			seen[permission] = struct{}{}
		}
	}
	mallIDs := []uint{}
	if account.MallScopeMode == model.MallScopeSelected {
		if err := r.db.WithContext(ctx).Model(&model.UserMallScope{}).Where("user_id = ?", userID).Pluck("mall_id", &mallIDs).Error; err != nil {
			return nil, err
		}
	}
	return &consoleProfile{Account: *account, Roles: roles, Permissions: permissions, MallIDs: mallIDs}, nil
}
