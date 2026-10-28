package fetch

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bogeia/vane/internal/config"
	"github.com/bogeia/vane/internal/infra/retry"
)

type proxyFixture struct {
	proxy, origin                     *httptest.Server
	client                            *Client
	mu                                sync.Mutex
	queries                           int
	targets, auth, hosts, sni         []string
	addresses                         []string
	ttl                               uint32
	dnsStatus, dnsHTTP, connectStatus int
	blockDNS, blockConnect            bool
	failIPv4                          bool
	redirect                          string
}

func newProxyFixture(t *testing.T) *proxyFixture {
	t.Helper()
	f := &proxyFixture{addresses: []string{"2606:4700:4700::1111", "1.1.1.1"}, ttl: 120}
	f.origin = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		if r.Header.Get("Proxy-Authorization") != "" {
			t.Error("proxy credentials leaked to origin")
		}
		f.hosts = append(f.hosts, r.Host)
		redirect := f.redirect
		f.mu.Unlock()
		if redirect != "" {
			http.Redirect(w, r, redirect, http.StatusFound)
			return
		}
		_, _ = io.WriteString(w, "via proxy")
	}))
	f.origin.TLS = &tls.Config{GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
		f.mu.Lock()
		f.sni = append(f.sni, hello.ServerName)
		f.mu.Unlock()
		return nil, nil
	}}
	f.origin.StartTLS()
	t.Cleanup(f.origin.Close)
	f.proxy = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			f.mu.Lock()
			f.queries++
			ips := append([]string(nil), f.addresses...)
			ttl, status, code, block := f.ttl, f.dnsStatus, f.dnsHTTP, f.blockDNS
			f.mu.Unlock()
			if block {
				<-r.Context().Done()
				return
			}
			if r.Header.Get("Accept") != "application/dns-json" {
				t.Error("missing DNS JSON Accept header")
			}
			if code != 0 {
				w.WriteHeader(code)
				return
			}
			if r.URL.Query().Get("name") == "private.invalid" {
				ips = []string{"10.0.0.1"}
			}
			records := []map[string]any{}
			for _, value := range ips {
				ip := net.ParseIP(value)
				kind := 1
				if ip != nil && ip.To4() == nil {
					kind = 28
				}
				if (kind == 1 && r.URL.Query().Get("type") == "A") || (kind == 28 && r.URL.Query().Get("type") == "AAAA") {
					records = append(records, map[string]any{"type": kind, "TTL": ttl, "data": value})
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"Status": status, "Answer": records})
			return
		}
		f.mu.Lock()
		f.targets = append(f.targets, r.Host)
		f.auth = append(f.auth, r.Header.Get("Proxy-Authorization"))
		status, block, fail4 := f.connectStatus, f.blockConnect, f.failIPv4
		f.mu.Unlock()
		if status != 0 {
			w.WriteHeader(status)
			return
		}
		if fail4 && strings.HasPrefix(r.Host, "1.1.1.1:") {
			w.WriteHeader(502)
			return
		}
		if block {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			defer conn.Close()
			_, _ = io.Copy(io.Discard, conn)
			return
		}
		upstream, err := net.Dial("tcp", f.origin.Listener.Addr().String())
		if err != nil {
			t.Error(err)
			return
		}
		conn, buf, err := w.(http.Hijacker).Hijack()
		if err != nil {
			upstream.Close()
			t.Error(err)
			return
		}
		defer conn.Close()
		defer upstream.Close()
		_, _ = buf.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		_ = buf.Flush()
		done := make(chan struct{})
		go func() { _, _ = io.Copy(upstream, buf); upstream.Close(); close(done) }()
		_, _ = io.Copy(conn, upstream)
		conn.Close()
		<-done
	}))
	t.Cleanup(f.proxy.Close)
	cfg := config.Fetch{ProxyURL: strings.Replace(f.proxy.URL, "http://", "http://proxy-user:proxy-secret@", 1), RequestTimeout: time.Second, Retry: config.Retry{MaxAttempts: 2, InitialInterval: time.Millisecond, MaxInterval: time.Millisecond}}
	f.client = New(false, cfg)
	f.client.proxy.dohURL = "http://doh.invalid/dns-query"
	// Only the in-process origin uses a test certificate; production TLS validation remains enabled.
	f.client.client.Transport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	t.Cleanup(func() { f.client.client.CloseIdleConnections(); f.client.proxy.doh.CloseIdleConnections() })
	return f
}

