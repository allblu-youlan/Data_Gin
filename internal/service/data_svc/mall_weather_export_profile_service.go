package data_svc

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/internal/requestbody"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/database"
)

const (
	maxMallWeatherExportDatasets = 8
	maxMallWeatherExportColumns  = 128
	maxMallWeatherExportRules    = 64
	maxMallWeatherExportRows     = 1_048_575
	maxMallWeatherFilterMalls    = 1_000
	maxMallWeatherFilterCities   = 100
	defaultExportProfilePageSize = 50
	maxExportProfilePageSize     = 100

	defaultMallWeatherExportUnit           = "metric"
	defaultMallWeatherExportDateFormat     = "2006-01-02"
	defaultMallWeatherExportDateTimeFormat = "2006-01-02 15:04:05"
)

var (
	ErrMallWeatherExportProfileInvalid  = errors.New("mall weather export profile: invalid input")
	ErrMallWeatherExportProfileConflict = errors.New("mall weather export profile: conflict")

	mallWeatherExportProfileCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{2,99}$`)
	mallWeatherExportColorPattern       = regexp.MustCompile(`^#[0-9a-f]{6}$`)
)

type mallWeatherExportProfileStore interface {
	Save(context.Context, *model.MallWeatherExportProfile, *uint64) (bool, error)
	List(context.Context, data_dao.MallWeatherExportProfileListQuery) ([]model.MallWeatherExportProfile, error)
}

type mallWeatherExportProfileCursor struct {
	Version uint8  `json:"v"`
	Code    string `json:"code"`
}

type MallWeatherExportProfileConfig struct {
	TimeZone         string                                 `json:"timeZone"`
	UnitSystem       string                                 `json:"unitSystem"`
	DateFormat       string                                 `json:"dateFormat"`
	DateTimeFormat   string                                 `json:"dateTimeFormat"`
	FileNameTemplate string                                 `json:"fileNameTemplate"`
	Filters          requestbody.MallWeatherExportFilters   `json:"filters"`
	Datasets         []requestbody.MallWeatherExportDataset `json:"datasets"`
}

type MallWeatherExportProfileDTO struct {
	ID               uint                                   `json:"id"`
	Code             string                                 `json:"code"`
	Name             string                                 `json:"name"`
	Version          uint64                                 `json:"version"`
	Enabled          bool                                   `json:"enabled"`
	TimeZone         string                                 `json:"timeZone"`
	UnitSystem       string                                 `json:"unitSystem"`
	DateFormat       string                                 `json:"dateFormat"`
	DateTimeFormat   string                                 `json:"dateTimeFormat"`
	FileNameTemplate string                                 `json:"fileNameTemplate"`
	Filters          requestbody.MallWeatherExportFilters   `json:"filters"`
	Datasets         []requestbody.MallWeatherExportDataset `json:"datasets"`
	CreatedBy        uint                                   `json:"createdBy"`
	UpdatedBy        uint                                   `json:"updatedBy"`
	CreatedAt        time.Time                              `json:"createdAt"`
	UpdatedAt        time.Time                              `json:"updatedAt"`
}

type MallWeatherExportProfileListResult struct {
	Items      []MallWeatherExportProfileDTO `json:"items"`
	Pagination MallWeatherPagination         `json:"pagination"`
}

type MallWeatherExportProfileService struct {
	store       mallWeatherExportProfileStore
	permissions mallPermissionChecker
	now         func() time.Time
}

func NewMallWeatherExportProfileService() *MallWeatherExportProfileService {
	return &MallWeatherExportProfileService{
		store:       data_dao.NewMallWeatherExportProfileDAO(),
		permissions: newAccountPermissionChecker(database.DB),
		now:         time.Now,
	}
}

