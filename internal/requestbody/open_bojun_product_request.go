package requestbody

// OpenBojunProductQueryRequest is the public contract for querying one
// product from the Bojun product view.
type OpenBojunProductQueryRequest struct {
	ProductCode string `json:"productCode"`
}
