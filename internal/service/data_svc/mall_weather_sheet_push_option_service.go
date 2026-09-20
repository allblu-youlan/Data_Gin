package data_svc

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/database"
)

const maxMallWeatherSheetPushOptions = 200

var ErrMallWeatherSheetPushOptionsUnavailable = errors.New("mall weather sheet push options: unavailable")

type mallWeatherSheetPushOptionStore interface {
	ListEnabledDestinations(context.Context, string, int) ([]model.DestinationDefinition, error)
	ListEnabledProfilesByCodes(context.Context, []string) ([]model.MallWeatherExportProfile, error)
}

type MallWeatherSheetPushOption struct {
	DestinationID  uint   `json:"destinationId"`
	Name           string `json:"name"`
	Code           string `json:"code"`
	ProfileID      uint   `json:"profileId"`
	ProfileCode    string `json:"profileCode"`
	ProfileVersion uint64 `json:"profileVersion"`
}

type MallWeatherSheetPushOptionResult struct {
	Items []MallWeatherSheetPushOption `json:"items"`
}

type MallWeatherSheetPushOptionService struct {
	store       mallWeatherSheetPushOptionStore
	permissions mallPermissionChecker
	now         func() time.Time
}

func NewMallWeatherSheetPushOptionService() *MallWeatherSheetPushOptionService {
	return &MallWeatherSheetPushOptionService{
		store:       data_dao.NewMallWeatherSheetPushOptionDAO(database.DB),
		permissions: newAccountPermissionChecker(database.DB),
		now:         time.Now,
	}
}

func newMallWeatherSheetPushOptionService(
	store mallWeatherSheetPushOptionStore,
	permissions mallPermissionChecker,
	now func() time.Time,
) (*MallWeatherSheetPushOptionService, error) {
	if store == nil || permissions == nil || now == nil {
		return nil, ErrMallWeatherSheetPushOptionsUnavailable
	}
	return &MallWeatherSheetPushOptionService{
		store:       store,
		permissions: permissions,
		now:         now,
	}, nil
}

func (service *MallWeatherSheetPushOptionService) List(
	ctx context.Context,
	actorUserID uint,
) (*MallWeatherSheetPushOptionResult, error) {
	if service == nil || service.store == nil || service.permissions == nil || service.now == nil || ctx == nil {
		return nil, ErrMallWeatherSheetPushOptionsUnavailable
	}
	if actorUserID == 0 {
		return nil, ErrMallForbidden
	}
	allowed, err := service.permissions.HasPermission(
		ctx,
		actorUserID,
		PermissionWeatherFeishuPush,
		service.now().UTC(),
	)
	if err != nil {
		return nil, fmt.Errorf("mall weather sheet push options: authorize: %w", err)
	}
	if !allowed {
		return nil, ErrMallForbidden
	}

	destinations, err := service.store.ListEnabledDestinations(
		ctx,
		mallWeatherFeishuDestinationType,
		maxMallWeatherSheetPushOptions+1,
	)
	if err != nil {
		return nil, fmt.Errorf("mall weather sheet push options: load destinations: %w", err)
	}
	if len(destinations) > maxMallWeatherSheetPushOptions {
		return nil, ErrMallWeatherSheetPushOptionsUnavailable
	}

	profileCodesByDestinationID := make(map[uint]string, len(destinations))
	profileCodeSet := make(map[string]struct{}, len(destinations))
	for index := range destinations {
		destination := &destinations[index]
		config, parseErr := parseMallWeatherFeishuDestinationConfig(destination.ConfigJSON)
		isValid := parseErr == nil && destination.ID != 0 && destination.Enabled &&
			strings.TrimSpace(destination.DestinationType) == mallWeatherFeishuDestinationType &&
			strings.TrimSpace(destination.Name) != "" && strings.TrimSpace(destination.Code) != ""
		if !isValid {
			continue
		}
		profileCodesByDestinationID[destination.ID] = config.ProfileCode
		profileCodeSet[config.ProfileCode] = struct{}{}
	}
	if len(profileCodeSet) == 0 {
		return &MallWeatherSheetPushOptionResult{Items: []MallWeatherSheetPushOption{}}, nil
	}

	profileCodes := make([]string, 0, len(profileCodeSet))
	for code := range profileCodeSet {
		profileCodes = append(profileCodes, code)
	}
	sort.Strings(profileCodes)
	profiles, err := service.store.ListEnabledProfilesByCodes(ctx, profileCodes)
	if err != nil {
		return nil, fmt.Errorf("mall weather sheet push options: load profiles: %w", err)
	}
	profilesByCode := make(map[string]model.MallWeatherExportProfile, len(profiles))
	for index := range profiles {
		profile := profiles[index]
		if profile.ID == 0 || profile.Version == 0 || !profile.Enabled ||
			strings.TrimSpace(profile.Code) == "" {
			continue
		}
		profilesByCode[profile.Code] = profile
	}

	items := make([]MallWeatherSheetPushOption, 0, len(destinations))
	for index := range destinations {
		destination := destinations[index]
		profileCode := profileCodesByDestinationID[destination.ID]
		profile, matched := profilesByCode[profileCode]
		if !matched {
			continue
		}
		items = append(items, MallWeatherSheetPushOption{
			DestinationID:  destination.ID,
			Name:           strings.TrimSpace(destination.Name),
			Code:           strings.TrimSpace(destination.Code),
			ProfileID:      profile.ID,
			ProfileCode:    profile.Code,
			ProfileVersion: profile.Version,
		})
	}
	return &MallWeatherSheetPushOptionResult{Items: items}, nil
}
