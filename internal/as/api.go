package as

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"github.com/brocaar/lora-simulator/internal/config"
	"github.com/brocaar/lora-simulator/internal/utils"
	"github.com/brocaar/lorawan"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"golang.org/x/crypto/ssh"
)

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

const (
	APPLICATION_NAME = "test"
)

var mqttClient mqtt.Client
var globalHttpClient *http.Client

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
	url = config.C.ChirpStack.API.Server + url

	var response []byte

	req, err := http.NewRequest("POST", url, strings.NewReader(data))
	if err != nil {
		return response, err
	}

	resp, err := globalHttpClient.Do(req)
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

func get(url, data string) ([]byte, error) {
	url = config.C.ChirpStack.API.Server + url

	var response []byte

	req, err := http.NewRequest("GET", url, strings.NewReader(data))
	if err != nil {
		return response, err
	}

	resp, err := globalHttpClient.Do(req)
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

func delete(url, data string) ([]byte, error) {
	url = config.C.ChirpStack.API.Server + url

	var response []byte

	req, err := http.NewRequest("DELETE", url, strings.NewReader(data))
	if err != nil {
		return response, err
	}

	resp, err := globalHttpClient.Do(req)
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
	conf := c.ChirpStack

	log.WithFields(log.Fields{
		"server":   conf.API.Server,
		"insecure": conf.API.Insecure,
	}).Info("as: connecting api client")

	// 创建一个支持Cookie管理的CookieJar
	jar, err := cookiejar.New(&cookiejar.Options{})
	if err != nil {
		return errors.Wrap(err, "创建cookie jar失败")
	}

	// 使用CookieJar初始化HTTP客户端
	globalHttpClient = &http.Client{
		Jar: jar,
	}

	err = ASLogin()
	if err != nil {
		log.Error(err)
		return err
	}

	err = CGILogin()
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

func CGILogin() error {
	url := config.C.ChirpStack.API.Server + "/cgi"

	key := []byte("1111111111111111")
	iv := []byte("2222222222222222")
	password, err := utils.AesCBCEncrypt([]byte(config.C.ChirpStack.API.Password), key, iv)
	if err != nil {
		log.Error("AesCBCEncrypt error: ", err)
		return err
	}

	data := CGILoginReq{
		ID:       "1",
		Execute:  1,
		Core:     "user",
		Function: "login",
		Values: []CGILoginValue{
			{
				Username: config.C.ChirpStack.API.Username,
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

	resp, err := globalHttpClient.Do(req)
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

func ASLogin() error {
	url := config.C.ChirpStack.API.LoginUrl

	password := ""
	if config.C.ChirpStack.API.IsLNS {
		password = sha256Hash("password")
	} else {
		key := []byte("1111111111111111")
		iv := []byte("2222222222222222")
		ps, err := utils.AesCBCEncrypt([]byte(config.C.ChirpStack.API.Password), key, iv)
		if err != nil {
			log.Error("AesCBCEncrypt error: ", err)
			return err
		}
		password = ps
	}

	if config.C.ChirpStack.API.UseOldAuth {
		password = "NicJjG18XOV3U1efQyo8AQ=="
	}

	data := `{
		"username": "` + config.C.ChirpStack.API.Username + `",
		"password": "` + password + `"
	}`

	// 直接使用http.Request而不是post函数，以确保Cookie能被获取到
	req, err := http.NewRequest("POST", config.C.ChirpStack.API.Server+url, strings.NewReader(data))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "*/*")

	resp, err := globalHttpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// 检查响应状态
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("AS login failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	jsonObj, err := parseJSON(bodyBytes)
	if err != nil {
		return err
	}

	jwt := ""
	if config.C.ChirpStack.API.IsLNS {
		dataMap, ok := jsonObj["data"].(map[string]interface{})
		if !ok {
			return fmt.Errorf("data field type assertion failed")
		}
		j, ok := dataMap["token"].(string)
		if !ok {
			return fmt.Errorf("token field type assertion failed")
		}
		jwt = j
	} else {
		j, ok := jsonObj["jwt"].(string)
		if !ok {
			return fmt.Errorf("token field type assertion failed")
		}
		jwt = j
	}

	// 仍然保持使用JWT认证
	transport := &TransportWithHeaders{
		underlying: http.DefaultTransport,
		headers: map[string]string{
			"Content-Type":  "application/json",
			"Authorization": "Bearer " + jwt,
			"Accept":        "*/*",
		},
	}

	// 保存原始的CookieJar
	jar := globalHttpClient.Jar

	// 设置新的传输层
	globalHttpClient.Transport = transport

	// 恢复CookieJar
	globalHttpClient.Jar = jar

	log.Info("AS登录成功，已获取JWT和Cookie")

	return nil
}

func CreateApplication() (string, error) {
	url := "/lns/api/v1/urapplications"
	data := `
	{
		"organizationID": "1",
		"serviceProfileID": "f6f7d81d-647f-4c7f-8409-3e5218c0c523",
		"name": "` + APPLICATION_NAME + `"
	}
	`

	if !config.C.ChirpStack.API.IsLNS {
		url = "/api/urapplications"
		data = `
		{
			"metadata": true,
			"description": "test",
			"name": "` + APPLICATION_NAME + `",
			"organizationID": "1",
			"serviceProfileID": "f6f7d81d-647f-4c7f-8409-3e5218c0c523"
		}
		`
	}

	bytes, err := post(url, data)
	if err != nil {
		log.Error("CreateApplication failed ", err)
		return "", err
	}

	jsonObj, err := parseJSON(bytes)
	if err != nil {
		return "", err
	}

	id := jsonObj["id"].(string)

	return id, nil
}

func DeleteApplication(applicationID string) error {
	url := "/lns/api/v1/urapplications/" + applicationID
	if !config.C.ChirpStack.API.IsLNS {
		url = "/api/urapplications/" + applicationID
	}

	data := `
	{
		"organizationID": "1",
		"serviceProfileID": "f6f7d81d-647f-4c7f-8409-3e5218c0c523",
		"name": "simulator_test"
	}
	`

	bytes, err := delete(url, data)
	if err != nil {
		return err
	}

	resp := string(bytes)
	if resp != "{}" {
		return errors.New("failed to delete application: " + resp)
	}

	return nil
}

func CreateGateway(id string, name string) error {
	url := "/lns/api/v1/gateways"
	if !config.C.ChirpStack.API.IsLNS {
		url = "/api/gateways"
	}

	data := `
	{
		"mac":"` + id + `",
		"name":"` + name + `",
		"latitude":0,
		"longitude":0,
		"altitude":0,
		"description":"test",
		"organizationID":1,
		"ping":false,
		"networkServerID":1,
		"gatewayProfileID":""
	}
	`

	log.Info(data)

	bytes, err := post(url, data)
	if err != nil {
		return err
	}

	resp := string(bytes)
	if resp != "{}" {
		return errors.Errorf("failed to create gateway")
	}

	return nil
}

func DeleteGateway(id string) error {
	url := "/lns/api/v1/gateways/" + id
	if !config.C.ChirpStack.API.IsLNS {
		url = "/api/gateways/" + id
	}

	bytes, err := delete(url, "")
	if err != nil {
		return err
	}

	resp := string(bytes)
	if resp != "{}" {
		return errors.New("failed to delete gateway " + resp)
	}

	return nil
}

func CreateDeviceProfile() (string, error) {
	url := "/lns/api/v1/urprofiles"
	if !config.C.ChirpStack.API.IsLNS {
		url = "/api/urprofiles"
	}
	data := `
	{
		"name": "simulator_test",
		"organizationID": "1",
		"profile": {
			"factoryPresetFreqs": [],
			"macVersion": "1.0.2",
			"maxEIRP": 0,
			"regParamsRevision": "B",
			"rxDROffset1": 0,
			"rxDataRate2": 0,
			"rxFreq2": 869525000,
			"supports32bitFCnt": true,
			"supportsClassB": false,
			"supportsClassC": false,
			"supportsJoin": true,
			"pingSlotPeriod": 128,
			"pingSlotDR": 3,
			"pingSlotFreq": 869525000,
			"classBTimeout": 10,
			"classCTimeout": 10,
			"enableUplinkChannels": []
		}
	}
	`

	bytes, err := post(url, data)
	if err != nil {
		return "", err
	}

	jsonObj, err := parseJSON(bytes)
	if err != nil {
		return "", err
	}

	profileID, ok := jsonObj["profileID"].(string)
	if !ok {
		log.Error("CreateDeviceProfile failed")
		return "", nil
	}

	return profileID, nil
}

func DeleteDeviceProfile(profileID string) error {
	url := "/lns/api/v1/urprofiles/" + profileID
	if !config.C.ChirpStack.API.IsLNS {
		url = "/api/urprofiles/" + profileID
	}

	bytes, err := delete(url, "")
	if err != nil {
		return err
	}

	ret := string(bytes)
	if ret != `{}` {
		return errors.New("failed to delete device: " + ret)
	}

	return nil
}

func CreateDevices(eui, name, profileId, appKey, payloadCodecID, applicationID string) error {
	url := "/lns/api/v1/urdevices"
	if !config.C.ChirpStack.API.IsLNS {
		url = "/api/urdevices"
	}

	data := `
	{
		"devEUI": "` + eui + `",
		"name": "` + name + `",
		"description": "` + eui + `",
		"profileID": "` + profileId + `",
		"payloadCodecID": "` + payloadCodecID + `",
		"fPort": 1,
		"appKey": "` + appKey + `",
		"skipFCntCheck": true,
		"devAddr": "",
		"appSKey": "",
		"nwkSKey": "",
		"fCntUp": 0,
		"fCntDown": 0,
		"applicationID": "` + applicationID + `"
	}
	`

	bytes, err := post(url, data)
	if err != nil {
		return err
	}

	ret := string(bytes)
	if ret != `{"code":200,"error":""}` {
		return errors.New("failed to create device: " + ret)
	}

	return nil
}

func DeleteDevices(eui string) error {
	url := "/lns/api/v1/urdevices/" + eui
	if !config.C.ChirpStack.API.IsLNS {
		url = "/api/urdevices/" + eui
	}

	bytes, err := delete(url, "")
	if err != nil {
		return err
	}

	ret := string(bytes)
	if ret != `{}` {
		return errors.New("failed to delete device: " + ret)
	}

	return nil
}

type PayloadCodecItem struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	TemplateID    string `json:"templateID"`
	DevEUIPrefix  string `json:"devEUIPrefix"`
	EncoderScript string `json:"encoderScript"`
	DecoderScript string `json:"decoderScript"`
	TestEnabled   bool   `json:"testEnabled"`
	FPort         int    `json:"fPort"`
}

type ListPayloadCodecResponse struct {
	TotalCount string             `json:"totalCount"`
	Result     []PayloadCodecItem `json:"result"`
}

func GetPayloadCoedc() ([]PayloadCodecItem, error) {
	res := []PayloadCodecItem{}

	url := "/lns/api/v1/payloadcodecs/lns?limit=9999&offset=0&type=default"
	if !config.C.ChirpStack.API.IsLNS {
		url = "/api/payloadcodecs?limit=9999&offset=0&type=default"
	}

	bytes, err := get(url, "")
	if err != nil {
		return res, err
	}

	var resp ListPayloadCodecResponse
	err = json.Unmarshal(bytes, &resp)
	if err != nil {
		return res, err
	}

	if len(resp.Result) == 0 {
		log.Error("GetPayloadCoedc failed")
		return res, fmt.Errorf("no payload codec")
	}
	res = resp.Result

	return res, nil
}

type GatewayJson struct {
	MAC             string  `json:"mac"`
	Name            string  `json:"name"`
	Description     string  `json:"description"`
	CreatedAt       string  `json:"createdAt"`
	UpdatedAt       string  `json:"updatedAt"`
	OrganizationID  string  `json:"organizationID"`
	NetworkServerID string  `json:"networkServerID"`
	FirstSeenAt     string  `json:"firstSeenAt"`
	LastSeenAt      string  `json:"lastSeenAt"`
	Latitude        float64 `json:"latitude"`
	Longitude       float64 `json:"longitude"`
	Altitude        float64 `json:"altitude"`
	Connected       bool    `json:"connected"`
}

type GatewayJSONData struct {
	TotalCount int           `json:"totalCount"`
	LocalNS    bool          `json:"localNS"`
	Result     []GatewayJson `json:"result"`
}

func GetGateway() ([]lorawan.EUI64, error) {
	res := []lorawan.EUI64{}

	url := "/lns/api/v1/gateways?limit=9999&offset=0&organizationID=1"
	if !config.C.ChirpStack.API.IsLNS {
		url = "/api/gateways?limit=9999&offset=0&organizationID=1"
	}

	bytes, err := get(url, "")
	if err != nil {
		return res, err
	}

	var gateways GatewayJSONData
	err = json.Unmarshal(bytes, &gateways)
	if err != nil {
		log.Error("GetGateway failed to Unmarshal Json")
		return res, err
	}

	for _, g := range gateways.Result {
		gatewayEUI := lorawan.EUI64{}
		err := gatewayEUI.UnmarshalText([]byte(g.MAC))
		if err != nil {
			log.Error("GetGateway gatewayEUI.UnmarshalText error ", err)

		}

		res = append(res, gatewayEUI)
	}

	return res, nil
}

type ProfileJson struct {
	ProfileID            string `json:"profileID"`
	SupportsClassB       bool   `json:"supportsClassB"`
	ClassBTimeout        int    `json:"classBTimeout"`
	PingSlotPeriod       int    `json:"pingSlotPeriod"`
	PingSlotDR           int    `json:"pingSlotDR"`
	PingSlotFreq         int    `json:"pingSlotFreq"`
	SupportsClassC       bool   `json:"supportsClassC"`
	ClassCTimeout        int    `json:"classCTimeout"`
	MacVersion           string `json:"macVersion"`
	RegParamsRevision    string `json:"regParamsRevision"`
	RxDROffset1          int    `json:"rxDROffset1"`
	RxDataRate2          int    `json:"rxDataRate2"`
	RxFreq2              int    `json:"rxFreq2"`
	FactoryPresetFreqs   []int  `json:"factoryPresetFreqs"`
	MaxEIRP              int    `json:"maxEIRP"`
	MaxDutyCycle         int    `json:"maxDutyCycle"`
	SupportsJoin         bool   `json:"supportsJoin"`
	RfRegion             string `json:"rfRegion"`
	Supports32bitFCnt    bool   `json:"supports32bitFCnt"`
	EnableUplinkChannels []int  `json:"enableUplinkChannels"`
}

type ProfileResultJson struct {
	Profile         ProfileJson `json:"profile"`
	Name            string      `json:"name"`
	OrganizationID  string      `json:"organizationID"`
	NetworkServerID string      `json:"networkServerID"`
	Using           bool        `json:"using"`
	IsDefault       bool        `json:"isDefault"`
}

type ProfileJSONData struct {
	TotalCount  string              `json:"totalCount"`
	Disable     bool                `json:"disable"`
	Result      []ProfileResultJson `json:"result"`
	ChannelPlan string              `json:"channelPlan"`
}

func GetProfiles() ([]ProfileResultJson, error) {
	res := []ProfileResultJson{}

	url := "/lns/api/v1/urprofiles?limit=9999&offset=0&organizationID=1"
	if !config.C.ChirpStack.API.IsLNS {
		url = "/api/urprofiles?limit=9999&offset=0&organizationID=1"
	}

	bytes, err := get(url, "")
	if err != nil {
		return res, err
	}
	var profilesJson ProfileJSONData
	err = json.Unmarshal(bytes, &profilesJson)
	if err != nil {
		log.Error("GetProfiles failed ", err)
		return res, err
	}

	return profilesJson.Result, nil
}

// Device represents the information for a device.
type ApplicationJson struct {
	ID                   string   `json:"id"`
	Name                 string   `json:"name"`
	Description          string   `json:"description"`
	OrganizationID       string   `json:"organizationID"`
	ServiceProfileID     string   `json:"serviceProfileID"`
	PayloadCodec         string   `json:"payloadCodec"`
	PayloadEncoderScript string   `json:"payloadEncoderScript"`
	PayloadDecoderScript string   `json:"payloadDecoderScript"`
	Using                bool     `json:"using"`
	Kinds                []string `json:"kinds"`
}

// JSONData represents the top-level structure of the JSON data.
type ApplicationJSONData struct {
	TotalCount string            `json:"totalCount"`
	Disable    bool              `json:"disable"`
	Result     []ApplicationJson `json:"result"`
}

func GetApplications() ([]ApplicationJson, error) {
	res := []ApplicationJson{}

	url := "/lns/api/v1/urapplications?limit=9999&offset=0&organizationID=1"
	if !config.C.ChirpStack.API.IsLNS {
		url = "/api/urapplications?limit=9999&offset=0&organizationID=1"
	}

	bytes, err := get(url, "")
	if err != nil {
		return res, err
	}
	var apps ApplicationJSONData
	err = json.Unmarshal(bytes, &apps)
	if err != nil {
		log.Error("GetApplications failed ", err)
		return res, err
	}

	return apps.Result, nil
}

func RestartAppServer() error {
	server := strings.TrimPrefix(config.C.ChirpStack.API.Server, "http://")
	server = strings.TrimPrefix(server, "https://")
	server = strings.Split(server, ":")[0] // 移除端口号（如果有）

	sshConfig := &ssh.ClientConfig{
		User: "root",
		Auth: []ssh.AuthMethod{
			ssh.Password(config.C.ChirpStack.API.SshPassword), // 无密码，根据注释
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
	url := "/lns/api/v1/urdevicesall"
	if !config.C.ChirpStack.API.IsLNS {
		url = "/api/urdevicesall"
	}

	bytes, err := delete(url, "")
	if err != nil {
		return err
	}

	ret := string(bytes)
	if ret != `{}` {
		return errors.New("failed to delete all devices: " + ret)
	}

	return nil
}

type AvailableBACnetObjects struct {
	Total   int64         `json:"total"`
	NOCData []interface{} `json:"noc_data"`
	Data    []BACnetDatum `json:"data"`
}

type BACnetDatum struct {
	ID      string         `json:"id"`
	DevEui  string         `json:"dev_eui"`
	Name    string         `json:"name"`
	Type    string         `json:"type"`
	IDS     []interface{}  `json:"ids"`
	Objects []BACnetObject `json:"objects"`
}

type BACnetObject struct {
	ID                   string        `json:"id"`
	DevEui               string        `json:"dev_eui"`
	Name                 string        `json:"name"`
	PayloadCodecObjectID string        `json:"payload_codec_object_id"`
	LoraName             string        `json:"lora_name"`
	LoraUnitTypeID       int64         `json:"lora_unit_type_id"`
	Type                 string        `json:"type"`
	Description          string        `json:"description"`
	UnitTypeID           int64         `json:"unit_type_id"`
	CovEnable            int64         `json:"cov_enable"`
	CovIncrement         string        `json:"cov_increment"`
	Polarity             int64         `json:"polarity"`
	RelinquishDefault    string        `json:"relinquish_default"`
	InactiveText         string        `json:"inactive_text"`
	ActiveText           string        `json:"active_text"`
	NotificationClass    int64         `json:"notification_class"`
	HighLimit            string        `json:"high_limit"`
	LowLimit             string        `json:"low_limit"`
	Deadband             string        `json:"deadband"`
	LimitEnable          int64         `json:"limit_enable"`
	EventEnable          int64         `json:"event_enable"`
	NotifyType           int64         `json:"notify_type"`
	AlarmValue           int64         `json:"alarm_value"`
	AlarmValueArray      []interface{} `json:"alarm_value_array"`
	FaultValueArray      []interface{} `json:"fault_value_array"`
	FeedbackValue        int64         `json:"feedback_value"`
	TimeDelay            int64         `json:"time_delay"`
	Unit                 string        `json:"unit"`
	InstanceID           int64         `json:"instance_id"`
	NumberOfStates       int64         `json:"number_of_states"`
	StateText            []string      `json:"state_text"`
	Values               []Value       `json:"values"`
	Reference            []string      `json:"reference"`
	Count                int64         `json:"count"`
	Units                []interface{} `json:"units"`
	TypeAlias            string        `json:"type_alias"`
	Value                string        `json:"value"`
	UpdatedTimes         int64         `json:"updated_times"`
	UpdatedAt            string        `json:"updated_at"`
}

type Value struct {
	Name  string `json:"name"`
	Value int64  `json:"value"`
}

func GetAvailableBACnetObjects(search string, order string, offset int, limit int) (AvailableBACnetObjects, error) {
	url := "/lns/api/v1/bacnet/getAll"
	if !config.C.ChirpStack.API.IsLNS {
		url = "/api/bacnet/getAll"
	}

	data := fmt.Sprintf(`{"search":"%s","order":"%s","offset":%d,"limit":%d}`, search, order, offset, limit)

	var res AvailableBACnetObjects

	bytes, err := post(url, data)
	if err != nil {
		return res, err
	}

	err = json.Unmarshal(bytes, &res)
	if err != nil {
		return res, err
	}

	return res, nil
}

type AddBACnetObjectsRequest struct {
	Base        string        `json:"base"`
	BACnetDatum []BACnetDatum `json:"data"`
}

func AddBACnetObjects(data []BACnetDatum) error {
	url := "/lns/api/v1/bacnet/add"
	if !config.C.ChirpStack.API.IsLNS {
		url = "/api/bacnet/add"
	}

	request := AddBACnetObjectsRequest{
		Base:        "object",
		BACnetDatum: data,
	}

	requestJSON, err := json.Marshal(request)
	if err != nil {
		return err
	}

	bytes, err := post(url, string(requestJSON))
	if err != nil {
		return err
	}

	ret := string(bytes)
	if ret != `{"error":"","code":0}` {
		return errors.New("failed to add BACnet objects: " + ret)
	}

	return nil
}

type FuotaTaskReq struct {
	FuotaTask FuotaTask `json:"fuota_task"`
}

type FuotaTask struct {
	Name               string       `json:"name"`
	StartedAt          time.Time    `json:"startedAt"`
	Description        string       `json:"description"`
	Deveui             []string     `json:"deveui"`
	FirmwareInfo       FirmwareInfo `json:"firmwareInfo"`
	IsOfficialFirmware bool         `json:"isOfficialFirmware"`
	FragmentInfo       FragmentInfo `json:"fragmentInfo"`
	TmpMcInfo          TmpMcInfo    `json:"tmpMcInfo"`
}

type FirmwareInfo struct {
	FileContent  string `json:"fileContent"`
	FirmwareName string `json:"firmwareName"`
}

type FragmentInfo struct {
	FragmentSize       int64 `json:"fragmentSize"`
	FragmentInterval   int64 `json:"fragmentInterval"`
	FragmentRedundancy int64 `json:"fragmentRedundancy"`
}

type TmpMcInfo struct {
	DR        int64 `json:"dr"`
	Frequency int64 `json:"frequency"`
}

func CreateFuotaTask(fuotaTaskReq FuotaTaskReq) error {
	url := "/lns/api/v1/fuota/task"
	if !config.C.ChirpStack.API.IsLNS {
		url = "/api/fuota/task"
	}

	requestJSON, err := json.Marshal(fuotaTaskReq)
	if err != nil {
		return err
	}

	bytes, err := post(url, string(requestJSON))
	if err != nil {
		return err
	}

	ret := string(bytes)
	if !strings.Contains(ret, "id") {
		return errors.New("failed to create fuota task: " + ret)
	}

	return nil
}

type FuotaTaskRes struct {
	Total int64  `json:"total"`
	Tasks []Task `json:"tasks"`
}

type Task struct {
	ID                   int64        `json:"id"`
	Name                 string       `json:"name"`
	Description          string       `json:"description"`
	Status               int64        `json:"status"`
	StartedAt            time.Time    `json:"startedAt"`
	StoppedAt            string       `json:"stoppedAt"`
	CreatedAt            time.Time    `json:"createdAt"`
	UpdatedAt            time.Time    `json:"updatedAt"`
	FirmwareName         string       `json:"firmwareName"`
	DoneDevices          int64        `json:"doneDevices"`
	TotalDevices         int64        `json:"totalDevices"`
	Deveui               []string     `json:"deveui"`
	FirmwareInfo         FirmwareInfo `json:"firmwareInfo"`
	FragmentInfo         FragmentInfo `json:"fragmentInfo"`
	TmpMcInfo            TmpMcInfo    `json:"tmpMcInfo"`
	OfficialFirmwareInfo interface{}  `json:"officialFirmwareInfo"`
	IsOfficialFirmware   bool         `json:"isOfficialFirmware"`
}

type FirmwareInfoRes struct {
	FirmwareName           string `json:"firmwareName"`
	Description            string `json:"description"`
	FileContent            string `json:"fileContent"`
	ProductModel           string `json:"productModel"`
	FirmwareVersion        string `json:"firmwareVersion"`
	SupportHardwareVersion string `json:"supportHardwareVersion"`
	SupportFirmwareVersion string `json:"supportFirmwareVersion"`
	OfficialFirmwareURL    string `json:"officialFirmwareUrl"`
}

type FragmentInfoRes struct {
	FragmentSize       int64 `json:"fragmentSize"`
	FragmentInterval   int64 `json:"fragmentInterval"`
	FragmentRedundancy int64 `json:"fragmentRedundancy"`
}

type TmpMcInfoRes struct {
	SessionTimeDelay int64 `json:"sessionTimeDelay"`
	SessionTimeOut   int64 `json:"sessionTimeOut"`
	Status           int64 `json:"status"`
	GroupType        int64 `json:"groupType"`
	DR               int64 `json:"dr"`
	Frequency        int64 `json:"frequency"`
}

func GetFuotaTask(search string, order string, offset int, limit int) (FuotaTaskRes, error) {
	url := "/lns/api/v1/fuota/task"
	if !config.C.ChirpStack.API.IsLNS {
		url = "/api/fuota/task"
	}

	urlWithParams := fmt.Sprintf("%s?search=%s&order=%s&offset=%d&limit=%d", url, search, order, offset, limit)

	var res FuotaTaskRes

	bytes, err := get(urlWithParams, "")
	if err != nil {
		return res, err
	}

	err = json.Unmarshal(bytes, &res)
	if err != nil {
		return res, err
	}

	return res, nil
}

type DeleteFuotaTaskReq struct {
	IDS []int64 `json:"ids"`
}

func DeleteFuotaTask(ids []int64) error {
	url := "/lns/api/v1/fuota/task/delete"
	if !config.C.ChirpStack.API.IsLNS {
		url = "/api/fuota/task/delete"
	}

	requestJSON, err := json.Marshal(DeleteFuotaTaskReq{IDS: ids})
	if err != nil {
		return err
	}

	bytes, err := post(url, string(requestJSON))
	if err != nil {
		return err
	}

	ret := string(bytes)
	if ret != `{}` {
		return errors.New("failed to delete fuota task: " + ret)
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
	url := "/lns/cgi"
	if !config.C.ChirpStack.API.IsLNS {
		url = "/cgi"
	}

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
	url := "/lns/cgi"
	if !config.C.ChirpStack.API.IsLNS {
		url = "/cgi"
	}

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
	url := "/lns/cgi"
	if !config.C.ChirpStack.API.IsLNS {
		url = "/cgi"
	}

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
	if globalHttpClient == nil || globalHttpClient.Jar == nil {
		log.Error("HTTP客户端或Cookie管理器未初始化")
		return
	}

	serverURL, err := url.Parse(config.C.ChirpStack.API.Server)
	if err != nil {
		log.Errorf("解析服务器URL失败: %v", err)
		return
	}

	cookies := globalHttpClient.Jar.Cookies(serverURL)

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

func GetAllAvaliableModbusObjects(data GetAllAvaliableModbusObjectsDataReq) (GetAllAvaliableModbusObjectsRes, error) {
	url := "/lns/api/protocol/modbus_object/getall"
	if !config.C.ChirpStack.API.IsLNS {
		url = "/api/protocol/modbus_object/getall"
	}

	requestJSON, err := json.Marshal(data)
	if err != nil {
		return GetAllAvaliableModbusObjectsRes{}, err
	}

	bytes, err := post(url, string(requestJSON))
	if err != nil {
		return GetAllAvaliableModbusObjectsRes{}, err
	}

	var res GetAllAvaliableModbusObjectsRes
	err = json.Unmarshal(bytes, &res)
	if err != nil {
		return GetAllAvaliableModbusObjectsRes{}, err
	}

	return res, nil
}

type AddModbusDatumReq struct {
	ServerID string        `json:"server_id"`
	Data     []ModbusDatum `json:"data"`
}

func AddModbusDatum(data AddModbusDatumReq) error {
	url := "/lns/api/protocol/modbus_object/add"
	if !config.C.ChirpStack.API.IsLNS {
		url = "/api/protocol/modbus_object/add"
	}

	requestJSON, err := json.Marshal(data)
	if err != nil {
		return err
	}

	bytes, err := post(url, string(requestJSON))
	if err != nil {
		return err
	}

	ret := string(bytes)
	log.Info("add modbus datum: ", ret)

	return nil
}
