package gateway

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io/ioutil"
	"sync"
	"text/template"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/golang/protobuf/proto"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/brocaar/chirpstack-simulator/internal/config"
	"github.com/brocaar/lorawan"
	"github.com/brocaar/lorawan/band"
	"github.com/chirpstack/chirpstack/api/go/v4/gw"
)

// GatewayOption is the interface for a gateway option.
type GatewayOption func(*Gateway) error

// Gateway defines a simulated LoRa gateway.
type Gateway struct {
	mqtt      mqtt.Client
	gatewayID lorawan.EUI64

	deviceMux sync.RWMutex
	devices   map[lorawan.EUI64]chan *gw.DownlinkFrame

	downlinkTxNAckRate int
	downlinkTxCounter  int
	downlinkTxAckDelay time.Duration

	eventTopicTemplate   *template.Template
	commandTopicTemplate *template.Template
}

type Duration time.Duration

// RXInfo contains the RX information.
type RXInfo struct {
	MAC               lorawan.EUI64 `json:"mac"`                         // MAC address of the gateway
	Time              *time.Time    `json:"time,omitempty"`              // Receive timestamp (only set when the gateway has a GPS time-source)
	TimeSinceGPSEpoch *Duration     `json:"timeSinceGPSEpoch,omitempty"` // Time since GPS epoch (1980-01-06, only set when the gateway has a GPS time source)
	Timestamp         uint32        `json:"timestamp"`                   // gateway internal receive timestamp with microsecond precision, will rollover every ~ 72 minutes
	Frequency         int           `json:"frequency"`                   // frequency in Hz
	Channel           int           `json:"channel"`                     // concentrator IF channel used for RX
	RFChain           int           `json:"rfChain"`                     // RF chain used for RX
	CRCStatus         int           `json:"crcStatus"`                   // 1 = OK, -1 = fail, 0 = no CRC
	CodeRate          string        `json:"codeRate"`                    // ECC code rate
	RSSI              int           `json:"rssi"`                        // RSSI in dBm
	LoRaSNR           float64       `json:"loRaSNR"`                     // LoRa signal-to-noise ratio in dB
	Size              int           `json:"size"`                        // packet payload size
	DataRate          band.DataRate `json:"dataRate"`                    // RX datarate (either LoRa or FSK)
	Board             int           `json:"board"`                       // Concentrator board used for RX
	Antenna           int           `json:"antenna"`                     // Antenna number on which signal has been received
}

type RXPacketBytes struct {
	RXInfo     RXInfo `json:"rxInfo"`
	PHYPayload []byte    `json:"phyPayload"`
}

// TXInfo contains the information used for TX.
type TXInfo struct {
	MAC               lorawan.EUI64 `json:"mac"`                         // MAC address of the gateway
	Immediately       bool          `json:"immediately"`                 // send the packet immediately (ignore Time)
	TimeSinceGPSEpoch *Duration     `json:"timeSinceGPSEpoch,omitempty"` // Transmit at time since GPS epoch (since 1980-01-06, only possible when the gateway has a GPS time source)
	Timestamp         *uint32       `json:"timestamp,omitempty"`         // transmit at gateway internal timestamp (microsecond precision, will rollover every ~ 72 minutes)
	Frequency         int           `json:"frequency"`                   // frequency in Hz
	Power             int           `json:"power"`                       // TX power to use in dBm
	DataRate          band.DataRate `json:"dataRate"`                    // TX datarate (either LoRa or FSK)
	CodeRate          string        `json:"codeRate"`                    // ECC code rate
	IPol              *bool         `json:"iPol"`                        // when left nil, the gateway-bridge will use the default (true for LoRa modulation)
	Board             int           `json:"board"`                       // Concentrator board used for RX
	Antenna           int           `json:"antenna"`                     // Antenna number on which signal has been received
}

var as923_1TxInfo = TXInfo{
	Frequency: 923200000,
	DataRate: band.DataRate{
		Modulation:   "LORA",
		SpreadFactor: 10,
		Bandwidth:    125,
	},
}

var as923_2TxInfo = TXInfo{
	Frequency: 921400000,
	DataRate: band.DataRate{
		Modulation:   "LORA",
		SpreadFactor: 10,
		Bandwidth:    125,
	},
}

var as915TxInfo = TXInfo{
	Frequency: 921400000,
	DataRate: band.DataRate{
		Modulation:   "LORA",
		SpreadFactor: 10,
		Bandwidth:    125,
	},
}

var as923_3TxInfo = TXInfo{
	Frequency: 916600000,
	DataRate: band.DataRate{
		Modulation:   "LORA",
		SpreadFactor: 10,
		Bandwidth:    125,
	},
}

