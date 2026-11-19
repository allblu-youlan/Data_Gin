package Trigger

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"time"

	"gin-biz-web-api/internal/dao/auth_dao"
	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/qimai"
)

// QimaiOrderTrigger 企迈订单触发器
type QimaiOrderTrigger struct {
	tokenDataDAO *auth_dao.TokenDataDAO
	rawDataDAO   *data_dao.RawDataDAO
	qimaiDataDAO *data_dao.QimaiDataDAO
}

// NewQimaiOrderTrigger 创建企迈订单触发器实例
func NewQimaiOrderTrigger() *QimaiOrderTrigger {
	return &QimaiOrderTrigger{
		tokenDataDAO: auth_dao.NewTokenDataDAO(),
		rawDataDAO:   data_dao.NewRawDataDAO(),
		qimaiDataDAO: data_dao.NewQimaiDataDAO(),
	}
}

// Trigger 触发企迈订单处理
func (t *QimaiOrderTrigger) Trigger(ctx context.Context, rawData *model.RawData) error {
	// 1. 检查remark是否为qimai_order
	var metadata map[string]interface{}
	if err := json.Unmarshal([]byte(rawData.Metadata), &metadata); err != nil {
		return fmt.Errorf("解析元数据失败: %w", err)
	}

	remark, ok := metadata["remark"].(string)
	if !ok || remark != "qimai_order" {
		return nil // 不是企迈订单，不处理
	}

	// 2. 解析原始数据，提取orderNo
	var rawContent map[string]interface{}
	if err := json.Unmarshal([]byte(rawData.RawContent), &rawContent); err != nil {
		return fmt.Errorf("解析原始数据失败: %w", err)
	}

	params, ok := rawContent["params"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("原始数据格式错误，缺少params字段")
	}

	orderNo, ok := params["orderNo"].(string)
	if !ok {
		return fmt.Errorf("原始数据格式错误，缺少orderNo字段")
	}

	// 3. 从token_data表中获取account_name为野选的verification_info
	tokenDataList, err := t.tokenDataDAO.FindAll(ctx)
	if err != nil {
		return fmt.Errorf("查询token数据失败: %w", err)
	}

	var verificationInfo map[string]string
	for _, tokenData := range tokenDataList {
		if tokenData.AccountName == "野选" {
			// 解析verification_info
			// 处理指针类型
			viValue := tokenData.VerificationInfo
			// 解引用指针，直到得到实际值
			for {
				if ptr, ok := viValue.(*interface{}); ok {
					viValue = *ptr
				} else if ptr2, ok := viValue.(**interface{}); ok {
					viValue = **ptr2
				} else {
					break
				}
			}
			// 处理[]uint8类型（字节切片，GORM读取TEXT类型的结果）
			if viBytes, ok := viValue.([]uint8); ok {
				viValue = string(viBytes)
			}

			if viJSON, ok := viValue.(string); ok {
				if err := json.Unmarshal([]byte(viJSON), &verificationInfo); err != nil {
					return fmt.Errorf("解析verification_info失败: %w", err)
				}
				break
			} else if viMap, ok := viValue.(map[string]interface{}); ok {
				verificationInfo = make(map[string]string)
				for k, v := range viMap {
					if vStr, ok := v.(string); ok {
						verificationInfo[k] = vStr
					}
				}
				break
			}
		}
	}

	if verificationInfo == nil {
		return fmt.Errorf("未找到account_name为野选的token数据")
	}

	// 4. 验证token并获取值
	openId := verificationInfo["openId"]
	grantCode := verificationInfo["grantCode"]
	openKey := verificationInfo["open_key"]
	nonce := verificationInfo["nonce"]

	// 5. 调用GetOrderDetail获取订单详情
	orderDetail, err := qimai.GetOrderDetail(openId, grantCode, openKey, nonce, orderNo, 7)
	if err != nil {
		return fmt.Errorf("获取订单详情失败: %w", err)
	}

	// 6. 将订单详情存入qimai_order_data表
	orderData := &model.QIMAI_ORDER_DATA{
		RawDataID: rawData.ID,
		OrderNo:   orderNo,
		Status:    "processed",
	}

	// 映射API返回的数据到模型
	if data, ok := orderDetail["data"].(map[string]interface{}); ok {
		// 处理整数字段
		setIntField := func(key string, setter func(int)) {
			if v, ok := data[key].(float64); ok {
				setter(int(v))
			} else if v, ok := data[key].(int); ok {
				setter(v)
			} else if v, ok := data[key].(string); ok {
				if num, err := strconv.Atoi(v); err == nil {
					setter(num)
				}
			}
		}

		// 处理字符串字段
		setStringField := func(key string, setter func(string)) {
			if v, ok := data[key].(string); ok {
				setter(v)
			}
		}

		// 处理状态字段（可能是数字或字符串）
		if v, ok := data["status"]; ok {
			switch val := v.(type) {
			case float64:
				orderData.Status = strconv.Itoa(int(val))
			case int:
				orderData.Status = strconv.Itoa(val)
			case string:
				orderData.Status = val
			}
		}

		setIntField("orderType", func(v int) { orderData.OrderType = v })
		setIntField("source", func(v int) { orderData.Source = v })
		setIntField("payStatus", func(v int) { orderData.PayStatus = v })
		setIntField("totalAmount", func(v int) { orderData.TotalAmount = v })
		setIntField("actualAmount", func(v int) { orderData.ActualAmount = v })
		setIntField("discountAmount", func(v int) { orderData.DiscountAmount = v })
		setIntField("itemAmount", func(v int) { orderData.ItemAmount = v })
		setIntField("packAmount", func(v int) { orderData.PackAmount = v })
		setIntField("freightAmount", func(v int) { orderData.FreightAmount = v })

		setStringField("userId", func(v string) { orderData.UserID = v })
		setStringField("multiStoreId", func(v string) { orderData.MultiStoreID = v })
		setStringField("scene", func(v string) { orderData.Scene = v })
		setStringField("orderNo", func(v string) { orderData.OrderNo = v })
		setStringField("payNo", func(v string) { orderData.PayNo = v })
		setStringField("thirdPayNo", func(v string) { orderData.ThirdPayNo = v })
		setStringField("storeOrderNo", func(v string) { orderData.StoreOrderNo = v })
		setStringField("buyerRemarks", func(v string) { orderData.BuyerRemarks = v })
		setStringField("shopCode", func(v string) { orderData.ShopCode = v })
		setStringField("shopName", func(v string) { orderData.ShopName = v })
		setStringField("contactTel", func(v string) { orderData.ContactTel = v })
		setStringField("contactName", func(v string) { orderData.ContactName = v })

		// 处理收货信息
		if addr, ok := data["addr"].(map[string]interface{}); ok {
			if v, ok := addr["acceptName"].(string); ok {
				orderData.AddrAcceptName = v
			}
			if v, ok := addr["mobile"].(string); ok {
				orderData.AddrMobile = v
			}
			if v, ok := addr["sex"].(float64); ok {
				orderData.AddrSex = int(v)
			} else if v, ok := addr["sex"].(int); ok {
				orderData.AddrSex = v
			}
			if v, ok := addr["provinceName"].(string); ok {
				orderData.AddrProvinceName = v
			}
			if v, ok := addr["cityName"].(string); ok {
				orderData.AddrCityName = v
			}
			if v, ok := addr["areaName"].(string); ok {
				orderData.AddrAreaName = v
			}
			if v, ok := addr["acceptAddr"].(string); ok {
				orderData.AddrAcceptAddr = v
			}
		}

		// 处理JSON数组字段（itemList, payList, discountList）
		if itemList, ok := data["itemList"]; ok {
			if jsonBytes, err := json.Marshal(itemList); err == nil {
				orderData.ItemListJSON = string(jsonBytes)
			}
		}
		if payList, ok := data["payList"]; ok {
			if jsonBytes, err := json.Marshal(payList); err == nil {
				orderData.PayListJSON = string(jsonBytes)
			}
		}
		if discountList, ok := data["discountList"]; ok {
			if jsonBytes, err := json.Marshal(discountList); err == nil {
				orderData.DiscountListJSON = string(jsonBytes)
			}
		}

		// 处理额外数据（除了已知字段外的其他数据）
		knownKeys := map[string]bool{
			"shopCode": true, "orderType": true, "completedAt": true, "orderNo": true,
			"freightAmount": true, "actualAmount": true, "discountAmount": true, "shopName": true,
			"source": true, "payList": true, "packAmount": true, "payNo": true, "createdAt": true,
			"totalAmount": true, "thirdPayNo": true, "itemAmount": true, "itemList": true, "status": true,
			"userId": true, "multiStoreId": true, "scene": true, "addr": true, "discountList": true,
			"contactTel": true, "contactName": true,
		}
		extraData := make(map[string]interface{})
		for k, v := range data {
			if !knownKeys[k] {
				extraData[k] = v
			}
		}
		if len(extraData) > 0 {
			if jsonBytes, err := json.Marshal(extraData); err == nil {
				orderData.ExtraDataJSON = string(jsonBytes)
			}
		}

		// 处理时间字段（可能是字符串或数字）
		parseTimestamp := func(key string, setter func(*time.Time)) {
			var timestamp int64
			if v, ok := data[key].(float64); ok {
				timestamp = int64(v)
			} else if v, ok := data[key].(string); ok {
				if t, err := strconv.ParseInt(v, 10, 64); err == nil {
					timestamp = t
				}
			}
			if timestamp > 0 {
				t := time.Unix(0, timestamp*int64(time.Millisecond)).Local()
				setter(&t)
			}
		}

		parseTimestamp("orderAt", func(t *time.Time) { orderData.OrderAt = t })
		parseTimestamp("completedAt", func(t *time.Time) { orderData.CompletedAt = t })
	}

	_, err = t.qimaiDataDAO.Create(ctx, orderData)
	if err != nil {
		return fmt.Errorf("存储订单数据失败: %w", err)
	}

	log.Printf("企迈订单处理成功，订单号: %s", orderNo)
	return nil
}
