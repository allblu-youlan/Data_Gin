package model

// UsesJSONSnapshot isolates cleanup and reads from source business objects.
func (version ReportVersion) UsesJSONSnapshot() bool {
	return version.ExecutionMode == ReportExecutionModeRefCursor || version.ExecutionMode == ReportExecutionModeSelectSnapshot
}
func (run ReportRun) RetainsQuerySnapshot() bool {
	return run.DataScopeHash != "" && len(run.ExecutionPolicyJSON) > 0
}