func newMallWeatherExportProfileService(
	store mallWeatherExportProfileStore,
	permissions mallPermissionChecker,
	now func() time.Time,
) (*MallWeatherExportProfileService, error) {
	if store == nil || permissions == nil || now == nil {
		return nil, fmt.Errorf("mall weather export profile: invalid service configuration")
	}
	return &MallWeatherExportProfileService{store: store, permissions: permissions, now: now}, nil
}

func (service *MallWeatherExportProfileService) Save(
	ctx context.Context,
	actorUserID uint,
	request requestbody.MallWeatherExportProfileSaveRequest,
) (*MallWeatherExportProfileDTO, bool, error) {
	if service == nil || ctx == nil || actorUserID == 0 {
		return nil, false, fmt.Errorf("%w: invalid request", ErrMallWeatherExportProfileInvalid)
	}
	if err := service.authorize(ctx, actorUserID, PermissionWeatherConfigManage); err != nil {
		return nil, false, err
	}
	normalized, config, err := normalizeMallWeatherExportProfile(request)
	if err != nil {
		return nil, false, err
	}
	if reservedFixedMallWeatherExportProfileCode(normalized.Code) {
		return nil, false, fmt.Errorf("%w: reserved profile code", ErrMallWeatherExportProfileInvalid)
	}
	profileJSON, err := json.Marshal(config)
	if err != nil {
		return nil, false, fmt.Errorf("mall weather export profile: encode config: %w", err)
	}
	row := &model.MallWeatherExportProfile{
		Code:        normalized.Code,
		Name:        normalized.Name,
		ProfileJSON: model.JSONText(profileJSON),
		Enabled:     *normalized.Enabled,
		UpdatedBy:   actorUserID,
	}
	created, err := service.store.Save(ctx, row, normalized.ExpectedVersion)
	if errors.Is(err, data_dao.ErrMallWeatherExportProfileConflict) {
		return nil, false, ErrMallWeatherExportProfileConflict
	}
	if err != nil {
		return nil, false, err
	}
	dto, err := mallWeatherExportProfileDTO(row)
	if err != nil {
		return nil, false, err
	}
	return &dto, created, nil
}