var as923_4TxInfo = TXInfo{
	Frequency: 917300000,
	DataRate: band.DataRate{
		Modulation:   "LORA",
		SpreadFactor: 10,
		Bandwidth:    125,
	},
}

var au915TxInfo = TXInfo{
	Frequency: 915200000,
	DataRate: band.DataRate{
		Modulation:   "LORA",
		SpreadFactor: 12,
		Bandwidth:    500,
	},
}

var cn470TxInfo = TXInfo{
	Frequency: 470300000,
	DataRate: band.DataRate{
		Modulation:   "LORA",
		SpreadFactor: 12,
		Bandwidth:    125,
	},
}

var kr920TxInfo = TXInfo{
	Frequency: 920900000,
	DataRate: band.DataRate{
		Modulation:   "LORA",
		SpreadFactor: 12,
		Bandwidth:    125,
	},
}

var eu868TxInfo = TXInfo{
	Frequency: 868100000,
	DataRate: band.DataRate{
		Modulation:   "LORA",
		SpreadFactor: 12,
		Bandwidth:    125,
	},
}

var in865TxInfo = TXInfo{
	Frequency: 865062500,
	DataRate: band.DataRate{
		Modulation:   "LORA",
		SpreadFactor: 12,
		Bandwidth:    125,
	},
}

var ru864TxInfo = TXInfo{
	Frequency: 868900000,
	DataRate: band.DataRate{
		Modulation:   "LORA",
		SpreadFactor: 12,
		Bandwidth:    125,
	},
}

var txInfoMap = map[string]TXInfo{
	"AS923-1": as923_1TxInfo,
	"AS923-2": as923_2TxInfo,
	"AS923-3": as923_3TxInfo,
	"AS923-4": as923_4TxInfo,
	"AU915":   au915TxInfo,
	"CN470":   cn470TxInfo,
	"KR920":   kr920TxInfo,
	"EU868":   eu868TxInfo,
	"IN865":   in865TxInfo,
	"RU864":   ru864TxInfo,
	"AS915":   as915TxInfo,
}

type TXPacketBytes struct {
	Token      uint16    `json:"token"`
	TXInfo     TXInfo `json:"txInfo"`
	PHYPayload []byte    `json:"phyPayload"`
}

// WithMQTTClient sets the MQTT client for the gateway.
func WithMQTTClient(client mqtt.Client) GatewayOption {
	return func(g *Gateway) error {
		g.mqtt = client
		return nil
	}
}

// WithMQTTCredentials initializes a new MQTT client with the given credentials.
func WithMQTTCredentials(server, username, password string) GatewayOption {
	return func(g *Gateway) error {
		opts := mqtt.NewClientOptions()
		opts.AddBroker(server)
		opts.SetUsername(username)
		opts.SetPassword(password)
		opts.SetCleanSession(true)
		opts.SetAutoReconnect(true)

		log.WithFields(log.Fields{
			"server": server,
		}).Info("simulator: connecting to mqtt broker")

		client := mqtt.NewClient(opts)
		if token := client.Connect(); token.Wait() && token.Error() != nil {
			return errors.Wrap(token.Error(), "mqtt client connect error")
		}

		g.mqtt = client

		return nil
	}
}

// WithMQTTCertificates initializes a new MQTT client with the given CA and
// client-certificate files.
func WithMQTTCertificates(server, caCert, tlsCert, tlsKey string) GatewayOption {
	return func(g *Gateway) error {
		tlsConfig := &tls.Config{}

		if caCert != "" {
			b, err := ioutil.ReadFile(caCert)
			if err != nil {
				return errors.Wrap(err, "read ca certificate error")
			}

			certpool := x509.NewCertPool()
			certpool.AppendCertsFromPEM(b)
			tlsConfig.RootCAs = certpool
		}

		if tlsCert != "" && tlsKey != "" {
			kp, err := tls.LoadX509KeyPair(tlsCert, tlsKey)
			if err != nil {
				return errors.Wrap(err, "read tls key-pair error")
			}

			tlsConfig.Certificates = []tls.Certificate{kp}
		}

		opts := mqtt.NewClientOptions()
		opts.AddBroker(server)
		opts.SetCleanSession(true)
		opts.SetAutoReconnect(true)
		opts.SetTLSConfig(tlsConfig)

		log.WithFields(log.Fields{
			"ca_cert":  caCert,
			"tls_cert": tlsCert,
			"tls_key":  tlsKey,
		}).Info("simulator: connecting to mqtt broker")

		client := mqtt.NewClient(opts)
		if token := client.Connect(); token.Wait() && token.Error() != nil {
			return errors.Wrap(token.Error(), "mqtt client connect error")
		}

		g.mqtt = client

		return nil
	}
}

