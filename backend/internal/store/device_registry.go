package store

import (
	"sort"
	"sync"
	"time"

	"github.com/landrop/landrop/backend/internal/model"
)

type DeviceRegistry struct {
	mu      sync.RWMutex
	devices map[string]model.Device
}

func NewDeviceRegistry() *DeviceRegistry {
	return &DeviceRegistry{
		devices: make(map[string]model.Device),
	}
}

func (r *DeviceRegistry) Upsert(device model.Device) model.Device {
	r.mu.Lock()
	defer r.mu.Unlock()

	device.LastSeen = time.Now().UTC()
	r.devices[device.ID] = device

	return device
}

func (r *DeviceRegistry) List() []model.Device {
	r.mu.RLock()
	defer r.mu.RUnlock()

	devices := make([]model.Device, 0, len(r.devices))
	for _, device := range r.devices {
		devices = append(devices, device)
	}

	sort.Slice(devices, func(i int, j int) bool {
		if devices[i].Name == devices[j].Name {
			return devices[i].ID < devices[j].ID
		}

		return devices[i].Name < devices[j].Name
	})

	return devices
}

func (r *DeviceRegistry) Get(id string) (model.Device, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	device, ok := r.devices[id]
	return device, ok
}