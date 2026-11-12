package reportoracle

// Fixed public projections from the YL_DBS attendance DDL. The three identity
// fields on detail datasets come from their unique employee/day DAILY row.
var feishuAttendanceTables = map[FeishuAttendanceDataset]feishuAttendanceTable{
	FeishuAttendanceDaily: {
		name:    "YL_DBS.ODS_FEISHU_ATTENDANCE_DAILY",
		orderBy: "a.WORK_DATE, a.USER_ID",
		columns: attendanceColumnsWithIdentity(
			FeishuAttendanceColumn{Name: "ATTENDANCE_GROUP_NAME", Comment: "考勤组名称", DataType: "VARCHAR2", Length: 500, Nullable: true},
			FeishuAttendanceColumn{Name: "SHIFT_NAME", Comment: "班次名称及时间段原文", DataType: "VARCHAR2", Length: 1000, Nullable: true},
			FeishuAttendanceColumn{Name: "TIME_ZONE", Comment: "飞书统计时区", DataType: "VARCHAR2", Length: 64, Nullable: true},
			FeishuAttendanceColumn{Name: "RESIGN_DATE", Comment: "离职日期；缺值为NULL", DataType: "DATE", Length: 7, Nullable: true},
			FeishuAttendanceColumn{Name: "REQUIRED_MINUTES", Comment: "应出勤分钟；无法明确换算时为NULL", DataType: "NUMBER", Length: 22, Nullable: true},
			FeishuAttendanceColumn{Name: "ACTUAL_MINUTES", Comment: "飞书统计实际出勤分钟；不由打卡时间差推算", DataType: "NUMBER", Length: 22, Nullable: true},
			FeishuAttendanceColumn{Name: "OVERTIME_MINUTES", Comment: "飞书统计加班分钟；不由实际减应出勤推算", DataType: "NUMBER", Length: 22, Nullable: true},
			FeishuAttendanceColumn{Name: "LEAVE_MINUTES", Comment: "请假分钟；缺值为NULL，不按每天8小时折算", DataType: "NUMBER", Length: 22, Nullable: true},
		),
	},
	FeishuAttendancePunch: {
		name:    "YL_DBS.ODS_FEISHU_ATTENDANCE_PUNCH",
		orderBy: "a.WORK_DATE, a.USER_ID, a.SOURCE_ROW_NO, a.PUNCH_INDEX, a.PUNCH_TYPE",
		columns: attendanceColumnsWithIdentity(
			FeishuAttendanceColumn{Name: "SOURCE_ROW_NO", Comment: "同员工同日期来源行序号，从1开始", DataType: "NUMBER", Length: 22},
			FeishuAttendanceColumn{Name: "PUNCH_INDEX", Comment: "指标编码中的打卡次序，不等同于来源打卡序号", DataType: "NUMBER", Length: 22},
			FeishuAttendanceColumn{Name: "PUNCH_TYPE", Comment: "打卡类型原值；缺失时取编码末段，1上班、2下班", DataType: "VARCHAR2", Length: 32},
			FeishuAttendanceColumn{Name: "TIME_CODE", Comment: "打卡时间指标编码", DataType: "VARCHAR2", Length: 128, Nullable: true},
			FeishuAttendanceColumn{Name: "RESULT_CODE", Comment: "打卡结果指标编码", DataType: "VARCHAR2", Length: 128, Nullable: true},
			FeishuAttendanceColumn{Name: "PUNCH_TIME_TEXT", Comment: "打卡时间原文；不拼接统计日期推断跨天时间", DataType: "VARCHAR2", Length: 64, Nullable: true},
			FeishuAttendanceColumn{Name: "RESULT_TEXT", Comment: "打卡结果原文", DataType: "VARCHAR2", Length: 1000, Nullable: true},
			FeishuAttendanceColumn{Name: "FEATURE_DATE", Comment: "打卡结果中的来源日期", DataType: "DATE", Length: 7, Nullable: true},
			FeishuAttendanceColumn{Name: "GROUP_NAME", Comment: "打卡所属考勤组名称", DataType: "VARCHAR2", Length: 500, Nullable: true},
			FeishuAttendanceColumn{Name: "GROUP_TYPE", Comment: "考勤组类型原值", DataType: "VARCHAR2", Length: 32, Nullable: true},
			FeishuAttendanceColumn{Name: "DAY_TYPE", Comment: "日期类型原值", DataType: "VARCHAR2", Length: 32, Nullable: true},
			FeishuAttendanceColumn{Name: "SOURCE_PUNCH_NO", Comment: "来源打卡序号原值", DataType: "NUMBER", Length: 22, Nullable: true},
			FeishuAttendanceColumn{Name: "PUNCH_TITLE", Comment: "打卡标题", DataType: "VARCHAR2", Length: 200, Nullable: true},
			FeishuAttendanceColumn{Name: "FEATURE_PUNCH_TIME_TEXT", Comment: "打卡结果中的时间原文", DataType: "VARCHAR2", Length: 64, Nullable: true},
			FeishuAttendanceColumn{Name: "SHIFT_TIME_TEXT", Comment: "班次时间原文", DataType: "VARCHAR2", Length: 64, Nullable: true},
			FeishuAttendanceColumn{Name: "PUNCH_SHIFT_TIME_TEXT", Comment: "打卡对应班次时间原文", DataType: "VARCHAR2", Length: 64, Nullable: true},
			FeishuAttendanceColumn{Name: "PUNCH_STATUS", Comment: "打卡状态原值", DataType: "VARCHAR2", Length: 32, Nullable: true},
			FeishuAttendanceColumn{Name: "PUNCH_SUB_STATUS", Comment: "打卡子状态原值", DataType: "VARCHAR2", Length: 32, Nullable: true},
			FeishuAttendanceColumn{Name: "STATUS_MESSAGE", Comment: "打卡状态说明原文", DataType: "VARCHAR2", Length: 1000, Nullable: true},
			FeishuAttendanceColumn{Name: "PUNCH_LOCATION_NAME", Comment: "打卡地点名称", DataType: "VARCHAR2", Length: 1000, Nullable: true},
			FeishuAttendanceColumn{Name: "REMARK", Comment: "打卡备注原文", DataType: "CLOB", Nullable: true},
		),
	},
	FeishuAttendanceItem: {
		name:    "YL_DBS.ODS_FEISHU_ATTENDANCE_ITEM",
		orderBy: "a.WORK_DATE, a.USER_ID, a.SOURCE_ROW_NO, a.ITEM_SEQ",
		columns: attendanceColumnsWithIdentity(
			FeishuAttendanceColumn{Name: "SOURCE_ROW_NO", Comment: "同员工同日期来源行序号，从1开始", DataType: "NUMBER", Length: 22},
			FeishuAttendanceColumn{Name: "ITEM_SEQ", Comment: "来源行内单元格顺序，从1开始，保留重复指标编码", DataType: "NUMBER", Length: 22},
			FeishuAttendanceColumn{Name: "METRIC_CODE", Comment: "单元格指标编码原值", DataType: "VARCHAR2", Length: 128},
			FeishuAttendanceColumn{Name: "METRIC_TITLE", Comment: "单元格标题原文", DataType: "VARCHAR2", Length: 500, Nullable: true},
			FeishuAttendanceColumn{Name: "VALUE_TEXT", Comment: "单元格展示值原文，保留横杠等值", DataType: "CLOB", Nullable: true},
			FeishuAttendanceColumn{Name: "DURATION_DAY", Comment: "天数原值；缺失为NULL，不自动折算分钟", DataType: "NUMBER", Length: 22, Nullable: true},
			FeishuAttendanceColumn{Name: "DURATION_HALF_DAY", Comment: "半天数原值；缺失为NULL，不自动折算分钟", DataType: "NUMBER", Length: 22, Nullable: true},
			FeishuAttendanceColumn{Name: "DURATION_HOUR", Comment: "小时数原值；缺失为NULL", DataType: "NUMBER", Length: 22, Nullable: true},
			FeishuAttendanceColumn{Name: "DURATION_HALF_HOUR", Comment: "半小时数原值；缺失为NULL", DataType: "NUMBER", Length: 22, Nullable: true},
			FeishuAttendanceColumn{Name: "DURATION_MINUTE", Comment: "分钟数原值；缺失为NULL", DataType: "NUMBER", Length: 22, Nullable: true},
		),
	},
}

