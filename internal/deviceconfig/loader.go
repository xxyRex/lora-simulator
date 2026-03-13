package deviceconfig

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
)

const (
	DefaultDevicesJSONPath = "payload_en_decoder/codec-release/vendors/milesight-iot/devices.json"
	DefaultSimConfigPath   = "config/simulation-config.json"
	DefaultTestDataPath    = "payload_en_decoder/test-data.json"
)

// DeviceConfigLoader loads and manages device configurations
type DeviceConfigLoader struct {
	baseDir     string
	devicesJSON *DevicesJSON
	simConfig   *SimulationConfig
	registry    *DeviceTypeRegistry
}

// NewDeviceConfigLoader creates a new configuration loader
func NewDeviceConfigLoader(baseDir string) *DeviceConfigLoader {
	return &DeviceConfigLoader{
		baseDir:  baseDir,
		registry: NewDeviceTypeRegistry(),
	}
}

// Load loads all configuration files
func (l *DeviceConfigLoader) Load() error {
	// 1. Load devices.json
	if err := l.loadDevicesJSON(); err != nil {
		return fmt.Errorf("load devices.json failed: %w", err)
	}

	// 2. Register all device types
	for _, device := range l.devicesJSON.Devices {
		l.registry.Register(device)
	}

	log.Infof("Registered %d device types from devices.json", l.registry.Count())

	// 3. Load simulation config from CWD (per-instance config, relative to workdir)
	simConfigPath := DefaultSimConfigPath
	if _, err := os.Stat(simConfigPath); err == nil {
		if err := l.loadSimulationConfig(simConfigPath); err != nil {
			return fmt.Errorf("load simulation config failed: %w", err)
		}
		log.Infof("Loaded simulation config with %d device types", len(l.simConfig.DeviceTypes))
	} else {
		log.Info("No simulation-config.json found, using default configuration")
	}

	return nil
}

// loadDevicesJSON loads the devices.json file
func (l *DeviceConfigLoader) loadDevicesJSON() error {
	path := filepath.Join(l.baseDir, DefaultDevicesJSONPath)

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read devices.json failed: %w", err)
	}

	var devicesJSON DevicesJSON
	if err := json.Unmarshal(data, &devicesJSON); err != nil {
		return fmt.Errorf("parse devices.json failed: %w", err)
	}

	l.devicesJSON = &devicesJSON
	return nil
}

// loadSimulationConfig loads the simulation-config.json file
func (l *DeviceConfigLoader) loadSimulationConfig(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read simulation config failed: %w", err)
	}

	var simConfig SimulationConfig
	if err := json.Unmarshal(data, &simConfig); err != nil {
		return fmt.Errorf("parse simulation config failed: %w", err)
	}

	l.simConfig = &simConfig
	return nil
}

// GetRegistry returns the device type registry
func (l *DeviceConfigLoader) GetRegistry() *DeviceTypeRegistry {
	return l.registry
}

// GetSimulationConfig returns the simulation configuration
func (l *DeviceConfigLoader) GetSimulationConfig() *SimulationConfig {
	return l.simConfig
}

// HasSimulationConfig returns true if simulation config was loaded
func (l *DeviceConfigLoader) HasSimulationConfig() bool {
	return l.simConfig != nil
}

