package reportoracle

type youzanColumn struct {
	name   string
	isDate bool
}

type youzanTable struct {
	name         string
	columns      []youzanColumn
	filterLimits map[string]int
}

// Only explicitly published columns are selected; buyer data and source JSON
// remain outside the open API projection.
var youzanTables = map[YouzanDataset]youzanTable{
	YouzanOrders: {
		name: "YL_DBS.DWD_YOUZAN_ORDERS",
		columns: []youzanColumn{
			{name: "ID"},
			{name: "TID"},
			{name: "STATUS"},
			{name: "STATUS_STR"},
			{name: "TYPE"},
			{name: "NODE_KDT_ID"},
			{name: "ROOT_KDT_ID"},
			{name: "SHOP_NAME"},
			{name: "CREATED", isDate: true},
			{name: "UPDATE_TIME", isDate: true},
			{name: "PAY_TIME", isDate: true},
			{name: "SUCCESS_TIME", isDate: true},
			{name: "CONSIGN_TIME", isDate: true},
			{name: "CONFIRM_TIME", isDate: true},
			{name: "TOTAL_FEE"},
			{name: "POST_FEE"},
			{name: "PAYMENT"},
			{name: "REFUND_STATE"},
			{name: "CREATED_DWD", isDate: true},
			{name: "MODIFIED_DWD", isDate: true},
		},
		filterLimits: map[string]int{"TID": 200, "STATUS": 100, "NODE_KDT_ID": 200, "ROOT_KDT_ID": 200},
	},
	YouzanOrderItems: {
		name: "YL_DBS.DWD_YOUZAN_ORDERS_ITEM",
		columns: []youzanColumn{
			{name: "ID"},
			{name: "TID"},
			{name: "OID"},
			{name: "TITLE"},
			{name: "NUM"},
			{name: "ITEM_ID"},
			{name: "SKU_ID"},
			{name: "ITEM_NO"},
			{name: "SKU_NO"},
			{name: "ITEM_BARCODE"},
			{name: "SKU_BARCODE"},
			{name: "PRICE"},
			{name: "DISCOUNT_PRICE"},
			{name: "TOTAL_FEE"},
			{name: "PAYMENT"},
			{name: "CREATED", isDate: true},
			{name: "UPDATE_TIME", isDate: true},
			{name: "CREATED_DWD", isDate: true},
			{name: "MODIFIED_DWD", isDate: true},
		},
		filterLimits: map[string]int{"TID": 200, "OID": 200, "ITEM_ID": 200, "SKU_ID": 200, "SKU_NO": 200},
	},
}
