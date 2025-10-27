package test_payload_codec

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/brocaar/lora-simulator/internal/as"
	"github.com/brocaar/lora-simulator/internal/as_api/models"
	"github.com/brocaar/lora-simulator/internal/config"
	log "github.com/sirupsen/logrus"
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

// 新结构体
type TSLConfigTestData struct {
	Description string                 `json:"description"`
	Command     string                 `json:"command"`
	Response    string                 `json:"response"`
	CodecType   string                 `json:"codec_type"`
	IPSOType    string                 `json:"ipso_type"`
	Code        string                 `json:"code"`
	Raw         string                 `json:"raw"`
	Result      string                 `json:"result"`
	ErrorMsg    string                 `json:"error_msg"`
	TSLConfig   map[string]interface{} `json:"tsl_config"`
}

// 新函数
func ParseTSLConfigExcelSheetByHeader(filePath, sheetName string) ([]TSLConfigTestData, error) {
	f, err := excelize.OpenFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("打开Excel文件失败: %v", err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			fmt.Printf("关闭Excel文件时出错: %v\n", err)
		}
	}()

	rows, err := f.GetRows(sheetName)
	if err != nil {
		return nil, fmt.Errorf("读取工作表 %s 失败: %v", sheetName, err)
	}
	if len(rows) < 1 {
		return nil, fmt.Errorf("工作表 %s 为空", sheetName)
	}

	// 1. 解析表头
	header := rows[0]
	headerMap := make(map[string]int)
	for idx, name := range header {
		headerMap[strings.TrimSpace(name)] = idx
	}

	// 2. 逐行解析
	var result []TSLConfigTestData
	for i := 1; i < len(rows); i++ {
		row := rows[i]
		get := func(col string) string {
			if idx, ok := headerMap[col]; ok && idx < len(row) {
				return strings.TrimSpace(row[idx])
			}
			return ""
		}
		data := TSLConfigTestData{
			Description: get("中文含义"),
			Command:     get("指令"),
			Response:    get("回复指令"),
			CodecType:   get("编码格式"),
			IPSOType:    get("IPSO类型"),
			Code:        get("代码"),
			Raw:         get("RAW"),
			Result:      get("结果"),
			ErrorMsg:    get("错误信息"),
		}
		tslStr := get("tsl_config")
		if tslStr != "" {
			var tsl map[string]interface{}
			if err := json.Unmarshal([]byte(tslStr), &tsl); err == nil {
				data.TSLConfig = tsl
			} else {
				data.TSLConfig = map[string]interface{}{"raw": tslStr, "parse_error": err.Error()}
			}
		}
		result = append(result, data)
	}
	return result, nil
}

type value struct {
	Value int    `json:"value"`
	Name  string `json:"name"`
}

type codecItem struct {
	ID                      string   `json:"id"`
	Name                    string   `json:"name"`
	Value                   string   `json:"value"`
	Unit                    string   `json:"unit"`
	AccessMode              string   `json:"access_mode"`
	DataType                string   `json:"data_type"`
	ValueType               string   `json:"value_type"`
	MaxLength               uint32   `json:"max_length"`
	Values                  []*value `json:"values"`
	BacnetType              string   `json:"bacnet_type"`
	BacnetUnitType          string   `json:"bacnet_unit_type"`
	BacnetUnitTypeID        *uint32  `json:"bacnet_unit_type_id"`
	Reference               []string `json:"reference"`
	BacnetPolarity          *int     `json:"bacnet_polarity,omitempty"`
	BacnetRelinquishDefault *string  `json:"bacnet_relinquish_default,omitempty"`
	Description             string   `json:"description"`
	PayloadCodecObjectID    int64    `json:"-"`
}

// TSLConfig 结构体用于解析包含 object 字段的 JSON 文件
type TSLConfig struct {
	Version string      `json:"version"`
	Bytes   string      `json:"bytes"`
	Object  []codecItem `json:"object"`
}

