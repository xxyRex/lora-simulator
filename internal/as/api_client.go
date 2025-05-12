package as

import (
	"crypto/sha256"
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

func post(url, data string) ([]byte, error) {
	url = "http://" + config.C.LoraSimulator.API.Server + url

	var response []byte

	req, err := http.NewRequest("POST", url, strings.NewReader(data))
	if err != nil {
		return response, err
	}

	resp, err := cgiClient.Do(req)
	if err != nil {
		return response, err
	}

	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return bodyBytes, nil
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
	err = ASLogin(username, asPassword)
	if err != nil {
		log.Error(err)
		return err
	}

	err = CGILogin(username, aesPassword)
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

func CGILogin(username, password string) error {
	// 创建一个支持Cookie管理的CookieJar
	jar, err := cookiejar.New(&cookiejar.Options{})
	if err != nil {
		return err
	}

	cgiClient = &http.Client{
		Jar: jar,
	}
	url := "http://" + config.C.LoraSimulator.API.Server + "/cgi"

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

	req, err := http.NewRequest("POST", url, strings.NewReader(string(requestJSON)))
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

func ASLogin(username string, password string) error {
	cfg := as_api.DefaultTransportConfig().WithHost(config.C.LoraSimulator.API.Server)
	cfg.Schemes = []string{"http"} // 强制使用 HTTP
	asClient = as_api.NewHTTPClientWithConfig(strfmt.Default, cfg)

	params := internal_swagger.NewPostAPIInternalLoginParams()
	params.Body = &models.APILoginRequest{
		Username: username,
		Password: password,
	}

	resp, err := asClient.InternalSwagger.PostAPIInternalLogin(params)
	if err != nil {
		return err
	}

	transport := httptransport.New(cfg.Host, cfg.BasePath, cfg.Schemes)
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
	params := payload_codec.NewGetAPIPayloadcodecsParams()
	limitStr := strconv.Itoa(math.MaxInt16)
	offsetStr := strconv.Itoa(0)
	searchStr := ""

	params.Limit = &limitStr
	params.Offset = &offsetStr
	params.Search = &searchStr

	resp, err := asClient.PayloadCodec.GetAPIPayloadcodecs(params)
	if err != nil {
		return nil, err
	}

	return resp.Payload.Result, nil
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

type ModbusServerCreateReq struct {
	ID       int64                        `json:"id"`
	Execute  int64                        `json:"execute"`
	Core     string                       `json:"core"`
	Function string                       `json:"function"`
	Values   []ModbusServerCreateReqValue `json:"values"`
}

type ModbusServerCreateReqValue struct {
	Base    string                        `json:"base"`
	Servers []ModbusServerCreateReqServer `json:"servers"`
}

type ModbusServerCreateReqServer struct {
	Enable      int64  `json:"enable"`
	Interface   string `json:"interface"`
	ConnectType string `json:"connect_type"`
	Name        string `json:"name"`
	Port        int64  `json:"port"`
	SlaveID     int64  `json:"slave_id"`
	Description string `json:"description"`
}

func CreateModbusServer(data ModbusServerCreateReq) error {
	url := "/cgi"

	requestJSON, err := json.Marshal(data)
	if err != nil {
		return err
	}

	bytes, err := post(url, string(requestJSON))
	if err != nil {
		return err
	}

	ret := string(bytes)
	log.Info("create modbus server: ", ret)

	return nil
}

type ModbusGetServerReq struct {
	ID       int64                     `json:"id"`
	Execute  int64                     `json:"execute"`
	Core     string                    `json:"core"`
	Function string                    `json:"function"`
	Values   []ModbusGetServerReqValue `json:"values"`
}

type ModbusGetServerReqValue struct {
	Base   string `json:"base"`
	Search string `json:"search"`
	Order  string `json:"order"`
	Offset int64  `json:"offset"`
	Limit  int64  `json:"limit"`
}

type ModbusGetServerRes struct {
	ID     int64                      `json:"id"`
	Model  string                     `json:"model"`
	Pn     string                     `json:"pn"`
	OEM    string                     `json:"oem"`
	Rtver  string                     `json:"rtver"`
	Status int64                      `json:"status"`
	Result []ModbusGetServerResResult `json:"result"`
}

type ModbusGetServerResResult struct {
	Total   int64                      `json:"total"`
	Servers []ModbusGetServerResServer `json:"servers"`
}

type ModbusGetServerResServer struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Port        int64  `json:"port"`
	Enable      int64  `json:"enable"`
	SlaveID     int64  `json:"slave_id"`
	Interface   string `json:"interface"`
	ConnectType string `json:"connect_type"`
	Description string `json:"description"`
	Ipaddr      string `json:"ipaddr"`
	ObjectNum   int64  `json:"object_num"`
}

func GetModbusServer(data ModbusGetServerReq) (ModbusGetServerRes, error) {
	url := "/cgi"

	requestJSON, err := json.Marshal(data)
	if err != nil {
		return ModbusGetServerRes{}, err
	}

	bytes, err := post(url, string(requestJSON))
	if err != nil {
		return ModbusGetServerRes{}, err
	}

	var res ModbusGetServerRes
	err = json.Unmarshal(bytes, &res)
	log.Infof("GetModbusServer bytes: %s", string(bytes))
	if err != nil {
		log.Error("failed to unmarshal modbus server: ", err)
		return ModbusGetServerRes{}, nil
	}

	log.Infof("GetModbusServer: %v", res)

	return res, nil
}

type ModbusServerDeleteReq struct {
	ID       int64                        `json:"id"`
	Execute  int64                        `json:"execute"`
	Core     string                       `json:"core"`
	Function string                       `json:"function"`
	Values   []ModbusServerDeleteReqValue `json:"values"`
}

type ModbusServerDeleteReqValue struct {
	Base string   `json:"base"`
	IDS  []string `json:"ids"`
}

func DeleteModbusServer(data ModbusServerDeleteReq) error {
	url := "/cgi"

	requestJSON, err := json.Marshal(data)
	if err != nil {
		return err
	}

	bytes, err := post(url, string(requestJSON))
	if err != nil {
		return err
	}

	ret := string(bytes)
	log.Info("delete modbus server: ", ret)

	return nil
}

func CheckSavedCookies() {
	if cgiClient == nil || cgiClient.Jar == nil {
		log.Error("HTTP客户端或Cookie管理器未初始化")
		return
	}

	serverURL, err := url.Parse("http://" + config.C.LoraSimulator.API.Server)
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
