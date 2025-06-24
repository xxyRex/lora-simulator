package test_payload_codec

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

// PayloadCodecTestData 表示编解码测试数据的结构体
type PayloadCodecTestData struct {
	Description    string                 `json:"description"`       // 描述信息
	Command        string                 `json:"command"`           // 16进制数据
	Response       string                 `json:"response"`          // 编码后的数据
	CodecType      string                 `json:"codec_type"`        // 编码类型 (encode/encodeJSON等)
	JSONContent    map[string]interface{} `json:"json_content"`      // JSON内容
	Raw            string                 `json:"raw"`               // 期望结果
	Result         string                 `json:"result"`            // 通过状态
	APIDEResult    bool                   `json:"api_de_result"`     // API解码结果
	APIDEResultMsg string                 `json:"api_de_result_msg"` // API解码结果信息
	APIENResult    bool                   `json:"api_en_result"`     // API编码结果
	APIENResultMsg string                 `json:"api_en_result_msg"` // API编码结果信息
}

// PayloadCodecTestSuite 表示整个测试套件
type PayloadCodecTestSuite struct {
	TestCases []PayloadCodecTestData `json:"test_cases"`
	Summary   TestSummary            `json:"summary"`
}

// TestSummary 测试摘要统计
type TestSummary struct {
	TotalCount     int            `json:"total_count"`
	StatusCount    map[string]int `json:"status_count"`
	CodecTypeCount map[string]int `json:"codec_type_count"`
}

// ParseExcelSheet 解析Excel表格中的编解码数据
func ParseExcelSheet(filePath, sheetName string) (*PayloadCodecTestSuite, error) {
	// 打开Excel文件
	f, err := excelize.OpenFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("打开Excel文件失败: %v", err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			fmt.Printf("关闭Excel文件时出错: %v\n", err)
		}
	}()

	// 获取工作表的所有行
	rows, err := f.GetRows(sheetName)
	if err != nil {
		return nil, fmt.Errorf("读取工作表 %s 失败: %v", sheetName, err)
	}

	if len(rows) == 0 {
		return nil, fmt.Errorf("工作表 %s 为空", sheetName)
	}

	suite := &PayloadCodecTestSuite{
		TestCases: make([]PayloadCodecTestData, 0),
		Summary: TestSummary{
			StatusCount:    make(map[string]int),
			CodecTypeCount: make(map[string]int),
		},
	}

	// 跳过表头，从第二行开始解析
	for i := 1; i < len(rows); i++ {
		row := rows[i]

		// 确保行有足够的列，根据表格结构调整
		if len(row) < 4 {
			continue
		}

		testData := PayloadCodecTestData{}

		// 解析各列数据 - 根据实际Excel表格列序调整
		if len(row) > 0 {
			testData.Description = strings.TrimSpace(row[0])
		}
		if len(row) > 1 {
			testData.Command = strings.TrimSpace(row[1])
		}
		if len(row) > 2 {
			testData.Response = strings.TrimSpace(row[2])
		}
		if len(row) > 3 {
			testData.CodecType = strings.TrimSpace(row[3])
		}
		if len(row) > 4 {
			jsonStr := strings.TrimSpace(row[4])
			if jsonStr != "" {
				testData.JSONContent = parseJSONContent(jsonStr)
			}
		}
		if len(row) > 5 {
			testData.Raw = strings.TrimSpace(row[5])
		}
		if len(row) > 6 {
			testData.Result = strings.TrimSpace(row[6])
		}

		// 只添加有意义的测试用例（至少包含描述和数据）
		if testData.Description != "" && testData.Command != "" {
			suite.TestCases = append(suite.TestCases, testData)

			// 统计信息
			if testData.Result != "" {
				suite.Summary.StatusCount[testData.Result]++
			}
			if testData.CodecType != "" {
				suite.Summary.CodecTypeCount[testData.CodecType]++
			}
		}
	}

	suite.Summary.TotalCount = len(suite.TestCases)
	return suite, nil
}

// parseJSONContent 解析JSON内容
func parseJSONContent(jsonStr string) map[string]interface{} {
	var jsonContent map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &jsonContent); err == nil {
		return jsonContent
	} else {
		// 如果JSON解析失败，将其作为字符串存储
		return map[string]interface{}{
			"raw":         jsonStr,
			"parse_error": err.Error(),
		}
	}
}

// SaveToJSON 将解析的数据保存为JSON文件
func (suite *PayloadCodecTestSuite) SaveToJSON(filePath string) error {
	jsonData, err := json.MarshalIndent(suite, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化JSON失败: %v", err)
	}

	err = os.WriteFile(filePath, jsonData, 0644)
	if err != nil {
		return fmt.Errorf("写入文件失败: %v", err)
	}

	fmt.Printf("成功保存JSON文件到: %s\n", filePath)
	return nil
}

// GetTestCaseByDescription 根据描述查找测试用例
func (suite *PayloadCodecTestSuite) GetTestCaseByDescription(description string) *PayloadCodecTestData {
	for i, testCase := range suite.TestCases {
		if strings.Contains(testCase.Description, description) {
			return &suite.TestCases[i]
		}
	}
	return nil
}

// GetTestCasesByStatus 根据状态筛选测试用例
func (suite *PayloadCodecTestSuite) GetTestCasesByStatus(status string) []PayloadCodecTestData {
	var result []PayloadCodecTestData
	for _, testCase := range suite.TestCases {
		if testCase.Result == status {
			result = append(result, testCase)
		}
	}
	return result
}

