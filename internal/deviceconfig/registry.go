package deviceconfig

import (
	"strings"
	"sync"
)

// DeviceTypeRegistry manages device type configurations
type DeviceTypeRegistry struct {
	mu      sync.RWMutex
	devices map[string]*DeviceTypeConfig
}

// NewDeviceTypeRegistry creates a new device type registry
func NewDeviceTypeRegistry() *DeviceTypeRegistry {
	return &DeviceTypeRegistry{
		devices: make(map[string]*DeviceTypeConfig),
	}
}

// Register adds a device type to the registry
func (r *DeviceTypeRegistry) Register(device DeviceTypeConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()
	// Store by ID (lowercase for case-insensitive lookup)
	r.devices[strings.ToLower(device.ID)] = &device
	// Also store by Name for convenience
	r.devices[strings.ToLower(device.Name)] = &device
}

// Get retrieves a device type by ID or Name (case-insensitive)
func (r *DeviceTypeRegistry) Get(idOrName string) *DeviceTypeConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.devices[strings.ToLower(idOrName)]
}

// GetByID retrieves a device type by ID
func (r *DeviceTypeRegistry) GetByID(id string) *DeviceTypeConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.devices[strings.ToLower(id)]
}

// GetAll returns all registered device types (deduplicated)
func (r *DeviceTypeRegistry) GetAll() []*DeviceTypeConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()

	seen := make(map[string]bool)
	result := make([]*DeviceTypeConfig, 0)
	for _, device := range r.devices {
		if !seen[device.ID] {
			result = append(result, device)
			seen[device.ID] = true
		}
	}
	return result
}

// GetEnabled returns all device types (simulation enablement is now controlled by simulation-config.json)
// This function is kept for backward compatibility but simply returns all device types
func (r *DeviceTypeRegistry) GetEnabled() []*DeviceTypeConfig {
	return r.GetAll()
}

// Count returns the number of registered device types
func (r *DeviceTypeRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	seen := make(map[string]bool)
	for _, device := range r.devices {
		seen[device.ID] = true
	}
	return len(seen)
}

// Exists checks if a device type exists
func (r *DeviceTypeRegistry) Exists(idOrName string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, exists := r.devices[strings.ToLower(idOrName)]
	return exists
}
