package test_payload_codec

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

func TestDebugJSONParse(t *testing.T) {
	filePath := "/mnt/data/work_code/gateway/lora-simulator/GS601.json"

	// 读取文件内容
	jsonData, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}

	fmt.Printf("文件大小: %d 字节\n", len(jsonData))
	fmt.Printf("文件前100个字符: %s\n", string(jsonData[:100]))

	// 尝试解析为 TSLConfig
	var tslConfig TSLConfig
	if err := json.Unmarshal(jsonData, &tslConfig); err != nil {
		t.Logf("解析为 TSLConfig 失败: %v", err)

		// 尝试解析为 codecItem 数组
		var codecItems []codecItem
		if err := json.Unmarshal(jsonData, &codecItems); err != nil {
			t.Fatalf("解析为 codecItem 数组也失败: %v", err)
		}
		t.Logf("成功解析为 codecItem 数组，共 %d 个项目", len(codecItems))
	} else {
		t.Logf("成功解析为 TSLConfig，Object 字段包含 %d 个项目", len(tslConfig.Object))
	}
}

// TestParseIndividualItem 测试解析单个项目
func TestParseIndividualItem(t *testing.T) {
	// 创建一个简单的测试 JSON
	testJSON := `{
		"id": "test",
		"name": "Test Item",
		"value": "",
		"unit": "",
		"access_mode": "R",
		"data_type": "ENUM",
		"value_type": "UINT8",
		"values": [
			{
				"value": 0,
				"name": "off"
			},
			{
				"value": 1,
				"name": "on"
			}
		],
		"bacnet_type": "multistate_value_object",
		"bacnet_unit_type_id": 95,
		"bacnet_unit_type": "UNITS_NO_UNITS"
	}`

	var item codecItem
	if err := json.Unmarshal([]byte(testJSON), &item); err != nil {
		t.Fatalf("解析单个项目失败: %v", err)
	}

	t.Logf("成功解析单个项目: ID=%s, Name=%s", item.ID, item.Name)
}

// TestStepByStepParse 逐步解析 JSON 文件
func TestStepByStepParse(t *testing.T) {
	filePath := "/mnt/data/work_code/gateway/lora-simulator/GS601.json"

	// 读取文件内容
	jsonData, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}

	// 首先解析为通用结构
	var rawData map[string]json.RawMessage
	if err := json.Unmarshal(jsonData, &rawData); err != nil {
		t.Fatalf("解析为通用结构失败: %v", err)
	}

	// 检查是否有 object 字段
	if objectData, exists := rawData["object"]; exists {
		t.Logf("找到 object 字段，长度: %d", len(objectData))

		// 解析 object 字段为数组
		var objectArray []json.RawMessage
		if err := json.Unmarshal(objectData, &objectArray); err != nil {
			t.Fatalf("解析 object 数组失败: %v", err)
		}

		t.Logf("object 数组包含 %d 个项目", len(objectArray))

		// 逐个解析每个项目
		for i, itemData := range objectArray {
			var item codecItem
			if err := json.Unmarshal(itemData, &item); err != nil {
				t.Errorf("解析第 %d 个项目失败: %v", i, err)
				t.Logf("项目数据: %s", string(itemData))
			}
			// t.Logf("成功解析第 %d 个项目: ID=%s", i, item.ID)
		}
	} else {
		t.Fatalf("未找到 object 字段")
	}
}