// WithGatewayID sets the gateway ID.
func WithGatewayID(gatewayID lorawan.EUI64) GatewayOption {
	return func(g *Gateway) error {
		g.gatewayID = gatewayID
		return nil
	}
}

// WithDownlinkTxNackRate sets the rate in which Tx NAck messages are sent.
// Setting this to:
//
//	0: always ACK
//	1: NAck every message
//	2: NAck every other message
//	3: NAck every third message
//	...
func WithDownlinkTxNackRate(rate int) GatewayOption {
	return func(g *Gateway) error {
		g.downlinkTxNAckRate = rate
		return nil
	}
}

// WithDownlinkTxAckDelay sets the delay in which the Tx Ack is returned.
func WithDownlinkTxAckDelay(d time.Duration) GatewayOption {
	return func(g *Gateway) error {
		g.downlinkTxAckDelay = d
		return nil
	}
}

// WithEventTopicTemplate sets the event (gw > ns) topic template.
// Example: 'gateway/{{ .GatewayID }}/event/{{ .Event }}'
func WithEventTopicTemplate(tt string) GatewayOption {
	return func(g *Gateway) error {
		var err error
		g.eventTopicTemplate, err = template.New("event").Parse(tt)
		if err != nil {
			return errors.Wrap(err, "parse event topic template error")
		}

		return nil
	}
}

// WithCommandTopicTemplate sets the command (ns > gw) topic template.
// Example: 'gateway/{{ .GatewayID }}/command/{{ .Command }}'
func WithCommandTopicTemplate(ct string) GatewayOption {
	return func(g *Gateway) error {
		var err error
		g.commandTopicTemplate, err = template.New("command").Parse(ct)
		if err != nil {
			return errors.Wrap(err, "parse command topic template error")
		}

		return nil
	}
}

// NewGateway creates a new gateway, using the given MQTT client for sending
// and receiving.
func NewGateway(opts ...GatewayOption) (*Gateway, error) {
	gw := &Gateway{
		devices: make(map[lorawan.EUI64]chan *gw.DownlinkFrame),
	}

	for _, o := range opts {
		if err := o(gw); err != nil {
			return nil, err
		}
	}

	downlinkTopic := gw.getCommandTopic("down")

	log.WithFields(log.Fields{
		"gateway_id": gw.gatewayID,
		"topic":      downlinkTopic,
	}).Info("simulator: subscribing to gateway mqtt topic")
	for {
		if token := gw.mqtt.Subscribe(downlinkTopic, 0, gw.downlinkEventHandler); token.Wait() && token.Error() != nil {
			log.WithError(token.Error()).WithFields(log.Fields{
				"gateway_id": gw.gatewayID,
				"topic":      downlinkTopic,
			}).Error("simulator: subscribe to mqtt topic error")
			time.Sleep(time.Second * 2)
		} else {
			break
		}
	}

	return gw, nil
}

// SendUplinkFrame sends the given uplink frame.
func (g *Gateway) SendUplinkFrame(pl RXPacketBytes) error {
	currentTime := time.Now()
	channelPlanTxInfo := txInfoMap[config.C.General.ChannelPlan]

	pl.RXInfo = RXInfo{
		MAC:       g.gatewayID,
		Time:      &currentTime,
		Frequency: channelPlanTxInfo.Frequency,
		Channel:   2,
		CodeRate:  "4/5",
		RSSI:      -63,
		LoRaSNR:   8.5,
		DataRate:  channelPlanTxInfo.DataRate,
		Board:     0,
		Antenna:   0,
	}

	jsonBytes, err := json.Marshal(&pl)

	if err != nil {
		return errors.Wrap(err, "send uplink frame error")
	}

	uplinkTopic := g.getEventTopic("up")

	jsonStr := string(jsonBytes)

	log.WithFields(log.Fields{
		"gateway_id": g.gatewayID,
		"topic":      uplinkTopic,
		"json:":      jsonStr,
	}).Debug("simulator: publish uplink frame")

	if token := g.mqtt.Publish(uplinkTopic, 0, false, jsonBytes); token.Wait() && token.Error() != nil {
		return errors.Wrap(err, "simulator: publish uplink frame error")
	}

	gatewayUplinkCounter().Inc()

	return nil
}

