package as

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/brocaar/chirpstack-simulator/internal/config"
	"github.com/brocaar/chirpstack-simulator/internal/utils"
	"github.com/brocaar/lorawan"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

var jwtConn string

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

func post(url, data, jwt string) ([]byte, error) {
	url = config.C.ChirpStack.API.Server + url

	var response []byte

	req, err := http.NewRequest("POST", url, strings.NewReader(data))
	if err != nil {
		return response, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+jwt)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return response, err
	}

	defer resp.Body.Close()

	bodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return bodyBytes, nil
}

func get(url, data, jwt string) ([]byte, error) {
	url = config.C.ChirpStack.API.Server + url

	var response []byte

	req, err := http.NewRequest("GET", url, strings.NewReader(data))
	if err != nil {
		return response, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+jwt)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return response, err
	}

	defer resp.Body.Close()

	bodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return bodyBytes, nil
}

func delete(url, data, jwt string) ([]byte, error) {
	url = config.C.ChirpStack.API.Server + url

	var response []byte

	req, err := http.NewRequest("DELETE", url, strings.NewReader(data))
	if err != nil {
		return response, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+jwt)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return response, err
	}

	defer resp.Body.Close()

	bodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return bodyBytes, nil
}

func LNSSetup(c config.Config) error {
	conf := c.ChirpStack

	log.WithFields(log.Fields{
		"server":   conf.API.Server,
		"insecure": conf.API.Insecure,
	}).Info("as: connecting api client")

	jwt, err := LoginDeviceHub()
	if err != nil {
		log.Error(err)
	}

	log.Info(jwt)
	jwtConn = jwt

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

func LoginDeviceHub() (string, error) {
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
			return "", err
		}
		password = ps
	}

	data := `{
		"username": "` + config.C.ChirpStack.API.Username + `",
		"password": "` + password + `"
	}`

	bytes, err := post(url, data, "")
	if err != nil {
		return "", err
	}

	jsonObj, err := parseJSON(bytes)
	if err != nil {
		return "", err
	}

	jwt := ""

	if config.C.ChirpStack.API.IsLNS {
		dataMap, ok := jsonObj["data"].(map[string]interface{})
		if !ok {
			return "", fmt.Errorf("data field type assertion failed")
		}
		j, ok := dataMap["token"].(string)
		if !ok {
			return "", fmt.Errorf("token field type assertion failed")
		}
		jwt = j
	} else {
		j, ok := jsonObj["jwt"].(string)
		if !ok {
			return "", fmt.Errorf("token field type assertion failed")
		}
		jwt = j
	}

	return jwt, nil
}

func LNSCreateApplication() (string, error) {
	url := "/lns/api/v1/urapplications"
	if !config.C.ChirpStack.API.IsLNS {
		url = "/api/urapplications"
	}

	data := `
	{
		"organizationID": "1",
		"serviceProfileID": "f6f7d81d-647f-4c7f-8409-3e5218c0c523",
		"name": "simulator_test"
	}
	`

	bytes, err := post(url, data, jwtConn)
	if err != nil {
		return "", err
	}

	jsonObj, err := parseJSON(bytes)
	if err != nil {
		return "", err
	}

	id := jsonObj["id"].(string)

	return id, nil
}

func LNSDeleteApplication(applicationID string) error {
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

	bytes, err := delete(url, data, jwtConn)
	if err != nil {
		return err
	}

	resp := string(bytes)
	if resp != "{}" {
		return errors.Errorf("failed to delete application " + resp)
	}

	return nil
}

func LNSCreateGateway(id string, name string) error {
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

	bytes, err := post(url, data, jwtConn)
	if err != nil {
		return err
	}

	resp := string(bytes)
	if resp != "{}" {
		return errors.Errorf("failed to create gateway")
	}

	return nil
}

func LNSDeleteGateway(id string) error {
	url := "/lns/api/v1/gateways/" + id
	if !config.C.ChirpStack.API.IsLNS {
		url = "/api/gateways/" + id
	}

	bytes, err := delete(url, "", jwtConn)
	if err != nil {
		return err
	}

	resp := string(bytes)
	if resp != "{}" {
		return errors.Errorf("failed to delete gateway " + resp)
	}

	return nil
}

func LNSCreateDeviceProfile() (string, error) {
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

	bytes, err := post(url, data, jwtConn)
	if err != nil {
		return "", err
	}

	jsonObj, err := parseJSON(bytes)
	if err != nil {
		return "", err
	}

	profileID, ok := jsonObj["profileID"].(string)
	if !ok {
		log.Error("LNSCreateDeviceProfile failed")
		return "", nil
	}

	return profileID, nil
}

