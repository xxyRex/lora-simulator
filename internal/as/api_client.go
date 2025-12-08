package as

import (
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"time"

	as_api "github.com/brocaar/lora-simulator/internal/as_api/client"
	"github.com/brocaar/lora-simulator/internal/as_api/client/b_a_cnet_service"
	"github.com/brocaar/lora-simulator/internal/as_api/client/fuota_service"
	"github.com/brocaar/lora-simulator/internal/as_api/client/gateway"
	"github.com/brocaar/lora-simulator/internal/as_api/client/internal_swagger"
	"github.com/brocaar/lora-simulator/internal/as_api/client/modbus_service"
	"github.com/brocaar/lora-simulator/internal/as_api/client/payload_codec"
	"github.com/brocaar/lora-simulator/internal/as_api/client/ursalink_application"
	"github.com/brocaar/lora-simulator/internal/as_api/client/ursalink_device"
	"github.com/brocaar/lora-simulator/internal/as_api/client/ursalink_profiles_service"
	"github.com/brocaar/lora-simulator/internal/as_api/models"
	"github.com/brocaar/lora-simulator/internal/config"
	"github.com/brocaar/lora-simulator/internal/utils"
	"github.com/brocaar/lorawan"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	httptransport "github.com/go-openapi/runtime/client"
	"github.com/go-openapi/strfmt"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"golang.org/x/crypto/ssh"
)

const (
	APPLICATION_NAME    = "test"
	AES_KEY             = "1111111111111111"
	AES_IV              = "2222222222222222"
	SERVICE_PROFILE_ID  = "f6f7d81d-647f-4c7f-8409-3e5218c0c523"
	ORGANIZATION_ID     = "1"
	NETWORK_SERVER_ID   = "1"
	DEVICE_PROFILE_NAME = "simulator_test"
	DEVICE_TIME_OUT     = 1440
)

var mqttClient mqtt.Client
var asClient *as_api.AsAPI
var cgiClient *http.Client

// TransportWithHeaders 是一个自定义的http.RoundTripper，用于为每个请求添加固定的HTTP头
type TransportWithHeaders struct {
	underlying http.RoundTripper
	headers    map[string]string
}

// RoundTrip 实现http.RoundTripper接口，为请求添加默认的HTTP头
func (t *TransportWithHeaders) RoundTrip(req *http.Request) (*http.Response, error) {
	for key, value := range t.headers {
		req.Header.Set(key, value)
	}
	return t.underlying.RoundTrip(req)
}

func sha256Hash(text string) string {
	// 创建一个 SHA-256 的哈希对象
	sha256Hash := sha256.New()

	// 更新哈希对象的输入内容
	_, _ = io.WriteString(sha256Hash, text)

	// 获取十六进制表示的哈希值
	hashedText := hex.EncodeToString(sha256Hash.Sum(nil))

	return hashedText
}

func parseJSON(data []byte) (map[string]interface{}, error) {
	var jsonObj map[string]interface{}

	err := json.Unmarshal(data, &jsonObj)
	if err != nil {
		return nil, err
	}

	return jsonObj, nil
}

func SimpleSetup(host string, username string, password string) error {
	aesKey := []byte(AES_KEY)
	aesIv := []byte(AES_IV)

	aesPassword, err := utils.AesCBCEncrypt([]byte(password), aesKey, aesIv)
	if err != nil {
		return err
	}

	asPassword := aesPassword
	if config.C.LoraSimulator.API.UseOldAuth {
		asPassword = "NicJjG18XOV3U1efQyo8AQ=="
	}

	err = ASLogin(host, username, asPassword)
	if err != nil {
		log.Error(err)
		return err
	}

	err = CGILogin(host, username, aesPassword)
	if err != nil {
		log.Error(err)
		return err
	}

	return nil
}

