package send

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "gin-biz-web-api/config"
	pkgConfig "gin-biz-web-api/pkg/config"
)

const duplicateSalesResponse = `<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"><soap:Body><postsalescreateResponse xmlns="http://tempurl.org"><postsalescreateResult><header><responsecode>1</responsecode><responsemessage>交易保存到MPOS失败，POS机41620101，已存在交易流水ABCN002A001P12610151236520008</responsemessage><pagerecords>0</pagerecords><pageno>0</pageno><updatecount>0</updatecount><maxrecords>0</maxrecords><maxpageno>0</maxpageno><messagetype>SALESDATA</messagetype><messageid>332</messageid><version>V332M</version></header><salestotal><localstorecode>416201</localstorecode><reservedocno></reservedocno><txdate_yyyymmdd>20261016</txdate_yyyymmdd><txtime_hhmmss>132703</txtime_hhmmss><mallid>WESTLAKE66</mallid><storecode>416201</storecode><tillid>01</tillid><salestype>SA</salestype><txdocno>ABCN002A001P12610151236520008</txdocno><orgtxdate_yyyymmdd></orgtxdate_yyyymmdd><orgstorecode></orgstorecode><orgtillid></orgtillid><mallitemcode>E6600000099</mallitemcode><cashier>416201</cashier><netqty>1</netqty><originalamount>0</originalamount><sellingamount>536.40</sellingamount><couponqty>0</couponqty><totaldiscount/><ttltaxamount1>0</ttltaxamount1><ttltaxamount2>0</ttltaxamount2><netamount>536.40</netamount><paidamount>536.40</paidamount><changeamount>0</changeamount><priceincludetax></priceincludetax><issueby>000</issueby><issuedate_yyyymmdd>20261016</issuedate_yyyymmdd><issuetime_hhmmss>132703</issuetime_hhmmss></salestotal></postsalescreateResult></postsalescreateResponse></soap:Body></soap:Envelope>`

type salesDataTestTransport func(*http.Request) (*http.Response, error)

func (transport salesDataTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestSendSalesDataWithResultRecognizesDuplicateTransaction(t *testing.T) {
	setupSalesDataTestConfig(t)
	originalTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = originalTransport })
	const tid = "ABCN002A001P12610151236520008"
	tests := []struct {
		name       string
		response   string
		httpStatus int
		want       bool
	}{
		{name: "reported duplicate", response: duplicateSalesResponse, want: true},
		{name: "normal success code", response: `<header><responsecode>0</responsecode></header>`, want: true},
		{name: "legacy success message", response: "上传成功", want: true},
		{name: "ordinary rejection", response: strings.Replace(duplicateSalesResponse, "已存在交易流水"+tid, "门店不存在", 1)},
		{name: "different transaction", response: strings.ReplaceAll(duplicateSalesResponse, tid, tid+"9")},
		{name: "different transaction in message", response: strings.Replace(duplicateSalesResponse, "已存在交易流水"+tid, "已存在交易流水OTHER", 1)},
		{name: "different transaction in result", response: strings.Replace(duplicateSalesResponse, "<txdocno>"+tid, "<txdocno>OTHER", 1)},
		{name: "malformed response", response: strings.TrimSuffix(duplicateSalesResponse, "</soap:Envelope>")},
		{name: "http failure", response: duplicateSalesResponse, httpStatus: http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := tt.httpStatus
			if status == 0 {
				status = http.StatusOK
			}
			http.DefaultTransport = salesDataTestTransport(func(request *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: status,
					Body:       io.NopCloser(strings.NewReader(tt.response)),
					Header:     make(http.Header),
					Request:    request,
				}, nil
			})
			result, err := SendSalesDataWithResult(536.40, tid, nil, "416201", "E6600000099", "SA")
			if err != nil {
				t.Fatalf("SendSalesDataWithResult() error = %v", err)
			}
			if result == nil || result.Success != tt.want {
				t.Fatalf("result = %+v, want success %t", result, tt.want)
			}
			if result.ResponseBody != tt.response || result.HTTPStatus != status {
				t.Fatalf("response was changed: %+v", result)
			}
		})
	}
}

func setupSalesDataTestConfig(t *testing.T) {
	t.Helper()
	configDir := t.TempDir()
	configBody := []byte("HengLong:\n  TillID: \"01\"\n")
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), configBody, 0o600); err != nil {
		t.Fatalf("write test config: %v", err)
	}
	pkgConfig.NewConfig("", configDir+string(os.PathSeparator))
}

func TestBuildSoapXMLUsesProvidedIssueTimeAndMallItemCode(t *testing.T) {
	setupSalesDataTestConfig(t)
	issuedAt := time.Date(2026, 7, 3, 15, 45, 11, 0, time.FixedZone("CST", 8*60*60))

	xml := buildSoapXML(118.15, "ABCN002A001P12607031545110012", &issuedAt, "416201", "E6600000099", "SA")

	for _, want := range []string{
		"<issuedate_yyyymmdd>20260703</issuedate_yyyymmdd>",
		"<issuetime_hhmmss>154511</issuetime_hhmmss>",
		"<txtime_hhmmss>154511</txtime_hhmmss>",
		"<mallitemcode>E6600000099</mallitemcode>",
	} {
		if !strings.Contains(xml, want) {
			t.Fatalf("xml missing %s", want)
		}
	}
	if strings.Contains(xml, "<issuetime_hhmmss>000000</issuetime_hhmmss>") {
		t.Fatal("xml contains zero issue time")
	}
}