func (service *MallWeatherExportProfileService) List(
	ctx context.Context,
	actorUserID uint,
	enabled *bool,
	cursorValue string,
	pageSize int,
) (*MallWeatherExportProfileListResult, error) {
	if service == nil || ctx == nil || actorUserID == 0 {
		return nil, fmt.Errorf("%w: invalid request", ErrMallWeatherExportProfileInvalid)
	}
	if err := service.authorize(ctx, actorUserID, PermissionWeatherExport); err != nil {
		return nil, err
	}
	if pageSize == 0 {
		pageSize = defaultExportProfilePageSize
	}
	if pageSize < 1 || pageSize > maxExportProfilePageSize || len(cursorValue) > maxWeatherCursorLength {
		return nil, fmt.Errorf("%w: invalid pagination", ErrMallWeatherExportProfileInvalid)
	}
	cursor, err := decodeMallWeatherExportProfileCursor(cursorValue)
	if err != nil {
		return nil, err
	}
	query := data_dao.MallWeatherExportProfileListQuery{Enabled: enabled, Limit: pageSize + 1}
	if cursor != nil {
		query.AfterCode = cursor.Code
	}
	rows, err := service.store.List(ctx, query)
	if err != nil {
		return nil, err
	}
	hasNext := len(rows) > pageSize
	if hasNext {
		rows = rows[:pageSize]
	}
	result := &MallWeatherExportProfileListResult{
		Items:      make([]MallWeatherExportProfileDTO, len(rows)),
		Pagination: MallWeatherPagination{PageSize: pageSize},
	}
	for index := range rows {
		result.Items[index], err = mallWeatherExportProfileDTO(&rows[index])
		if err != nil {
			return nil, err
		}
	}
	if hasNext {
		result.Pagination.NextCursor, err = encodeMallWeatherExportProfileCursor(&rows[len(rows)-1])
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func encodeMallWeatherExportProfileCursor(row *model.MallWeatherExportProfile) (string, error) {
	if row == nil || !mallWeatherExportProfileCodePattern.MatchString(row.Code) {
		return "", fmt.Errorf("mall weather export profile: invalid cursor row")
	}
	payload, err := json.Marshal(mallWeatherExportProfileCursor{Version: 1, Code: row.Code})
	if err != nil {
		return "", fmt.Errorf("mall weather export profile: encode cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeMallWeatherExportProfileCursor(value string) (*mallWeatherExportProfileCursor, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	if len(value) > maxWeatherCursorLength {
		return nil, fmt.Errorf("%w: invalid cursor", ErrMallWeatherExportProfileInvalid)
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid cursor", ErrMallWeatherExportProfileInvalid)
	}
	var cursor mallWeatherExportProfileCursor
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cursor); err != nil || cursor.Version != 1 ||
		!mallWeatherExportProfileCodePattern.MatchString(cursor.Code) {
		return nil, fmt.Errorf("%w: invalid cursor", ErrMallWeatherExportProfileInvalid)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: invalid cursor", ErrMallWeatherExportProfileInvalid)
	}
	return &cursor, nil
}

func (service *MallWeatherExportProfileService) authorize(
	ctx context.Context,
	actorUserID uint,
	permission string,
) error {
	allowed, err := service.permissions.HasPermission(ctx, actorUserID, permission, service.now().UTC())
	if err != nil {
		return fmt.Errorf("mall weather export profile: authorize: %w", err)
	}
	if !allowed {
		return ErrMallForbidden
	}
	return nil
}

func normalizeMallWeatherExportProfile(
	request requestbody.MallWeatherExportProfileSaveRequest,
) (requestbody.MallWeatherExportProfileSaveRequest, MallWeatherExportProfileConfig, error) {
	request.Code = strings.ToLower(strings.TrimSpace(request.Code))
	request.Name = strings.TrimSpace(request.Name)
	request.TimeZone = strings.TrimSpace(request.TimeZone)
	request.UnitSystem = strings.ToLower(strings.TrimSpace(request.UnitSystem))
	request.DateFormat = strings.TrimSpace(request.DateFormat)
	request.DateTimeFormat = strings.TrimSpace(request.DateTimeFormat)
	request.FileNameTemplate = strings.TrimSpace(request.FileNameTemplate)
	invalidIdentity := !mallWeatherExportProfileCodePattern.MatchString(request.Code) ||
		request.Name == "" || utf8.RuneCountInString(request.Name) > 255
	if invalidIdentity {
		return request, MallWeatherExportProfileConfig{}, fmt.Errorf(
			"%w: invalid profile identity",
			ErrMallWeatherExportProfileInvalid,
		)
	}
	if request.TimeZone == "" {
		request.TimeZone = "Asia/Shanghai"
	}
	location, err := time.LoadLocation(request.TimeZone)
	if err != nil {
		return request, MallWeatherExportProfileConfig{}, fmt.Errorf(
			"%w: invalid time zone",
			ErrMallWeatherExportProfileInvalid,
		)
	}
	request.TimeZone = location.String()
	if request.UnitSystem == "" {
		request.UnitSystem = defaultMallWeatherExportUnit
	}
	if request.UnitSystem != "metric" && request.UnitSystem != "imperial" {
		return request, MallWeatherExportProfileConfig{}, fmt.Errorf(
			"%w: invalid unit system",
			ErrMallWeatherExportProfileInvalid,
		)
	}
	if request.DateFormat == "" {
		request.DateFormat = defaultMallWeatherExportDateFormat
	}
	if request.DateTimeFormat == "" {
		request.DateTimeFormat = defaultMallWeatherExportDateTimeFormat
	}
	if !validMallWeatherExportTimeFormat(request.DateFormat) || !validMallWeatherExportTimeFormat(request.DateTimeFormat) {
		return request, MallWeatherExportProfileConfig{}, fmt.Errorf(
			"%w: invalid date format",
			ErrMallWeatherExportProfileInvalid,
		)
	}
	if err := validateMallWeatherExportFileNameTemplate(request.FileNameTemplate); err != nil {
		return request, MallWeatherExportProfileConfig{}, err
	}
	filters, err := normalizeMallWeatherExportFilters(request.Filters)
	if err != nil {
		return request, MallWeatherExportProfileConfig{}, err
	}
	request.Filters = filters
	if len(request.Datasets) == 0 || len(request.Datasets) > maxMallWeatherExportDatasets {
		return request, MallWeatherExportProfileConfig{}, fmt.Errorf(
			"%w: invalid datasets",
			ErrMallWeatherExportProfileInvalid,
		)
	}
	if request.Enabled == nil {
		enabled := true
		request.Enabled = &enabled
	}
	seenKinds := make(map[string]struct{}, len(request.Datasets))
	seenSheets := make(map[string]struct{}, len(request.Datasets))
	for index := range request.Datasets {
		dataset, err := normalizeMallWeatherExportDataset(request.Datasets[index])
		if err != nil {
			return request, MallWeatherExportProfileConfig{}, err
		}
		if _, exists := seenKinds[dataset.Kind]; exists {
			return request, MallWeatherExportProfileConfig{}, fmt.Errorf(
				"%w: duplicate dataset",
				ErrMallWeatherExportProfileInvalid,
			)
		}
		sheetKey := strings.ToLower(dataset.SheetName)
		if _, exists := seenSheets[sheetKey]; exists {
			return request, MallWeatherExportProfileConfig{}, fmt.Errorf(
				"%w: duplicate sheet name",
				ErrMallWeatherExportProfileInvalid,
			)
		}
		seenKinds[dataset.Kind], seenSheets[sheetKey] = struct{}{}, struct{}{}
		request.Datasets[index] = dataset
	}
	config := MallWeatherExportProfileConfig{
		TimeZone:         request.TimeZone,
		UnitSystem:       request.UnitSystem,
		DateFormat:       request.DateFormat,
		DateTimeFormat:   request.DateTimeFormat,
		FileNameTemplate: request.FileNameTemplate,
		Filters:          request.Filters,
		Datasets:         append([]requestbody.MallWeatherExportDataset(nil), request.Datasets...),
	}
	return request, config, nil
}

func normalizeMallWeatherExportDataset(
	dataset requestbody.MallWeatherExportDataset,
) (requestbody.MallWeatherExportDataset, error) {
	dataset.Kind = strings.ToLower(strings.TrimSpace(dataset.Kind))
	dataset.SheetName = strings.TrimSpace(dataset.SheetName)
	dataset.SplitBy = strings.ToLower(strings.TrimSpace(dataset.SplitBy))
	allowedFields, exists := data_dao.MallWeatherExportDatasetFields(dataset.Kind)
	if !exists || !validMallWeatherSheetName(dataset.SheetName) {
		return dataset, fmt.Errorf("%w: invalid dataset identity", ErrMallWeatherExportProfileInvalid)
	}
	switch dataset.SplitBy {
	case "", "city", "mall", "date", "data_type":
	default:
		return dataset, fmt.Errorf("%w: invalid split strategy", ErrMallWeatherExportProfileInvalid)
	}
	dataset.AsOf = strings.TrimSpace(dataset.AsOf)
	if dataset.Latest != nil && *dataset.Latest && dataset.AsOf != "" {
		return dataset, fmt.Errorf("%w: latest and asOf conflict", ErrMallWeatherExportProfileInvalid)
	}
	if dataset.Kind == "malls" || dataset.Kind == "fetch_runs" {
		if dataset.AsOf != "" || (dataset.Latest != nil && *dataset.Latest) {
			return dataset, fmt.Errorf("%w: unsupported dataset version filter", ErrMallWeatherExportProfileInvalid)
		}
	} else if dataset.Latest == nil {
		latest := dataset.AsOf == ""
		dataset.Latest = &latest
	}
	if dataset.AsOf != "" {
		asOf, err := time.Parse(time.RFC3339Nano, dataset.AsOf)
		if err != nil {
			return dataset, fmt.Errorf("%w: invalid asOf", ErrMallWeatherExportProfileInvalid)
		}
		dataset.AsOf = asOf.UTC().Format(time.RFC3339Nano)
	}
	if dataset.MaxRows == 0 {
		dataset.MaxRows = maxMallWeatherExportRows
	}
	invalidRows := dataset.MaxRows < 1 || dataset.MaxRows > maxMallWeatherExportRows
	tooManyColumns := len(dataset.Columns) > maxMallWeatherExportColumns
	tooManyRules := len(dataset.ConditionalFormats) > maxMallWeatherExportRules
	if invalidRows || tooManyColumns || tooManyRules {
		return dataset, fmt.Errorf("%w: invalid dataset limits", ErrMallWeatherExportProfileInvalid)
	}
	seenFields := make(map[string]struct{}, len(dataset.Columns))
	for index := range dataset.Columns {
		column := &dataset.Columns[index]
		column.Field = strings.ToLower(strings.TrimSpace(column.Field))
		column.Title = strings.TrimSpace(column.Title)
		column.Format = strings.ToLower(strings.TrimSpace(column.Format))
		if column.Format == "" {
			column.Format = "general"
		}
		_, allowed := allowedFields[column.Field]
		invalidTitle := column.Title == "" || utf8.RuneCountInString(column.Title) > 128
		invalidWidth := column.Width < 0 || column.Width > 255 || math.IsNaN(column.Width) || math.IsInf(column.Width, 0)
		if !allowed || invalidTitle || invalidWidth || !mallWeatherExportColumnFormats[column.Format] {
			return dataset, fmt.Errorf("%w: invalid dataset column", ErrMallWeatherExportProfileInvalid)
		}
		if _, duplicate := seenFields[column.Field]; duplicate {
			return dataset, fmt.Errorf("%w: duplicate dataset column", ErrMallWeatherExportProfileInvalid)
		}
		seenFields[column.Field] = struct{}{}
	}
	for index := range dataset.ConditionalFormats {
		conditionalFields := allowedFields
		if len(seenFields) > 0 {
			conditionalFields = seenFields
		}
		rule, err := normalizeMallWeatherExportConditionalFormat(dataset.ConditionalFormats[index], conditionalFields)
		if err != nil {
			return dataset, err
		}
		dataset.ConditionalFormats[index] = rule
	}
	return dataset, nil
}

func normalizeMallWeatherExportFilters(
	filters requestbody.MallWeatherExportFilters,
) (requestbody.MallWeatherExportFilters, error) {
	if len(filters.MallIDs) > maxMallWeatherFilterMalls || len(filters.Cities) > maxMallWeatherFilterCities {
		return filters, fmt.Errorf("%w: export filter is too large", ErrMallWeatherExportProfileInvalid)
	}
	requestedMallIDs := filters.MallIDs
	mallIDs := make(map[uint]struct{}, len(filters.MallIDs))
	filters.MallIDs = filters.MallIDs[:0]
	for _, mallID := range requestedMallIDs {
		if mallID == 0 {
			return filters, fmt.Errorf("%w: invalid mall filter", ErrMallWeatherExportProfileInvalid)
		}
		mallIDs[mallID] = struct{}{}
	}
	for mallID := range mallIDs {
		filters.MallIDs = append(filters.MallIDs, mallID)
	}
	slices.Sort(filters.MallIDs)

	var err error
	filters.Cities, err = normalizeMallWeatherExportStringFilter(filters.Cities, nil, maxMallWeatherFilterCities)
	if err != nil {
		return filters, err
	}
	filters.MallStatuses, err = normalizeMallWeatherExportStringFilter(
		filters.MallStatuses,
		map[string]bool{"draft": true, "active": true, "disabled": true},
		3,
	)
	if err != nil {
		return filters, err
	}
	filters.QualityStatuses, err = normalizeMallWeatherExportStringFilter(
		filters.QualityStatuses,
		map[string]bool{"valid": true, "warning": true},
		2,
	)
	if err != nil {
		return filters, err
	}
	start, err := normalizeMallWeatherExportFilterTime(filters.Start)
	if err != nil {
		return filters, err
	}
	end, err := normalizeMallWeatherExportFilterTime(filters.End)
	if err != nil {
		return filters, err
	}
	if start != "" && end != "" {
		startTime, _ := time.Parse(time.RFC3339Nano, start)
		endTime, _ := time.Parse(time.RFC3339Nano, end)
		if !startTime.Before(endTime) {
			return filters, fmt.Errorf("%w: invalid export time range", ErrMallWeatherExportProfileInvalid)
		}
	}
	filters.Start, filters.End = start, end
	return filters, nil
}

func normalizeMallWeatherExportStringFilter(values []string, allowed map[string]bool, maxValues int) ([]string, error) {
	if len(values) > maxValues {
		return nil, fmt.Errorf("%w: export filter is too large", ErrMallWeatherExportProfileInvalid)
	}
	unique := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" || utf8.RuneCountInString(value) > 128 || (allowed != nil && !allowed[value]) {
			return nil, fmt.Errorf("%w: invalid export filter", ErrMallWeatherExportProfileInvalid)
		}
		unique[value] = struct{}{}
	}
	result := make([]string, 0, len(unique))
	for value := range unique {
		result = append(result, value)
	}
	slices.Sort(result)
	return result, nil
}

