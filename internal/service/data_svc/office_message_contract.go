package data_svc

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/model"

	"github.com/godror/godror"
)

const (
	officeMessageMaxColumns    = 256
	officeMessageMaxParameters = 32
)

var (
	officeIdentifierPattern        = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_$#]{0,127}$`)
	officeParameterPattern         = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,63}$`)
	officeDecimalPattern           = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)$`)
	officeFileNameDateTokenPattern = regexp.MustCompile(`\{\{date(?::(yyyyMMdd|yyyy-MM-dd))?\}\}`)
	officeShanghaiLocation         = time.FixedZone("Asia/Shanghai", 8*60*60)
)

type OfficeColumnMapping struct {
	SourceColumn string  `json:"sourceColumn"`
	Header       string  `json:"header"`
	ValueType    string  `json:"valueType"`
	Order        int     `json:"order"`
	Width        float64 `json:"width"`
}

type OfficeQueryParameter struct {
	Code      string `json:"code"`
	Label     string `json:"label"`
	ValueType string `json:"valueType"`
	Format    string `json:"format,omitempty"`
	Required  bool   `json:"required"`
}

type officePushSnapshot struct {
	Target  officePushTargetSnapshot  `json:"target"`
	Message officePushMessageSnapshot `json:"message"`
}

type officePushTargetSnapshot struct {
	Channel                  string `json:"channel,omitempty"`
	BotAppID                 string `json:"botAppId,omitempty"`
	ReceiveIDType            string `json:"receiveIdType"`
	ReceiveID                string `json:"receiveId"`
	WebDAVURL                string `json:"webdavUrl,omitempty"`
	WebDAVUsername           string `json:"webdavUsername,omitempty"`
	WebDAVPath               string `json:"webdavPath,omitempty"`
	WebDAVPasswordCiphertext string `json:"webdavPasswordCiphertext,omitempty"`
	CredentialKeyVersion     string `json:"credentialKeyVersion,omitempty"`
}

type officePushMessageSnapshot struct {
	ID                  uint            `json:"id"`
	Name                string          `json:"name"`
	SourceType          string          `json:"sourceType"`
	Content             string          `json:"content"`
	ProcedureOwner      string          `json:"procedureOwner"`
	PackageName         string          `json:"packageName"`
	ProcedureName       string          `json:"procedureName"`
	ProcedureOverload   string          `json:"procedureOverload"`
	ResultTableOwner    string          `json:"resultTableOwner"`
	ResultTableName     string          `json:"resultTableName"`
	SelectSQL           string          `json:"selectSql"`
	FileNameTemplate    string          `json:"fileNameTemplate,omitempty"`
	ParameterSchemaJSON json.RawMessage `json:"parameters"`
	ColumnMappingJSON   json.RawMessage `json:"columnMapping"`
}

func newOfficePushSnapshot(target model.OfficePushTarget, message model.OfficeMessage) (model.JSONText, error) {
	snapshot := officePushSnapshot{
		Target: officePushTargetSnapshot{
			Channel: effectiveOfficePushChannel(target.Channel), BotAppID: target.BotAppID,
			ReceiveIDType: target.ReceiveIDType, ReceiveID: target.ReceiveID,
			WebDAVURL: target.WebDAVURL, WebDAVUsername: target.WebDAVUsername, WebDAVPath: target.WebDAVPath,
			WebDAVPasswordCiphertext: target.WebDAVPasswordCiphertext, CredentialKeyVersion: target.CredentialKeyVersion,
		},
		Message: officePushMessageSnapshot{
			ID: message.ID, Name: message.Name, SourceType: message.SourceType, Content: message.Content,
			ProcedureOwner: message.ProcedureOwner, PackageName: message.PackageName, ProcedureName: message.ProcedureName,
			ProcedureOverload: message.ProcedureOverload, ResultTableOwner: message.ResultTableOwner, ResultTableName: message.ResultTableName,
			SelectSQL: message.SelectSQL, FileNameTemplate: message.FileNameTemplate,
			ParameterSchemaJSON: json.RawMessage(message.ParameterSchemaJSON), ColumnMappingJSON: json.RawMessage(message.ColumnMappingJSON),
		},
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return "", fmt.Errorf("office message snapshot: encode: %w", err)
	}
	return model.JSONText(encoded), nil
}

