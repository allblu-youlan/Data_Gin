package data_svc

import (
	"context"
	"strings"
	"unicode/utf8"

	"gin-biz-web-api/internal/reportrepo"
)

type reportDownloadCatalogStore interface {
	ListDrafts(context.Context, uint, reportrepo.DraftListQuery) (reportrepo.DraftPage, error)
}

type ReportDownloadCatalogService struct {
	store reportDownloadCatalogStore
}

func NewReportDownloadCatalogService() *ReportDownloadCatalogService {
	return NewReportDownloadCatalogServiceWithStore(reportrepo.New())
}

func NewReportDownloadCatalogServiceWithStore(store reportDownloadCatalogStore) *ReportDownloadCatalogService {
	if store == nil {
		panic("report download catalog service: nil store")
	}
	return &ReportDownloadCatalogService{store: store}
}

func (service *ReportDownloadCatalogService) List(
	ctx context.Context,
	actor, afterID uint,
	limit int,
	category, search string,
) (*ReportDraftListDTO, error) {
	if service == nil || service.store == nil || ctx == nil || actor == 0 {
		return nil, invalidReport("service, context and actor are required")
	}
	if limit == 0 {
		limit = defaultReportDraftPageSize
	}
	category = strings.TrimSpace(category)
	search = strings.TrimSpace(search)
	if limit < 1 || limit > maxReportDraftPageSize || utf8.RuneCountInString(category) > 64 || utf8.RuneCountInString(search) > 128 {
		return nil, invalidReport("invalid list filters")
	}
	page, err := service.store.ListDrafts(ctx, actor, reportrepo.DraftListQuery{
		AfterID:              afterID,
		Limit:                limit,
		Category:             category,
		Search:               search,
		PublishedOnly:        true,
		AllowQueryOnlySelect: true,
		Action:               reportrepo.ReportActionExport,
		AdditionalAction:     reportrepo.ReportActionQuery,
	})
	if err != nil {
		return nil, classifyReportStoreError(err)
	}
	result := &ReportDraftListDTO{
		Items:       make([]ReportDraftSummaryDTO, 0, len(page.Items)),
		HasMore:     page.HasMore,
		NextAfterID: page.NextAfterID,
	}
	for _, item := range page.Items {
		result.Items = append(result.Items, reportDraftSummaryDTO(item))
	}
	return result, nil
}
