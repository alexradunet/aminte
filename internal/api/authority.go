package api

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
)

// LoopbackAddress validates the local-only serve address without DNS or service
// lookups. localhost binds deterministically to IPv4 loopback.
func LoopbackAddress(authority string) (string, error) {
	host, port, ok := loopbackAuthority(authority, true)
	if !ok {
		return "", fmt.Errorf("serve address %q: use a numeric loopback IP or localhost and a numeric port (0–65535)", authority)
	}
	if strings.ToLower(host) == "localhost" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, strconv.Itoa(port)), nil
}

func loopbackAuthority(authority string, allowZero bool) (string, int, bool) {
	host, ip, port, ok := authorityOf(authority)
	if !ok || (!allowZero && port == 0) || (ip.IsValid() && !ip.Unmap().IsLoopback()) {
		return "", 0, false
	}
	return host, port, true
}

// authorityOf parses the host:port of a serve address or of a request, with no DNS and no service names. The host is
// localhost in any ASCII case, or a numeric IP with no zone; ip is the zero Addr for localhost. The port is decimal
// digits, 0–65535.
func authorityOf(authority string) (host string, ip netip.Addr, port int, ok bool) {
	host, text, err := net.SplitHostPort(authority)
	if err != nil || host == "" || text == "" {
		return "", netip.Addr{}, 0, false
	}
	for _, c := range text {
		if c < '0' || c > '9' {
			return "", netip.Addr{}, 0, false
		}
	}
	p, err := strconv.ParseUint(text, 10, 16)
	if err != nil {
		return "", netip.Addr{}, 0, false
	}
	if strings.ToLower(host) == "localhost" {
		if strings.HasPrefix(authority, "[") {
			return "", netip.Addr{}, 0, false
		}
		return host, netip.Addr{}, int(p), true
	}
	ip, err = netip.ParseAddr(host)
	if err != nil || ip.Zone() != "" {
		return "", netip.Addr{}, 0, false
	}
	// Brackets belong only to IPv6 literals, not IPv4 or names.
	if strings.HasPrefix(authority, "[") != ip.Is6() {
		return "", netip.Addr{}, 0, false
	}
	return host, ip, int(p), true
}

// absoluteForm reports a request target in absolute form (GET http://host/path). Go then replaces Host with the
// authority of the target. No proxy deployment is supported, so the listener refuses it instead of trusting it.
func absoluteForm(r *http.Request) bool {
	return r.URL == nil || r.URL.IsAbs() || r.URL.Host != ""
}

// NetworkAuthority wraps only socket serving; api.New remains usable by the
// in-process CLI (client.InProcess). The listener's actual port includes ephemeral port binds. It is the outer handler
// of every serve, also one with a token (RequireToken): a request to a Host that is not the address of the listener is
// refused with 403 before the token is looked at, so a page that rebinds a name to the loopback learns nothing.
func NetworkAuthority(addr net.Addr, next http.Handler) (http.Handler, error) {
	_, port, ok := loopbackAuthority(addr.String(), false)
	if !ok {
		return nil, fmt.Errorf("serve listener %q is not a bound loopback address", addr)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authority := r.Host
		// HTTP's omitted port means 80, never the arbitrary listener port.
		// Bare IPv6 remains invalid: only a bracketed literal gets this default.
		if !strings.Contains(authority, ":") || strings.HasSuffix(authority, "]") {
			authority += ":80"
		}
		_, requestPort, allowed := loopbackAuthority(authority, false)
		if !allowed || requestPort != port || absoluteForm(r) {
			writeError(w, r, authorityRefusal())
			return
		}
		next.ServeHTTP(w, r)
	}), nil
}
