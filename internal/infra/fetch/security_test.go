package fetch_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"

	"github.com/bogeia/vane/internal/config"
	"github.com/bogeia/vane/internal/infra/fetch"
	"github.com/bogeia/vane/internal/infra/retry"
)

func TestClientRejectsNonPublicAddresses(t *testing.T) {
	addresses := []struct {
		name string
		ip   string
	}{
		{"IPv4 loopback", "127.0.0.1"},
		{"IPv6 loopback", "::1"},
		{"IPv4 private", "10.20.30.40"},
		{"IPv6 private", "fd12::1"},
		{"IPv4 link local", "169.254.1.2"},
		{"IPv6 link local", "fe80::1"},
		{"CGNAT", "100.64.0.1"},
		{"IPv4 multicast", "224.0.0.1"},
		{"IPv6 multicast", "ff02::1"},
		{"mapped private IPv4", "::ffff:10.20.30.40"},
	}
	for _, test := range addresses {
		t.Run(test.name, func(t *testing.T) {
			ip := netip.MustParseAddr(test.ip)
			target := "http://" + net.JoinHostPort(ip.String(), "80")
			for _, allowLoopback := range []bool{false, true} {
				if _, err := fetch.New(allowLoopback, config.Fetch{RequestTimeout: 20 * time.Second, Retry: config.Retry{MaxAttempts: 3, InitialInterval: time.Millisecond, MaxInterval: 16 * time.Millisecond}}).Get(t.Context(), target, nil); err == nil {
					t.Errorf("client allowLoopback=%v accepted %s", allowLoopback, ip)
				}
			}
		})
	}
}

func TestClientRejectsSpecialPurposeAddresses(t *testing.T) {
	for _, address := range []string{
		"::127.0.0.1", "100::1", "2001:db8::1", "192.0.0.1", "192.0.2.1",
		"198.18.0.1", "198.51.100.1", "203.0.113.1", "0.1.2.3", "240.0.0.1",
		"::8.8.8.8", "::ffff:ffff", "100::", "100::ffff:ffff:ffff:ffff",
		"2001:db8::", "2001:db8:ffff:ffff:ffff:ffff:ffff:ffff",
		"192.0.0.0", "192.0.0.255", "192.0.2.0", "192.0.2.255",
		"198.18.0.0", "198.19.255.255", "198.51.100.0", "198.51.100.255",
		"203.0.113.0", "203.0.113.255",
		"64:ff9b::7f00:1", "64:ff9b:1::1", "2002:7f00:1::1", "2001::1", "fec0::1",
	} {
		t.Run(address, func(t *testing.T) {
			target := "http://" + net.JoinHostPort(address, "80") + "/resource"
			cfg := config.Fetch{RequestTimeout: time.Second, Retry: config.Retry{MaxAttempts: 3, InitialInterval: time.Millisecond, MaxInterval: time.Millisecond}}
			for _, allowLoopback := range []bool{false, true} {
				_, err := fetch.New(allowLoopback, cfg).Get(t.Context(), target, nil)
				if !retry.IsPermanent(err) || err.Error() != "fetch request rejected" {
					t.Fatalf("allowLoopback=%v: want security rejection, got %v", allowLoopback, err)
				}
			}
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				http.Redirect(w, r, target, http.StatusFound)
			}))
			defer server.Close()
			_, err := fetch.New(true, cfg).Get(t.Context(), server.URL, nil)
			if !retry.IsPermanent(err) || err.Error() != "fetch request rejected" || requests.Load() != 1 {
				t.Fatalf("redirect: requests=%d err=%v", requests.Load(), err)
			}
		})
	}
}