func Setup(c config.Config) error {
	conf := c.LoraSimulator

	log.WithFields(log.Fields{
		"server":   conf.API.Server,
		"insecure": conf.API.Insecure,
	}).Info("as: connecting api client")

	username := config.C.LoraSimulator.API.Username
	password := config.C.LoraSimulator.API.Password

	aesKey := []byte(AES_KEY)
	aesIv := []byte(AES_IV)

	aesPassword, err := utils.AesCBCEncrypt([]byte(password), aesKey, aesIv)
	if err != nil {
		log.Error("AesCBCEncrypt error: ", err)
		return err
	}

	asPassword := aesPassword
	if config.C.LoraSimulator.API.IsLNS {
		asPassword = sha256Hash(password)
	}
	if config.C.LoraSimulator.API.UseOldAuth {
		asPassword = "NicJjG18XOV3U1efQyo8AQ=="
	}
	err = ASLogin(conf.API.Server, username, asPassword)
	if err != nil {
		log.Error(err)
		return err
	}

	err = CGILogin(conf.API.Server, username, aesPassword)
	if err != nil {
		log.Error(err)
		return err
	}

	// connect MQTT
	opts := mqtt.NewClientOptions()
	opts.AddBroker(conf.Integration.MQTT.Server)
	opts.SetUsername(conf.Integration.MQTT.Username)
	opts.SetPassword(conf.Integration.MQTT.Password)
	opts.SetCleanSession(true)
	opts.SetAutoReconnect(true)

	log.WithFields(log.Fields{
		"server": conf.Integration.MQTT.Server,
	}).Info("as: connecting to mqtt broker")

	mqttClient = mqtt.NewClient(opts)
	if token := mqttClient.Connect(); token.Wait() && token.Error() != nil {
		return errors.Wrap(token.Error(), "mqtt client connect error")
	}

	return nil
}

type CGILoginReq struct {
	ID       string          `json:"id"`
	Execute  int64           `json:"execute"`
	Core     string          `json:"core"`
	Function string          `json:"function"`
	Values   []CGILoginValue `json:"values"`
}

