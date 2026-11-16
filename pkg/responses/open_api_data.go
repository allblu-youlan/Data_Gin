package responses

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"
)

// OpenAPIDateTimeLayout is the canonical date-time format exposed by open APIs.
const OpenAPIDateTimeLayout = "2006-01-02 15:04:05"

type openAPIData struct {
	value       interface{}
	formatDates bool
}

// ForOpenAPI normalizes empty values and public date-time fields without
// changing the source data or omitting fields from the JSON response.
func ForOpenAPI(value interface{}) json.Marshaler {
	return openAPIData{value: value, formatDates: true}
}

func (data openAPIData) MarshalJSON() ([]byte, error) {
	raw, err := json.Marshal(data.value)
	if err != nil {
		return nil, fmt.Errorf("open api response: marshal data: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value interface{}
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("open api response: decode data: %w", err)
	}
	value = fillOpenAPIEmptyValues(value, reflect.ValueOf(data.value))
	if data.formatDates {
		value = formatOpenAPIDateTimes(value)
	}
	return json.Marshal(value)
}

// Use the original Go types to distinguish a missing string from a missing
// array or object. The decoded JSON preserves custom marshaling and omitempty.
func fillOpenAPIEmptyValues(value interface{}, source reflect.Value) interface{} {
	if value == nil {
		return openAPIEmptyValue(source)
	}
	source = openAPIIndirectValue(source)
	switch typed := value.(type) {
	case map[string]interface{}:
		fields := openAPIFields(source)
		for key, item := range typed {
			field := fields[key]
			if source.IsValid() && source.Kind() == reflect.Map && source.Type().Key().Kind() == reflect.String {
				field = source.MapIndex(reflect.ValueOf(key).Convert(source.Type().Key()))
			}
			typed[key] = fillOpenAPIEmptyValues(item, field)
		}
	case []interface{}:
		for index, item := range typed {
			var element reflect.Value
			if source.IsValid() && (source.Kind() == reflect.Slice || source.Kind() == reflect.Array) && index < source.Len() {
				element = source.Index(index)
			}
			typed[index] = fillOpenAPIEmptyValues(item, element)
		}
	}
	return value
}

func openAPIIndirectValue(value reflect.Value) reflect.Value {
	for value.IsValid() && (value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface) {
		if value.IsNil() {
			return reflect.Value{}
		}
		value = value.Elem()
	}
	return value
}

func openAPIEmptyValue(source reflect.Value) interface{} {
	if !source.IsValid() {
		return map[string]interface{}{}
	}
	if source.Kind() == reflect.Interface {
		if source.IsNil() {
			return map[string]interface{}{}
		}
		return openAPIEmptyValue(source.Elem())
	}
	typ := source.Type()
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ == reflect.TypeOf(time.Time{}) {
		return ""
	}
	if typ == reflect.TypeOf(json.RawMessage{}) {
		return map[string]interface{}{}
	}
	switch typ.Kind() {
	case reflect.String:
		return ""
	case reflect.Bool:
		return false
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return 0
	case reflect.Slice, reflect.Array:
		// encoding/json exposes byte slices as base64 strings.
		if typ.Kind() == reflect.Slice && typ.Elem().Kind() == reflect.Uint8 {
			return ""
		}
		return []interface{}{}
	default:
		return map[string]interface{}{}
	}
}

func openAPIFields(source reflect.Value) map[string]reflect.Value {
	fields := map[string]reflect.Value{}
	if !source.IsValid() || source.Kind() != reflect.Struct {
		return fields
	}
	typ := source.Type()
	for index := 0; index < typ.NumField(); index++ {
		field := typ.Field(index)
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "-" || (field.PkgPath != "" && !field.Anonymous) {
			continue
		}
		value := source.Field(index)
		if field.Anonymous && name == "" {
			embedded := openAPIIndirectValue(value)
			if embedded.IsValid() && embedded.Kind() == reflect.Struct {
				for key, item := range openAPIFields(embedded) {
					if _, exists := fields[key]; !exists {
						fields[key] = item
					}
				}
				continue
			}
		}
		if name == "" {
			name = field.Name
		}
		fields[name] = value
	}
	return fields
}

func formatOpenAPIDateTimes(value interface{}) interface{} {
	return formatOpenAPIField("", value)
}

func formatOpenAPIField(field string, value interface{}) interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		for key, item := range typed {
			typed[key] = formatOpenAPIField(key, item)
		}
		return typed
	case []interface{}:
		for index := range typed {
			typed[index] = formatOpenAPIField(field, typed[index])
		}
		return typed
	case string:
		if openAPIDateFields[field] {
			parsed, err := time.Parse(time.DateOnly, typed)
			if err == nil {
				return parsed.Format(OpenAPIDateTimeLayout)
			}
		}
		if openAPIDateTimeFields[field] {
			parsed, err := time.Parse(time.RFC3339Nano, typed)
			if err == nil {
				return parsed.Format(OpenAPIDateTimeLayout)
			}
		}
	}
	return value
}

var openAPIDateFields = map[string]bool{
	"forecastDateLocal": true,
}

var openAPIDateTimeFields = map[string]bool{
	"snapshotAtUtc":           true,
	"snapshotAtLocal":         true,
	"providerServerTimeUtc":   true,
	"providerServerTimeLocal": true,
	"forecastMinuteUtc":       true,
	"forecastMinuteLocal":     true,
	"forecastTimeUtc":         true,
	"forecastTimeLocal":       true,
	"issuedAtUtc":             true,
	"issuedAtLocal":           true,
	"fetchedAtUtc":            true,
	"fetchedAtLocal":          true,
	"publishedAtUtc":          true,
	"publishedAtLocal":        true,
	"firstSeenAtUtc":          true,
	"firstSeenAtLocal":        true,
	"lastSeenAtUtc":           true,
	"lastSeenAtLocal":         true,
	"endedAtUtc":              true,
	"endedAtLocal":            true,
	"observedStartUtc":        true,
	"observedStartLocal":      true,
	"observedEndUtc":          true,
	"observedEndLocal":        true,
}