func decodeOfficePushSnapshot(raw model.JSONText) (officePushSnapshot, error) {
	var snapshot officePushSnapshot
	if err := decodeOfficeJSON(raw, &snapshot); err != nil {
		return officePushSnapshot{}, fmt.Errorf("office message snapshot: decode: %w", err)
	}
	if snapshot.Message.ID == 0 || !validOfficePushTargetSnapshot(snapshot.Target) {
		return officePushSnapshot{}, fmt.Errorf("office message snapshot: invalid identity")
	}
	return snapshot, nil
}

func (snapshot officePushSnapshot) targetModel() model.OfficePushTarget {
	return model.OfficePushTarget{
		Channel: effectiveOfficePushChannel(snapshot.Target.Channel), BotAppID: snapshot.Target.BotAppID,
		ReceiveIDType: snapshot.Target.ReceiveIDType, ReceiveID: snapshot.Target.ReceiveID,
		WebDAVURL: snapshot.Target.WebDAVURL, WebDAVUsername: snapshot.Target.WebDAVUsername, WebDAVPath: snapshot.Target.WebDAVPath,
		WebDAVPasswordCiphertext: snapshot.Target.WebDAVPasswordCiphertext, CredentialKeyVersion: snapshot.Target.CredentialKeyVersion,
	}
}

func validOfficePushTargetSnapshot(target officePushTargetSnapshot) bool {
	switch effectiveOfficePushChannel(target.Channel) {
	case model.OfficePushChannelFeishu:
		return validOfficeReceiveIDType(target.ReceiveIDType) && strings.TrimSpace(target.ReceiveID) != ""
	case model.OfficePushChannelWebDAV:
		return validOfficeWebDAVTarget(model.OfficePushTarget{
			Channel: model.OfficePushChannelWebDAV, WebDAVURL: target.WebDAVURL, WebDAVUsername: target.WebDAVUsername,
			WebDAVPath: target.WebDAVPath, WebDAVPasswordCiphertext: target.WebDAVPasswordCiphertext,
			CredentialKeyVersion: target.CredentialKeyVersion,
		})
	default:
		return false
	}
}

func (snapshot officePushSnapshot) messageModel() model.OfficeMessage {
	message := snapshot.Message
	return model.OfficeMessage{
		BaseModel: model.BaseModel{ID: message.ID}, Name: message.Name, SourceType: message.SourceType, Content: message.Content,
		ProcedureOwner: message.ProcedureOwner, PackageName: message.PackageName, ProcedureName: message.ProcedureName,
		ProcedureOverload: message.ProcedureOverload, ResultTableOwner: message.ResultTableOwner, ResultTableName: message.ResultTableName,
		SelectSQL: message.SelectSQL, FileNameTemplate: message.FileNameTemplate,
		ParameterSchemaJSON: model.JSONText(message.ParameterSchemaJSON), ColumnMappingJSON: model.JSONText(message.ColumnMappingJSON),
	}
}

func normalizeOfficeWorkbookFileNameTemplate(value, messageName string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = officeWorkbookFileName(messageName)
	}
	if err := validateOfficeWorkbookFileNameTemplate(value); err != nil {
		return "", err
	}
	return value, nil
}

func validateOfficeWorkbookFileNameTemplate(value string) error {
	if value == "" || len(value) > 255 || strings.TrimSpace(value) != value || strings.ContainsAny(value, "/\\") ||
		strings.IndexFunc(value, unicode.IsControl) >= 0 || !strings.HasSuffix(strings.ToLower(value), ".xlsx") {
		return fmt.Errorf("office message file name: invalid template")
	}
	remaining := officeFileNameDateTokenPattern.ReplaceAllString(value, "")
	if strings.Contains(remaining, "{{") || strings.Contains(remaining, "}}") {
		return fmt.Errorf("office message file name: invalid template")
	}
	return nil
}

func renderOfficeWorkbookFileName(template, messageName string, now time.Time) (string, error) {
	template, err := normalizeOfficeWorkbookFileNameTemplate(template, messageName)
	if err != nil || now.IsZero() {
		return "", fmt.Errorf("office message file name: invalid render context")
	}
	rendered := officeFileNameDateTokenPattern.ReplaceAllStringFunc(template, func(token string) string {
		format := officeFileNameDateTokenPattern.FindStringSubmatch(token)[1]
		layout := "20060102"
		if format == "yyyy-MM-dd" {
			layout = "2006-01-02"
		}
		return now.In(officeShanghaiLocation).Format(layout)
	})
	if err := validateOfficeWorkbookFileNameTemplate(rendered); err != nil {
		return "", fmt.Errorf("office message file name: invalid rendered name")
	}
	return rendered, nil
}

