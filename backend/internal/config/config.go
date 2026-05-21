package config

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"runtime"
	"strconv"
	"strings"
)

type Config struct {
	DeviceID      string
	DeviceName    string
	DeviceOS      string
	ServiceType   string
	ServiceDomain string
	IPCBind       string
	TransferBind  string
	ProtocolName  string
	Version       string
}

func Load() Config {
	hostname, err := os.Hostname()
	if err != nil || strings.TrimSpace(hostname) == "" {
		hostname = "LAN-Drop"
	}

	return Config{
		DeviceID:      getenv("LANDROP_DEVICE_ID", randomID()),
		DeviceName:    getenv("LANDROP_DEVICE_NAME", hostname),
		DeviceOS:      getenv("LANDROP_DEVICE_OS", runtime.GOOS),
		ServiceType:   getenv("LANDROP_SERVICE_TYPE", "_landrop._tcp"),
		ServiceDomain: getenv("LANDROP_SERVICE_DOMAIN", "local."),
		IPCBind:       getenv("LANDROP_IPC_BIND", "127.0.0.1:0"),
		TransferBind:  getenv("LANDROP_TRANSFER_BIND", "0.0.0.0:0"),
		ProtocolName:  getenv("LANDROP_PROTOCOL", "landrop/0"),
		Version:       getenv("LANDROP_VERSION", "0.1.0"),
	}
}

func getenv(key string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	return value
}

func randomID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "landrop-local"
	}

	return hex.EncodeToString(buf)
}

func Int(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}

	return parsed
}