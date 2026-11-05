package shanghaimall

import (
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"gin-biz-web-api/pkg/config"
)

type syandataConfig struct {
	URL            string
	AppKey         string
	AppID          string
	AppSecret      string
	TerminalNumber string
	Client         *http.Client
}

type xianConfig struct {
	TokenURL string
	PostURL  string
	Account  string
	Password string
	ShopID   string
	BranchID string
	Num      string
	Client   *http.Client
}

type shangshengConfig struct {
	URL    string
	GID    string
	MID    string
	VSN    string
	Client *http.Client
}

type kerryConfig struct {
	LoginURL       string
	SalesURL       string
	ProgramName    string
	DeviceID       string
	ActivationCode string
	LocationCode   string
	ItemCode       string
	Cashier        string
	StoreCode      string
	TillID         string
	StaffCode      string
	TenderCode     string
	Client         *http.Client
}

type xinjiaCenterConfig struct {
	URL           string
	ProductCode   string
	StoreCode     string
	TenantCode    string
	Authorization string
	Client        *http.Client
	Now           func() time.Time
}

func syandataTargetConfig(target Target) syandataConfig {
	prefix := targetEnvPrefix(target)
	return syandataConfig{
		URL:            envString(prefix+"_SYANDATA_URL", config.GetString("ShanghaiMall.Syandata.URL", "http://api.syandata.com/oapi/rest")),
		AppKey:         envString(prefix+"_SYANDATA_APP_KEY", ""),
		AppID:          envString(prefix+"_SYANDATA_APP_ID", ""),
		AppSecret:      envString(prefix+"_SYANDATA_APP_SECRET", ""),
		TerminalNumber: envString(prefix+"_SYANDATA_TERMINAL_NUMBER", ""),
		Client:         defaultClient(),
	}
}

func qiantanConfig() xianConfig {
	return xianConfig{
		TokenURL: envString("SHANGHAI_QIANTAN_XIAN_TOKEN_URL", config.GetString("ShanghaiMall.Qiantan.TokenURL", "")),
		PostURL:  envString("SHANGHAI_QIANTAN_XIAN_POST_URL", config.GetString("ShanghaiMall.Qiantan.PostURL", "")),
		Account:  envString("SHANGHAI_QIANTAN_XIAN_ACCOUNT", ""),
		Password: envString("SHANGHAI_QIANTAN_XIAN_PASSWORD", ""),
		ShopID:   envString("SHANGHAI_QIANTAN_XIAN_SHOP_ID", ""),
		BranchID: envString("SHANGHAI_QIANTAN_XIAN_BRANCH_ID", ""),
		Num:      envString("SHANGHAI_QIANTAN_XIAN_NUM", ""),
		Client:   defaultClient(),
	}
}

func shangshengConfigFromEnv() shangshengConfig {
	return shangshengConfig{
		URL:    envString("SHANGHAI_SHANGSHENG_URL", config.GetString("ShanghaiMall.Shangsheng.URL", "")),
		GID:    envString("SHANGHAI_SHANGSHENG_GID", ""),
		MID:    envString("SHANGHAI_SHANGSHENG_MID", ""),
		VSN:    envString("SHANGHAI_SHANGSHENG_VSN", ""),
		Client: defaultClient(),
	}
}

func kerryConfigFromEnv() kerryConfig {
	return kerryConfig{
		LoginURL:       envString("SHANGHAI_JIALICHENG_KERRY_LOGIN_URL", config.GetString("ShanghaiMall.Jialicheng.LoginURL", "")),
		SalesURL:       envString("SHANGHAI_JIALICHENG_KERRY_SALES_URL", config.GetString("ShanghaiMall.Jialicheng.SalesURL", "")),
		ProgramName:    envString("SHANGHAI_JIALICHENG_KERRY_PROGRAM_NAME", ""),
		DeviceID:       envString("SHANGHAI_JIALICHENG_KERRY_DEVICE_ID", ""),
		ActivationCode: envString("SHANGHAI_JIALICHENG_KERRY_ACTIVATION_CODE", ""),
		LocationCode:   envString("SHANGHAI_JIALICHENG_KERRY_LOCATION_CODE", ""),
		ItemCode:       envString("SHANGHAI_JIALICHENG_KERRY_ITEM_CODE", ""),
		Cashier:        envString("SHANGHAI_JIALICHENG_KERRY_CASHIER", ""),
		StoreCode:      envString("SHANGHAI_JIALICHENG_KERRY_STORE_CODE", ""),
		TillID:         envString("SHANGHAI_JIALICHENG_KERRY_TILL_ID", ""),
		StaffCode:      envString("SHANGHAI_JIALICHENG_KERRY_STAFF_CODE", ""),
		TenderCode:     envString("SHANGHAI_JIALICHENG_KERRY_TENDER_CODE", "OT"),
		Client:         defaultClient(),
	}
}

func xinjiaCenterConfigFromEnv() xinjiaCenterConfig {
	return xinjiaCenterConfig{
		URL:           envString("SHANGHAI_XINJIA_CENTER_URL", config.GetString("ShanghaiMall.XinjiaCenter.URL", "")),
		ProductCode:   envString("SHANGHAI_XINJIA_CENTER_PRODUCT_CODE", config.GetString("ShanghaiMall.XinjiaCenter.ProductCode", "")),
		StoreCode:     envString("SHANGHAI_XINJIA_CENTER_STORE_CODE", config.GetString("ShanghaiMall.XinjiaCenter.StoreCode", "")),
		TenantCode:    envString("SHANGHAI_XINJIA_CENTER_TENANT_CODE", config.GetString("ShanghaiMall.XinjiaCenter.TenantCode", "")),
		Authorization: envString("SHANGHAI_XINJIA_CENTER_AUTHORIZATION", config.GetString("ShanghaiMall.XinjiaCenter.Authorization", "")),
		Client:        defaultClient(),
	}
}

func targetEnvPrefix(target Target) string {
	switch target {
	case TargetPanlong:
		return "SHANGHAI_PANLONG"
	case TargetXintiandi:
		return "SHANGHAI_XINTIANDI"
	default:
		return "SHANGHAI_UNKNOWN"
	}
}

func envString(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			return parsed
		}
	}
	return fallback
}
