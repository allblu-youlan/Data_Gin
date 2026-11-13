package model

const (
	PermissionTmallRead               = "tmall.read"
	PermissionBojunRead               = "bojun.read"
	PermissionYouzanRead              = "youzan.read"
	PermissionWeatherAPIRead          = "weather.api.read"
	PermissionBusinessOverviewAPIRead = "business_overview.api.read"
	PermissionMonitoringAPIRead       = "monitoring.api.read"
	PermissionMonitoringHourlyRead    = "monitoring.hourly.read"
	PermissionMonitoringDailyRead     = "monitoring.daily.read"
	PermissionMonitoringAreaRead      = "monitoring.area.read"
)

type PermissionDefinition struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type PermissionGroup struct {
	Code        string                 `json:"code"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Children    []PermissionDefinition `json:"children"`
}

// Only public API read permissions belong here. Administrative capabilities
// must be granted separately and never inherit access from an API module.
var openAPIPermissionGroups = []PermissionGroup{
	{
		Code: PermissionTmallRead, Name: "天猫", Description: "授权后可查询天猫下全部二级接口",
		Children: []PermissionDefinition{
			{Code: PermissionTmallSycmRead, Name: "天猫生意参谋查询", Description: "查询生意参谋商品订单数据"},
			{Code: PermissionTmallQztgRead, Name: "天猫全站推广查询", Description: "查询全站推广商品数据"},
			{Code: PermissionTmallTotRead, Name: "天猫商品汇总查询", Description: "查询商品汇总数据"},
			{Code: PermissionTmallOrdersRead, Name: "天猫订单商品明细查询", Description: "实时查询订单商品明细数据"},
		},
	},
	{
		Code: PermissionBojunRead, Name: "伯俊", Description: "授权后可查询伯俊下全部二级接口",
		Children: []PermissionDefinition{
			{Code: PermissionBojunOrderRead, Name: "Bojun 订单查询", Description: "查询伯俊订单数据"},
			{Code: PermissionBojunStoreRead, Name: "伯俊店仓查询", Description: "查询伯俊店仓档案"},
			{Code: PermissionBojunStockRead, Name: "伯俊实时库存查询", Description: "按店仓和商品条码、款色或款号查询实时库存"},
		},
	},
	{
		Code: PermissionYouzanRead, Name: "有赞", Description: "授权后可查询有赞订单主表与商品明细接口",
		Children: []PermissionDefinition{
			{Code: PermissionYouzanOrdersRead, Name: "有赞订单查询", Description: "查询有赞订单主表数据"},
			{Code: PermissionYouzanOrderItemsRead, Name: "有赞订单商品明细查询", Description: "查询有赞订单商品明细数据"},
		},
	},
	{
		Code: PermissionWeatherAPIRead, Name: "天气", Description: "授权后可查询天气开放接口，不包含天气管理操作",
		Children: []PermissionDefinition{
			{Code: PermissionWeatherRead, Name: "天气数据查询", Description: "查询授权商场的天气数据"},
		},
	},
	{
		Code: PermissionBusinessOverviewAPIRead, Name: "营业概况", Description: "授权后可查询营业概况下全部二级接口",
		Children: []PermissionDefinition{
			{Code: PermissionBusinessOverviewRead, Name: "营业金额查询", Description: "查询销售日结、收款构成与门店对账数据"},
		},
	},
	{
		Code: PermissionMonitoringAPIRead, Name: "监控数据", Description: "授权后可查询授权店铺的小时、日汇总和区域监控数据",
		Children: []PermissionDefinition{
			{Code: PermissionMonitoringHourlyRead, Name: "小时监控查询", Description: "按业务店铺编码查询多日小时监控数据"},
			{Code: PermissionMonitoringDailyRead, Name: "每日监控汇总", Description: "按日汇总已采集的小时客流计数"},
			{Code: PermissionMonitoringAreaRead, Name: "区域监控查询", Description: "查询单日区域曝光和停留数据"},
		},
	},
}

// OpenAPIPermissionGroups is the shared catalog for authorization and API docs.
// The returned values are safe for callers to modify.
func OpenAPIPermissionGroups() []PermissionGroup {
	groups := make([]PermissionGroup, len(openAPIPermissionGroups))
	copy(groups, openAPIPermissionGroups)
	for i := range groups {
		groups[i].Children = append([]PermissionDefinition(nil), groups[i].Children...)
	}
	return groups
}

func OpenAPIPermissionParent(code string) string {
	for _, group := range openAPIPermissionGroups {
		for _, child := range group.Children {
			if child.Code == code {
				return group.Code
			}
		}
	}
	return ""
}

func OpenAPIPermissionLevel(code string) int {
	for _, group := range openAPIPermissionGroups {
		if group.Code == code {
			return 1
		}
	}
	if OpenAPIPermissionParent(code) != "" {
		return 2
	}
	return 0
}

func OpenAPIPermissionName(code string) string {
	for _, group := range openAPIPermissionGroups {
		if group.Code == code {
			return group.Name
		}
		for _, child := range group.Children {
			if child.Code == code {
				return child.Name
			}
		}
	}
	return code
}

// OpenAPIPermissionCandidates never infers wildcards or grants parents from children.
func OpenAPIPermissionCandidates(code string) []string {
	if parent := OpenAPIPermissionParent(code); parent != "" {
		return []string{code, parent}
	}
	return []string{code}
}

func ExpandOpenAPIPermissions(codes []string) []string {
	result := make([]string, 0, len(codes))
	seen := make(map[string]bool, len(codes))
	appendCode := func(code string) {
		if !seen[code] {
			seen[code] = true
			result = append(result, code)
		}
	}
	for _, code := range codes {
		appendCode(code)
		for _, group := range openAPIPermissionGroups {
			if group.Code == code {
				for _, child := range group.Children {
					appendCode(child.Code)
				}
			}
		}
	}
	return result
}
