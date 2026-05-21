package app

import (
	"context"
	"fmt"

	"github.com/landrop/landrop/backend/internal/config"
	"github.com/landrop/landrop/backend/internal/discovery"
	"github.com/landrop/landrop/backend/internal/events"
	"github.com/landrop/landrop/backend/internal/ipc"
	"github.com/landrop/landrop/backend/internal/model"
	"github.com/landrop/landrop/backend/internal/store"
	"github.com/landrop/landrop/backend/internal/transfer"
	"github.com/landrop/landrop/backend/internal/transport"
)

type App struct {
	config    config.Config
	devices   *store.DeviceRegistry
	hub       *ipc.Hub
	transfers *transfer.Manager
	ipcServer *ipc.Server
	transport *transport.Server
}

func New(cfg config.Config) (*App, error) {
	hub := ipc.NewHub()
	devices := store.NewDeviceRegistry()
	transfers := transfer.NewManager(cfg, devices, hub)
	ipcServer := ipc.NewServer(cfg, devices, hub, transfers)
	transportServer, err := transport.New(cfg)
	if err != nil {
		return nil, err
	}
	transportServer.SetHandler(transfers.HandleIncoming)

	return &App{
		config:    cfg,
		devices:   devices,
		hub:       hub,
		transfers: transfers,
		ipcServer: ipcServer,
		transport: transportServer,
	}, nil
}

func (a *App) Run(ctx context.Context) error {
	if err := a.transport.Start(ctx); err != nil {
		return err
	}

	if err := a.ipcServer.Start(ctx); err != nil {
		return err
	}

	fmt.Printf("IPC listening on %s\n", a.ipcServer.Address())

	discoveryService := discovery.New(a.config, a.transport.Port(), a.transport.Fingerprint(), func(device model.Device) {
		stored := a.devices.Upsert(device)
		a.hub.Broadcast(events.New("device.upserted", stored))
	})

	if err := discoveryService.Start(ctx); err != nil {
		return err
	}

	a.hub.Broadcast(events.New("agent.ready", map[string]any{
		"ipc":          a.ipcServer.Address(),
		"transferPort": a.transport.Port(),
		"deviceId":     a.config.DeviceID,
		"deviceName":   a.config.DeviceName,
	}))

	<-ctx.Done()
	return ctx.Err()
}