type CGILoginValue struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func CGILogin(host string, username, password string) error {
	insecure := config.C.LoraSimulator.API.Insecure

	// 创建一个支持Cookie管理的CookieJar
	jar, err := cookiejar.New(&cookiejar.Options{})
	if err != nil {
		return err
	}

	// 根据 insecure 配置选择协议和 TLS 配置
	// insecure = true: 使用 HTTP (不使用 TLS)
	// insecure = false: 使用 HTTPS 但跳过证书验证
	var scheme string
	if insecure {
		scheme = "http"
	} else {
		scheme = "https"
	}

	cgiClient = &http.Client{
		Jar: jar,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: !insecure, // 当使用 HTTPS 时跳过证书验证
			},
		},
	}
	cgiURL := scheme + "://" + config.C.LoraSimulator.API.Server + "/cgi"

	data := CGILoginReq{
		ID:       "1",
		Execute:  1,
		Core:     "user",
		Function: "login",
		Values: []CGILoginValue{
			{
				Username: username,
				Password: password,
			},
		},
	}

	requestJSON, err := json.Marshal(data)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", cgiURL, strings.NewReader(string(requestJSON)))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "*/*")

	resp, err := cgiClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	log.Infof("CGI login resp: %v", resp)

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("CGI login failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	jsonObj, err := parseJSON(bodyBytes)
	if err != nil {
		return err
	}

	log.WithFields(log.Fields{
		"response": jsonObj,
	}).Info("CGI login response")

	CheckSavedCookies()

	return nil
}

func ASLogin(host string, username string, password string) error {
	insecure := config.C.LoraSimulator.API.Insecure
	cfg := as_api.DefaultTransportConfig().WithHost(host)

	// 根据 insecure 配置选择协议
	// insecure = true: 使用 HTTP (不使用 TLS)
	// insecure = false: 使用 HTTPS 但跳过证书验证
	if insecure {
		cfg.Schemes = []string{"http"}
	} else {
		cfg.Schemes = []string{"https"}
	}

	// 创建自定义 HTTP 客户端，支持跳过 TLS 证书验证
	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: !insecure, // 当使用 HTTPS 时跳过证书验证
			},
		},
	}

	// 创建 transport 并设置自定义 HTTP 客户端
	transport := httptransport.NewWithClient(cfg.Host, cfg.BasePath, cfg.Schemes, httpClient)
	asClient = as_api.New(transport, strfmt.Default)

	params := internal_swagger.NewPostAPIInternalLoginParams()
	params.Body = &models.APILoginRequest{
		Username: username,
		Password: password,
	}

	resp, err := asClient.InternalSwagger.PostAPIInternalLogin(params)
	if err != nil {
		return err
	}

	// 使用相同的 HTTP 客户端创建带认证的 transport
	transport = httptransport.NewWithClient(cfg.Host, cfg.BasePath, cfg.Schemes, httpClient)
	transport.DefaultAuthentication = httptransport.BearerToken(resp.Payload.Jwt)
	asClient.SetTransport(transport)

	return nil
}

func CreateApplication() (string, error) {
	params := ursalink_application.NewPostAPIUrapplicationsParams()
	params.Body = &models.APIURCreateApplicationRequest{
		OrganizationID:   ORGANIZATION_ID,
		ServiceProfileID: SERVICE_PROFILE_ID,
		Name:             APPLICATION_NAME,
	}

	resp, err := asClient.UrsalinkApplication.PostAPIUrapplications(params)
	if err != nil {
		return "", err
	}

	return resp.Payload.ID, nil
}

func DeleteApplication(applicationID string) error {
	params := ursalink_application.NewDeleteAPIUrapplicationsByIDParams()
	params.ID = applicationID

	_, err := asClient.UrsalinkApplication.DeleteAPIUrapplicationsByID(params)
	if err != nil {
		return err
	}

	return nil
}

func CreateGateway(id string, name string) error {
	params := gateway.NewPostAPIGatewaysParams()
	params.Body = &models.APICreateGatewayRequest{
		Mac:              id,
		Name:             name,
		Latitude:         0,
		Longitude:        0,
		Altitude:         0,
		Description:      "test",
		OrganizationID:   ORGANIZATION_ID,
		Ping:             false,
		NetworkServerID:  NETWORK_SERVER_ID,
		GatewayProfileID: "",
	}

	_, err := asClient.Gateway.PostAPIGateways(params)
	if err != nil {
		return err
	}

	return nil
}

func DeleteGateway(id string) error {
	params := gateway.NewDeleteAPIGatewaysByMacParams()
	params.Mac = id

	_, err := asClient.Gateway.DeleteAPIGatewaysByMac(params)
	if err != nil {
		return err
	}

	return nil
}

func CreateDeviceProfile() (string, error) {
	params := ursalink_profiles_service.NewPostAPIUrprofilesParams()
	params.Body = &models.APICreateProfileRequest{
		Name:           DEVICE_PROFILE_NAME,
		OrganizationID: ORGANIZATION_ID,
		Profile: &models.APIProfile{
			FactoryPresetFreqs:   []int64{},
			MacVersion:           "1.0.2",
			MaxEIRP:              0,
			RegParamsRevision:    "B",
			RxDROffset1:          0,
			RxDataRate2:          0,
			RxFreq2:              869525000,
			Supports32bitFCnt:    true,
			SupportsClassB:       false,
			SupportsClassC:       false,
			SupportsJoin:         true,
			PingSlotPeriod:       128,
			PingSlotDR:           3,
			PingSlotFreq:         869525000,
			ClassBTimeout:        10,
			ClassCTimeout:        10,
			EnableUplinkChannels: []int64{},
		},
	}

	resp, err := asClient.UrsalinkProfilesService.PostAPIUrprofiles(params)
	if err != nil {
		return "", err
	}

	return resp.Payload.ProfileID, nil
}

func DeleteDeviceProfile(profileID string) error {
	params := ursalink_profiles_service.NewDeleteAPIUrprofilesByProfileIDParams()
	params.ProfileID = profileID

	_, err := asClient.UrsalinkProfilesService.DeleteAPIUrprofilesByProfileID(params)
	if err != nil {
		return err
	}

	return nil
}

func CreateDevices(eui, name, profileId, appKey, payloadCodecID, applicationID string) error {
	params := ursalink_device.NewPostAPIUrdevicesParams()
	params.Body = &models.APIUrCreateDeviceRequest{
		DevEUI:         eui,
		Name:           name,
		Description:    eui,
		ProfileID:      profileId,
		PayloadCodecID: payloadCodecID,
		FPort:          1,
		AppKey:         appKey,
		SkipFCntCheck:  true,
		DevAddr:        "",
		AppSKey:        "",
		NwkSKey:        "",
		FCntUp:         0,
		FCntDown:       0,
		ApplicationID:  applicationID,
		Timeout:        DEVICE_TIME_OUT,
	}

	_, err := asClient.UrsalinkDevice.PostAPIUrdevices(params)
	if err != nil {
		return err
	}

	return nil
}

func DeleteDevices(eui string) error {
	params := ursalink_device.NewDeleteAPIUrdevicesByDevEUIParams()
	params.DevEUI = eui

	_, err := asClient.UrsalinkDevice.DeleteAPIUrdevicesByDevEUI(params)
	if err != nil {
		return err
	}

	return nil
}

func GetDevices(offset int, limit int) ([]*models.APIDeviceItem, error) {
	params := ursalink_device.NewGetAPIUrdevicesParams()
	offsetStr := strconv.Itoa(offset)
	limitStr := strconv.Itoa(limit)
	searchStr := ""
	organizationIDStr := ORGANIZATION_ID

	params.Offset = &offsetStr
	params.Limit = &limitStr
	params.OrganizationID = &organizationIDStr
	params.Search = &searchStr

	resp, err := asClient.UrsalinkDevice.GetAPIUrdevices(params)
	if err != nil {
		return nil, err
	}

	return resp.Payload.DeviceResult, nil
}

func GetPayloadCoedc() ([]*models.APIPayloadCodecItem, error) {
	var allResults []*models.APIPayloadCodecItem
	limit := 10 // 减少单次请求的数据量
	offset := 0

	for {
		params := payload_codec.NewGetAPIPayloadcodecsParams()
		limitStr := strconv.Itoa(limit)
		offsetStr := strconv.Itoa(offset)
		searchStr := ""
		typeStr := "default"

		params.Limit = &limitStr
		params.Offset = &offsetStr
		params.Search = &searchStr
		params.Type = &typeStr

		resp, err := asClient.PayloadCodec.GetAPIPayloadcodecs(params)
		if err != nil {
			log.Error("GetPayloadCoedc err: ", err)
			return nil, err
		}

		// 如果没有数据返回，说明已经获取完所有数据
		if resp.Payload == nil || len(resp.Payload.Result) == 0 {
			break
		}

		// 将当前页的结果添加到总结果中
		allResults = append(allResults, resp.Payload.Result...)

		// 如果返回的数据量小于limit，说明已经是最后一页
		if len(resp.Payload.Result) < limit {
			break
		}

		// 准备下一页请求
		offset += limit
	}

	return allResults, nil
}

func GetGateway() ([]lorawan.EUI64, error) {
	params := gateway.NewGetAPIGatewaysParams()
	searchStr := ""
	organizationIDStr := ORGANIZATION_ID

	int32Max := int32(math.MaxInt32)
	offset := int32(0)
	params.Limit = &int32Max
	params.Offset = &offset
	params.Search = &searchStr
	params.OrganizationID = &organizationIDStr

	resp, err := asClient.Gateway.GetAPIGateways(params)
	if err != nil {
		return nil, err
	}

	res := []lorawan.EUI64{}
	for _, g := range resp.Payload.Result {
		gatewayEUI := lorawan.EUI64{}
		err := gatewayEUI.UnmarshalText([]byte(g.Mac))
		if err != nil {
			return nil, err
		}

		res = append(res, gatewayEUI)
	}

	return res, nil
}

func GetProfiles() ([]*models.APIProfileData, error) {
	params := ursalink_profiles_service.NewGetAPIUrprofilesParams()
	limitStr := strconv.Itoa(math.MaxInt16)
	offsetStr := strconv.Itoa(0)
	organizationIDStr := ORGANIZATION_ID

	params.Limit = &limitStr
	params.Offset = &offsetStr
	params.OrganizationID = &organizationIDStr

	resp, err := asClient.UrsalinkProfilesService.GetAPIUrprofiles(params)
	if err != nil {
		return nil, err
	}

	return resp.Payload.Result, nil
}

func GetApplications() ([]*models.APIAppListItem, error) {
	params := ursalink_application.NewGetAPIUrapplicationsParams()
	limitStr := strconv.Itoa(math.MaxInt16)
	offsetStr := strconv.Itoa(0)
	organizationIDStr := ORGANIZATION_ID

	params.Limit = &limitStr
	params.Offset = &offsetStr
	params.OrganizationID = &organizationIDStr

	resp, err := asClient.UrsalinkApplication.GetAPIUrapplications(params)
	if err != nil {
		return nil, err
	}

	return resp.Payload.Result, nil
}

func RestartAppServer() error {
	server := strings.Split(config.C.LoraSimulator.API.Server, ":")[0] // 移除端口号（如果有）

	sshConfig := &ssh.ClientConfig{
		User: "root",
		Auth: []ssh.AuthMethod{
			ssh.Password(config.C.LoraSimulator.API.SshPassword), // 无密码，根据注释
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	// 连接到SSH服务器
	client, err := ssh.Dial("tcp", server+":22", sshConfig)
	if err != nil {
		return errors.Wrap(err, "ssh连接失败")
	}
	defer client.Close()

	// 创建会话
	session, err := client.NewSession()
	if err != nil {
		return errors.Wrap(err, "创建ssh会话失败")
	}
	defer session.Close()

	// 执行命令
	cmd := "/etc/init.d/lora_app_server restart"
	output, err := session.CombinedOutput(cmd)
	if err != nil {
		return errors.Wrap(err, fmt.Sprintf("执行命令失败: %s", string(output)))
	}

	log.Info("应用服务器重启中...等待5秒")
	time.Sleep(5 * time.Second)

	log.WithFields(log.Fields{
		"server": server,
		"output": string(output),
	}).Info("应用服务器重启成功")

	return nil
}

func DeleteAllDevices() error {
	params := ursalink_device.NewDeleteAPIUrdevicesallParams()

	_, err := asClient.UrsalinkDevice.DeleteAPIUrdevicesall(params)
	if err != nil {
		return err
	}

	return nil
}

func GetAvailableBACnetObjects(search string, order string, offset int, limit int) (*models.APIGetDevicesPCOsResponse, error) {
	params := b_a_cnet_service.NewPostAPIBacnetGetAllParams()

	params.Body = &models.APIGetDevicesPCOsRequest{
		Search: search,
		Order:  order,
		Offset: int32(offset),
		Limit:  int32(limit),
	}
	resp, err := asClient.BaCnetService.PostAPIBacnetGetAll(params)
	if err != nil {
		return nil, err
	}

	return resp.Payload, nil
}

func AddBACnetObjects(data []*models.APIBACnetDevice) error {
	apiAddDevicePCOs := []*models.APIAddDevicePCO{}
	for _, d := range data {
		apiAddDevicePCOs = append(apiAddDevicePCOs, &models.APIAddDevicePCO{
			DevEui:  d.DevEui,
			Name:    d.Name,
			Objects: d.Objects,
		})
	}

	params := b_a_cnet_service.NewPostAPIBacnetAddParams()
	params.Body = &models.APIAddDevicePCOsRequest{
		Data: apiAddDevicePCOs,
	}

	resp, err := asClient.BaCnetService.PostAPIBacnetAdd(params)
	if err != nil {
		return err
	}

	log.Infof("resp.Payload: %+v", resp.Payload)

	return nil
}

func CreateFuotaTask(fuotaTask *models.APIFuotaTask) error {
	params := fuota_service.NewPostAPIFuotaTaskParams()
	params.Body = &models.APICreateFuotaTaskRequest{
		FuotaTask: fuotaTask,
	}

	_, err := asClient.FuotaService.PostAPIFuotaTask(params)
	if err != nil {
		return err
	}

	return nil
}

func GetFuotaTask(search string, order string, offset int, limit int) (*models.APIListFuotaTasksResponse, error) {
	params := fuota_service.NewGetAPIFuotaTaskParams()
	offsetInt32 := int32(offset)
	limitInt32 := int32(limit)
	params.Search = &search
	params.Offset = &offsetInt32
	params.Limit = &limitInt32

	resp, err := asClient.FuotaService.GetAPIFuotaTask(params)
	if err != nil {
		return nil, err
	}

	return resp.Payload, nil
}

type DeleteFuotaTaskReq struct {
	IDS []int64 `json:"ids"`
}

func DeleteFuotaTask(ids []int32) error {
	params := fuota_service.NewPostAPIFuotaTaskDeleteParams()
	params.Body = &models.APIDeleteFuotaTaskRequest{
		Ids: ids,
	}

	_, err := asClient.FuotaService.PostAPIFuotaTaskDelete(params)
	if err != nil {
		return err
	}

	return nil
}

func CreateModbusServer(data *models.APIModbusServer) error {
	params := modbus_service.NewPostAPIProtocolModbusServerParams()
	params.Body = data

	_, err := asClient.ModbusService.PostAPIProtocolModbusServer(params)
	if err != nil {
		return err
	}

	return nil
}

func GetModbusServer(limit int, offset int, search string) ([]*models.APIModbusServer, error) {
	params := modbus_service.NewGetAPIProtocolModbusServerParams()
	limitInt32 := int32(limit)
	offsetInt32 := int32(offset)
	params.Limit = &limitInt32
	params.Offset = &offsetInt32
	params.Search = &search

	resp, err := asClient.ModbusService.GetAPIProtocolModbusServer(params)
	if err != nil {
		return nil, err
	}

	return resp.Payload.Servers, nil
}

func DeleteModbusServer(id string) error {
	params := modbus_service.NewDeleteAPIProtocolModbusServerParams()
	params.ID = &id

	_, err := asClient.ModbusService.DeleteAPIProtocolModbusServer(params)
	if err != nil {
		return err
	}

	return nil
}

func CheckSavedCookies() {
	if cgiClient == nil || cgiClient.Jar == nil {
		log.Error("HTTP客户端或Cookie管理器未初始化")
		return
	}

	// 根据 insecure 配置选择协议
	insecure := config.C.LoraSimulator.API.Insecure
	var scheme string
	if insecure {
		scheme = "http"
	} else {
		scheme = "https"
	}

	serverURL, err := url.Parse(scheme + "://" + config.C.LoraSimulator.API.Server)
	if err != nil {
		log.Errorf("解析服务器URL失败: %v", err)
		return
	}

	cookies := cgiClient.Jar.Cookies(serverURL)

	if len(cookies) == 0 {
		log.Info("HTTP客户端中没有存储任何Cookie")
	} else {
		log.Infof("HTTP客户端中存储了 %d 个Cookie:", len(cookies))
		for i, cookie := range cookies {
			log.Infof("Cookie %d: %s=%s, Domain=%s, Path=%s",
				i+1, cookie.Name, cookie.Value, cookie.Domain, cookie.Path)
		}
	}
}

type GetAllAvaliableModbusObjectsRes struct {
	Total int64         `json:"total"`
	Data  []ModbusDatum `json:"data"`
}

type ModbusDatum struct {
	ID         string         `json:"id"`
	DeviceName string         `json:"device_name"`
	DevEui     string         `json:"dev_eui"`
	IDS        []interface{}  `json:"ids"`
	Objects    []ModbusObject `json:"objects"`
}

type ModbusObject struct {
	ID                   string             `json:"id"`
	PayloadCodecObjectID int64              `json:"payload_codec_object_id"`
	Name                 string             `json:"name"`
	LoraName             string             `json:"lora_name"`
	RegisterAddr         int64              `json:"register_addr"`
	RegisterType         ModbusRegisterType `json:"register_type"`
	RegisterNum          int64              `json:"register_num"`
	DataType             ModbusDataType     `json:"data_type"`
	Unit                 ModbusUnit         `json:"unit"`
	Description          string             `json:"description"`
	Value                string             `json:"value"`
	UpdateTime           string             `json:"update_time"`
	Reference            []string           `json:"reference"`
}

type ModbusDataType string

const (
	Flag        ModbusDataType = "flag"
	Float32Dcba ModbusDataType = "float32_dcba"
	Int16Ba     ModbusDataType = "int16_ba"
	String      ModbusDataType = "string"
	Uint16Ba    ModbusDataType = "uint16_ba"
)

type ModbusRegisterType string

const (
	Coil            ModbusRegisterType = "coil"
	Discrete        ModbusRegisterType = "discrete"
	HoldingRegister ModbusRegisterType = "holding_register"
	InputRegister   ModbusRegisterType = "input_register"
)

type ModbusUnit string

const (
	C      ModbusUnit = "°C"
	Empty  ModbusUnit = ""
	Min    ModbusUnit = "min"
	Minute ModbusUnit = "minute"
	RH     ModbusUnit = "%r.h."
	S      ModbusUnit = "s"
)

type GetAllAvaliableModbusObjectsDataReq struct {
	Limit    int64  `json:"limit"`
	Offset   int64  `json:"offset"`
	Search   string `json:"search"`
	ServerID string `json:"server_id"`
	Order    string `json:"order"`
	FetchAll int64  `json:"fetch_all"`
}

func GetAllAvaliableModbusObjects(data *models.APIGetModbusObjectRequest) (*models.APIGetModbusObjectResponse, error) {
	params := modbus_service.NewPostAPIProtocolModbusObjectGetallParams()
	params.Body = data

	resp, err := asClient.ModbusService.PostAPIProtocolModbusObjectGetall(params)
	if err != nil {
		return nil, err
	}

	return resp.Payload, nil
}

type AddModbusDatumReq struct {
	ServerID string        `json:"server_id"`
	Data     []ModbusDatum `json:"data"`
}

func AddModbusDatum(body *models.APIAddModbusObjectRequest) error {
	params := modbus_service.NewPostAPIProtocolModbusObjectAddParams()
	params.Body = body

	_, err := asClient.ModbusService.PostAPIProtocolModbusObjectAdd(params)
	if err != nil {
		return err
	}

	log.Info("add modbus datum: ", body)

	return nil
}

func PayloadCodecTest(data *models.APITestPayloadCodecRequest) (string, error) {
	params := payload_codec.NewPostAPIPayloadcodecsTestParams()
	params.Body = data

	resp, err := asClient.PayloadCodec.PostAPIPayloadcodecsTest(params)
	if err != nil {
		return "", err
	}

	log.Infof("payload codec test resp: %v", resp)

	return resp.Payload.Result, nil
}

func ExportBulkDevice() (string, error) {
	params := ursalink_device.NewGetAPIUrdevicesallExportParams()

	resp, err := asClient.UrsalinkDevice.GetAPIUrdevicesallExport(params)
	if err != nil {
		return "", err
	}

	// base64 decode
	csv, err := base64.StdEncoding.DecodeString(resp.Payload.Csv)
	if err != nil {
		return "", err
	}

	return string(csv), nil
}

func GetPayloadCodecList(typeStr string) ([]*models.APIShortPayloadCodecItem, error) {
	params := payload_codec.NewGetAPIPayloadcodecsShortParams()
	params.Type = &typeStr

	resp, err := asClient.PayloadCodec.GetAPIPayloadcodecsShort(params)
	if err != nil {
		return nil, err
	}

	return resp.Payload.Result, nil
}

func GetPayloadCodecByID(id string) (*models.APIGetPayloadCodecResponse, error) {
	params := payload_codec.NewGetAPIPayloadcodecsByIDParams()
	params.ID = id

	resp, err := asClient.PayloadCodec.GetAPIPayloadcodecsByID(params)
	if err != nil {
		return nil, err
	}

	return resp.Payload, nil
}
