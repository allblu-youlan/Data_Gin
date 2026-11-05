package msg

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"gin-biz-web-api/pkg/logger"
	"gin-biz-web-api/pkg/providerhttp"

	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/locales/en"
	"github.com/go-playground/locales/zh"
	ut "github.com/go-playground/universal-translator"
	"github.com/go-playground/validator/v10"
	en_translation "github.com/go-playground/validator/v10/translations/en"
	zh_translation "github.com/go-playground/validator/v10/translations/zh"
	"go.uber.org/zap"
)

type Response struct {
	Code int             `json:"code"`
	Msg  any             `json:"msg"`
	Data *map[string]any `json:"data"`
}

type ErrResponseST struct {
	Code int             `json:"code"`
	Msg  any             `json:"msg"`
	Data *map[string]any `json:"data"`
	Err  any             `json:"err"`
}

var trans ut.Translator

func initTranslator(language string) error {
	//转换成go-playground的validator
	validate, ok := binding.Validator.Engine().(*validator.Validate)
	if ok {
		//创建翻译器
		zhT := zh.New()
		enT := en.New()

		//创建通用翻译器
		//第一个参数是备用语言，后面的是应当支持的语言
		uni := ut.New(enT, enT, zhT)

		//从通过中获取指定语言翻译器
		trans, ok = uni.GetTranslator(language)
		if !ok {
			return fmt.Errorf("not found translator %s", language)
		}

		//绑定到gin的验证器上，对binding的tag进行翻译
		switch language {
		case "zh":
			err := zh_translation.RegisterDefaultTranslations(validate, trans)
			if err != nil {
				return err
			}
		default:
			err := en_translation.RegisterDefaultTranslations(validate, trans)
			if err != nil {
				return err
			}
		}

	}

	return nil
}

func remove(errors map[string]string) map[string]string {
	result := map[string]string{}
	for key, value := range errors {
		result[key[strings.Index(key, ".")+1:]] = value
	}
	return result
}

func SuccessResponse(msg string, dataPtr *map[string]any) *Response {
	if dataPtr == nil {
		emptyMap := make(map[string]any)
		dataPtr = &emptyMap
	}
	return &Response{
		Code: 200,
		Msg:  msg,
		Data: dataPtr,
	}
}

func SuccessResponseStr(msg string) *Response {

	return &Response{
		Code: 200,
		Msg:  msg,
		Data: &map[string]any{},
	}
}

func ErrResponse(msg string, responseErr error) *ErrResponseST {
	logResponseError(msg, responseErr)
	err := initTranslator("zh")
	if err != nil {
		panic(err)
	}
	if validationErrors, ok := responseErr.(validator.ValidationErrors); ok {
		translated := remove(validationErrors.Translate(trans))
		return &ErrResponseST{
			Code: 201,
			Msg:  msg,
			Data: &map[string]any{},
			Err:  translated,
		}
	}
	return &ErrResponseST{
		Code: 201,
		Msg:  msg,
		Data: &map[string]any{},
		Err:  "",
	}
}

func ErrResponseStr(msg string) *ErrResponseST {
	logResponseError(msg, nil)
	return &ErrResponseST{
		Code: 201,
		Msg:  msg,
		Data: &map[string]any{},
		Err:  "",
	}
}

func logResponseError(message string, err error) {
	if logger.Logger == nil {
		return
	}
	fields := []zap.Field{
		zap.Int("business_code", 201),
		zap.String("error_message", providerhttp.RedactText(message)),
	}
	if err != nil {
		fields = append(fields, zap.String("error", providerhttp.RedactText(err.Error())))
	}
	if err == nil || isRequestInputError(err) {
		logger.Warn("HTTP business response rejected", fields...)
		return
	}
	logger.Error("HTTP business response failed", fields...)
}

func isRequestInputError(err error) bool {
	var (
		validationErrors validator.ValidationErrors
		syntaxError      *json.SyntaxError
		typeError        *json.UnmarshalTypeError
		numberError      *strconv.NumError
	)
	return errors.Is(err, io.EOF) || errors.As(err, &validationErrors) ||
		errors.As(err, &syntaxError) || errors.As(err, &typeError) || errors.As(err, &numberError)
}
