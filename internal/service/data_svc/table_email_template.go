package data_svc

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"gin-biz-web-api/internal/reportoracle"
)

var (
	ErrTableEmailInvalid        = errors.New("提醒配置无效")
	ErrTableEmailConflict       = errors.New("配置已被修改，请刷新后重试")
	tableEmailFieldPattern      = regexp.MustCompile(`\{\{\s*([A-Za-z][A-Za-z0-9_$#]*)\s*\}\}`)
	tableEmailIdentifierPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_$#]{0,127}$`)
)

type TableEmailRuleInput struct {
	Name                string `json:"name"`
	DatasourceID        uint   `json:"datasourceId"`
	Owner               string `json:"owner"`
	TableName           string `json:"tableName"`
	CursorColumn        string `json:"cursorColumn"`
	RecipientsTemplate  string `json:"recipientsTemplate"`
	SubjectTemplate     string `json:"subjectTemplate"`
	BodyTemplate        string `json:"bodyTemplate"`
	Enabled             bool   `json:"enabled"`
	PollIntervalSeconds int    `json:"pollIntervalSeconds"`
	LockVersion         uint64 `json:"lockVersion"`
}

type TableEmailPreview struct {
	Recipients []string `json:"recipients"`
	Subject    string   `json:"subject"`
	Body       string   `json:"body"`
}

func tableEmailInvalid(message string) error {
	return fmt.Errorf("%w：%s", ErrTableEmailInvalid, message)
}

func tableEmailFields(template string) ([]string, error) {
	rest := tableEmailFieldPattern.ReplaceAllString(template, "")
	if strings.Contains(rest, "{{") || strings.Contains(rest, "}}") {
		return nil, tableEmailInvalid("字段引用请使用 {{字段名}}")
	}
	fields := make([]string, 0)
	for _, match := range tableEmailFieldPattern.FindAllStringSubmatch(template, -1) {
		fields = append(fields, strings.ToUpper(match[1]))
	}
	return fields, nil
}

func normalizeTableEmailRule(input TableEmailRuleInput, columns []OfficeResultColumn) (TableEmailRuleInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	ref, err := reportoracle.NormalizeResultTableRef(reportoracle.ResultTableRef{Owner: input.Owner, Name: input.TableName})
	if err != nil {
		return input, tableEmailInvalid("请选择 Oracle 表")
	}
	input.Owner, input.TableName = ref.Owner, ref.Name
	input.CursorColumn = strings.ToUpper(strings.TrimSpace(input.CursorColumn))
	input.RecipientsTemplate = strings.TrimSpace(input.RecipientsTemplate)
	input.SubjectTemplate = strings.TrimSpace(input.SubjectTemplate)
	if input.Name == "" || len([]rune(input.Name)) > 100 {
		return input, tableEmailInvalid("名称需为 1–100 个字符")
	}
	if input.PollIntervalSeconds < 60 || input.PollIntervalSeconds > 86400 {
		return input, tableEmailInvalid("检测间隔需为 60–86400 秒")
	}
	if input.RecipientsTemplate == "" || len(input.RecipientsTemplate) > 2000 || input.SubjectTemplate == "" || len(input.SubjectTemplate) > 1000 || strings.TrimSpace(input.BodyTemplate) == "" || len(input.BodyTemplate) > 64000 {
		return input, tableEmailInvalid("收件人、标题或正文为空或过长")
	}
	known := make(map[string]OfficeResultColumn, len(columns))
	for _, column := range columns {
		known[strings.ToUpper(column.Name)] = column
	}
	cursor, ok := known[input.CursorColumn]
	if !ok || cursor.Nullable || cursor.DataType != "NUMBER" {
		return input, tableEmailInvalid("增量字段必须是非空、唯一且递增的整数 NUMBER 列")
	}
	sample := make(map[string]any, len(columns))
	for name := range known {
		sample[name] = "sample@example.com"
	}
	for _, template := range []string{input.RecipientsTemplate, input.SubjectTemplate, input.BodyTemplate} {
		fields, err := tableEmailFields(template)
		if err != nil {
			return input, err
		}
		for _, field := range fields {
			if _, ok := known[field]; !ok {
				return input, tableEmailInvalid("字段不存在：" + field)
			}
		}
	}
	if _, err := renderTableEmail(input, sample); err != nil {
		return input, err
	}
	return input, nil
}

func renderTableEmail(input TableEmailRuleInput, row map[string]any) (TableEmailPreview, error) {
	result := TableEmailPreview{Recipients: []string{}}
	values := make(map[string]any, len(row))
	for name, value := range row {
		values[strings.ToUpper(name)] = value
	}
	render := func(template string) (string, error) {
		if _, err := tableEmailFields(template); err != nil {
			return "", err
		}
		var renderErr error
		rendered := tableEmailFieldPattern.ReplaceAllStringFunc(template, func(token string) string {
			name := strings.ToUpper(tableEmailFieldPattern.FindStringSubmatch(token)[1])
			value, ok := values[name]
			if !ok {
				renderErr = tableEmailInvalid("触发行缺少字段：" + name)
				return ""
			}
			switch value := value.(type) {
			case nil:
				return ""
			case []byte:
				return string(value)
			case time.Time:
				return value.Format("2006-01-02 15:04:05")
			case map[string]any, []any:
				data, err := json.Marshal(value)
				if err != nil {
					renderErr = tableEmailInvalid("字段值无法转换：" + name)
				}
				return string(data)
			default:
				return fmt.Sprint(value)
			}
		})
		return rendered, renderErr
	}
	// Split the configured template before interpolation: a field must resolve to
	// one mailbox, and cannot inject additional recipients or mail headers.
	if strings.ContainsAny(input.RecipientsTemplate, "\r\n") {
		return result, tableEmailInvalid("收件人不能包含换行")
	}
	parts := strings.FieldsFunc(input.RecipientsTemplate, func(r rune) bool { return r == ',' || r == ';' || r == '，' || r == '；' })
	if len(parts) == 0 || len(parts) > 20 {
		return result, tableEmailInvalid("请指定 1–20 个收件邮箱")
	}
	seen := map[string]bool{}
	for _, part := range parts {
		address, err := render(strings.TrimSpace(part))
		if err != nil {
			return result, err
		}
		address = strings.TrimSpace(address)
		parsed, parseErr := mail.ParseAddress(address)
		if parseErr != nil || strings.ContainsAny(address, "\r\n,;") || parsed.Address != address || !strings.Contains(address, "@") || len(address) > 254 {
			return result, tableEmailInvalid("收件邮箱无效，请检查固定地址或触发行的邮箱字段")
		}
		if !seen[strings.ToLower(address)] {
			result.Recipients = append(result.Recipients, address)
			seen[strings.ToLower(address)] = true
		}
	}
	var err error
	result.Subject, err = render(input.SubjectTemplate)
	if err != nil {
		return result, err
	}
	if strings.TrimSpace(result.Subject) == "" || strings.ContainsAny(result.Subject, "\r\n") || len(result.Subject) > 2000 {
		return result, tableEmailInvalid("邮件标题为空、过长或包含换行")
	}
	result.Body, err = render(input.BodyTemplate)
	if err != nil {
		return result, err
	}
	if strings.TrimSpace(result.Body) == "" || len(result.Body) > 1024*1024 {
		return result, tableEmailInvalid("邮件正文为空或过长")
	}
	return result, nil
}

func tableEmailHTML(body string) string {
	return strings.ReplaceAll(html.EscapeString(strings.ReplaceAll(body, "\r\n", "\n")), "\n", "<br>\n")
}
