package data_svc

import (
	"context"
	"encoding/json"
	"fmt"
	"gin-biz-web-api/internal/reportdataset"
	"gin-biz-web-api/model"
	"strconv"
	"time"
)

func reportResultRetention(run model.ReportRun, fallback time.Duration) time.Duration {
	if run.DataScopeHash == "" {
		return fallback
	}
	var policy reportdataset.Policy
	if json.Unmarshal([]byte(run.ExecutionPolicyJSON), &policy) != nil || policy.ResultRetentionHours < 1 {
		return fallback
	}
	return time.Duration(policy.ResultRetentionHours) * time.Hour
}
func executeSelectReport(ctx context.Context, req reportProcedureExecutionRequest, password string) (int64, error) {
	if !selectReportsEnabled() {
		return 0, fmt.Errorf("SELECT_SNAPSHOT disabled")
	}
	runtime := req.Runtime
	c, e := decodeSelectContract(runtime.Version)
	if e != nil {
		return 0, e
	}
	c, hash, e := selectAuthorization(ctx, runtime.Version.DatasourceID, c, runtime.Run.RequestedBy)
	if e != nil {
		return 0, e
	}
	if hash != runtime.Run.DataScopeHash {
		return 0, fmt.Errorf("REPORT_METADATA_CHANGED: data authorization changed")
	}
	var params map[string]json.RawMessage
	if e = json.Unmarshal([]byte(runtime.Run.NormalizedParametersJSON), &params); e != nil {
		return 0, e
	}
	inputs, e := reportdataset.Decode[reportdataset.QueryInputs]([]byte(runtime.Run.QueryInputsJSON))
	if e != nil {
		return 0, e
	}
	plan, e := c.Build(params, inputs, strconv.FormatUint(uint64(runtime.Run.RequestedBy), 10), false)
	if e != nil {
		return 0, e
	}
	timeout := 300 * time.Second
	if runtime.Datasource.QueryTimeoutSeconds > 0 && runtime.Datasource.QueryTimeoutSeconds < 300 {
		timeout = time.Duration(runtime.Datasource.QueryTimeoutSeconds) * time.Second
	}
	queryCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	a, release, e := acquireSelectOracle(queryCtx, runtime.Datasource, password)
	if e != nil {
		return 0, e
	}
	defer release()
	if e = inspectSelectSource(queryCtx, a, c); e != nil {
		return 0, e
	}
	snapshot, e := a.ExecuteSelectSnapshot(queryCtx, runtime.Run.RunUUID, c, plan, 100000, 200*1024*1024)
	return snapshot.RowCount, e
}
