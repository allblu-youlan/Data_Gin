package send

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"gin-biz-web-api/pkg/config"
)

var salesXMLSensitiveTags = regexp.MustCompile(`(?is)<(password|licensekey)>(.*?)</(password|licensekey)>`)

type SalesDataResult struct {
	RequestBody  string
	ResponseBody string
	HTTPStatus   int
	Success      bool
}

func GetYesterdayDate() string {
	yesterday := time.Now().AddDate(0, 0, -1)
	return yesterday.Format("20060102")
}

func buildSoapXML(payAmount float64, tid string, completedAt *time.Time, storecode, mallitemcode, salestype string) string {
	var dayAgo, timeAgo string
	if completedAt != nil {
		dayAgo = completedAt.Format("20060102")
		timeAgo = completedAt.Format("150405")
	} else {
		dayAgo = GetYesterdayDate()
		timeAgo = "233000"
	}

	if salestype == "" {
		salestype = "SA"
	}

	licensekey := config.GetString("cfg.henglong.license_key")
	username := config.GetString("cfg.henglong.username")
	password := config.GetString("cfg.henglong.password")
	mallid := config.GetString("cfg.henglong.mall_id")
	tillid := config.GetString("cfg.henglong.till_id", "01")
	plucode := config.GetString("cfg.henglong.plu_code")

	xmlTemplate := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body>
    <postsalescreate xmlns="http://tempurl.org" xmlns:i="http://www.w3.org/2001/XMLSchema-instance">
      <astr_request>
        <header>
          <licensekey>%s</licensekey>
          <username>%s</username>
          <password>%s</password>
          <pagerecords>0</pagerecords>
          <pageno>0</pageno>
          <updatecount>0</updatecount>
          <messagetype>SALESDATA</messagetype>
          <messageid>332</messageid>
          <version>V332M</version>
        </header>
        <salestotal>
          <localstorecode>%s</localstorecode>
          <reservedocno></reservedocno>
          <txdate_yyyymmdd>%s</txdate_yyyymmdd>
          <txtime_hhmmss>%s</txtime_hhmmss>
          <mallid>%s</mallid>
          <storecode>%s</storecode>
          <tillid>%s</tillid>
          <salestype>%s</salestype>
          <txdocno>%s</txdocno>
          <orgtxdate_yyyymmdd></orgtxdate_yyyymmdd>
          <orgstorecode></orgstorecode>
          <orgtillid></orgtillid>
          <txorgdocno></txorgdocno>
          <mallitemcode>%s</mallitemcode>
          <cashier>%s</cashier>
          <netqty>1</netqty>
          <originalamount>0</originalamount>
          <sellingamount>%.2f</sellingamount>
          <couponqty>0</couponqty>
          <totaldiscount>
          </totaldiscount>
          <ttltaxamount1>0</ttltaxamount1>
          <ttltaxamount2>0</ttltaxamount2>
          <netamount>%.2f</netamount>
          <paidamount>%.2f</paidamount>
          <changeamount>0</changeamount>
          <priceincludetax></priceincludetax>
          <issueby>000</issueby>
          <issuedate_yyyymmdd>%s</issuedate_yyyymmdd>
          <issuetime_hhmmss>%s</issuetime_hhmmss>
        </salestotal>
        <salesitems>
          <salesitem>
            <iscounteritemcode>1</iscounteritemcode>
            <lineno>1</lineno>
            <storecode>%s</storecode>
            <mallitemcode>%s</mallitemcode>
            <counteritemcode>%s</counteritemcode>
            <itemcode>%s</itemcode>
            <plucode>%s</plucode>
            <invttype>1</invttype>
            <qty>1</qty>
            <exstk2sales>1</exstk2sales>
            <originalprice>0</originalprice>
            <sellingprice>0</sellingprice>
            <vipdiscountpercent>0</vipdiscountpercent>
            <vipdiscountless>0</vipdiscountless>
            <totaldiscountless1>0</totaldiscountless1>
            <totaldiscountless2>0</totaldiscountless2>
            <totaldiscountless>0</totaldiscountless>
            <netamount>%.2f</netamount>
            <bonusearn>0</bonusearn>
          </salesitem>
        </salesitems>
        <salestenders>
          <salestender>
            <lineno>1</lineno>
            <tendercode>CH</tendercode>
            <tendertype>0</tendertype>
            <tendercategory>0</tendercategory>
            <payamount>%.2f</payamount>
            <baseamount>%.2f</baseamount>
            <excessamount>0</excessamount>
          </salestender>
        </salestenders>
        <salesdelivery>
        </salesdelivery>
      </astr_request>
    </postsalescreate>
  </soap:Body>
</soap:Envelope>`,
		licensekey, username, password,
		storecode, dayAgo, timeAgo, mallid, storecode, tillid, salestype, tid,
		mallitemcode, storecode, payAmount, payAmount, payAmount,
		dayAgo, timeAgo,
		storecode, mallitemcode, mallitemcode, mallitemcode, plucode,
		payAmount, payAmount, payAmount,
	)

	return xmlTemplate
}

func sendPostRequest(xmlData string) (string, error) {
	result, err := sendPostRequestWithResult(xmlData)
	if result != nil {
		return result.ResponseBody, err
	}
	return "", err
}

func sendPostRequestWithResult(xmlData string) (*SalesDataResult, error) {
	headers := map[string]string{
		"Content-Type": "text/xml; charset=utf-8",
		"SOAPAction":   "http://tempurl.org/postsalescreate",
	}

	salesAPIURL := config.GetString("cfg.henglong.sales_api_url")

	req, err := http.NewRequest("POST", salesAPIURL, bytes.NewBufferString(xmlData))
	if err != nil {
		return nil, err
	}

	for key, value := range headers {
		req.Header.Set(key, value)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return &SalesDataResult{RequestBody: redactSalesXML(xmlData)}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return &SalesDataResult{
			RequestBody: redactSalesXML(xmlData),
			HTTPStatus:  resp.StatusCode,
		}, err
	}

	responseBody := string(body)
	return &SalesDataResult{
		RequestBody:  redactSalesXML(xmlData),
		ResponseBody: responseBody,
		HTTPStatus:   resp.StatusCode,
		Success:      Contains(responseBody, "responsecode>0<") || Contains(responseBody, "上传成功"),
	}, nil
}

func SendSalesData(payAmount float64, tid string, completedAt *time.Time, storecode, mallitemcode, salestype string) (string, error) {
	xmlData := buildSoapXML(payAmount, tid, completedAt, storecode, mallitemcode, salestype)
	return sendPostRequest(xmlData)
}

func SendSalesDataWithResult(
	payAmount float64,
	tid string,
	completedAt *time.Time,
	storecode string,
	mallitemcode string,
	salestype string,
) (*SalesDataResult, error) {
	xmlData := buildSoapXML(payAmount, tid, completedAt, storecode, mallitemcode, salestype)
	result, err := sendPostRequestWithResult(xmlData)
	if err == nil && result != nil && !result.Success && result.HTTPStatus >= http.StatusOK && result.HTTPStatus < http.StatusMultipleChoices {
		result.Success = isDuplicateSalesResponse(result.ResponseBody, tid)
	}
	return result, err
}

func isDuplicateSalesResponse(responseBody, tid string) bool {
	if tid == "" {
		return false
	}
	var response struct {
		XMLName xml.Name `xml:"Envelope"`
		Result  struct {
			Code    string `xml:"header>responsecode"`
			Message string `xml:"header>responsemessage"`
			DocNo   string `xml:"salestotal>txdocno"`
		} `xml:"Body>postsalescreateResponse>postsalescreateResult"`
	}
	if err := xml.Unmarshal([]byte(responseBody), &response); err != nil {
		return false
	}

	// 同一流水已在 MPOS 保存时，重试按成功处理，让调用方补齐同步状态。
	_, existingDocNo, exists := strings.Cut(response.Result.Message, "已存在交易流水")
	return strings.TrimSpace(response.Result.Code) == "1" && exists &&
		strings.TrimSpace(existingDocNo) == tid && strings.TrimSpace(response.Result.DocNo) == tid
}

func redactSalesXML(xmlData string) string {
	return salesXMLSensitiveTags.ReplaceAllString(xmlData, "<$1>***</$3>")
}

func Contains(s, substr string) bool {
	if len(s) < len(substr) {
		return false
	}
	if s == substr {
		return true
	}
	if len(s) == 0 {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
