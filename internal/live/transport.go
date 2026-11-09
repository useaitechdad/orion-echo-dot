package live

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

type Connection struct{ ws *websocket.Conn }

func Dial(ctx context.Context, c Config) (*Connection, error) {
	if strings.TrimSpace(c.Key) == "" || strings.TrimSpace(c.Model) == "" {
		return nil, fmt.Errorf("Gemini key and model are required")
	}
	if time.Now().Year() < 2025 {
		return nil, fmt.Errorf("device clock is unset; synchronize it before TLS")
	}
	dialer := net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	if c.DNS != "" {
		if _, _, err := net.SplitHostPort(c.DNS); err != nil {
			return nil, fmt.Errorf("DNS must be host:port")
		}
		dialer.Resolver = &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{Timeout: 5 * time.Second}
			return d.DialContext(ctx, network, c.DNS)
		}}
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if c.CAFile != "" {
		pem, err := os.ReadFile(c.CAFile)
		if err != nil {
			return nil, fmt.Errorf("read CA bundle: %w", err)
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("CA bundle contains no certificates")
		}
		tlsConfig.RootCAs = roots
	}
	d := websocket.Dialer{HandshakeTimeout: 15 * time.Second, TLSClientConfig: tlsConfig, NetDialContext: dialer.DialContext}
	// Header authentication keeps the key out of URLs and normal connection logs.
	ws, resp, err := d.DialContext(ctx, Endpoint, http.Header{"X-Goog-Api-Key": []string{c.Key}})
	if err != nil {
		if resp != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("Gemini handshake HTTP %d", resp.StatusCode)
		}
		return nil, fmt.Errorf("Gemini connection failed: %w", err)
	}
	conn := &Connection{ws: ws}
	// Cancellation must also interrupt setupComplete reads, not only TCP/TLS.
	stopSetup := context.AfterFunc(ctx, func() { ws.Close() })
	defer stopSetup()
	ws.SetReadLimit(2 << 20)
	if err = conn.Send(Setup(c)); err != nil {
		ws.Close()
		return nil, err
	}
	ws.SetReadDeadline(time.Now().Add(20 * time.Second))
	_, data, err := ws.ReadMessage()
	if err != nil {
		ws.Close()
		return nil, fmt.Errorf("waiting for setupComplete: %w", err)
	}
	m, err := Decode(data)
	if err != nil || m.SetupComplete == nil {
		ws.Close()
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("expected setupComplete")
	}
	ws.SetReadDeadline(time.Now().Add(75 * time.Second))
	ws.SetPongHandler(func(string) error { return ws.SetReadDeadline(time.Now().Add(75 * time.Second)) })
	return conn, nil
}
func (c *Connection) Close() error { return c.ws.Close() }

// Send/Ping have one owner (the session loop); Receive has one reader goroutine.
func (c *Connection) Send(v any) error {
	if err := c.ws.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return err
	}
	return c.ws.WriteJSON(v)
}
func (c *Connection) Audio(p []byte) error {
	return c.Send(map[string]any{"realtimeInput": map[string]any{"audio": Blob{MIMEType: "audio/pcm;rate=16000", Data: base64.StdEncoding.EncodeToString(p)}}})
}
func (c *Connection) StreamEnd() error {
	return c.Send(map[string]any{"realtimeInput": map[string]any{"audioStreamEnd": true}})
}
func (c *Connection) Ping() error {
	return c.ws.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second))
}
func (c *Connection) Receive() (Message, error) {
	_, p, err := c.ws.ReadMessage()
	if err != nil {
		return Message{}, err
	}
	c.ws.SetReadDeadline(time.Now().Add(75 * time.Second))
	return Decode(p)
}
