package util

import (
	"net"
	"net/http"
	"strings"
)

var defaultTrustedProxyCIDRs = []string{
	"127.0.0.0/8",
	"::1/128",
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
}

// DefaultTrustedProxyCIDRs returns the proxy networks used by the local Docker topology.
func DefaultTrustedProxyCIDRs() []string {
	return append([]string(nil), defaultTrustedProxyCIDRs...)
}

// IsTrustedProxyRequest reports whether forwarded headers came from a local or private proxy.
func IsTrustedProxyRequest(request *http.Request) bool {
	if request == nil {
		return false
	}

	remote := strings.TrimSpace(request.RemoteAddr)
	if host, _, err := net.SplitHostPort(remote); err == nil {
		remote = host
	}
	remoteIP := net.ParseIP(remote)
	if remoteIP == nil {
		return false
	}

	for _, cidr := range defaultTrustedProxyCIDRs {
		_, network, err := net.ParseCIDR(cidr)
		if err == nil && network.Contains(remoteIP) {
			return true
		}
	}
	return false
}
