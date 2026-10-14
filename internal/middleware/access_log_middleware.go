package middleware

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/spf13/cast"
	"go.uber.org/zap"

	"gin-biz-web-api/pkg/config"
	"gin-biz-web-api/pkg/helper/strx"
	"gin-biz-web-api/pkg/logger"
	"gin-biz-web-api/pkg/providerhttp"
)

const maxAccessLogResponseBodyBytes = 64 << 10

const maxAccessLogRequestBodyBytes = 64 << 10

const accessLogRedactedValue = "[REDACTED]"

// ConsoleAccessLog uses the same credential redaction as structured logs.
func ConsoleAccessLog() gin.HandlerFunc {
	return gin.LoggerWithFormatter(func(p gin.LogFormatterParams) string {
		_, uri, _ := sanitizedAccessLogURL(p.Request)
		return fmt.Sprintf("[GIN] %s | %3d | %13v | %15s | %-7s %s\n", p.TimeStamp.Format("2006/01/02 - 15:04:05"), p.StatusCode, p.Latency, p.ClientIP, p.Method, uri)
	})
}

type AccessLogWriter struct {
	gin.ResponseWriter
	body      *bytes.Buffer
	truncated bool
}

// Write records a bounded textual response preview and always streams the original bytes.
func (w *AccessLogWriter) Write(p []byte) (int, error) {
	w.capture(p)
	return w.ResponseWriter.Write(p)
}

func (w *AccessLogWriter) WriteString(value string) (int, error) {
	w.capture([]byte(value))
	return w.ResponseWriter.WriteString(value)
}

func (w *AccessLogWriter) capture(p []byte) {
	if w == nil || w.body == nil || len(p) == 0 ||
		!accessLogTextualContentType(w.Header().Get("Content-Type")) {
		return
	}
	remaining := maxAccessLogResponseBodyBytes - w.body.Len()
	if remaining <= 0 {
		w.truncated = true
		return
	}
	if len(p) > remaining {
		_, _ = w.body.Write(p[:remaining])
		w.truncated = true
		return
	}
	_, _ = w.body.Write(p)
}

func accessLogTextualContentType(value string) bool {
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(value))
	if err != nil {
		return false
	}
	mediaType = strings.ToLower(mediaType)
	return strings.HasPrefix(mediaType, "text/") ||
		mediaType == "application/json" ||
		strings.HasSuffix(mediaType, "+json") ||
		mediaType == "application/xml" ||
		strings.HasSuffix(mediaType, "+xml") ||
		mediaType == "application/x-www-form-urlencoded"
}

// AccessLog 记录请求日志
// 参考：gin.Logger()
func AccessLog() gin.HandlerFunc {
	return func(c *gin.Context) {

		// 如果是访问静态资源，那么不记录请求日志
		if strings.HasPrefix(c.Request.URL.Path, config.GetString("cfg.upload.static_fs_relative_path")) {
			c.Next()
			return
		}

		// 获取 response 内容
		responseBodyWriter := &AccessLogWriter{
			ResponseWriter: c.Writer,
			body:           bytes.NewBufferString(""),
		}
		c.Writer = responseBodyWriter

		// 获取请求数据
		requestBody := accessLogRequestBody(c.Request)

		// 设置开始时间
		start := time.Now()
		c.Next()
		// 程序执行花费时间
		cost := time.Since(start)

		// http 响应状态码
		responseStatus := responseBodyWriter.Status()

		requestURL, requestURI, requestQuery := sanitizedAccessLogURL(c.Request)

		// 开始记录日志
		logFields := []zap.Field{
			zap.String("request_method", c.Request.Method), // 当前请求的方法
			zap.String("request_url", requestURL),          // 完整的请求地址（host + path + query）eg：`0.0.0.0:3000/api/user?aa=11&bb=22`
			zap.String("request_path", c.Request.URL.Path), // 只有请求地址，不带参数 eg：`/api/user`
			zap.String("request_uri", requestURI),          // 带参数的地址 eg： `/api/user?aa=11&bb=22`
			zap.String("request_query", requestQuery),      // 只有参数 eg：`aa=11&bb=22`
			// zap.String("request_body", string(requestBody)),                   // 请求的内容
			zap.String("client_ip", c.ClientIP()), // 客户端的 ip 地址
			zap.String("remote_addr", c.Request.RemoteAddr),
			zap.String("user_agent", c.Request.UserAgent()),                 // 用户请求头
			zap.Any("headers", sanitizedAccessLogHeaders(c.Request.Header)), // 请求头
			zap.String("errors", sanitizedAccessLogPrivateErrors(c)),
			zap.Int("response_status", responseStatus), // 当前的响应结果状态码
			zap.Int("response_size", responseBodyWriter.Size()),
			zap.Bool("response_body_truncated", responseBodyWriter.truncated),
			zap.String("code_execute_time", strx.StrMicroseconds(cost)), // 程序执行时间
			// zap.String("response_body", responseBodyWriter.body.String()), // 当前的请求结果响应体
		}

		// 记录已经脱敏的结构化请求体；无法可靠解析的正文不写入日志。
		logRequestBody := sanitizedAccessLogRequestBody(c.ContentType(), requestBody, c.Request.PostForm)
		logFields = append(logFields, zap.String("request_body", logRequestBody))

		// 响应的内容同样按结构化字段脱敏，避免登录等接口把凭证写入访问日志。
		logResponseBody := sanitizedAccessLogBody(
			responseBodyWriter.Header().Get("Content-Type"),
			responseBodyWriter.body.Bytes(),
			nil,
		)
		logFields = append(logFields, zap.String("response_body", logResponseBody))

		// 记录访问日志
		logger.Info("HTTP Access Log [ "+cast.ToString(responseStatus)+" ]", logFields...)

	}
}