func LNSDeleteDeviceProfile(profileID string) error {
	url := "/lns/api/v1/urprofiles/" + profileID
	if !config.C.ChirpStack.API.IsLNS {
		url = "/api/urprofiles/" + profileID
	}

	bytes, err := delete(url, "", jwtConn)
	if err != nil {
		return err
	}

	ret := string(bytes)
	if ret != `{}` {
		return errors.Errorf("failed to delete device: " + ret)
	}

	return nil
}

func LNSCreateDevices(eui, profileId, appKey, payloadCodecID, applicationID string) error {
	url := "/lns/api/v1/urdevices"
	if !config.C.ChirpStack.API.IsLNS {
		url = "/api/urdevices"
	}

	data := `
	{
		"devEUI": "` + eui + `",
		"name": "` + eui + `",
		"description": "` + eui + `",
		"profileID": "` + profileId + `",
		"payloadCodecID": "` + payloadCodecID + `",
		"fPort": 1,
		"appKey": "` + "12345678123456781234567812345678" + `",
		"skipFCntCheck": true,
		"devAddr": "",
		"appSKey": "",
		"nwkSKey": "",
		"fCntUp": 0,
		"fCntDown": 0,
		"applicationID": "` + applicationID + `"
	}
	`

	bytes, err := post(url, data, jwtConn)
	if err != nil {
		return err
	}

	ret := string(bytes)
	if ret != `{"code":200,"error":""}` {
		return errors.Errorf("failed to create device: " + ret)
	}

	return nil
}

func LNSDeleteDevices(eui string) error {
	url := "/lns/api/v1/urdevices/" + eui
	if !config.C.ChirpStack.API.IsLNS {
		url = "/api/urdevices/" + eui
	}

	bytes, err := delete(url, "", jwtConn)
	if err != nil {
		return err
	}

	ret := string(bytes)
	if ret != `{}` {
		return errors.Errorf("failed to delete device: " + ret)
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

func LNSGetPayloadCoedc(name string) (string, error) {
	url := "/lns/api/v1/payloadcodecs/lns?limit=10&offset=0&type=default&search=" + name
	if !config.C.ChirpStack.API.IsLNS {
		url = "/api/payloadcodecs?limit=10&offset=0&type=default&search=" + name
	}

	bytes, err := get(url, "", jwtConn)
	if err != nil {
		return "", err
	}

	str := string(bytes)
	log.Info(str)

	var resp ListPayloadCodecResponse
	err = json.Unmarshal(bytes, &resp)
	if err != nil {
		return "", err
	}

	if len(resp.Result) == 0 {
		log.Error("LNSGetPayloadCoedc failed")
		return "", fmt.Errorf("no payload codec")
	}

	return resp.Result[0].ID, err
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

func LNSGetGateway() ([]lorawan.EUI64, error) {
	res := []lorawan.EUI64{}

	url := "/lns/api/v1/gateways?limit=9999&offset=0&organizationID=1"
	if !config.C.ChirpStack.API.IsLNS {
		url = "/api/gateways?limit=9999&offset=0&organizationID=1"
	}

	bytes, err := get(url, "", jwtConn)
	if err != nil {
		return res, err
	}

	var gateways GatewayJSONData
	err = json.Unmarshal(bytes, &gateways)
	if err != nil {
		log.Error("LNSGetGateway failed to Unmarshal Json")
		return res, err
	}

	for _, g := range gateways.Result {
		gatewayEUI := lorawan.EUI64{}
		err := gatewayEUI.UnmarshalText([]byte(g.MAC))
		if err != nil {
			log.Error("LNSGetGateway gatewayEUI.UnmarshalText error ", err)

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

func LNSGetProfiles() ([]ProfileResultJson, error) {
	res := []ProfileResultJson{}

	url := "/lns/api/v1/urprofiles?limit=9999&offset=0&organizationID=1"
	if !config.C.ChirpStack.API.IsLNS {
		url = "/api/urprofiles?limit=9999&offset=0&organizationID=1"
	}

	bytes, err := get(url, "", jwtConn)
	if err != nil {
		return res, err
	}
	var profilesJson ProfileJSONData
	err = json.Unmarshal(bytes, &profilesJson)
	if err != nil {
		log.Error("LNSGetProfiles failed ", err)
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

func LNSGetApplications() ([]ApplicationJson, error) {
	res := []ApplicationJson{}

	url := "/lns/api/v1/urapplications?limit=9999&offset=0&organizationID=1"
	if !config.C.ChirpStack.API.IsLNS {
		url = "/api/urapplications?limit=9999&offset=0&organizationID=1"
	}

	bytes, err := get(url, "", jwtConn)
	if err != nil {
		return res, err
	}
	var apps ApplicationJSONData
	err = json.Unmarshal(bytes, &apps)
	if err != nil {
		log.Error("LNSGetApplications failed ", err)
		return res, err
	}

	return apps.Result, nil
}