func normalizeMallWeatherExportFilterTime(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return "", fmt.Errorf("%w: invalid export filter time", ErrMallWeatherExportProfileInvalid)
	}
	return parsed.UTC().Format(time.RFC3339Nano), nil
}

func normalizeMallWeatherExportConditionalFormat(
	rule requestbody.MallWeatherExportConditionalFormat,
	allowedFields map[string]struct{},
) (requestbody.MallWeatherExportConditionalFormat, error) {
	rule.Field = strings.ToLower(strings.TrimSpace(rule.Field))
	rule.Operator = strings.ToLower(strings.TrimSpace(rule.Operator))
	rule.BackgroundColor = strings.ToLower(strings.TrimSpace(rule.BackgroundColor))
	rule.FontColor = strings.ToLower(strings.TrimSpace(rule.FontColor))
	_, fieldAllowed := allowedFields[rule.Field]
	colorPresent := rule.BackgroundColor != "" || rule.FontColor != ""
	colorsValid := (rule.BackgroundColor == "" || mallWeatherExportColorPattern.MatchString(rule.BackgroundColor)) &&
		(rule.FontColor == "" || mallWeatherExportColorPattern.MatchString(rule.FontColor))
	invalidFirstValue := rule.Value == nil
	if rule.Value != nil {
		invalidFirstValue = math.IsNaN(*rule.Value) || math.IsInf(*rule.Value, 0)
	}
	if !fieldAllowed || !colorPresent || !colorsValid || invalidFirstValue {
		return rule, fmt.Errorf("%w: invalid conditional format", ErrMallWeatherExportProfileInvalid)
	}
	needsSecondValue := rule.Operator == "between" || rule.Operator == "not_between"
	if needsSecondValue {
		if rule.SecondValue == nil || math.IsNaN(*rule.SecondValue) || math.IsInf(*rule.SecondValue, 0) {
			return rule, fmt.Errorf("%w: invalid conditional format", ErrMallWeatherExportProfileInvalid)
		}
	} else if rule.SecondValue != nil {
		return rule, fmt.Errorf("%w: invalid conditional format", ErrMallWeatherExportProfileInvalid)
	}
	if !mallWeatherExportConditionalOperators[rule.Operator] {
		return rule, fmt.Errorf("%w: invalid conditional format", ErrMallWeatherExportProfileInvalid)
	}
	return rule, nil
}

