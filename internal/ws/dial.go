package ws

import (
	"crypto/tls"
	"net"
	"time"
)

// DefaultPath is the endpoint the relay serves and clients dial when a URL
// carries no path of its own.
const DefaultPath = "/croc"

// dialTCP opens the transport under the WebSocket handshake, adding TLS for
// wss/https so a Cloudflare hostname works without extra configuration.
func dialTCP(hostPort, serverName string, secure bool, timeout time.Duration) (net.Conn, error) {
	d := &net.Dialer{Timeout: timeout}
	if !secure {
		return d.Dial("tcp", hostPort)
	}
	return tls.DialWithDialer(d, "tcp", hostPort, &tls.Config{ServerName: serverName})
}
