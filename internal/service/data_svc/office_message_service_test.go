package data_svc

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"gin-biz-web-api/job"
	"gin-biz-web-api/model"
)

func TestNormalizeOfficeMessageInputQuery(t *testing.T) {
	enabled := true
	message, err := normalizeOfficeMessageInput(OfficeMessageInput{
		Name: "销售日报", SourceType: model.OfficeMessageSourceOracleQuery,
		FileNameTemplate: "销售日报_{{date:yyyyMMdd}}.xlsx",
		SelectSQL:        "SELECT ORDER_NO, AMOUNT FROM SALES WHERE BILL_DATE = :bill_date",
		Parameters:       []OfficeQueryParameter{{Code: "bill_date", Label: "业务日期", ValueType: "date", Format: "yyyyMMdd", Required: true}},
		ColumnMapping: []OfficeColumnMapping{
			{SourceColumn: "ORDER_NO", Header: "单号", ValueType: "string", Order: 1, Width: 20},
			{SourceColumn: "AMOUNT", Header: "金额", ValueType: "decimal", Order: 2, Width: 16},
		}, Enabled: &enabled,
	})
	if err != nil {
		t.Fatalf("normalizeOfficeMessageInput() error = %v", err)
	}
	if message.SourceType != model.OfficeMessageSourceOracleQuery || message.SelectSQL == "" || string(message.ParameterSchemaJSON) == "[]" ||
		message.FileNameTemplate != "销售日报_{{date:yyyyMMdd}}.xlsx" {
		t.Fatalf("message = %#v", message)
	}
}

func TestNormalizeOfficeMessageInputQueryAcceptsExpressionColumnLabel(t *testing.T) {
	message, err := normalizeOfficeMessageInput(OfficeMessageInput{
		Name:       "线下店销售数据",
		SourceType: model.OfficeMessageSourceOracleQuery,
		SelectSQL:  "SELECT SUM(A.PAYAMOUNT) FROM YL_DBS.BJ_REPORT_RETAIL_DAY_SF A",
		ColumnMapping: []OfficeColumnMapping{
			{SourceColumn: "SUM(A.PAYAMOUNT)", Header: "销售额", ValueType: "decimal", Order: 0, Width: 18},
		},
	})
	if err != nil {
		t.Fatalf("normalizeOfficeMessageInput() error = %v", err)
	}
	if !strings.Contains(string(message.ColumnMappingJSON), `"sourceColumn":"SUM(A.PAYAMOUNT)"`) {
		t.Fatalf("column mapping = %s", message.ColumnMappingJSON)
	}
}

func TestNormalizeOfficeMessageInputProcedureRejectsExpressionColumnLabel(t *testing.T) {
	_, err := normalizeOfficeMessageInput(OfficeMessageInput{
		Name:             "销售日报",
		SourceType:       model.OfficeMessageSourceOracleProcedure,
		ProcedureOwner:   "REPORT",
		ProcedureName:    "BUILD_DAILY",
		ResultTableOwner: "REPORT",
		ResultTableName:  "DAILY_RESULT",
		ColumnMapping: []OfficeColumnMapping{
			{SourceColumn: "SUM(A.PAYAMOUNT)", Header: "销售额", ValueType: "decimal", Order: 0, Width: 18},
		},
	})
	if !errors.Is(err, ErrOfficeMessageInvalid) {
		t.Fatalf("normalizeOfficeMessageInput() error = %v", err)
	}
}

func TestNormalizeOfficeMessageInputEditedClearsOracleContract(t *testing.T) {
	message, err := normalizeOfficeMessageInput(OfficeMessageInput{Name: "通知", SourceType: model.OfficeMessageSourceEdited, Content: "系统维护完成"})
	if err != nil {
		t.Fatalf("normalizeOfficeMessageInput() error = %v", err)
	}
	if message.Content != "系统维护完成" || message.SelectSQL != "" || string(message.ColumnMappingJSON) != "[]" {
		t.Fatalf("message = %#v", message)
	}
}

func TestNewOfficePushOutboxContainsOnlyRunID(t *testing.T) {
	runUUID := "9ac63f51-1e15-40b0-ae0a-2b1c29b9de35"
	outbox, err := newOfficePushOutbox(17, runUUID, time.Now().UTC())
	if err != nil {
		t.Fatalf("newOfficePushOutbox() error = %v", err)
	}
	if outbox.TaskType != job.TypeOfficePush || outbox.QueueName != job.OfficePushQueueName || strings.Contains(string(outbox.PayloadJSON), "secret") {
		t.Fatalf("outbox = %#v", outbox)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(outbox.PayloadJSON), &payload); err != nil || len(payload) != 1 || payload["run_id"] != float64(17) {
		t.Fatalf("payload = %#v, %v", payload, err)
	}
}

