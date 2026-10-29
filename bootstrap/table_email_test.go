package bootstrap

import (
	"gin-biz-web-api/job"
	"testing"
)

func TestTableEmailScheduledScanUsesDeliveryQueue(t *testing.T) {
	registrar := &fakeScheduledTaskRegistrar{}
	registerTableEmailScheduledTasks(registrar)
	if registrar.calls != 1 || len(registrar.tasks) != 1 || registrar.tasks[0].Type() != job.TypeTableEmailScan || registrar.crons[0] != "* * * * *" {
		t.Fatalf("schedule: %#v", registrar)
	}
}
