package config

import (
	"fmt"
	"gin-biz-web-api/pkg/kaiyu"
	"os"
	"strconv"
	"strings"
	"time"
)

const KaiyuHourlyCron = "0 5 * * * *"

type KaiyuConfig struct {
	Enabled bool
	Client  kaiyu.Config
}

func LoadKaiyuConfig() (KaiyuConfig, error) {
	var result KaiyuConfig
	if raw := strings.TrimSpace(os.Getenv("KAIYU_ENABLED")); raw != "" {
		enabled, err := strconv.ParseBool(raw)
		if err != nil {
			return result, fmt.Errorf("KAIYU_ENABLED must be a boolean")
		}
		result.Enabled = enabled
	}
	result.Client = kaiyu.Config{BaseURL: strings.TrimSpace(os.Getenv("KAIYU_BASE_URL")), SecurityID: strings.TrimSpace(os.Getenv("KAIYU_SECURITY_ID")), SecurityCode: os.Getenv("KAIYU_SECURITY_CODE"), AppID: strings.TrimSpace(os.Getenv("KAIYU_APP_ID")), Version: strings.TrimSpace(os.Getenv("KAIYU_VERSION")), Timeout: 30 * time.Second}
	return result, nil
}