func validateMallWeatherExportFileNameTemplate(value string) error {
	if value == "" || utf8.RuneCountInString(value) > 255 || strings.ContainsAny(value, "/\\\x00\r\n") ||
		!strings.HasSuffix(strings.ToLower(value), ".xlsx") {
		return fmt.Errorf("%w: invalid file name template", ErrMallWeatherExportProfileInvalid)
	}
	return nil
}

func validMallWeatherSheetName(value string) bool {
	return value != "" && utf8.RuneCountInString(value) <= 31 && !strings.ContainsAny(value, `[]:*?/\\`) && value != "'"
}

func validMallWeatherExportTimeFormat(value string) bool {
	return value != "" && utf8.RuneCountInString(value) <= 64 && !strings.ContainsAny(value, "\x00\r\n")
}

func mallWeatherExportProfileDTO(row *model.MallWeatherExportProfile) (MallWeatherExportProfileDTO, error) {
	if row == nil || row.ID == 0 || row.Code == "" || row.Name == "" || row.Version == 0 || row.ProfileJSON == "" {
		return MallWeatherExportProfileDTO{}, fmt.Errorf("mall weather export profile: invalid stored profile")
	}
	var config MallWeatherExportProfileConfig
	decoder := json.NewDecoder(strings.NewReader(string(row.ProfileJSON)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return MallWeatherExportProfileDTO{}, fmt.Errorf("mall weather export profile: decode config: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return MallWeatherExportProfileDTO{}, fmt.Errorf("mall weather export profile: decode config: trailing data")
	}
	enabled := row.Enabled
	_, config, err := normalizeMallWeatherExportProfile(requestbody.MallWeatherExportProfileSaveRequest{
		Code:             row.Code,
		Name:             row.Name,
		Enabled:          &enabled,
		TimeZone:         config.TimeZone,
		UnitSystem:       config.UnitSystem,
		DateFormat:       config.DateFormat,
		DateTimeFormat:   config.DateTimeFormat,
		FileNameTemplate: config.FileNameTemplate,
		Filters:          config.Filters,
		Datasets:         config.Datasets,
	})
	if err != nil {
		return MallWeatherExportProfileDTO{}, fmt.Errorf("mall weather export profile: validate stored config: %w", err)
	}
	return MallWeatherExportProfileDTO{
		ID:               row.ID,
		Code:             row.Code,
		Name:             row.Name,
		Version:          row.Version,
		Enabled:          row.Enabled,
		TimeZone:         config.TimeZone,
		UnitSystem:       config.UnitSystem,
		DateFormat:       config.DateFormat,
		DateTimeFormat:   config.DateTimeFormat,
		FileNameTemplate: config.FileNameTemplate,
		Filters:          config.Filters,
		Datasets:         config.Datasets,
		CreatedBy:        row.CreatedBy,
		UpdatedBy:        row.UpdatedBy,
		CreatedAt:        row.CreatedAt.UTC(),
		UpdatedAt:        row.UpdatedAt.UTC(),
	}, nil
}

var mallWeatherExportColumnFormats = map[string]bool{
	"general":  true,
	"text":     true,
	"integer":  true,
	"decimal":  true,
	"percent":  true,
	"date":     true,
	"datetime": true,
}

var mallWeatherExportConditionalOperators = map[string]bool{
	"equal":                 true,
	"not_equal":             true,
	"less_than":             true,
	"less_than_or_equal":    true,
	"greater_than":          true,
	"greater_than_or_equal": true,
	"between":               true,
	"not_between":           true,
}
