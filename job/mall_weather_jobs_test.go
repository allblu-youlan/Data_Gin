package job

import (
	"reflect"
	"strings"
	"testing"

	"github.com/hibiken/asynq"
)

func TestMallWeatherTaskConstructorsUseNonSensitivePayloads(t *testing.T) {
	tests := []struct {
		name     string
		taskType string
		queue    string
		create   func() (*asynq.Task, error)
	}{
		{"geocode", TypeMallGeocode, MallWeatherQueueName, func() (*asynq.Task, error) {
			return NewMallGeocodeTask(MallGeocodeTaskPayload{MallID: 11, MallVersion: 3, AddressHash: strings.Repeat("a", 64)})
		}},
		{"fast", TypeMallWeatherFast, MallWeatherQueueName, func() (*asynq.Task, error) { return NewMallWeatherFastTask(weatherTaskPayload("fast:11:202607221030")) }},
		{"full", TypeMallWeatherFull, MallWeatherQueueName, func() (*asynq.Task, error) { return NewMallWeatherFullTask(weatherTaskPayload("full:11:2026072210")) }},
		{"life index", TypeMallWeatherLifeIndex, MallWeatherQueueName, func() (*asynq.Task, error) {
			return NewMallWeatherLifeIndexTask(weatherTaskPayload("life:11:2026072210"))
		}},
		{"repair", TypeMallWeatherRepair, MallWeatherQueueName, func() (*asynq.Task, error) {
			return NewMallWeatherRepairTask(weatherTaskPayloadForEndpoint("repair:42:1", "v26_weather"))
		}},
		{"manual", TypeMallWeatherManual, MallWeatherQueueName, func() (*asynq.Task, error) {
			return NewMallWeatherManualTask(weatherTaskPayloadForEndpoint("manual:4f8d2a", "v3_life_index"))
		}},
		{"export", TypeMallWeatherExport, MallExportQueueName, func() (*asynq.Task, error) {
			return NewMallWeatherExportTask(MallWeatherExportTaskPayload{ExportJobID: 22})
		}},
		{"export cleanup", TypeMallWeatherExportCleanup, MallExportQueueName, NewMallWeatherExportCleanupTask},
		{"feishu", TypeMallWeatherFeishu, MallDeliveryQueueName, func() (*asynq.Task, error) {
			return NewMallWeatherFeishuTask(MallWeatherFeishuTaskPayload{PipelineRunID: 33})
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task, err := tt.create()
			if err != nil {
				t.Fatalf("create task: %v", err)
			}
			if task.Type() != tt.taskType {
				t.Fatalf("task type = %q, want %q", task.Type(), tt.taskType)
			}
			queue, ok := ExpectedMallWeatherQueue(task.Type())
			if !ok || queue != tt.queue {
				t.Fatalf("queue = %q, %v, want %q, true", queue, ok, tt.queue)
			}

			payload := strings.ToLower(string(task.Payload()))
			for _, forbidden := range []string{"key", "secret", "token", "password", "url"} {
				if strings.Contains(payload, forbidden) {
					t.Fatalf("payload contains forbidden credential or URL marker %q: %s", forbidden, payload)
				}
			}
		})
	}
}

func TestMallWeatherScheduleDefinitionsCoverProfiles(t *testing.T) {
	definitions, err := MallWeatherScheduleDefinitions("*/10 * * * *", "7 * * * *", true)
	if err != nil {
		t.Fatalf("MallWeatherScheduleDefinitions() error=%v", err)
	}
	if len(definitions) != 7 {
		t.Fatalf("definitions=%v", definitions)
	}
	wantProfiles := map[string]int{"full": 2, "standard": 2, "economy": 2, "": 1}
	gotProfiles := make(map[string]int)
	for _, definition := range definitions {
		if definition.Payload.TaskType == TypeMallWeatherLifeIndex {
			t.Fatalf("life index schedule must use the comprehensive weather task: %+v", definition)
		}
		gotProfiles[definition.Payload.DetailProfile]++
		if _, err := NewMallWeatherScheduleTask(definition.Payload); err != nil {
			t.Fatalf("NewMallWeatherScheduleTask(%+v) error=%v", definition.Payload, err)
		}
	}
	if !reflect.DeepEqual(gotProfiles, wantProfiles) {
		t.Fatalf("profiles=%v", gotProfiles)
	}
	if definitions[len(definitions)-1].Payload.TaskType != TypeMallWeatherRepair || definitions[len(definitions)-1].CronExpr != "*/15 * * * *" {
		t.Fatalf("repair definition=%+v", definitions[len(definitions)-1])
	}
}

func TestMallWeatherScheduleDefinitionsOmitRepairWhenDisabled(t *testing.T) {
	definitions, err := MallWeatherScheduleDefinitions("*/10 * * * *", "7 * * * *", false)
	if err != nil {
		t.Fatalf("MallWeatherScheduleDefinitions() error=%v", err)
	}
	if len(definitions) != 6 {
		t.Fatalf("definitions=%v", definitions)
	}
	for _, definition := range definitions {
		if definition.Payload.TaskType == TypeMallWeatherRepair {
			t.Fatalf("disabled repair schedule was registered: %+v", definition)
		}
	}
}