func ReadCodecItem(filePath string) ([]codecItem, error) {
	jsonFile, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer jsonFile.Close()

	// 首先尝试解析为 TSLConfig 结构
	var tslConfig TSLConfig
	decoder := json.NewDecoder(jsonFile)
	if err := decoder.Decode(&tslConfig); err == nil {
		// 如果成功解析为 TSLConfig，返回其中的 Object 字段
		return tslConfig.Object, nil
	}

	// 如果解析失败，重置文件指针并尝试解析为 codecItem 数组
	jsonFile.Seek(0, 0)
	var codecItems []codecItem
	decoder = json.NewDecoder(jsonFile)
	if err := decoder.Decode(&codecItems); err != nil {
		return nil, err
	}
	return codecItems, nil
}

type TestResult struct {
	Device         string           `json:"device"`
	TestResultItem []TestResultItem `json:"test_result_item"`
}

type TestResultItem struct {
	Type     string `json:"type"`
	Data     string `json:"data"`
	Result   string `json:"result"`
	ErrorMsg string `json:"error_msg"`
	Success  bool   `json:"success"`
	Name     string `json:"name"`
}

type PayloadCodecTest struct {
	ctx        context.Context
	TestResult []TestResult
}

func Start(ctx context.Context) error {
	p := &PayloadCodecTest{
		ctx:        ctx,
		TestResult: make([]TestResult, 0),
	}
	as.SimpleSetup(config.C.LoraSimulator.TestPayloadCodec.OldHost, config.C.LoraSimulator.API.Username, config.C.LoraSimulator.API.Password)
	p.TestPayloadCodec()
	p.saveTestResultToFile("old_host_test_result.json")

	as.SimpleSetup(config.C.LoraSimulator.TestPayloadCodec.NewHost, config.C.LoraSimulator.API.Username, config.C.LoraSimulator.API.Password)
	p.TestPayloadCodec()
	p.saveTestResultToFile("new_host_test_result.json")

	p.compareTestResult("old_host_test_result.json", "new_host_test_result.json")

	return nil
}

func (p *PayloadCodecTest) TestPayloadCodec() error {
	pcShortList, err := as.GetPayloadCodecList("default")
	if err != nil {
		return err
	}

	pcShortMap := make(map[string]*models.APIShortPayloadCodecItem)
	for _, pcShortItem := range pcShortList {
		pcShortMap[pcShortItem.Name] = pcShortItem
	}

	for _, deviceSheet := range config.C.LoraSimulator.TestPayloadCodec.TestDeviceSheet {
		pcShortItem, ok := pcShortMap[deviceSheet.Device]
		if !ok {
			log.Errorf("payload codec not found: %s", deviceSheet.Device)
			continue
		}

		payloadCodec, err := as.GetPayloadCodecByID(pcShortItem.ID)
		if err != nil {
			log.Errorf("failed to get payload codec: %v", err)
			continue
		}

		testData, err := ParseTSLConfigExcelSheetByHeader(config.C.LoraSimulator.TestPayloadCodec.TestCaseFile, deviceSheet.Sheet)
		if err != nil {
			log.Errorf("failed to parse excel sheet: %v", err)
			continue
		}

		testResult := TestResult{
			Device:         deviceSheet.Device,
			TestResultItem: make([]TestResultItem, 0),
		}

		for _, data := range testData {
			if data.IPSOType != "正常" {
				continue
			}

			switch data.CodecType {
			case "decode":
				testResult.TestResultItem = append(testResult.TestResultItem, p.decode(data.Description, data.Command, payloadCodec.DecoderScript))
			case "encode":
				decodeApiResult := p.decode(data.Description, data.Command, payloadCodec.DecoderScript)
				testResult.TestResultItem = append(testResult.TestResultItem, p.encode(data.Description, decodeApiResult.Result, payloadCodec.EncoderScript))
			}
			time.Sleep(1 * time.Second)
		}

		p.TestResult = append(p.TestResult, testResult)
	}

	return nil
}

