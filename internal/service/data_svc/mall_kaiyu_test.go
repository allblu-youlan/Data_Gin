package data_svc

import (
	"encoding/json"
	"gin-biz-web-api/internal/requestbody"
	"testing"
	"time"
)

func TestMallKaiyuShopIDRoundTripAndIndependentPatch(t *testing.T) {
	request := validMallCreateRequest()
	if err := json.Unmarshal([]byte(`{"kaiyuShopId":" 8692 "}`), &request); err != nil {
		t.Fatal(err)
	}
	normalized, err := normalizeMallCreateRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	if err := applyMallWeatherDefaults(&normalized, "full", 1000); err != nil {
		t.Fatal(err)
	}
	mall, err := mallFromCreateRequest(normalized, 1, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	dto, err := mallDTO(mall)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(dto)
	var fields map[string]interface{}
	_ = json.Unmarshal(encoded, &fields)
	if fields["kaiyuShopId"] != "8692" {
		t.Fatalf("shop binding lost: %s", encoded)
	}
	var patch requestbody.MallPatchRequest
	_ = json.Unmarshal([]byte(`{"kaiyuShopId":"1234"}`), &patch)
	updates, geocode, err := buildMallPatch(mall, patch, 1)
	if err != nil || geocode || updates["kaiyu_shop_id"] != "1234" {
		t.Fatalf("updates=%v geocode=%v err=%v", updates, geocode, err)
	}
}