func TestProxyConnectUsesCheckedIPAndOriginalTLSName(t *testing.T) {
	f := newProxyFixture(t)
	result, err := f.client.Get(t.Context(), "https://target.invalid/feed", nil)
	if err != nil || string(result.Body) != "via proxy" {
		t.Fatalf("Get: %v, %s", err, result.Body)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.targets) != 1 || f.targets[0] != "1.1.1.1:443" {
		t.Fatalf("CONNECT targets: %v", f.targets)
	}
	if len(f.sni) != 1 || f.sni[0] != "target.invalid" || len(f.hosts) != 1 || f.hosts[0] != "target.invalid" {
		t.Fatalf("SNI=%v Host=%v", f.sni, f.hosts)
	}
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("proxy-user:proxy-secret"))
	if f.auth[0] != want {
		t.Fatal("proxy credentials missing")
	}
	if f.queries != 2 {
		t.Fatalf("DoH calls=%d", f.queries)
	}
}

func TestProxyRejectsEveryUnsafeAddressBeforeCONNECT(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "100.64.0.1", "198.18.0.1", "192.0.2.1", "::1", "fc00::1", "fe80::1", "::ffff:127.0.0.1", "64:ff9b::a00:1"} {
		t.Run(ip, func(t *testing.T) {
			f := newProxyFixture(t)
			f.addresses = []string{"1.1.1.1", ip}
			_, err := f.client.Get(t.Context(), "https://target.invalid/", nil)
			if !retry.IsPermanent(err) {
				t.Fatalf("unsafe DNS result not permanent: %v", err)
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if len(f.targets) != 0 {
				t.Fatalf("connected to unsafe DNS response: %v", f.targets)
			}
		})
	}
}