func (p *PayloadCodecTest) encode(name string, data string, script string) TestResultItem {
	testItem := TestResultItem{
		Type:     "encode",
		Data:     data,
		Result:   "",
		ErrorMsg: "",
		Name:     name,
		Success:  false,
	}

	if data == "" {
		testItem.ErrorMsg = "data is empty"
		testItem.Success = false
		return testItem
	}

	log.Infof("encode data: %s", data)
	encodeApiResult, err := as.PayloadCodecTest(&models.APITestPayloadCodecRequest{
		Data:   data,
		FPort:  1,
		Script: script,
		Type:   "encode",
	})
	if err != nil {
		log.Errorf("failed to encode data: %s, error: %v", data, err)
		testItem.ErrorMsg = err.Error()
		testItem.Success = false
		return testItem
	}
	testItem.Result = encodeApiResult
	testItem.Success = true
	return testItem
}

func (p *PayloadCodecTest) decode(name string, data string, script string) TestResultItem {
	testItem := TestResultItem{
		Type:     "decode",
		Data:     data,
		Result:   "",
		ErrorMsg: "",
		Name:     name,
		Success:  false,
	}

	decodeApiResult, err := as.PayloadCodecTest(&models.APITestPayloadCodecRequest{
		Data:   data,
		FPort:  1,
		Script: script,
		Type:   "decode",
	})
	if err != nil {
		testItem.ErrorMsg = err.Error()
		testItem.Success = false
		return testItem
	}
	testItem.Result = decodeApiResult
	testItem.Success = true
	return testItem
}

func (p *PayloadCodecTest) saveTestResultToFile(filePath string) {
	jsonData, err := json.Marshal(p.TestResult)
	if err != nil {
		log.Errorf("failed to marshal test result: %v", err)
		return
	}
	p.TestResult = []TestResult{}
	os.WriteFile(filePath, jsonData, 0644)
}