func TestOfficePushSnapshotFreezesReceiverAndMessage(t *testing.T) {
	target := model.OfficePushTarget{BotAppID: "cli_original", ReceiveIDType: "chat_id", ReceiveID: "oc_original"}
	message := model.OfficeMessage{
		BaseModel: model.BaseModel{ID: 7}, Name: "销售日报", SourceType: model.OfficeMessageSourceOracleQuery,
		SelectSQL:           "SELECT ORDER_NO FROM SALES WHERE BILL_DATE = :bill_date",
		FileNameTemplate:    "销售日报_{{date:yyyyMMdd}}.xlsx",
		ParameterSchemaJSON: model.JSONText(`[{"code":"bill_date","label":"业务日期","valueType":"date","format":"yyyyMMdd","required":true}]`),
		ColumnMappingJSON:   model.JSONText(`[{"sourceColumn":"ORDER_NO","header":"单号","valueType":"string","order":0,"width":18}]`),
	}
	raw, err := newOfficePushSnapshot(target, message)
	if err != nil {
		t.Fatalf("newOfficePushSnapshot() error = %v", err)
	}
	target.BotAppID, target.ReceiveID, message.SelectSQL, message.FileNameTemplate = "cli_changed", "oc_changed", "SELECT 1 FROM DUAL", "changed.xlsx"
	snapshot, err := decodeOfficePushSnapshot(raw)
	if err != nil {
		t.Fatalf("decodeOfficePushSnapshot() error = %v", err)
	}
	if snapshot.targetModel().BotAppID != "cli_original" || snapshot.targetModel().ReceiveID != "oc_original" ||
		snapshot.messageModel().SelectSQL == message.SelectSQL || snapshot.messageModel().FileNameTemplate == message.FileNameTemplate {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestNormalizeOfficePushTargetInputSupportsWebDAV(t *testing.T) {
	target, err := normalizeOfficePushTargetInput(OfficePushTargetInput{
		Name: "坚果云销售日报", MessageID: 7, Channel: "webdav", WebDAVURL: "https://dav.jianguoyun.com/dav/",
		WebDAVUsername: " account@example.com ", WebDAVPath: "/固定报表/销售/",
	})
	if err != nil {
		t.Fatalf("normalizeOfficePushTargetInput() error = %v", err)
	}
	if target.Channel != model.OfficePushChannelWebDAV || target.WebDAVURL != "https://dav.jianguoyun.com/dav" ||
		target.WebDAVUsername != "account@example.com" || target.WebDAVPath != "/固定报表/销售" || target.BotAppID != "" || target.ReceiveID != "" {
		t.Fatalf("target = %#v", target)
	}
}

func TestNormalizeOfficePushTargetInputDefaultsLegacyRequestToFeishu(t *testing.T) {
	target, err := normalizeOfficePushTargetInput(OfficePushTargetInput{
		Name: "运营群", MessageID: 7, ReceiveIDType: "chat_id", ReceiveID: "oc_legacy",
	})
	if err != nil {
		t.Fatalf("normalizeOfficePushTargetInput() error = %v", err)
	}
	if target.Channel != model.OfficePushChannelFeishu || target.ReceiveID != "oc_legacy" {
		t.Fatalf("target = %#v", target)
	}
}

func TestNormalizeOfficePushTargetInputRejectsUnsafeWebDAVDestination(t *testing.T) {
	tests := []struct {
		name string
		url  string
		path string
	}{
		{name: "plain HTTP", url: "http://dav.jianguoyun.com/dav", path: "/reports"},
		{name: "parent path", url: "https://dav.jianguoyun.com/dav", path: "/reports/../private"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := normalizeOfficePushTargetInput(OfficePushTargetInput{
				Name: "坚果云", MessageID: 7, Channel: model.OfficePushChannelWebDAV,
				WebDAVURL: test.url, WebDAVUsername: "account@example.com", WebDAVPath: test.path,
			})
			if !errors.Is(err, ErrOfficeMessageInvalid) {
				t.Fatalf("normalizeOfficePushTargetInput() error = %v", err)
			}
		})
	}
}

func TestOfficeWebDAVCredentialIsEncryptedAndPreservedOnBlankUpdate(t *testing.T) {
	cipher := &fakeOfficePushCredentialCipher{}
	service := &OfficeMessageService{credential: cipher}
	target := model.OfficePushTarget{Channel: model.OfficePushChannelWebDAV}
	input := OfficePushTargetInput{WebDAVPassword: "app-password"}
	if err := service.prepareOfficePushTargetCredential(&target, input, nil); err != nil {
		t.Fatalf("prepareOfficePushTargetCredential(create) error = %v", err)
	}
	if cipher.encrypted != "app-password" || cipher.purpose != officeWebDAVCredentialPurpose || target.WebDAVPasswordCiphertext != "ciphertext" || target.CredentialKeyVersion != "key-v1" {
		t.Fatalf("credential result = cipher %#v, target %#v", cipher, target)
	}
	existing := target
	preserved := model.OfficePushTarget{Channel: model.OfficePushChannelWebDAV}
	if err := service.prepareOfficePushTargetCredential(&preserved, OfficePushTargetInput{}, &existing); err != nil {
		t.Fatalf("prepareOfficePushTargetCredential(update) error = %v", err)
	}
	if preserved.WebDAVPasswordCiphertext != "ciphertext" || preserved.CredentialKeyVersion != "key-v1" {
		t.Fatalf("preserved target = %#v", preserved)
	}
}

func TestOfficeWebDAVTargetRequiresExcelMessage(t *testing.T) {
	target := model.OfficePushTarget{
		Channel: model.OfficePushChannelWebDAV, WebDAVURL: "https://dav.jianguoyun.com/dav", WebDAVUsername: "account@example.com",
		WebDAVPath: "/reports", WebDAVPasswordCiphertext: "ciphertext", CredentialKeyVersion: "key-v1",
	}
	if err := validateOfficePushTargetMessage(target, model.OfficeMessage{SourceType: model.OfficeMessageSourceEdited}); !errors.Is(err, ErrOfficeMessageInvalid) {
		t.Fatalf("validateOfficePushTargetMessage(edited) error = %v", err)
	}
	if err := validateOfficePushTargetMessage(target, model.OfficeMessage{SourceType: model.OfficeMessageSourceOracleQuery}); err != nil {
		t.Fatalf("validateOfficePushTargetMessage(query) error = %v", err)
	}
}

func TestOfficeWebDAVTargetJSONRedactsCredential(t *testing.T) {
	target := model.OfficePushTarget{
		Channel: model.OfficePushChannelWebDAV, WebDAVURL: "https://dav.jianguoyun.com/dav", WebDAVUsername: "account@example.com",
		WebDAVPath: "/reports", WebDAVPasswordCiphertext: "ciphertext-secret", CredentialKeyVersion: "key-v1",
	}
	sanitizeOfficePushTarget(&target)
	payload, err := json.Marshal(target)
	if err != nil {
		t.Fatalf("marshal target: %v", err)
	}
	if strings.Contains(string(payload), "ciphertext-secret") || strings.Contains(string(payload), "key-v1") || !strings.Contains(string(payload), `"hasWebdavPassword":true`) {
		t.Fatalf("target JSON = %s", payload)
	}
}

func TestOfficeWebDAVSnapshotContainsCiphertextWithoutPlaintext(t *testing.T) {
	target := model.OfficePushTarget{
		Channel: model.OfficePushChannelWebDAV, WebDAVURL: "https://dav.jianguoyun.com/dav", WebDAVUsername: "account@example.com",
		WebDAVPath: "/reports", WebDAVPasswordCiphertext: "encrypted-value", CredentialKeyVersion: "key-v1",
	}
	message := model.OfficeMessage{
		BaseModel: model.BaseModel{ID: 7}, SourceType: model.OfficeMessageSourceOracleQuery,
		ParameterSchemaJSON: model.JSONText("[]"), ColumnMappingJSON: model.JSONText("[]"),
	}
	raw, err := newOfficePushSnapshot(target, message)
	if err != nil {
		t.Fatalf("newOfficePushSnapshot() error = %v", err)
	}
	if strings.Contains(string(raw), "app-password") || !strings.Contains(string(raw), "encrypted-value") {
		t.Fatalf("snapshot = %s", raw)
	}
	snapshot, err := decodeOfficePushSnapshot(raw)
	if err != nil || snapshot.targetModel().Channel != model.OfficePushChannelWebDAV {
		t.Fatalf("decodeOfficePushSnapshot() = %#v, %v", snapshot, err)
	}
}

type fakeOfficePushCredentialCipher struct {
	purpose   string
	encrypted string
	err       error
}

func (cipher *fakeOfficePushCredentialCipher) EncryptScoped(purpose, plaintext string) (string, string, error) {
	cipher.purpose, cipher.encrypted = purpose, plaintext
	return "key-v1", "ciphertext", cipher.err
}

func (*fakeOfficePushCredentialCipher) DecryptScoped(purpose, version, ciphertext string) (string, error) {
	if purpose != officeWebDAVCredentialPurpose || version == "" || ciphertext == "" {
		return "", fmt.Errorf("invalid scoped credential")
	}
	return "app-password", nil
}

func TestOfficeMessageServiceListsOnlyConfiguredFeishuBot(t *testing.T) {
	tests := []struct {
		name       string
		config     officeFeishuBotConfig
		wantLength int
	}{
		{name: "configured", config: officeFeishuBotConfig{appID: "cli_office", configured: true}, wantLength: 1},
		{name: "missing secret", config: officeFeishuBotConfig{appID: "cli_office"}, wantLength: 0},
		{name: "missing app id", config: officeFeishuBotConfig{configured: true}, wantLength: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &OfficeMessageService{feishuBot: test.config}
			items := service.ListFeishuBots(t.Context())
			if len(items) != test.wantLength {
				t.Fatalf("ListFeishuBots() length = %d, want %d", len(items), test.wantLength)
			}
			if len(items) == 1 && (items[0].ID != "cli_office" || items[0].Source != officeFeishuBotSourceEnvironment) {
				t.Fatalf("ListFeishuBots() = %#v", items)
			}
		})
	}
}

