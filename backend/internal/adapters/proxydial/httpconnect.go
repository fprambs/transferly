package proxydial

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
)

// httpConnect asks an HTTP proxy to tunnel to address with the CONNECT method (RFC 9110 §9.3.6), over conn.
func httpConnect(ctx context.Context, conn net.Conn, cfg Config, address string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodConnect, "http://"+address, nil)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrRefused, err)
	}
	req.Host = address
	if cfg.Username != "" {
		req.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(cfg.Username+":"+cfg.Password)))
	}
	if err := req.Write(conn); err != nil {
		return err
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), req)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrRefused, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: the proxy answered %s", ErrRefused, resp.Status)
	}
	return nil
}