func TestClientChecksRedirectsAndAllowsAtMostFive(t *testing.T) {
	t.Run("rejects private redirect even when loopback testing is enabled", func(t *testing.T) {
		var requests atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests.Add(1)
			http.Redirect(w, r, "http://10.20.30.40/resource", http.StatusFound)
		}))
		defer server.Close()

		if _, err := fetch.New(true, config.Fetch{RequestTimeout: 20 * time.Second, Retry: config.Retry{MaxAttempts: 3, InitialInterval: time.Millisecond, MaxInterval: 16 * time.Millisecond}}).Get(t.Context(), server.URL, nil); err == nil {
			t.Fatal("client followed a redirect to a private address")
		}
		if got := requests.Load(); got != 1 {
			t.Fatalf("requests reached local server = %d, want 1", got)
		}
	})

	t.Run("allows five redirects", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			step, err := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/"))
			if err != nil {
				t.Errorf("invalid redirect path %q", r.URL.Path)
				return
			}
			if step < 5 {
				http.Redirect(w, r, fmt.Sprintf("/%d", step+1), http.StatusFound)
				return
			}
			_, _ = io.WriteString(w, "done")
		}))
		defer server.Close()

		result, err := fetch.New(true, config.Fetch{RequestTimeout: 20 * time.Second, Retry: config.Retry{MaxAttempts: 3, InitialInterval: time.Millisecond, MaxInterval: 16 * time.Millisecond}}).Get(t.Context(), server.URL+"/0", nil)
		if err != nil {
			t.Fatalf("client rejected five redirects: %v", err)
		}
		if result.URL != server.URL+"/5" || string(result.Body) != "done" {
			t.Fatalf("final response URL/body = %q/%q", result.URL, result.Body)
		}
	})

	t.Run("rejects a sixth redirect", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			step, err := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/"))
			if err != nil {
				t.Errorf("invalid redirect path %q", r.URL.Path)
				return
			}
			http.Redirect(w, r, fmt.Sprintf("/%d", step+1), http.StatusFound)
		}))
		defer server.Close()

		if _, err := fetch.New(true, config.Fetch{RequestTimeout: 20 * time.Second, Retry: config.Retry{MaxAttempts: 3, InitialInterval: time.Millisecond, MaxInterval: 16 * time.Millisecond}}).Get(t.Context(), server.URL+"/0", nil); err == nil {
			t.Fatal("client followed more than five redirects")
		}
	})
}

func TestClientLimitsBodyAndHonorsRequestDeadline(t *testing.T) {
	t.Run("accepts exactly five MiB", func(t *testing.T) {
		const limit = 5 << 20
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Length", strconv.Itoa(limit))
			_, _ = w.Write(make([]byte, limit))
		}))
		defer server.Close()

		result, err := fetch.New(true, config.Fetch{RequestTimeout: 20 * time.Second, Retry: config.Retry{MaxAttempts: 3, InitialInterval: time.Millisecond, MaxInterval: 16 * time.Millisecond}}).Get(t.Context(), server.URL, nil)
		if err != nil {
			t.Fatalf("client rejected body at size limit: %v", err)
		}
		if len(result.Body) != limit {
			t.Fatalf("body size = %d, want %d", len(result.Body), limit)
		}
	})

	t.Run("rejects chunked body larger than five MiB", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			flusher := w.(http.Flusher)
			chunk := make([]byte, 64<<10)
			for sent := 0; sent < (5<<20)+1; sent += len(chunk) {
				if _, err := w.Write(chunk); err != nil {
					return
				}
				flusher.Flush()
				select {
				case <-r.Context().Done():
					return
				default:
				}
			}
		}))
		defer server.Close()

		if _, err := fetch.New(true, config.Fetch{RequestTimeout: 20 * time.Second, Retry: config.Retry{MaxAttempts: 3, InitialInterval: time.Millisecond, MaxInterval: 16 * time.Millisecond}}).Get(t.Context(), server.URL, nil); err == nil {
			t.Fatal("client accepted a chunked body larger than five MiB")
		}
	})

	t.Run("returns when caller deadline expires", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			<-r.Context().Done()
		}))
		defer server.Close()
		ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		defer cancel()

		if _, err := fetch.New(true, config.Fetch{RequestTimeout: 20 * time.Second, Retry: config.Retry{MaxAttempts: 3, InitialInterval: time.Millisecond, MaxInterval: 16 * time.Millisecond}}).Get(ctx, server.URL, nil); err == nil {
			t.Fatal("client ignored caller deadline")
		}
	})
}

func TestClientRequiresHTTPOrHTTPS(t *testing.T) {
	for _, rawURL := range []string{"ftp://example.com/feed", "file:///etc/passwd", "http:relative"} {
		t.Run(rawURL, func(t *testing.T) {
			if _, err := fetch.New(true, config.Fetch{RequestTimeout: 20 * time.Second, Retry: config.Retry{MaxAttempts: 3, InitialInterval: time.Millisecond, MaxInterval: 16 * time.Millisecond}}).Get(t.Context(), rawURL, nil); err == nil {
				t.Fatalf("client accepted unsupported URL %q", rawURL)
			}
		})
	}
}

func TestClientRejectsURLCredentials(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = io.WriteString(w, "unexpected request")
	}))
	defer server.Close()
	target := strings.Replace(server.URL, "http://", "http://user:pass@", 1)

	if _, err := fetch.New(true, config.Fetch{RequestTimeout: 20 * time.Second, Retry: config.Retry{MaxAttempts: 3, InitialInterval: time.Millisecond, MaxInterval: 16 * time.Millisecond}}).Get(t.Context(), target, nil); err == nil {
		t.Fatal("client accepted URL credentials")
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("requests reached server = %d, want 0", got)
	}
}

func TestClientEnforcesConfiguredTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	started := time.Now()

	if _, err := fetch.New(true, config.Fetch{RequestTimeout: 10 * time.Millisecond, Retry: config.Retry{MaxAttempts: 3, InitialInterval: time.Millisecond, MaxInterval: 16 * time.Millisecond}}).Get(t.Context(), server.URL, nil); err == nil {
		t.Fatal("client exceeded its configured request timeout")
	}
	if elapsed := time.Since(started); elapsed < 25*time.Millisecond || elapsed > time.Second {
		t.Fatalf("client timeout elapsed %s, want three short bounded requests", elapsed)
	}
}

func TestClientRejectsMixedAndRebindingDNSAnswers(t *testing.T) {
	t.Run("rejects mixed public and private answers", func(t *testing.T) {
		restore := useTestResolver(t, func(q dnsmessage.Question, _ int) []netip.Addr {
			if q.Type == dnsmessage.TypeA {
				return []netip.Addr{netip.MustParseAddr("8.8.8.8")}
			}
			if q.Type == dnsmessage.TypeAAAA {
				return []netip.Addr{netip.MustParseAddr("fd12::1")}
			}
			return nil
		})
		defer restore()

		if _, err := fetch.New(false, config.Fetch{RequestTimeout: 20 * time.Second, Retry: config.Retry{MaxAttempts: 3, InitialInterval: time.Millisecond, MaxInterval: 16 * time.Millisecond}}).Get(t.Context(), "http://mixed.test:80/feed", nil); err == nil {
			t.Fatal("client accepted mixed public and private DNS answers")
		}
	})

	t.Run("rejects a private answer after a public preflight answer", func(t *testing.T) {
		var lookups atomic.Int32
		restore := useTestResolver(t, func(q dnsmessage.Question, _ int) []netip.Addr {
			if q.Type != dnsmessage.TypeA {
				return nil
			}
			if lookups.Add(1) == 1 {
				return []netip.Addr{netip.MustParseAddr("8.8.8.8")}
			}
			return []netip.Addr{netip.MustParseAddr("10.20.30.40")}
		})
		defer restore()

		if _, err := fetch.New(false, config.Fetch{RequestTimeout: 20 * time.Second, Retry: config.Retry{MaxAttempts: 3, InitialInterval: time.Millisecond, MaxInterval: 16 * time.Millisecond}}).Get(t.Context(), "http://rebind.test:80/feed", nil); err == nil {
			t.Fatal("client accepted a private DNS answer after public preflight")
		}
		if got := lookups.Load(); got < 2 {
			t.Fatalf("DNS lookups = %d, want preflight and dial checks", got)
		}
	})
}

func useTestResolver(t *testing.T, answer func(dnsmessage.Question, int) []netip.Addr) func() {
	t.Helper()
	server, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal("start test DNS server")
	}
	var requests atomic.Int32
	go func() {
		buffer := make([]byte, 1500)
		for {
			n, peer, err := server.ReadFrom(buffer)
			if err != nil {
				return
			}
			var query dnsmessage.Message
			if err := query.Unpack(buffer[:n]); err != nil || len(query.Questions) == 0 {
				continue
			}
			response := dnsmessage.Message{Header: dnsmessage.Header{ID: query.ID, Response: true, Authoritative: true}, Questions: query.Questions}
			for _, question := range query.Questions {
				for _, ip := range answer(question, int(requests.Add(1))) {
					header := dnsmessage.ResourceHeader{Name: question.Name, Class: dnsmessage.ClassINET, TTL: 1}
					switch {
					case question.Type == dnsmessage.TypeA && ip.Is4():
						header.Type = dnsmessage.TypeA
						response.Answers = append(response.Answers, dnsmessage.Resource{Header: header, Body: &dnsmessage.AResource{A: ip.As4()}})
					case question.Type == dnsmessage.TypeAAAA && ip.Is6():
						header.Type = dnsmessage.TypeAAAA
						response.Answers = append(response.Answers, dnsmessage.Resource{Header: header, Body: &dnsmessage.AAAAResource{AAAA: ip.As16()}})
					}
				}
			}
			packed, err := response.Pack()
			if err == nil {
				_, _ = server.WriteTo(packed, peer)
			}
		}
	}()

	previous := net.DefaultResolver
	address := server.LocalAddr().String()
	net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}}
	return func() {
		net.DefaultResolver = previous
		_ = server.Close()
	}
}
