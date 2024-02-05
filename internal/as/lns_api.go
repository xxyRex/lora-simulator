package as

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/brocaar/chirpstack-simulator/internal/config"
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
	url := "/devicehub/api/v1/user/login"

	password := sha256Hash("password")

	data := `{
		"username": "admin",
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

	jwt := jsonObj["data"].(map[string]interface{})["token"].(string)

	return jwt, nil
}

func LNSCreateApplication() (string, error) {
	url := "/lns/api/v1/urapplications"

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

	profileID := jsonObj["profileID"].(string)

	return profileID, nil
}

func LNSDeleteDeviceProfile(profileID string) error {
	url := "/lns/api/v1/urprofiles/" + profileID

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

	return resp.Result[0].ID, err
}