// accessLogRequestBody only captures a bounded preview and restores every
// consumed byte so downstream handlers receive the original request body.
func accessLogRequestBody(request *http.Request) []byte {
	if request == nil || request.Body == nil || request.ContentLength > maxAccessLogRequestBodyBytes {
		return nil
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, maxAccessLogRequestBodyBytes+1))
	if err != nil || len(body) > maxAccessLogRequestBodyBytes {
		request.Body = io.NopCloser(io.MultiReader(bytes.NewReader(body), request.Body))
		return nil
	}
	request.Body = io.NopCloser(bytes.NewReader(body))
	return body
}

func sanitizedAccessLogPrivateErrors(c *gin.Context) string {
	if c == nil {
		return ""
	}
	return providerhttp.RedactText(c.Errors.ByType(gin.ErrorTypePrivate).String())
}

func sanitizedAccessLogHeaders(headers http.Header) http.Header {
	sanitized := headers.Clone()
	for name := range sanitized {
		if sensitiveAccessLogKey(name) {
			sanitized[name] = []string{accessLogRedactedValue}
		}
	}
	return sanitized
}

func sanitizedAccessLogURL(request *http.Request) (string, string, string) {
	if request == nil || request.URL == nil {
		return "", "", ""
	}
	sanitizedURL := *request.URL
	values := sanitizedAccessLogValues(request.URL.Query())
	if strings.HasSuffix(request.URL.Path, "/feishu-mention-view/callback") {
		for _, key := range []string{"code", "state"} {
			if values.Has(key) {
				values.Set(key, accessLogRedactedValue)
			}
		}
	}
	sanitizedURL.RawQuery = values.Encode()
	return request.Host + sanitizedURL.String(), sanitizedURL.RequestURI(), sanitizedURL.RawQuery
}

func sanitizedAccessLogRequestBody(contentType string, body []byte, postForm url.Values) string {
	return sanitizedAccessLogBody(contentType, body, postForm)
}

func sanitizedAccessLogBody(contentType string, body []byte, form url.Values) string {
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(contentType))
	if err != nil && len(bytes.TrimSpace(body)) > 0 {
		return "[unparseable request body omitted]"
	}

	switch strings.ToLower(mediaType) {
	case "multipart/form-data":
		return sanitizedAccessLogValues(form).Encode()
	case "application/x-www-form-urlencoded":
		values, err := url.ParseQuery(string(body))
		if err != nil {
			return "[unparseable form body omitted]"
		}
		return sanitizedAccessLogValues(values).Encode()
	case "application/json":
		if len(bytes.TrimSpace(body)) == 0 {
			return ""
		}
		var value any
		if err := json.Unmarshal(body, &value); err != nil {
			return "[unparseable JSON body omitted]"
		}
		sanitized, err := json.Marshal(sanitizedAccessLogJSON(value))
		if err != nil {
			return "[unserializable JSON body omitted]"
		}
		return string(sanitized)
	default:
		if len(bytes.TrimSpace(body)) == 0 {
			return ""
		}
		return "[unstructured request body omitted]"
	}
}

func sanitizedAccessLogValues(values url.Values) url.Values {
	sanitized := make(url.Values, len(values))
	for name, items := range values {
		if sensitiveAccessLogKey(name) {
			sanitized[name] = []string{accessLogRedactedValue}
			continue
		}
		sanitized[name] = append([]string(nil), items...)
	}
	return sanitized
}

func sanitizedAccessLogJSON(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		for name, item := range typed {
			if sensitiveAccessLogKey(name) {
				typed[name] = accessLogRedactedValue
				continue
			}
			typed[name] = sanitizedAccessLogJSON(item)
		}
		return typed
	case []any:
		for index, item := range typed {
			typed[index] = sanitizedAccessLogJSON(item)
		}
		return typed
	default:
		return value
	}
}

func sensitiveAccessLogKey(name string) bool {
	normalized := strings.Map(func(character rune) rune {
		if character >= 'A' && character <= 'Z' {
			return character + ('a' - 'A')
		}
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' {
			return character
		}
		return -1
	}, name)
	if strings.Contains(normalized, "token") || strings.Contains(normalized, "password") ||
		strings.Contains(normalized, "passwd") || strings.Contains(normalized, "secret") ||
		strings.Contains(normalized, "signature") {
		return true
	}
	switch normalized {
	case "authorization", "proxyauthorization", "cookie", "setcookie", "jwtkey",
		"amapwebservicekey", "caiyunappkey", "aliyunossaccesskeyid", "ossaccesskeyid",
		"alibabacloudaccesskeyid":
		return true
	default:
		return false
	}
}
