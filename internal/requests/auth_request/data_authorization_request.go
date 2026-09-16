package auth_request

type DataAuthorizationPermissionInput struct {
	Permission string `json:"permission"`
	ExpiresAt  string `json:"expiresAt"`
	Permanent  bool   `json:"permanent,omitempty"`
}

type DataAuthorizationAccountCreateRequest struct {
	Account     string                             `json:"account"`
	Nickname    string                             `json:"nickname"`
	Permissions []DataAuthorizationPermissionInput `json:"permissions"`
	Reason      string                             `json:"reason"`
}

type DataAuthorizationAccountQueryRequest struct {
	Keyword  string `json:"keyword"`
	BeforeID uint   `json:"beforeId"`
	PageSize int    `json:"pageSize"`
}

type DataAuthorizationGrantRequest struct {
	Permission string `json:"permission"`
	ExpiresAt  string `json:"expiresAt"`
	Permanent  bool   `json:"permanent,omitempty"`
	Reason     string `json:"reason"`
}

type DataAuthorizationRevokeRequest struct {
	Permission string `json:"permission"`
	Reason     string `json:"reason"`
}

type DataAuthorizationTokenReissueRequest struct {
	Reason string `json:"reason"`
}

type DataAuthorizationAuditQueryRequest struct {
	TargetUserID uint   `json:"targetUserId"`
	Permission   string `json:"permission"`
	Action       string `json:"action"`
	StartTime    string `json:"startTime"`
	EndTime      string `json:"endTime"`
	BeforeID     uint   `json:"beforeId"`
	PageSize     int    `json:"pageSize"`
}
