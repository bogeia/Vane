package config

import (
	"strconv"
	"strings"
	"testing"
)

func TestLoadFetchProxy(t *testing.T) {
	sample, err := readConfigSample(t)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"", "http://127.0.0.1:7890", "http://user:password@proxy.test:8080", "http://[::1]:7890", "https://proxy.test:80", "socks5://proxy.test:1080", "http://proxy.test", "http://proxy.test:0", "http://proxy.test:65536", "http://proxy.test:abc", "http://proxy.test:80/", "http://proxy.test:80/path", "http://proxy.test:80?x=secret", "http://proxy.test:80?", "http://proxy.test:80#secret", "http://proxy.test:80#", "http://:80", " http://proxy.test:80"} {
		t.Run(value, func(t *testing.T) {
			valid := value == "" || value == "http://127.0.0.1:7890" || value == "http://user:password@proxy.test:8080" || value == "http://[::1]:7890"
			content := strings.Replace(string(sample), `proxy_url: ""`, "proxy_url: "+strconv.Quote(value), 1)
			cfg, err := Load(writeTestConfig(t, content))
			if valid {
				if err != nil || cfg.Fetch.ProxyURL != value {
					t.Fatalf("valid proxy configuration rejected: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("invalid proxy accepted")
			}
			if !strings.Contains(err.Error(), "ProxyURL failed http_proxy_url") || strings.Contains(err.Error(), value) {
				t.Fatalf("unsafe or imprecise error: %v", err)
			}
		})
	}
	for _, replacement := range []string{"", "proxy_url: null", "proxy_url: 123"} {
		content := strings.Replace(string(sample), `proxy_url: ""`, replacement, 1)
		if _, err := Load(writeTestConfig(t, content)); err == nil {
			t.Fatalf("invalid field accepted: %q", replacement)
		}
	}
}
