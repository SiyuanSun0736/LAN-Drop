package transport

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/landrop/landrop/backend/internal/config"
	"github.com/landrop/landrop/backend/internal/model"
	"github.com/landrop/landrop/backend/internal/selfsigned"
)

func Dial(ctx context.Context, cfg config.Config, device model.Device) (*tls.Conn, error) {
	if strings.TrimSpace(device.Address) == "" || device.Port <= 0 {
		return nil, errors.New("target device address is incomplete")
	}

	if strings.TrimSpace(device.Fingerprint) == "" {
		return nil, errors.New("target device fingerprint is missing")
	}

	dialer := &tls.Dialer{
		NetDialer: &net.Dialer{},
		Config: &tls.Config{
			MinVersion:         tls.VersionTLS13,
			InsecureSkipVerify: true,
			NextProtos:         []string{cfg.ProtocolName},
			VerifyConnection: func(state tls.ConnectionState) error {
				if len(state.PeerCertificates) == 0 {
					return errors.New("peer certificate missing")
				}

				actual := selfsigned.FingerprintDER(state.PeerCertificates[0].Raw)
				if !strings.EqualFold(actual, device.Fingerprint) {
					return fmt.Errorf("peer fingerprint mismatch: want %s got %s", device.Fingerprint, actual)
				}

				return nil
			},
		},
	}

	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(device.Address, strconv.Itoa(device.Port)))
	if err != nil {
		return nil, err
	}

	tlsConn, ok := conn.(*tls.Conn)
	if !ok {
		_ = conn.Close()
		return nil, fmt.Errorf("unexpected connection type %T", conn)
	}

	return tlsConn, nil
}