// GenerateDeviceInstances generates device instances based on simulation config
func (l *DeviceConfigLoader) GenerateDeviceInstances() ([]*DeviceInstance, error) {
	if l.simConfig == nil {
		return nil, fmt.Errorf("no simulation config loaded")
	}

	var instances []*DeviceInstance

	for _, typeInstance := range l.simConfig.DeviceTypes {
		deviceType := l.registry.Get(typeInstance.DeviceID)
		if deviceType == nil {
			log.Warnf("Unknown device type: %s, skipping", typeInstance.DeviceID)
			continue
		}

		// Load test data
		testDataPath := deviceType.TestData
		if typeInstance.TestDataOverride != nil && *typeInstance.TestDataOverride != "" {
			testDataPath = *typeInstance.TestDataOverride
		}

		testData, err := l.loadTestData(testDataPath, deviceType.Name)
		if err != nil {
			log.Warnf("Failed to load test data for %s: %v, using empty data", typeInstance.DeviceID, err)
			testData = make(map[string]interface{})
		}

		// Load encoder script
		encoderScript, err := l.loadEncoderScript(deviceType.EncoderScript)
		if err != nil {
			log.Warnf("Failed to load encoder script for %s: %v", typeInstance.DeviceID, err)
			continue
		}

		// Use uplink interval from simulation-config.json if specified,
		// otherwise use default (5 minutes)
		interval := 5 * time.Minute // default 5 minutes
		if typeInstance.UplinkInterval > 0 {
			interval = time.Duration(typeInstance.UplinkInterval) * time.Millisecond
		}

		// Determine count from simulation-config.json
		count := typeInstance.Count
		if count <= 0 {
			count = 1
		}

		// Determine FPort
		fPort := deviceType.DefaultFPort
		if fPort <= 0 {
			fPort = 1
		}

		// Determine device profile
		deviceProfile := "ClassA-OTAA"
		if len(deviceType.DeviceProfile) > 0 {
			deviceProfile = deviceType.DeviceProfile[0]
		}
		if typeInstance.DeviceProfileOverride != "" {
			deviceProfile = typeInstance.DeviceProfileOverride
		}

		// Generate device instances
		for i := 0; i < count; i++ {
			devEUI := l.generateDevEUI(deviceType.DevEUIPrefix, typeInstance.DevEUIIndexStart+i)
			appKey := generateRandomHexString(32) // 16 bytes = 32 hex chars

			instance := &DeviceInstance{
				DevEUI:         devEUI,
				Name:           fmt.Sprintf("%s-%d", deviceType.Name, typeInstance.DevEUIIndexStart+i+1),
				AppKey:         appKey,
				DeviceType:     deviceType,
				TestData:       testData,
				EncoderScript:  encoderScript,
				FPort:          fPort,
				UplinkInterval: interval,
				DeviceProfile:  deviceProfile,
				// Per-device-type uplink configuration
				UplinkPaused:  typeInstance.UplinkPaused,
				UplinkConfirm: typeInstance.UplinkConfirm,
			}
			instances = append(instances, instance)
		}

		log.Infof("Generated %d instances for device type %s", count, deviceType.Name)
	}

	return instances, nil
}

// loadTestData loads test data for a device type
func (l *DeviceConfigLoader) loadTestData(path string, deviceName string) (map[string]interface{}, error) {
	var fullPath string

	if path != "" {
		// Try path relative to CWD (workdir) first — for per-instance test_data_override
		if _, err := os.Stat(path); err == nil {
			fullPath = path
		} else {
			// Fall back to baseDir for shared resources (payload_en_decoder/)
			fullPath = filepath.Join(l.baseDir, path)
		}
		if _, err := os.Stat(fullPath); err != nil {
			// Try path relative to device directory
			deviceDir := strings.ToLower(deviceName)
			fullPath = filepath.Join(l.baseDir, "vendors/milesight-iot", deviceDir, deviceDir+"-test-data.json")
		}
	} else {
		// Try device-specific test data with naming convention
		deviceDir := strings.ToLower(deviceName)
		fullPath = filepath.Join(l.baseDir, "vendors/milesight-iot", deviceDir, deviceDir+"-test-data.json")
	}

	// Try to read the file
	data, err := os.ReadFile(fullPath)
	if err != nil {
		// Fall back to default test data
		defaultPath := filepath.Join(l.baseDir, DefaultTestDataPath)
		data, err = os.ReadFile(defaultPath)
		if err != nil {
			return nil, fmt.Errorf("no test data found for %s", deviceName)
		}
		log.Debugf("Using default test data for %s", deviceName)
	} else {
		log.Debugf("Using device-specific test data for %s from %s", deviceName, fullPath)
	}

	var testData map[string]interface{}
	if err := json.Unmarshal(data, &testData); err != nil {
		return nil, fmt.Errorf("parse test data failed: %w", err)
	}

	return testData, nil
}

// loadEncoderScript loads the encoder script for a device type
func (l *DeviceConfigLoader) loadEncoderScript(path string) (string, error) {
	fullPath := filepath.Join(l.baseDir, path)

	data, err := os.ReadFile(fullPath)
	if err != nil {
		return "", fmt.Errorf("read encoder script failed: %w", err)
	}

	return string(data), nil
}

