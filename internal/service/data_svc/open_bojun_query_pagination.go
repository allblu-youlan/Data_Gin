package data_svc

import "fmt"

type OpenBojunQueryPagination struct {
	Page     int  `json:"page"`
	PageSize int  `json:"pageSize"`
	HasMore  bool `json:"hasMore"`
}

func normalizeOpenBojunQueryPagination(page, pageSize *int) (OpenBojunQueryPagination, error) {
	return normalizeOpenBojunQueryPaginationWithMaxPage(page, pageSize, 1000)
}

func normalizeOpenBojunQueryPaginationWithMaxPage(page, pageSize *int, maxPage int) (OpenBojunQueryPagination, error) {
	pagination := OpenBojunQueryPagination{Page: 1, PageSize: 50}
	if page != nil {
		pagination.Page = *page
	}
	if pageSize != nil {
		pagination.PageSize = *pageSize
	}
	if pagination.Page < 1 || pagination.Page > maxPage || pagination.PageSize < 1 || pagination.PageSize > 100 {
		return OpenBojunQueryPagination{}, fmt.Errorf("invalid page or pageSize")
	}
	return pagination, nil
}
