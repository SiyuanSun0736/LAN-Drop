package discovery

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/grandcat/zeroconf"
	"github.com/landrop/landrop/backend/internal/config"
	"github.com/landrop/landrop/backend/internal/model"
)

type Service struct {
	config   config.Config
	port     int
	onDevice func(device model.Device)
	server   *zeroconf.Server
}

func New(cfg config.Config, port int, onDevice func(device model.Device)) *Service {
	return &Service{
		config:   cfg,
		port:     port,
		onDevice: onDevice,
	}
}

func (s *Service) Start(ctx context.Context) error {
	server, err := zeroconf.Register(
		s.config.DeviceName,
		s.config.ServiceType,
		s.config.ServiceDomain,
		s.port,
		[]string{
			fmt.Sprintf("id=%s", s.config.DeviceID),
			fmt.Sprintf("name=%s", s.config.DeviceName),
			fmt.Sprintf("os=%s", s.config.DeviceOS),
			fmt.Sprintf("protocol=%s", s.config.ProtocolName),
			fmt.Sprintf("version=%s", s.config.Version),
		},
		nil,
	)
	if err != nil {
		return err
	}

	s.server = server

	go func() {
		<-ctx.Done()
		server.Shutdown()
	}()

	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		server.Shutdown()
		return err
	}

	entries := make(chan *zeroconf.ServiceEntry)
	go s.consume(ctx, entries)

	return resolver.Browse(ctx, s.config.ServiceType, s.config.ServiceDomain, entries)
}

func (s *Service) consume(ctx context.Context, entries <-chan *zeroconf.ServiceEntry) {
	for {
		select {
		case <-ctx.Done():
			return
		case entry, ok := <-entries:
			if !ok || entry == nil {
				return
			}

			device, ok := s.toDevice(entry)
			if !ok {
				continue
			}

			s.onDevice(device)
		}
	}
}

func (s *Service) toDevice(entry *zeroconf.ServiceEntry) (model.Device, bool) {
	records := make(map[string]string, len(entry.Text))
	for _, item := range entry.Text {
		parts := strings.SplitN(item, "=", 2)
		if len(parts) != 2 {
			continue
		}

		records[strings.ToLower(parts[0])] = parts[1]
	}

	deviceID := records["id"]
	if deviceID == "" {
		deviceID = entry.Instance
	}

	if deviceID == s.config.DeviceID {
		return model.Device{}, false
	}

	address := ""
	if len(entry.AddrIPv4) > 0 {
		address = entry.AddrIPv4[0].String()
	} else if len(entry.AddrIPv6) > 0 {
		address = entry.AddrIPv6[0].String()
	}

	if address == "" {
		return model.Device{}, false
	}

	name := records["name"]
	if name == "" {
		name = entry.Instance
	}

	return model.Device{
		ID:       deviceID,
		Name:     name,
		OS:       records["os"],
		Address:  address,
		Port:     entry.Port,
		Protocol: records["protocol"],
		Version:  records["version"],
		LastSeen: time.Now().UTC(),
	}, true
}