// GetTestCasesByCodecType 根据编码类型筛选测试用例
func (suite *PayloadCodecTestSuite) GetTestCasesByCodecType(codecType string) []PayloadCodecTestData {
	var result []PayloadCodecTestData
	for _, testCase := range suite.TestCases {
		if testCase.CodecType == codecType {
			result = append(result, testCase)
		}
	}
	return result
}

// ValidateHexData 验证16进制数据格式
func ValidateHexData(hexData string) bool {
	// 移除可能的空格和前缀
	hexData = strings.ReplaceAll(hexData, " ", "")
	hexData = strings.TrimPrefix(hexData, "0x")
	hexData = strings.TrimPrefix(hexData, "0X")

	if len(hexData) == 0 {
		return false
	}

	// 检查每个字符是否为有效的16进制字符
	for _, c := range hexData {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}

	return true
}

// ConvertHexToBytes 将16进制字符串转换为字节数组
func ConvertHexToBytes(hexData string) ([]byte, error) {
	// 移除空格和前缀
	hexData = strings.ReplaceAll(hexData, " ", "")
	hexData = strings.TrimPrefix(hexData, "0x")
	hexData = strings.TrimPrefix(hexData, "0X")

	// 如果是奇数长度，在前面补0
	if len(hexData)%2 != 0 {
		hexData = "0" + hexData
	}

	bytes := make([]byte, len(hexData)/2)
	for i := 0; i < len(hexData); i += 2 {
		b, err := strconv.ParseUint(hexData[i:i+2], 16, 8)
		if err != nil {
			return nil, fmt.Errorf("解析16进制字符串失败: %v", err)
		}
		bytes[i/2] = byte(b)
	}

	return bytes, nil
}

// PrintTestCaseSummary 打印测试用例摘要
func (suite *PayloadCodecTestSuite) PrintTestCaseSummary() {
	fmt.Printf("编解码测试数据摘要:\n")
	fmt.Printf("总测试用例数: %d\n", suite.Summary.TotalCount)

	fmt.Println("按状态统计:")
	for status, count := range suite.Summary.StatusCount {
		fmt.Printf("  %s: %d\n", status, count)
	}

	fmt.Println("按编码类型统计:")
	for codecType, count := range suite.Summary.CodecTypeCount {
		fmt.Printf("  %s: %d\n", codecType, count)
	}
}

// PrintTestCaseDetails 打印指定测试用例的详细信息
func (suite *PayloadCodecTestSuite) PrintTestCaseDetails(index int) {
	if index < 0 || index >= len(suite.TestCases) {
		fmt.Printf("无效的测试用例索引: %d\n", index)
		return
	}

	testCase := suite.TestCases[index]
	fmt.Printf("测试用例 #%d:\n", index)
	fmt.Printf("  描述: %s\n", testCase.Description)
	fmt.Printf("  16进制数据: %s\n", testCase.Command)
	fmt.Printf("  编码数据: %s\n", testCase.Response)
	fmt.Printf("  编码类型: %s\n", testCase.CodecType)
	fmt.Printf("  期望结果: %s\n", testCase.Raw)
	fmt.Printf("  状态: %s\n", testCase.Result)

	if len(testCase.JSONContent) > 0 {
		fmt.Printf("  JSON内容:\n")
		for key, value := range testCase.JSONContent {
			fmt.Printf("    %s: %v\n", key, value)
		}
	}
}

// ExportFailedCases 导出失败的测试用例
func (suite *PayloadCodecTestSuite) ExportFailedCases(filePath string) error {
	failedCases := suite.GetTestCasesByStatus("failed")
	if len(failedCases) == 0 {
		fmt.Println("没有失败的测试用例")
		return nil
	}

	failedSuite := &PayloadCodecTestSuite{
		TestCases: failedCases,
		Summary: TestSummary{
			TotalCount:     len(failedCases),
			StatusCount:    map[string]int{"failed": len(failedCases)},
			CodecTypeCount: make(map[string]int),
		},
	}

	// 重新统计编码类型
	for _, testCase := range failedCases {
		if testCase.CodecType != "" {
			failedSuite.Summary.CodecTypeCount[testCase.CodecType]++
		}
	}

	return failedSuite.SaveToJSON(filePath)
}

// Example 使用示例函数
func Example() {
	// 使用示例
	filePath := "cmd/lora-simulator/payload_en_decoder/编解码数据表.xlsx"
	sheetName := "WT201" // 根据实际的sheet名称调整，可能是 "WT101" 等

	fmt.Printf("开始解析Excel文件: %s, Sheet: %s\n", filePath, sheetName)

	suite, err := ParseExcelSheet(filePath, sheetName)
	if err != nil {
		fmt.Printf("解析Excel失败: %v\n", err)
		return
	}

	// 打印摘要
	suite.PrintTestCaseSummary()

	// 查找特定测试用例
	testCase := suite.GetTestCaseByDescription("温度")
	if testCase != nil {
		fmt.Printf("找到温度相关测试用例: %s\n", testCase.Description)
	}

	// 保存为JSON
	err = suite.SaveToJSON("payload_codec_test_data.json")
	if err != nil {
		fmt.Printf("保存JSON失败: %v\n", err)
		return
	}

	// 导出失败的测试用例
	err = suite.ExportFailedCases("failed_test_cases.json")
	if err != nil {
		fmt.Printf("导出失败用例失败: %v\n", err)
	}

	// 显示前几个测试用例的详细信息
	fmt.Println("\n前3个测试用例详情:")
	for i := 0; i < min(3, len(suite.TestCases)); i++ {
		suite.PrintTestCaseDetails(i)
		fmt.Println()
	}
}

// min 辅助函数，返回两个整数的最小值
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