func TestOfficeFeishuBotOptionJSONContainsOnlyPublicMetadata(t *testing.T) {
	payload, err := json.Marshal(OfficeFeishuBotOption{ID: "cli_office", Name: "办公消息机器人", Source: officeFeishuBotSourceEnvironment})
	if err != nil {
		t.Fatalf("marshal OfficeFeishuBotOption: %v", err)
	}
	if got, want := string(payload), `{"id":"cli_office","name":"办公消息机器人","source":"ENVIRONMENT"}`; got != want {
		t.Fatalf("OfficeFeishuBotOption JSON = %s, want %s", got, want)
	}
}

func TestResolveOfficeFeishuBotDefaultsAndRejectsUnknownBot(t *testing.T) {
	service := &OfficeMessageService{feishuBot: officeFeishuBotConfig{appID: "cli_office", configured: true}}
	for _, requested := range []string{"", "cli_office"} {
		if got, err := service.resolveOfficeFeishuBot(requested); err != nil || got != "cli_office" {
			t.Fatalf("resolveOfficeFeishuBot(%q) = %q, %v", requested, got, err)
		}
	}
	if _, err := service.resolveOfficeFeishuBot("cli_other"); !errors.Is(err, ErrOfficeMessageInvalid) {
		t.Fatalf("resolveOfficeFeishuBot() error = %v", err)
	}
}