func TestMallWeatherScheduleRetryPolicyOnlyDisablesRepairRetry(t *testing.T) {
	tests := []struct {
		name      string
		payload   MallWeatherSchedulePayload
		wantRetry int
	}{
		{name: "repair", payload: MallWeatherSchedulePayload{TaskType: TypeMallWeatherRepair}, wantRetry: 0},
		{name: "fast", payload: MallWeatherSchedulePayload{TaskType: TypeMallWeatherFast, DetailProfile: "full"}, wantRetry: mallWeatherScheduleDefaultMaxRetry},
		{name: "full", payload: MallWeatherSchedulePayload{TaskType: TypeMallWeatherFull, DetailProfile: "economy"}, wantRetry: mallWeatherScheduleDefaultMaxRetry},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			maxRetry := mallWeatherScheduleMaxRetry(test.payload.TaskType)
			if maxRetry != test.wantRetry {
				t.Fatalf("max retry = %d, want %d", maxRetry, test.wantRetry)
			}
		})
	}
}

func TestDecodeMallWeatherSchedulePayloadRejectsUnknownFields(t *testing.T) {
	if _, err := DecodeMallWeatherSchedulePayload([]byte(`{"task_type":"mall:weather:fast","detail_profile":"full","secret":"x"}`)); err == nil {
		t.Fatal("DecodeMallWeatherSchedulePayload() accepted unknown field")
	}
}

func TestDecodeMallWeatherSchedulePayloadRoutesLegacyLifeIndexThroughFullWeather(t *testing.T) {
	decoded, err := DecodeMallWeatherSchedulePayload([]byte(
		`{"task_type":"mall:weather:life_index","detail_profile":"standard"}`,
	))
	if err != nil {
		t.Fatalf("DecodeMallWeatherSchedulePayload() error=%v", err)
	}
	if decoded.TaskType != TypeMallWeatherFull || decoded.DetailProfile != "standard" {
		t.Fatalf("decoded=%+v", decoded)
	}
}

func TestDecodeMallWeatherExportTaskPayloadIsStrict(t *testing.T) {
	payload, err := DecodeMallWeatherExportTaskPayload([]byte(`{"export_job_id":17}`))
	if err != nil || payload.ExportJobID != 17 {
		t.Fatalf("DecodeMallWeatherExportTaskPayload() payload=%+v error=%v", payload, err)
	}
	for _, invalid := range [][]byte{
		[]byte(`{"export_job_id":0}`),
		[]byte(`{"export_job_id":17,"secret":"x"}`),
		[]byte(`{"export_job_id":17}{}`),
	} {
		if _, err := DecodeMallWeatherExportTaskPayload(invalid); err == nil {
			t.Fatalf("DecodeMallWeatherExportTaskPayload(%s) accepted invalid payload", invalid)
		}
	}
}

func TestDecodeMallWeatherFeishuTaskPayloadIsStrict(t *testing.T) {
	payload, err := DecodeMallWeatherFeishuTaskPayload([]byte(`{"pipeline_run_id":17}`))
	if err != nil || payload.PipelineRunID != 17 {
		t.Fatalf("DecodeMallWeatherFeishuTaskPayload() payload=%+v error=%v", payload, err)
	}
	for _, invalid := range [][]byte{
		[]byte(`{"pipeline_run_id":0}`),
		[]byte(`{"pipeline_run_id":17,"spreadsheet_token":"secret"}`),
		[]byte(`{"pipeline_run_id":17}{}`),
	} {
		if _, err := DecodeMallWeatherFeishuTaskPayload(invalid); err == nil {
			t.Fatalf("DecodeMallWeatherFeishuTaskPayload(%s) accepted invalid payload", invalid)
		}
	}
}

func TestMallWeatherExportCleanupTaskPayloadIsStrictAndNonSensitive(t *testing.T) {
	task, err := NewMallWeatherExportCleanupTask()
	if err != nil {
		t.Fatalf("NewMallWeatherExportCleanupTask() error=%v", err)
	}
	if task.Type() != TypeMallWeatherExportCleanup || string(task.Payload()) != `{}` {
		t.Fatalf("cleanup task type=%q payload=%s", task.Type(), task.Payload())
	}
	for _, invalid := range [][]byte{
		nil,
		[]byte(`null`),
		[]byte(`{"secret":"x"}`),
		[]byte(`{}{}`),
	} {
		if err := DecodeMallWeatherExportCleanupTaskPayload(invalid); err == nil {
			t.Fatalf("DecodeMallWeatherExportCleanupTaskPayload(%s) accepted invalid payload", invalid)
		}
	}
}

