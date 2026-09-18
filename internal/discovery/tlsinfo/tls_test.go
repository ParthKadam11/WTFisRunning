package tlsinfo_test

import (
	"strings"
	"testing"

	"github.com/ParthKadam11/WTFisRunning/internal/discovery/tlsinfo"
)

func TestParseOpenSSLX509(t *testing.T) {
	out := `notBefore=Jan  1 00:00:00 2024 GMT
notAfter=Dec 31 23:59:59 2026 GMT
subject=CN = api.example.com
issuer=CN = Let's Encrypt
X509v3 Subject Alternative Name:
    DNS:api.example.com, DNS:www.example.com
`
	c := tlsinfo.ParseOpenSSLX509(out, 443)
	if !c.Accessible {
		t.Fatal("expected accessible")
	}
	if c.CN != "api.example.com" {
		t.Errorf("cn=%q", c.CN)
	}
	if c.ExpiresIn == "" || c.ExpiresIn == "EXPIRED" {
		t.Errorf("expires=%q", c.ExpiresIn)
	}
	if len(c.SANs) < 1 {
		t.Errorf("sans=%v", c.SANs)
	}
	if !strings.Contains(strings.Join(c.SANs, ","), "api.example.com") {
		t.Errorf("sans=%v", c.SANs)
	}
}
