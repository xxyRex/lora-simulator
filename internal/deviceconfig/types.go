package deviceconfig

import "time"

// DevicesJSON represents the structure of devices.json
type DevicesJSON struct {
	Version string             `json:"version"`
	Devices []DeviceTypeConfig `json:"devices"`
}

// DeviceTypeConfig represents a device type configuration from devices.json
// Note: This struct only contains device definition fields.
// Simulation parameters (uplink_interval, count, etc.) are defined in simulation-config.json
type DeviceTypeConfig struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Catalog       string   `json:"catalog"`
	SN            string   `json:"sn"`
	DevEUIPrefix  string   `json:"deveui"`
	DeviceProfile []string `json:"device_profile"`
	DecoderScript string   `json:"decoder_script"`
	EncoderScript string   `json:"encoder_script"`
	TestData      string   `json:"test_data"`
	DefaultFPort  int      `json:"default_fport"`
}

// SimulationConfig represents the simulation-config.json structure
// This config only contains the device types to simulate and their counts
// All runtime parameters are controlled by the main config file (lora-simulator.toml)
type SimulationConfig struct {
	DeviceTypes []DeviceTypeInstance `json:"device_types"`
}

// DeviceTypeInstance represents a device type instance in simulation config
// Each device type can have its own independent uplink configuration
type DeviceTypeInstance struct {
	DeviceID         string  `json:"device_id"`
	Count            int     `json:"count"`
	TestDataOverride *string `json:"test_data_override,omitempty"`
	// Per-device-type uplink configuration
	UplinkPaused   bool  `json:"uplink_paused"`
	UplinkConfirm  bool  `json:"uplink_confirm"`
	UplinkInterval int64 `json:"uplink_interval"` // in milliseconds
}

// DeviceInstance represents a runtime device instance
type DeviceInstance struct {
	DevEUI         string
	Name           string
	AppKey         string
	DeviceType     *DeviceTypeConfig
	TestData       map[string]interface{}
	EncoderScript  string
	FPort          int
	UplinkInterval time.Duration
	DeviceProfile  string
	Application    string
	// Per-device uplink configuration
	UplinkPaused  bool
	UplinkConfirm bool
}

// ParseDuration parses a duration string (e.g., "300s", "5m") to time.Duration
func ParseDuration(s string) (time.Duration, error) {
	if s == "" {
		return 0, nil
	}
	return time.ParseDuration(s)
}