func TestNewMallWeatherTaskRejectsInvalidPayload(t *testing.T) {
	tests := []struct {
		name     string
		taskType string
		payload  string
	}{
		{"unknown type", "mall:weather:unknown", `{"mall_id":1}`},
		{"missing id", TypeMallWeatherFast, `{"mall_id":0}`},
		{"missing window", TypeMallWeatherFast, `{"mall_id":1}`},
		{"mismatched window", TypeMallWeatherFast, `{"mall_id":1,"task_window":"full:1:2026072210"}`},
		{"mismatched mall", TypeMallWeatherFast, `{"mall_id":1,"task_window":"fast:2:202607221030"}`},
		{"empty window identity", TypeMallWeatherManual, `{"mall_id":1,"task_window":"manual:"}`},
		{"repair missing endpoint", TypeMallWeatherRepair, `{"mall_id":1,"task_window":"repair:42:1"}`},
		{"manual invalid endpoint", TypeMallWeatherManual, `{"mall_id":1,"task_window":"manual:abc","endpoint_kind":"v4_unknown"}`},
		{"fast mismatched endpoint", TypeMallWeatherFast, `{"mall_id":1,"task_window":"fast:1:202607221030","endpoint_kind":"v3_life_index"}`},
		{"unsafe window", TypeMallWeatherFast, `{"mall_id":1,"task_window":"fast:../secret"}`},
		{"credential field", TypeMallWeatherFast, `{"mall_id":1,"app_secret":"do-not-queue"}`},
		{"URL field", TypeMallWeatherFast, `{"mall_id":1,"provider_url":"https://example.invalid"}`},
		{"multiple values", TypeMallWeatherFast, `{"mall_id":1}{"mall_id":2}`},
		{"geocode missing version", TypeMallGeocode, `{"mall_id":1,"address_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`},
		{"geocode invalid hash", TypeMallGeocode, `{"mall_id":1,"mall_version":2,"address_hash":"not-a-sha256"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewMallWeatherTask(tt.taskType, []byte(tt.payload)); err == nil {
				t.Fatal("NewMallWeatherTask() error = nil")
			}
		})
	}
}

func TestDecodeMallWeatherTaskPayloadReturnsStableIdentity(t *testing.T) {
	want := weatherTaskPayloadForEndpoint("fast:11:202607221030", "v26_weather")
	data := []byte(`{"mall_id":11,"task_window":"fast:11:202607221030"}`)
	got, err := DecodeMallWeatherTaskPayload(TypeMallWeatherFast, data)
	if err != nil {
		t.Fatalf("DecodeMallWeatherTaskPayload() error = %v", err)
	}
	if got != want {
		t.Fatalf("DecodeMallWeatherTaskPayload() = %+v, want %+v", got, want)
	}
}

func weatherTaskPayload(taskWindow string) MallTaskPayload {
	return MallTaskPayload{MallID: 11, TaskWindow: taskWindow}
}

func weatherTaskPayloadForEndpoint(taskWindow, endpointKind string) MallTaskPayload {
	return MallTaskPayload{MallID: 11, TaskWindow: taskWindow, EndpointKind: endpointKind}
}

func TestMallWeatherTaskTypesReturnsCopy(t *testing.T) {
	types := MallWeatherTaskTypes()
	types[0] = "changed"
	if MallWeatherTaskTypes()[0] == "changed" {
		t.Fatal("MallWeatherTaskTypes returned shared storage")
	}
}

func TestMallWeatherFetchTaskTypesReturnsOnlyFetchHandlers(t *testing.T) {
	types := MallWeatherFetchTaskTypes()
	if len(types) != 5 {
		t.Fatalf("fetch task types=%v", types)
	}
	for _, taskType := range types {
		if !IsMallWeatherFetchTaskType(taskType) {
			t.Fatalf("fetch task type %q was not recognized", taskType)
		}
		if taskType == TypeMallGeocode || taskType == TypeMallWeatherExport || taskType == TypeMallWeatherFeishu {
			t.Fatalf("non-fetch task type %q was returned", taskType)
		}
	}
	if IsMallWeatherFetchTaskType(TypeMallGeocode) || IsMallWeatherFetchTaskType(TypeMallWeatherExport) {
		t.Fatal("non-fetch task type was recognized as a fetch task")
	}
	types[0] = "changed"
	if MallWeatherFetchTaskTypes()[0] == "changed" {
		t.Fatal("MallWeatherFetchTaskTypes returned shared storage")
	}
}

func TestDecodeMallGeocodeTaskPayloadReturnsIdentity(t *testing.T) {
	want := MallGeocodeTaskPayload{MallID: 11, MallVersion: 3, AddressHash: strings.Repeat("b", 64)}
	data := []byte(`{"mall_id":11,"mall_version":3,"address_hash":"` + want.AddressHash + `"}`)
	got, err := DecodeMallGeocodeTaskPayload(data)
	if err != nil {
		t.Fatalf("DecodeMallGeocodeTaskPayload() error = %v", err)
	}
	if got != want {
		t.Fatalf("DecodeMallGeocodeTaskPayload() = %+v, want %+v", got, want)
	}
}
