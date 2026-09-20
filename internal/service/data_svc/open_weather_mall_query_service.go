package data_svc

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/internal/requestbody"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/database"
)

const (
	openWeatherMallDefaultPageSize = 50
	openWeatherMallMaxPageSize     = 100
	openWeatherMallMaxCursorLength = 512
	openWeatherMallQueryTimeout    = 3 * time.Second
)

var (
	ErrOpenWeatherMallForbidden    = errors.New("open weather mall: forbidden")
	ErrOpenWeatherMallInvalidQuery = errors.New("open weather mall: invalid query")
)

type openWeatherMallReader interface {
	ListOpenWeatherMallsAfterID(context.Context, uint, uint, int) ([]model.Mall, error)
	CountOpenWeatherMalls(context.Context, uint) (int64, error)
}

type openWeatherMallPermissionReader interface {
	HasPermission(context.Context, uint, string, time.Time) (bool, error)
}

type OpenWeatherMallQueryService struct {
	malls       openWeatherMallReader
	permissions openWeatherMallPermissionReader
	now         func() time.Time
}

type OpenWeatherMallQueryResult struct {
	Items      []OpenWeatherMallDTO      `json:"items"`
	Pagination OpenWeatherMallPagination `json:"pagination"`
}

type OpenWeatherMallDTO struct {
	MallID           uint     `json:"mallId"`
	MallCode         string   `json:"mallCode"`
	NameCN           string   `json:"nameCn"`
	NameEN           string   `json:"nameEn"`
	Location         string   `json:"location"`
	Address          string   `json:"address"`
	Longitude        *float64 `json:"longitude"`
	Latitude         *float64 `json:"latitude"`
	CoordinateSystem string   `json:"coordinateSystem"`
	TimeZone         string   `json:"timeZone"`
}

type OpenWeatherMallPagination struct {
	OpenPagination
	NextCursor string `json:"nextCursor"`
	HasMore    bool   `json:"hasMore"`
}

type openWeatherMallCursor struct {
	Version int  `json:"version"`
	ID      uint `json:"id"`
	Page    int  `json:"page,omitempty"`
}

func NewOpenWeatherMallQueryService() *OpenWeatherMallQueryService {
	return newOpenWeatherMallQueryService(
		data_dao.NewMallDAO(database.DB),
		newAccountPermissionChecker(database.DB),
		time.Now,
	)
}

func newOpenWeatherMallQueryService(
	malls openWeatherMallReader,
	permissions openWeatherMallPermissionReader,
	now func() time.Time,
) *OpenWeatherMallQueryService {
	if malls == nil || permissions == nil || now == nil {
		panic("open weather mall query service: nil dependency")
	}
	return &OpenWeatherMallQueryService{malls: malls, permissions: permissions, now: now}
}

func (service *OpenWeatherMallQueryService) Query(
	ctx context.Context,
	actorUserID uint,
	request requestbody.OpenWeatherMallQueryRequest,
) (*OpenWeatherMallQueryResult, error) {
	if ctx == nil {
		return nil, fmt.Errorf("open weather mall query: nil context")
	}
	if err := service.authorize(ctx, actorUserID); err != nil {
		return nil, err
	}
	afterID, page, pageSize, err := normalizeOpenWeatherMallQuery(request)
	if err != nil {
		return nil, err
	}

	queryCtx, cancel := context.WithTimeout(ctx, openWeatherMallQueryTimeout)
	defer cancel()
	totalItems, err := service.malls.CountOpenWeatherMalls(queryCtx, actorUserID)
	if err != nil {
		return nil, fmt.Errorf("open weather mall query: count malls: %w", err)
	}
	rows, err := service.malls.ListOpenWeatherMallsAfterID(queryCtx, actorUserID, afterID, pageSize+1)
	if err != nil {
		return nil, fmt.Errorf("open weather mall query: list malls: %w", err)
	}

	hasMore := len(rows) > pageSize
	if hasMore {
		rows = rows[:pageSize]
	}
	items := make([]OpenWeatherMallDTO, 0, len(rows))
	for index := range rows {
		items = append(items, openWeatherMallDTO(&rows[index]))
	}
	nextCursor := ""
	if hasMore && len(rows) > 0 {
		nextPage, pageErr := nextOpenCursorPage(page)
		if pageErr != nil {
			return nil, pageErr
		}
		nextCursor, err = encodeOpenWeatherMallCursor(rows[len(rows)-1].ID, nextPage)
		if err != nil {
			return nil, fmt.Errorf("open weather mall query: encode cursor: %w", err)
		}
	}
	return &OpenWeatherMallQueryResult{
		Items: items,
		Pagination: OpenWeatherMallPagination{
			OpenPagination: newOpenPagination(page, pageSize, totalItems),
			NextCursor:     nextCursor, HasMore: hasMore,
		},
	}, nil
}