func attendanceColumnsWithIdentity(columns ...FeishuAttendanceColumn) []FeishuAttendanceColumn {
	common := []FeishuAttendanceColumn{
		{Name: "ID", Comment: "本行主键ID，按字符串返回", DataType: "NUMBER", Length: 22},
		{Name: "USER_ID", Comment: "飞书查询返回的员工标识，不等同于工号", DataType: "VARCHAR2", Length: 64},
		{Name: "USER_NAME", Comment: "日汇总员工姓名，精确匹配", DataType: "VARCHAR2", Length: 200, Nullable: true, Filterable: true},
		{Name: "EMPLOYEE_NO", Comment: "员工工号文本，保留前导零", DataType: "VARCHAR2", Length: 64, Nullable: true},
		{Name: "DEPARTMENT_NAME", Comment: "日汇总来源完整部门文本；不保证为考勤当日组织快照", DataType: "VARCHAR2", Length: 1000, Nullable: true, Filterable: true},
		{Name: "WORK_DATE", Comment: "考勤统计日期，YYYY-MM-DD", DataType: "DATE", Length: 7},
		{Name: "MODIFIED_TIME", Comment: "本行本地ODS写入时间，不代表飞书业务修改时间", DataType: "DATE", Length: 7},
	}
	return append(common, columns...)
}