func TestProxyIPLiteralSkipsDoHAndRejectsPrivateRedirect(t *testing.T) {
	f := newProxyFixture(t)
	if _, err := f.client.Get(t.Context(), "https://1.1.1.1/", nil); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	queries := f.queries
	f.redirect = "http://169.254.169.254/metadata"
	f.mu.Unlock()
	if queries != 0 {
		t.Fatalf("literal IP queried DoH: %d", queries)
	}
	_, err := f.client.Get(t.Context(), "https://target.invalid/", nil)
	if !retry.IsPermanent(err) {
		t.Fatalf("private redirect accepted: %v", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, target := range f.targets {
		if strings.Contains(target, "169.254") {
			t.Fatal("connected to private redirect")
		}
	}
}

func TestProxyRetriesFailuresWithoutLeakingConfiguration(t *testing.T) {
	for _, kind := range []string{"DNS HTTP", "DNS status", "DNS empty", "DNS malformed address", "407", "502", "unreachable"} {
		t.Run(kind, func(t *testing.T) {
			f := newProxyFixture(t)
			switch kind {
			case "DNS HTTP":
				f.dnsHTTP = 502
			case "DNS status":
				f.dnsStatus = 2
			case "DNS empty":
				f.addresses = nil
			case "DNS malformed address":
				f.addresses = []string{"broken"}
			case "407":
				f.connectStatus = 407
			case "502":
				f.connectStatus = 502
			case "unreachable":
				f.proxy.Close()
			}
			_, err := f.client.Get(t.Context(), "https://target.invalid/", nil)
			if err == nil || retry.IsPermanent(err) || err.Error() != "fetch request failed" {
				t.Fatalf("wrong error classification: %v", err)
			}
			for _, secret := range []string{"proxy-user", "proxy-secret", f.proxy.URL} {
				if strings.Contains(err.Error(), secret) {
					t.Fatal("proxy information exposed")
				}
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if kind == "407" || kind == "502" {
				if len(f.targets) != 4 {
					t.Fatalf("CONNECT attempts=%d, want four (two IPs twice)", len(f.targets))
				}
			} else if kind != "unreachable" && f.queries < 2 {
				t.Fatalf("DoH failure not retried: %d", f.queries)
			}
		})
	}
}

func TestProxyTriesIPv6AfterIPv4Failure(t *testing.T) {
	f := newProxyFixture(t)
	f.failIPv4 = true
	if _, err := f.client.Get(t.Context(), "https://target.invalid/", nil); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.targets) != 2 || f.targets[0] != "1.1.1.1:443" || f.targets[1] != "[2606:4700:4700::1111]:443" {
		t.Fatalf("CONNECT order: %v", f.targets)
	}
}

func TestProxyDNSCacheTTLAndCapacity(t *testing.T) {
	f := newProxyFixture(t)
	for range 2 {
		if _, err := f.client.Get(t.Context(), "https://cached.invalid/", nil); err != nil {
			t.Fatal(err)
		}
	}
	f.mu.Lock()
	queries := f.queries
	f.mu.Unlock()
	if queries != 2 {
		t.Fatalf("uncached requests: %d", queries)
	}
	p := f.client.proxy
	p.mu.Lock()
	entry := p.cache["cached.invalid"]
	p.mu.Unlock()
	if time.Until(entry.expires) > 60*time.Second {
		t.Fatal("DNS cache exceeded 60s cap")
	}
	// Force expiration instead of waiting a minute in the regression suite.
	p.mu.Lock()
	entry.expires = time.Now().Add(-time.Second)
	p.cache["cached.invalid"] = entry
	p.mu.Unlock()
	if _, err := f.client.Get(t.Context(), "https://cached.invalid/", nil); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	queries = f.queries
	f.mu.Unlock()
	if queries != 4 {
		t.Fatalf("expired DNS result reused: %d", queries)
	}
	f.mu.Lock()
	f.ttl = 0
	f.mu.Unlock()
	for range 2 {
		if _, err := f.client.Get(t.Context(), "https://zero.invalid/", nil); err != nil {
			t.Fatal(err)
		}
	}
	f.mu.Lock()
	queries = f.queries
	f.mu.Unlock()
	if queries != 10 {
		t.Fatalf("zero TTL cached: %d", queries)
	} // checkURL and dial each resolve when TTL is zero.
	f.mu.Lock()
	f.ttl = 120
	f.mu.Unlock()
	for i := range 257 {
		if _, err := f.client.Get(t.Context(), fmt.Sprintf("https://cache-%d.invalid/", i), nil); err != nil {
			t.Fatal(err)
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.cache) != 256 {
		t.Fatalf("cache size=%d", len(p.cache))
	}
	if _, ok := p.cache["cache-0.invalid"]; ok {
		t.Fatal("oldest cache entry was not evicted")
	}
}

func TestProxyCancellation(t *testing.T) {
	for _, stage := range []string{"DoH", "CONNECT"} {
		t.Run(stage, func(t *testing.T) {
			f := newProxyFixture(t)
			if stage == "DoH" {
				f.blockDNS = true
			} else {
				f.blockConnect = true
			}
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
			defer cancel()
			started := time.Now()
			_, err := f.client.Get(ctx, "https://target.invalid/", nil)
			if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > 500*time.Millisecond {
				t.Fatalf("cancellation delayed: %v", err)
			}
		})
	}
}

func TestProxyRedirectDomainIsResolvedAndChecked(t *testing.T) {
	f := newProxyFixture(t)
	f.redirect = "https://private.invalid/"
	_, err := f.client.Get(t.Context(), "https://target.invalid/", nil)
	if !retry.IsPermanent(err) {
		t.Fatalf("private redirect not rejected: %v", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.targets) != 1 || f.queries != 4 {
		t.Fatalf("redirect bypassed DNS validation: targets=%v queries=%d", f.targets, f.queries)
	}
}

func TestProxyPlainHTTPAndAnonymousCONNECT(t *testing.T) {
	f := newProxyFixture(t)
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "target.invalid:8080" || r.Header.Get("Proxy-Authorization") != "" {
			t.Errorf("unsafe origin request: host=%s", r.Host)
		}
		_, _ = io.WriteString(w, "plain tunnel")
	}))
	defer plain.Close()
	f.origin.Close()
	f.origin = plain
	f.client.proxy.url.User = nil
	result, err := f.client.Get(t.Context(), "http://target.invalid:8080/feed", nil)
	if err != nil || string(result.Body) != "plain tunnel" {
		t.Fatalf("HTTP tunnel: %v, %q", err, result.Body)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.targets) != 1 || f.targets[0] != "1.1.1.1:8080" || f.auth[0] != "" {
		t.Fatalf("anonymous CONNECT: targets=%v auth=%v", f.targets, f.auth)
	}
}

func TestProxyHonorsShortRecordTTL(t *testing.T) {
	f := newProxyFixture(t)
	f.ttl = 5
	if _, err := f.client.Get(t.Context(), "https://short.invalid/", nil); err != nil {
		t.Fatal(err)
	}
	p := f.client.proxy
	p.mu.Lock()
	defer p.mu.Unlock()
	remaining := time.Until(p.cache["short.invalid"].expires)
	if remaining <= 0 || remaining > 5*time.Second {
		t.Fatalf("record TTL ignored: %s", remaining)
	}
}