func (service *OpenWeatherMallQueryService) authorize(ctx context.Context, actorUserID uint) error {
	if actorUserID == 0 {
		return ErrOpenWeatherMallForbidden
	}
	allowed, err := service.permissions.HasPermission(
		ctx,
		actorUserID,
		model.PermissionWeatherRead,
		service.now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("open weather mall query: authorize: %w", err)
	}
	if !allowed {
		return ErrOpenWeatherMallForbidden
	}
	return nil
}

func normalizeOpenWeatherMallQuery(request requestbody.OpenWeatherMallQueryRequest) (uint, int, int, error) {
	pageSize := request.PageSize
	if pageSize == 0 {
		pageSize = openWeatherMallDefaultPageSize
	}
	if pageSize < 1 || pageSize > openWeatherMallMaxPageSize {
		return 0, 0, 0, fmt.Errorf("%w: invalid pageSize", ErrOpenWeatherMallInvalidQuery)
	}
	cursorValue := strings.TrimSpace(request.Cursor)
	if cursorValue == "" {
		return 0, 1, pageSize, nil
	}
	cursor, err := decodeOpenWeatherMallCursor(cursorValue)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("%w: invalid cursor", ErrOpenWeatherMallInvalidQuery)
	}
	return cursor.ID, openCursorPage(cursor.Page, true), pageSize, nil
}

func openWeatherMallDTO(mall *model.Mall) OpenWeatherMallDTO {
	return OpenWeatherMallDTO{
		MallID:           mall.ID,
		MallCode:         mall.MallCode,
		NameCN:           mall.NameCN,
		NameEN:           mall.NameEN,
		Location:         openWeatherMallLocation(mall),
		Address:          openWeatherMallAddress(mall),
		Longitude:        mall.WeatherLongitude,
		Latitude:         mall.WeatherLatitude,
		CoordinateSystem: strings.ToUpper(mall.WeatherCoordinateSystem),
		TimeZone:         mall.Timezone,
	}
}

func openWeatherMallAddress(mall *model.Mall) string {
	if mall == nil {
		return ""
	}
	if address := strings.TrimSpace(mall.AddressStandardized); address != "" {
		return address
	}
	return strings.TrimSpace(mall.AddressRaw)
}

func openWeatherMallLocation(mall *model.Mall) string {
	if mall == nil {
		return ""
	}
	parts := make([]string, 0, 4)
	seen := make(map[string]struct{}, 4)
	for _, value := range []string{mall.Province, mall.City, mall.District, mall.Township} {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		parts = append(parts, value)
	}
	return strings.Join(parts, " ")
}

func encodeOpenWeatherMallCursor(id uint, page int) (string, error) {
	payload, err := json.Marshal(openWeatherMallCursor{Version: 1, ID: id, Page: page})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeOpenWeatherMallCursor(value string) (openWeatherMallCursor, error) {
	if len(value) > openWeatherMallMaxCursorLength {
		return openWeatherMallCursor{}, ErrOpenWeatherMallInvalidQuery
	}
	payload, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return openWeatherMallCursor{}, ErrOpenWeatherMallInvalidQuery
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var cursor openWeatherMallCursor
	if err := decoder.Decode(&cursor); err != nil || cursor.Version != 1 || cursor.ID == 0 || invalidOpenCursorPage(cursor.Page) {
		return openWeatherMallCursor{}, ErrOpenWeatherMallInvalidQuery
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return openWeatherMallCursor{}, ErrOpenWeatherMallInvalidQuery
	}
	return cursor, nil
}