// generateDevEUI generates a DevEUI based on prefix and index
// The prefix from devices.json (e.g., "24e124617") is treated as the high-order part
// and the index is appended to create unique DevEUIs like "24e1246170000001", "24e1246170000002"
func (l *DeviceConfigLoader) generateDevEUI(prefix string, index int) string {
	// Normalize prefix - remove any non-hex characters
	prefix = strings.ReplaceAll(prefix, "-", "")
	prefix = strings.ReplaceAll(prefix, ":", "")
	prefix = strings.ToLower(prefix)

	// DevEUI is 8 bytes = 16 hex characters
	// Strategy: pad the prefix to make room for the index
	// e.g., prefix "24e124617" (9 chars) -> "24e124617" + "0000001" = 16 chars

	if len(prefix) >= 16 {
		// If prefix is already 16+ chars, parse it as base and add index
		baseNum := uint64(0)
		fmt.Sscanf(prefix[:16], "%x", &baseNum)
		newNum := baseNum + uint64(index)
		return fmt.Sprintf("%016x", newNum)
	}

	// Calculate how many hex digits we have for the index
	indexDigits := 16 - len(prefix)
	if indexDigits < 1 {
		indexDigits = 1
	}

	// Format the index with leading zeros to fill remaining space
	// Add 1 to index to start from 1 instead of 0
	indexFormat := fmt.Sprintf("%%0%dd", indexDigits)
	indexStr := fmt.Sprintf(indexFormat, index+1)

	// If index string is too long, we need to handle overflow
	if len(indexStr) > indexDigits {
		// Parse prefix as number and add index
		baseNum := uint64(0)
		fmt.Sscanf(prefix, "%x", &baseNum)
		// Shift left to make room for index
		shift := uint64(indexDigits * 4) // 4 bits per hex digit
		baseNum = baseNum << shift
		newNum := baseNum + uint64(index+1)
		return fmt.Sprintf("%016x", newNum)
	}

	return prefix + indexStr
}

// generateRandomHexString generates a random hex string of specified length
func generateRandomHexString(length int) string {
	bytes := make([]byte, length/2)
	_, err := rand.Read(bytes)
	if err != nil {
		// Fallback to a default if random generation fails
		return strings.Repeat("0", length)
	}
	return hex.EncodeToString(bytes)
}

// GetDeviceTypeByPayloadCodec finds a device type by payload codec name
func (l *DeviceConfigLoader) GetDeviceTypeByPayloadCodec(codecName string) *DeviceTypeConfig {
	return l.registry.Get(codecName)
}

// GenerateSingleTypeInstances generates instances for a single device type (backward compatibility)
func (l *DeviceConfigLoader) GenerateSingleTypeInstances(deviceTypeName string, count int, uplinkInterval time.Duration) ([]*DeviceInstance, error) {
	deviceType := l.registry.Get(deviceTypeName)
	if deviceType == nil {
		return nil, fmt.Errorf("unknown device type: %s", deviceTypeName)
	}

	// Load test data
	testData, err := l.loadTestData(deviceType.TestData, deviceType.Name)
	if err != nil {
		log.Warnf("Failed to load test data for %s: %v, using empty data", deviceTypeName, err)
		testData = make(map[string]interface{})
	}

	// Load encoder script
	encoderScript, err := l.loadEncoderScript(deviceType.EncoderScript)
	if err != nil {
		return nil, fmt.Errorf("failed to load encoder script: %w", err)
	}

	// Determine FPort
	fPort := deviceType.DefaultFPort
	if fPort <= 0 {
		fPort = 1
	}

	// Determine device profile
	deviceProfile := "ClassA-OTAA"
	if len(deviceType.DeviceProfile) > 0 {
		deviceProfile = deviceType.DeviceProfile[0]
	}

	var instances []*DeviceInstance

	for i := 0; i < count; i++ {
		devEUI := l.generateDevEUI(deviceType.DevEUIPrefix, i)
		appKey := generateRandomHexString(32)

		instance := &DeviceInstance{
			DevEUI:         devEUI,
			Name:           fmt.Sprintf("%s-%d", deviceType.Name, i+1),
			AppKey:         appKey,
			DeviceType:     deviceType,
			TestData:       testData,
			EncoderScript:  encoderScript,
			FPort:          fPort,
			UplinkInterval: uplinkInterval,
			DeviceProfile:  deviceProfile,
		}
		instances = append(instances, instance)
	}

	return instances, nil
}