func (p *PayloadCodecTest) compareTestResult(old_host_test_result_file string, new_host_test_result_file string) {
	old_host_test_result, err := os.ReadFile(old_host_test_result_file)
	if err != nil {
		log.Errorf("failed to read old host test result: %v", err)
		return
	}

	old_host_test_result_list := []TestResult{}
	err = json.Unmarshal(old_host_test_result, &old_host_test_result_list)
	if err != nil {
		log.Errorf("failed to unmarshal old host test result: %v", err)
		return
	}

	new_host_test_result, err := os.ReadFile(new_host_test_result_file)
	if err != nil {
		log.Errorf("failed to read new host test result: %v", err)
		return
	}

	new_host_test_result_list := []TestResult{}
	err = json.Unmarshal(new_host_test_result, &new_host_test_result_list)
	if err != nil {
		log.Errorf("failed to unmarshal new host test result: %v", err)
		return
	}

	// 创建旧主机测试结果的映射，方便查找
	oldResultMap := make(map[string]map[string]TestResultItem)
	for _, deviceResult := range old_host_test_result_list {
		deviceMap := make(map[string]TestResultItem)
		for _, item := range deviceResult.TestResultItem {
			// 使用 name_type 作为唯一键
			key := fmt.Sprintf("%s_%s", item.Name, item.Type)
			deviceMap[key] = item
		}
		oldResultMap[deviceResult.Device] = deviceMap
	}

	// 存储差异结果
	var diffResults []map[string]interface{}

	// 遍历新主机测试结果，与旧主机进行比较
	for _, newDeviceResult := range new_host_test_result_list {
		deviceName := newDeviceResult.Device
		oldDeviceMap, exists := oldResultMap[deviceName]

		if !exists {
			// 如果旧主机没有这个设备，记录所有新主机的测试项
			for _, item := range newDeviceResult.TestResultItem {
				diffResults = append(diffResults, map[string]interface{}{
					"device":        deviceName,
					"name":          item.Name,
					"type":          item.Type,
					"data":          item.Data,
					"old_result":    "设备不存在",
					"new_result":    item.Result,
					"old_success":   false,
					"new_success":   item.Success,
					"old_error_msg": "",
					"new_error_msg": item.ErrorMsg,
					"diff_type":     "device_not_exist_in_old",
				})
			}
			continue
		}

		// 比较同一设备下的测试项
		for _, newItem := range newDeviceResult.TestResultItem {
			key := fmt.Sprintf("%s_%s", newItem.Name, newItem.Type)
			oldItem, exists := oldDeviceMap[key]

			if !exists {
				// 如果旧主机没有这个测试项，记录差异
				diffResults = append(diffResults, map[string]interface{}{
					"device":        deviceName,
					"name":          newItem.Name,
					"type":          newItem.Type,
					"data":          newItem.Data,
					"old_result":    "测试项不存在",
					"new_result":    newItem.Result,
					"old_success":   false,
					"new_success":   newItem.Success,
					"old_error_msg": "",
					"new_error_msg": newItem.ErrorMsg,
					"diff_type":     "test_item_not_exist_in_old",
				})
				continue
			}

			// 比较结果和成功状态
			if oldItem.Result != newItem.Result || oldItem.Success != newItem.Success {
				diffResults = append(diffResults, map[string]interface{}{
					"device":        deviceName,
					"name":          newItem.Name,
					"type":          newItem.Type,
					"data":          newItem.Data,
					"old_result":    oldItem.Result,
					"new_result":    newItem.Result,
					"old_success":   oldItem.Success,
					"new_success":   newItem.Success,
					"old_error_msg": oldItem.ErrorMsg,
					"new_error_msg": newItem.ErrorMsg,
					"diff_type":     "result_or_success_different",
				})
			}
		}

		// 检查旧主机中有但新主机中没有的测试项
		for key, oldItem := range oldDeviceMap {
			parts := strings.Split(key, "_")
			if len(parts) < 2 {
				continue
			}
			name := strings.Join(parts[:len(parts)-1], "_")
			testType := parts[len(parts)-1]

			found := false
			for _, newItem := range newDeviceResult.TestResultItem {
				if newItem.Name == name && newItem.Type == testType {
					found = true
					break
				}
			}

			if !found {
				diffResults = append(diffResults, map[string]interface{}{
					"device":        deviceName,
					"name":          name,
					"type":          testType,
					"data":          oldItem.Data,
					"old_result":    oldItem.Result,
					"new_result":    "测试项不存在",
					"old_success":   oldItem.Success,
					"new_success":   false,
					"old_error_msg": oldItem.ErrorMsg,
					"new_error_msg": "",
					"diff_type":     "test_item_not_exist_in_new",
				})
			}
		}
	}

	// 检查新主机中没有但旧主机中有的设备
	for deviceName, oldDeviceMap := range oldResultMap {
		found := false
		for _, newDeviceResult := range new_host_test_result_list {
			if newDeviceResult.Device == deviceName {
				found = true
				break
			}
		}

		if !found {
			// 如果新主机没有这个设备，记录所有旧主机的测试项
			for _, oldItem := range oldDeviceMap {
				parts := strings.Split(fmt.Sprintf("%s_%s", oldItem.Name, oldItem.Type), "_")
				if len(parts) < 2 {
					continue
				}
				name := strings.Join(parts[:len(parts)-1], "_")
				testType := parts[len(parts)-1]

				diffResults = append(diffResults, map[string]interface{}{
					"device":        deviceName,
					"name":          name,
					"type":          testType,
					"data":          oldItem.Data,
					"old_result":    oldItem.Result,
					"new_result":    "设备不存在",
					"old_success":   oldItem.Success,
					"new_success":   false,
					"old_error_msg": oldItem.ErrorMsg,
					"new_error_msg": "",
					"diff_type":     "device_not_exist_in_new",
				})
			}
		}
	}

	// 保存差异结果到文件
	diffFileName := "test_result_diff.json"
	diffData := map[string]interface{}{
		"total_diff_count": len(diffResults),
		"diff_items":       diffResults,
		"summary": map[string]interface{}{
			"old_host_file": old_host_test_result_file,
			"new_host_file": new_host_test_result_file,
			"compare_time":  time.Now().Format("2006-01-02 15:04:05"),
		},
	}

	jsonData, err := json.MarshalIndent(diffData, "", "  ")
	if err != nil {
		log.Errorf("failed to marshal diff result: %v", err)
		return
	}

	err = os.WriteFile(diffFileName, jsonData, 0644)
	if err != nil {
		log.Errorf("failed to write diff result to file: %v", err)
		return
	}

	log.Infof("比较完成，发现 %d 个差异项，结果已保存到 %s", len(diffResults), diffFileName)
}