func normalizeOfficeColumnMappings(raw model.JSONText, sourceType string) ([]OfficeColumnMapping, model.JSONText, error) {
	var mappings []OfficeColumnMapping
	if err := decodeOfficeJSON(raw, &mappings); err != nil {
		return nil, "", fmt.Errorf("office message columns: %w", err)
	}
	if len(mappings) == 0 || len(mappings) > officeMessageMaxColumns {
		return nil, "", fmt.Errorf("office message columns: column count is invalid")
	}
	seen := make(map[string]struct{}, len(mappings))
	for index := range mappings {
		mapping := &mappings[index]
		mapping.SourceColumn = strings.ToUpper(strings.TrimSpace(mapping.SourceColumn))
		mapping.Header = strings.TrimSpace(mapping.Header)
		mapping.ValueType = strings.ToLower(strings.TrimSpace(mapping.ValueType))
		if !validOfficeSourceColumn(sourceType, mapping.SourceColumn) || mapping.Header == "" || len(mapping.Header) > 128 ||
			!validOfficeValueType(mapping.ValueType) || mapping.Order < 0 || mapping.Width < 0 || mapping.Width > reportExcelMaximumWidth {
			return nil, "", fmt.Errorf("office message columns: invalid column mapping")
		}
		key := strings.ToUpper(mapping.SourceColumn)
		if _, exists := seen[key]; exists {
			return nil, "", fmt.Errorf("office message columns: duplicate source column")
		}
		seen[key] = struct{}{}
	}
	sort.SliceStable(mappings, func(left, right int) bool {
		if mappings[left].Order == mappings[right].Order {
			return mappings[left].SourceColumn < mappings[right].SourceColumn
		}
		return mappings[left].Order < mappings[right].Order
	})
	canonical, err := json.Marshal(mappings)
	if err != nil {
		return nil, "", fmt.Errorf("office message columns: encode mappings: %w", err)
	}
	return mappings, model.JSONText(canonical), nil
}

func validOfficeSourceColumn(sourceType, value string) bool {
	switch sourceType {
	case model.OfficeMessageSourceOracleProcedure:
		return officeIdentifierPattern.MatchString(value)
	case model.OfficeMessageSourceOracleQuery:
		return value != "" && len(value) <= 128 && !strings.ContainsFunc(value, unicode.IsControl)
	default:
		return false
	}
}

func normalizeOfficeQueryParameters(statement string, raw model.JSONText) ([]OfficeQueryParameter, model.JSONText, error) {
	analysis, valid := reportoracle.AnalyzeSelect(statement)
	if !valid {
		return nil, "", fmt.Errorf("office message query: only one SELECT statement is allowed")
	}
	if analysis.HasPositionalBind {
		return nil, "", fmt.Errorf("office message query parameters: positional binds are not allowed")
	}
	var parameters []OfficeQueryParameter
	if len(bytes.TrimSpace([]byte(raw))) == 0 {
		parameters = []OfficeQueryParameter{}
	} else if err := decodeOfficeJSON(raw, &parameters); err != nil {
		return nil, "", fmt.Errorf("office message query parameters: %w", err)
	}
	if len(parameters) > officeMessageMaxParameters {
		return nil, "", fmt.Errorf("office message query parameters: parameter count is excessive")
	}
	configured := make(map[string]struct{}, len(parameters))
	for index := range parameters {
		parameter := &parameters[index]
		parameter.Code = strings.ToLower(strings.TrimSpace(parameter.Code))
		parameter.Label = strings.TrimSpace(parameter.Label)
		parameter.ValueType = strings.ToLower(strings.TrimSpace(parameter.ValueType))
		parameter.Format = strings.TrimSpace(parameter.Format)
		if !officeParameterPattern.MatchString(parameter.Code) || parameter.Label == "" || len(parameter.Label) > 128 ||
			!validOfficeParameter(parameter.ValueType, parameter.Format) {
			return nil, "", fmt.Errorf("office message query parameters: invalid parameter")
		}
		if _, exists := configured[parameter.Code]; exists {
			return nil, "", fmt.Errorf("office message query parameters: duplicate parameter")
		}
		configured[parameter.Code] = struct{}{}
	}
	bound := analysis.NamedBinds
	if len(bound) != len(configured) {
		return nil, "", fmt.Errorf("office message query parameters: SELECT binds do not match configured parameters")
	}
	for code := range configured {
		if _, exists := bound[code]; !exists {
			return nil, "", fmt.Errorf("office message query parameters: SELECT binds do not match configured parameters")
		}
	}
	canonical, err := json.Marshal(parameters)
	if err != nil {
		return nil, "", fmt.Errorf("office message query parameters: encode schema: %w", err)
	}
	return parameters, model.JSONText(canonical), nil
}

