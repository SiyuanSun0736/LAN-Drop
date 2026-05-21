package transport

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net"
	"time"

	"github.com/landrop/landrop/backend/internal/config"
	"github.com/landrop/landrop/backend/internal/selfsigned"
)

type Server struct {
	config      config.Config
	certificate tls.Certificate
	listener    net.Listener
}

type Hello struct {
	DeviceID   string `json:"deviceId"`
	DeviceName string `json:"deviceName"`
	OS         string `json:"os"`
	Protocol   string `json:"protocol"`
	Version    string `json:"version"`
	Status     string `json:"status"`
}

func New(cfg config.Config) (*Server, error) {
	certificate, err := selfsigned.GenerateServerCertificate(cfg.DeviceName)
	if err != nil {
		return nil, err
	}

	return &Server{
		config:      cfg,
		certificate: certificate,
	}, nil
}

func (s *Server) Start(ctx context.Context) error {
	listener, err := tls.Listen("tcp", s.config.TransferBind, &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{s.certificate},
		NextProtos:   []string{s.config.ProtocolName},
	})
	if err != nil {
		return err
	}

	s.listener = listener

	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()

	go s.acceptLoop(ctx)

	return nil
}

func (s *Server) Port() int {
	if s.listener == nil {
		return 0
	}

	if address, ok := s.listener.Addr().(*net.TCPAddr); ok {
		return address.Port
	}

	return 0
}

func (s *Server) acceptLoop(ctx context.Context) {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return
			}

			continue
		}

		go s.handleConnection(conn)
	}
}

func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	hello, err := json.Marshal(Hello{
		DeviceID:   s.config.DeviceID,
		DeviceName: s.config.DeviceName,
		OS:         s.config.DeviceOS,
		Protocol:   s.config.ProtocolName,
		Version:    s.config.Version,
		Status:     "ready",
	})
	if err != nil {
		return
	}

	_, _ = conn.Write(append(hello, '\n'))
}