func TestValidateOfficeRunReplayRequiresSameCanonicalParameters(t *testing.T) {
	message := model.OfficeMessage{
		BaseModel: model.BaseModel{ID: 7}, Name: "销售日报", SourceType: model.OfficeMessageSourceOracleQuery,
		SelectSQL:           "SELECT ORDER_NO FROM SALES WHERE BILL_DATE = :bill_date",
		ParameterSchemaJSON: model.JSONText(`[{"code":"bill_date","label":"业务日期","valueType":"date","format":"yyyyMMdd","required":true}]`),
		ColumnMappingJSON:   model.JSONText(`[{"sourceColumn":"ORDER_NO","header":"单号","valueType":"string","order":0,"width":18}]`),
	}
	snapshot, err := newOfficePushSnapshot(model.OfficePushTarget{ReceiveIDType: "chat_id", ReceiveID: "oc_original"}, message)
	if err != nil {
		t.Fatalf("newOfficePushSnapshot() error = %v", err)
	}
	existing := model.OfficePushRun{TargetID: 3, RequestedBy: 5, SnapshotJSON: snapshot, ParametersJSON: model.JSONText(`{ "bill_date" : "20260901" }`)}
	if err := validateOfficeRunReplay(existing, 5, 3, map[string]string{"BILL_DATE": "20260901"}); err != nil {
		t.Fatalf("validateOfficeRunReplay() same request error = %v", err)
	}
	if err := validateOfficeRunReplay(existing, 5, 3, map[string]string{"bill_date": "20260902"}); !errors.Is(err, ErrOfficeMessageConflict) {
		t.Fatalf("validateOfficeRunReplay() mismatched parameters error = %v", err)
	}
}

func TestCanonicalOfficeUUIDNormalizesAcceptedForms(t *testing.T) {
	canonical := "9ac63f51-1e15-40b0-ae0a-2b1c29b9de35"
	got, err := canonicalOfficeUUID("urn:uuid:" + canonical)
	if err != nil || got != canonical {
		t.Fatalf("canonicalOfficeUUID() = %q, %v", got, err)
	}
}
