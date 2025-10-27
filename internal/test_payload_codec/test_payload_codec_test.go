package test_payload_codec

import (
	"fmt"
	"testing"
)

// TestParseExcelSheet 测试Excel解析功能
func TestParseExcelSheet(t *testing.T) {
	// 测试文件路径
	filePath := "../../cmd/lora-simulator/payload_en_decoder/编解码数据表.xlsx"

	// 可能的sheet名称列表
	sheetNames := []string{"WT101"}

	var suite *PayloadCodecTestSuite
	var err error

	// 尝试不同的sheet名称
	for _, sheetName := range sheetNames {
		fmt.Printf("尝试解析 sheet: %s\n", sheetName)
		suite, err = ParseExcelSheet(filePath, sheetName)
		if err == nil {
			fmt.Printf("成功解析 sheet: %s\n", sheetName)
			break
		}
		fmt.Printf("解析 sheet %s 失败: %v\n", sheetName, err)
	}

	if err != nil {
		t.Logf("无法解析Excel文件，可能文件不存在或sheet名称不正确: %v", err)
		return // 不标记为失败，因为这是可选的测试
	}

	// 打印解析结果
	suite.PrintTestCaseSummary()

	// 验证解析结果
	if len(suite.TestCases) == 0 {
		t.Error("没有解析到任何测试用例")
	}

	// 显示前几个测试用例
	fmt.Println("\n测试用例示例:")
	for i := 0; i < min(3, len(suite.TestCases)); i++ {
		suite.PrintTestCaseDetails(i)
		fmt.Println()
	}

	suite.SaveToJSON("payload_codec_test_data.json")
}

// TestValidateHexData 测试16进制数据验证
func TestValidateHexData(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"ff1010a0100", true},
		{"FF1010A0100", true},
		{"0xFF1010A0100", true},
		{"ff10", true},
		{"", false},
		{"gg", false},
		{"f", false},    // 奇数长度
		{"f1 f2", true}, // 包含空格
	}

	for _, test := range tests {
		result := ValidateHexData(test.input)
		if result != test.expected {
			t.Errorf("ValidateHexData(%s) = %v, expected %v", test.input, result, test.expected)
		}
	}
}

// TestConvertHexToBytes 测试16进制转字节数组
func TestConvertHexToBytes(t *testing.T) {
	tests := []struct {
		input    string
		expected []byte
		hasError bool
	}{
		{"ff10", []byte{0xff, 0x10}, false},
		{"FF10", []byte{0xff, 0x10}, false},
		{"0xFF10", []byte{0xff, 0x10}, false},
		{"f", nil, true},  // 奇数长度
		{"gg", nil, true}, // 无效字符
	}

	for _, test := range tests {
		result, err := ConvertHexToBytes(test.input)

		if test.hasError {
			if err == nil {
				t.Errorf("ConvertHexToBytes(%s) expected error but got none", test.input)
			}
		} else {
			if err != nil {
				t.Errorf("ConvertHexToBytes(%s) unexpected error: %v", test.input, err)
			}
			if len(result) != len(test.expected) {
				t.Errorf("ConvertHexToBytes(%s) length mismatch: got %d, expected %d",
					test.input, len(result), len(test.expected))
			}
			for i, b := range result {
				if b != test.expected[i] {
					t.Errorf("ConvertHexToBytes(%s) byte %d: got 0x%02x, expected 0x%02x",
						test.input, i, b, test.expected[i])
				}
			}
		}
	}
}

// ExampleParseExcelSheet 使用示例
func ExampleParseExcelSheet() {
	// 解析Excel文件
	suite, err := ParseExcelSheet("test.xlsx", "Sheet1")
	if err != nil {
		fmt.Printf("解析失败: %v\n", err)
		return
	}

	// 打印摘要
	suite.PrintTestCaseSummary()

	// 查找特定测试用例
	testCase := suite.GetTestCaseByDescription("温度")
	if testCase != nil {
		fmt.Printf("找到测试用例: %s\n", testCase.Description)
	}

	// 保存为JSON
	err = suite.SaveToJSON("output.json")
	if err != nil {
		fmt.Printf("保存失败: %v\n", err)
	}
}

func TestParseTSLConfigExcelSheet(t *testing.T) {
	filePath := "/mnt/data/work_code/gateway/lora-simulator/cmd/lora-simulator/payload_en_decoder/codec.xlsx"
	sheetName := "AM103L"
	tslConfigTestData, err := ParseTSLConfigExcelSheetByHeader(filePath, sheetName)
	if err != nil {
		t.Errorf("ParseTSLConfigExcelSheet(%s, %s) error: %v", filePath, sheetName, err)
	}

	for _, data := range tslConfigTestData {
		fmt.Printf("Description: %s\n", data.Description)
		fmt.Printf("Command: %s\n", data.Command)
		fmt.Printf("Response: %s\n", data.Response)
		fmt.Printf("CodecType: %s\n", data.CodecType)
		fmt.Printf("IPSOType: %s\n", data.IPSOType)
		fmt.Printf("Code: %s\n", data.Code)
		fmt.Printf("Raw: %s\n", data.Raw)
		fmt.Printf("Result: %s\n", data.Result)
		fmt.Printf("ErrorMsg: %s\n", data.ErrorMsg)
		fmt.Printf("TSLConfig: %v\n", data.TSLConfig)
	}
}
