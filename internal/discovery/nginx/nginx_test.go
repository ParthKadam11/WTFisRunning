package nginx_test

import (
	"strings"
	"testing"

	"github.com/ParthKadam11/WTFisRunning/internal/discovery/nginx"
	"github.com/ParthKadam11/WTFisRunning/internal/model"
)

func TestParseConfigProxyPass(t *testing.T) {
	cfg := `
http {
  upstream api_upstream {
    server 127.0.0.1:4000;
  }

  server {
    listen 80;
    server_name api.example.com;

    location / {
      proxy_pass http://api_upstream;
    }
  }

  server {
    listen 80;
    server_name app.example.com;

    location / {
      proxy_pass http://127.0.0.1:3000;
    }
  }
}
`
	res := nginx.ParseConfig(cfg)
	if len(res.Locations) < 2 {
		t.Fatalf("locations=%d %+v", len(res.Locations), res.Locations)
	}
	if len(res.Relations) < 2 {
		t.Fatalf("relations=%d", len(res.Relations))
	}
	foundAPI := false
	foundApp := false
	for _, r := range res.Relations {
		if r.Type != model.RelProxiesTo {
			continue
		}
		if r.Source != "nginx" {
			t.Errorf("source=%s", r.Source)
		}
		if strings.Contains(r.Destination, "4000") || r.Destination == "127.0.0.1:4000" {
			foundAPI = true
		}
		if strings.Contains(r.Destination, "3000") {
			foundApp = true
		}
	}
	if !foundAPI || !foundApp {
		t.Fatalf("relations %+v", res.Relations)
	}
}

func TestParseConfigComments(t *testing.T) {
	cfg := `
server {
  # server_name fake.example.com;
  server_name real.example.com;
  location / {
    # proxy_pass http://127.0.0.1:9999;
    proxy_pass http://127.0.0.1:8080;
  }
}
`
	res := nginx.ParseConfig(cfg)
	if len(res.Relations) != 1 {
		t.Fatalf("got %d relations: %+v", len(res.Relations), res.Relations)
	}
	if !strings.Contains(res.Relations[0].Destination, "8080") {
		t.Errorf("%+v", res.Relations[0])
	}
}

func TestParseConfigEmpty(t *testing.T) {
	res := nginx.ParseConfig("")
	if len(res.Relations) != 0 {
		t.Fatalf("expected none")
	}
}
