package tlsinfo

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	execx "github.com/ParthKadam11/WTFisRunning/internal/exec"
	"github.com/ParthKadam11/WTFisRunning/internal/model"
)

const collectorName = "tls"

// Collect probes TLS on common HTTPS ports that are listening.
func Collect(ctx context.Context, runner execx.Runner, ports []model.Port) ([]model.TLSCert, model.CollectorResult) {
	ctx, cancel := execx.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	candidates := tlsCandidatePorts(ports)
	if len(candidates) == 0 {
		return nil, model.CollectorResult{
			Name:    collectorName,
			Status:  model.CollectorOK,
			Message: "no tls ports",
		}
	}

	var certs []model.TLSCert
	for _, p := range candidates {
		if ctx.Err() != nil {
			break
		}
		cert := probePort(ctx, runner, p)
		certs = append(certs, cert)
	}

	ok := 0
	for _, c := range certs {
		if c.Accessible {
			ok++
		}
	}
	return certs, model.CollectorResult{
		Name:    collectorName,
		Status:  model.CollectorOK,
		Count:   ok,
		Message: fmt.Sprintf("%d certs", ok),
	}
}

func tlsCandidatePorts(ports []model.Port) []model.Port {
	want := map[int]bool{443: true, 8443: true, 9443: true, 6443: true}
	var out []model.Port
	seen := map[int]bool{}
	for _, p := range ports {
		if p.Protocol != "tcp" {
			continue
		}
		if !want[p.Port] && p.Port != 443 {
			// also include any public high port? stick to common TLS ports for MVP
			if !want[p.Port] {
				continue
			}
		}
		if seen[p.Port] {
			continue
		}
		seen[p.Port] = true
		out = append(out, p)
	}
	return out
}

func probePort(ctx context.Context, runner execx.Runner, p model.Port) model.TLSCert {
	cert := model.TLSCert{
		Port:    p.Port,
		Address: p.Address,
	}

	// Prefer openssl when available (works over SSH remote).
	if c, ok := probeOpenSSL(ctx, runner, p.Port); ok {
		return c
	}

	// Local-only fallback via Go tls (won't work for remote runner targets).
	if runner.Host() == "local" {
		if c, ok := probeLocalTLS(p.Port); ok {
			return c
		}
	}

	cert.Error = "unreachable"
	return cert
}

func probeOpenSSL(ctx context.Context, runner execx.Runner, port int) (model.TLSCert, bool) {
	// echo | openssl s_client -connect 127.0.0.1:443 -servername localhost 2>/dev/null | openssl x509 -noout -dates -subject -issuer -ext subjectAltName
	script := fmt.Sprintf(
		`echo | openssl s_client -connect 127.0.0.1:%d -servername localhost -brief 2>/dev/null | openssl x509 -noout -dates -subject -issuer -ext subjectAltName 2>/dev/null`,
		port,
	)
	res := runner.Run(ctx, "sh", "-c", script)
	if res.Err != nil || strings.TrimSpace(res.Stdout) == "" {
		// try without -brief
		script = fmt.Sprintf(
			`echo | timeout 5 openssl s_client -connect 127.0.0.1:%d -servername localhost 2>/dev/null | openssl x509 -noout -dates -subject -issuer -ext subjectAltName 2>/dev/null`,
			port,
		)
		res = runner.Run(ctx, "sh", "-c", script)
		if res.Err != nil || strings.TrimSpace(res.Stdout) == "" {
			return model.TLSCert{}, false
		}
	}
	return ParseOpenSSLX509(res.Stdout, port), true
}

// ParseOpenSSLX509 parses openssl x509 -noout text.
func ParseOpenSSLX509(out string, port int) model.TLSCert {
	cert := model.TLSCert{Port: port, Accessible: true}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "notAfter="):
			raw := strings.TrimPrefix(line, "notAfter=")
			if t, err := time.Parse("Jan 2 15:04:05 2006 MST", raw); err == nil {
				cert.NotAfter = t
				cert.ExpiresIn = formatExpiry(t)
			} else if t, err := time.Parse("Jan _2 15:04:05 2006 MST", raw); err == nil {
				cert.NotAfter = t
				cert.ExpiresIn = formatExpiry(t)
			}
		case strings.HasPrefix(line, "subject="):
			cert.CN = extractCN(strings.TrimPrefix(line, "subject="))
		case strings.HasPrefix(line, "issuer="):
			cert.Issuer = extractCN(strings.TrimPrefix(line, "issuer="))
		case strings.Contains(line, "DNS:") || strings.HasPrefix(line, "X509v3 Subject Alternative Name:"):
			cert.SANs = append(cert.SANs, parseSANs(line)...)
		}
	}
	cert.SANs = uniqueStrings(cert.SANs)
	return cert
}

func probeLocalTLS(port int) (model.TLSCert, bool) {
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	dialer := &net.Dialer{Timeout: 3 * time.Second}
	conn, err := tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		return model.TLSCert{}, false
	}
	defer conn.Close()
	state := conn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return model.TLSCert{}, false
	}
	c := state.PeerCertificates[0]
	return fromX509(c, port), true
}

func fromX509(c *x509.Certificate, port int) model.TLSCert {
	sans := append([]string{}, c.DNSNames...)
	cert := model.TLSCert{
		Port:       port,
		CN:         c.Subject.CommonName,
		SANs:       sans,
		NotAfter:   c.NotAfter,
		ExpiresIn:  formatExpiry(c.NotAfter),
		Issuer:     c.Issuer.CommonName,
		Accessible: true,
	}
	return cert
}

func extractCN(subject string) string {
	// CN = example.com or CN=example.com
	parts := strings.Split(subject, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if strings.HasPrefix(p, "CN=") || strings.HasPrefix(p, "CN =") {
			return strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(p, "CN ="), "CN="))
		}
	}
	return strings.TrimSpace(subject)
}

func parseSANs(line string) []string {
	var out []string
	for _, part := range strings.Split(line, ",") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "DNS:") {
			out = append(out, strings.TrimPrefix(part, "DNS:"))
		}
	}
	return out
}

func formatExpiry(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Until(t)
	days := int(d.Hours() / 24)
	if days < 0 {
		return "EXPIRED"
	}
	if days == 0 {
		return "<1d"
	}
	return fmt.Sprintf("%dd", days)
}

func uniqueStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