// sendDownlinkTxAck sends the given downlink Ack.
func (g *Gateway) sendDownlinkTxAck(pl *gw.DownlinkTxAck) error {
	b, err := proto.Marshal(pl)
	if err != nil {
		return errors.Wrap(err, "send tx ack error")
	}

	ackTopic := g.getEventTopic("ack")

	log.WithFields(log.Fields{
		"gateway_id": g.gatewayID,
		"topic":      ackTopic,
	}).Debug("simulator: publish downlink tx ack")

	if token := g.mqtt.Publish(ackTopic, 0, false, b); token.Wait() && token.Error() != nil {
		return errors.Wrap(err, "simulator: publish downlink tx ack error")
	}

	return nil
}

// addDevice adds the given device to the 'coverage' of the gateway.
// This means that any downlink sent to the gateway will be forwarded to added
// devices (which will each validate the DevAddr and MIC).
func (g *Gateway) AddDevice(devEUI lorawan.EUI64, c chan *gw.DownlinkFrame) {
	g.deviceMux.Lock()
	defer g.deviceMux.Unlock()

	// log.WithFields(log.Fields{
	// 	"dev_eui":    devEUI,
	// 	"gateway_id": g.gatewayID,
	// }).Info("simulator: add device to gateway")

	g.devices[devEUI] = c
}

func (g *Gateway) getEventTopic(event string) string {
	topic := bytes.NewBuffer(nil)

	err := g.eventTopicTemplate.Execute(topic, struct {
		GatewayID lorawan.EUI64
		Event     string
	}{g.gatewayID, event})
	if err != nil {
		log.WithError(err).Fatal("execute event topic template error")
	}

	return topic.String()
}

func (g *Gateway) getCommandTopic(command string) string {
	topic := bytes.NewBuffer(nil)

	err := g.commandTopicTemplate.Execute(topic, struct {
		GatewayID lorawan.EUI64
		Command   string
	}{g.gatewayID, command})
	if err != nil {
		log.WithError(err).Fatal("execute command topic template error")
	}

	return topic.String()
}

func (g *Gateway) downlinkEventHandler(c mqtt.Client, msg mqtt.Message) {
	g.deviceMux.RLock()
	defer g.deviceMux.RUnlock()

	log.WithFields(log.Fields{
		"gateway_id": g.gatewayID,
		"topic":      msg.Topic(),
	}).Debug("simulator: downlink command received")

	gatewayDownlinkCounter().Inc()

	var lnsPl TXPacketBytes
	if err := json.Unmarshal(msg.Payload(), &lnsPl); err != nil {
		log.WithError(err).Error("prase TXPacketBytes error")
	}

	downLinkFrameItems := []*gw.DownlinkFrameItem{}
	downLinkFrameItems = append(downLinkFrameItems, &gw.DownlinkFrameItem{
		PhyPayload: lnsPl.PHYPayload,
		TxInfo: &gw.DownlinkTxInfo{
			Frequency: uint32(lnsPl.TXInfo.Frequency),
			Power:     int32(lnsPl.TXInfo.Power),
			// Modulation: lnsPl.TXInfo.DataRate.Modulation,
			Board:   uint32(lnsPl.TXInfo.Board),
			Antenna: uint32(lnsPl.TXInfo.Antenna),
			// Timing: lnsPl.TXInfo.Timestamp,
		},
	})

	pl := gw.DownlinkFrame{
		GatewayId: lnsPl.TXInfo.MAC.String(),
		Items:     downLinkFrameItems,
	}

	for devEUI, downChan := range g.devices {
		log.WithFields(log.Fields{
			"dev_eui":    devEUI,
			"gateway_id": g.gatewayID,
		}).Debug("simulator: forwarding downlink to device")
		downChan <- &pl
	}

	time.Sleep(g.downlinkTxAckDelay)

	items := []*gw.DownlinkTxAckItem{}

	g.downlinkTxCounter++
	if g.downlinkTxCounter == g.downlinkTxNAckRate {
		g.downlinkTxCounter = 0

		for range pl.Items {
			items = append(items, &gw.DownlinkTxAckItem{
				Status: gw.TxAckStatus_COLLISION_PACKET,
			})
		}

	} else {
		for range pl.Items {
			items = append(items, &gw.DownlinkTxAckItem{
				Status: gw.TxAckStatus_OK,
			})
		}
	}

	txNack := gw.DownlinkTxAck{
		GatewayId:  g.gatewayID.String(),
		DownlinkId: pl.DownlinkId,
		Items:      items,
	}

	if err := g.sendDownlinkTxAck(&txNack); err != nil {
		log.WithError(err).WithFields(log.Fields{
			"gateway_id": g.gatewayID,
		}).Error("simulator: send downlink tx ack error")
	}
}