func normalizeOfficeParameterValues(schema []OfficeQueryParameter, input map[string]string) (model.JSONText, []interface{}, error) {
	if input == nil {
		input = map[string]string{}
	}
	normalizedInput := make(map[string]string, len(input))
	for code, value := range input {
		normalizedCode := strings.ToLower(strings.TrimSpace(code))
		if _, exists := normalizedInput[normalizedCode]; exists {
			return "", nil, fmt.Errorf("office message query parameter %q is duplicated", code)
		}
		normalizedInput[normalizedCode] = value
	}
	known := make(map[string]struct{}, len(schema))
	stored := make(map[string]string, len(schema))
	arguments := make([]interface{}, 0, len(schema))
	for _, parameter := range schema {
		known[parameter.Code] = struct{}{}
		raw, exists := normalizedInput[parameter.Code]
		value := strings.TrimSpace(raw)
		if (!exists || value == "") && parameter.Required {
			return "", nil, fmt.Errorf("office message query parameter %q is required", parameter.Code)
		}
		var bound interface{}
		if exists && value != "" {
			var err error
			bound, value, err = officeParameterValue(parameter, value)
			if err != nil {
				return "", nil, err
			}
			stored[parameter.Code] = value
		}
		arguments = append(arguments, sql.Named(parameter.Code, bound))
	}
	for code := range normalizedInput {
		if _, exists := known[code]; !exists {
			return "", nil, fmt.Errorf("office message query parameter %q is not configured", code)
		}
	}
	encoded, err := json.Marshal(stored)
	if err != nil {
		return "", nil, fmt.Errorf("office message query parameters: encode values: %w", err)
	}
	return model.JSONText(encoded), arguments, nil
}

func officeParameterArguments(schema []OfficeQueryParameter, raw model.JSONText) ([]interface{}, error) {
	values := make(map[string]string)
	if err := decodeOfficeJSON(raw, &values); err != nil {
		return nil, fmt.Errorf("office message query parameters: decode values: %w", err)
	}
	_, arguments, err := normalizeOfficeParameterValues(schema, values)
	return arguments, err
}

func officeParameterValue(parameter OfficeQueryParameter, value string) (interface{}, string, error) {
	switch parameter.ValueType {
	case "string":
		if len(value) > 4000 {
			return nil, "", fmt.Errorf("office message query parameter %q is too long", parameter.Code)
		}
		return value, value, nil
	case "integer":
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return nil, "", fmt.Errorf("office message query parameter %q must be an integer", parameter.Code)
		}
		return parsed, strconv.FormatInt(parsed, 10), nil
	case "decimal":
		if !officeDecimalPattern.MatchString(value) {
			return nil, "", fmt.Errorf("office message query parameter %q must be a decimal", parameter.Code)
		}
		return godror.Number(value), value, nil
	case "date":
		layout, ok := officeDateLayout(parameter.Format)
		if !ok {
			return nil, "", fmt.Errorf("office message query parameter %q has an invalid date format", parameter.Code)
		}
		parsed, err := time.ParseInLocation(layout, value, time.Local)
		if err != nil {
			return nil, "", fmt.Errorf("office message query parameter %q must match %s", parameter.Code, parameter.Format)
		}
		normalized := parsed.Format(layout)
		return parsed, normalized, nil
	default:
		return nil, "", fmt.Errorf("office message query parameter %q has an unsupported type", parameter.Code)
	}
}

func officeDateLayout(format string) (string, bool) {
	switch format {
	case "yyyyMMdd":
		return "20060102", true
	case "yyyy-MM-dd":
		return "2006-01-02", true
	case "yyyy-MM-dd HH:mm:ss":
		return "2006-01-02 15:04:05", true
	default:
		return "", false
	}
}

func validOfficeParameter(valueType, format string) bool {
	switch valueType {
	case "string", "integer", "decimal":
		return format == ""
	case "date":
		_, ok := officeDateLayout(format)
		return ok
	default:
		return false
	}
}

func validOfficeValueType(value string) bool {
	switch value {
	case "string", "integer", "decimal", "date", "datetime", "boolean":
		return true
	default:
		return false
	}
}

func decodeOfficeJSON(raw model.JSONText, destination interface{}) error {
	decoder := json.NewDecoder(bytes.NewReader([]byte(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("JSON contains trailing data")
	}
	return nil
}
