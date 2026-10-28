package fetch

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"sync"
	"time"
)

type dnsCacheEntry struct {
	ips      []netip.Addr
	expires  time.Time
	sequence uint64
}

type proxyTransport struct {
	url      *url.URL
	dohURL   string
	doh      *http.Client
	timeout  time.Duration
	mu       sync.Mutex
	cache    map[string]dnsCacheEntry
	sequence uint64
}

// Proxy failures are temporary network failures, without retaining credentials or remote text.
func proxyFailure() error {
	return &net.DNSError{Err: "fetch request failed", IsTemporary: true}
}

func newProxyTransport(rawURL string, timeout time.Duration) *proxyTransport {
	proxyURL, _ := url.Parse(rawURL) // Startup configuration has already validated the URL.
	return &proxyTransport{
		url: proxyURL, timeout: timeout, dohURL: "https://cloudflare-dns.com/dns-query",
		cache: make(map[string]dnsCacheEntry),
		doh:   &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL), IdleConnTimeout: 90 * time.Second}, Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}
}

func (p *proxyTransport) resolve(ctx context.Context, host string) ([]netip.Addr, error) {
	p.mu.Lock()
	entry, found := p.cache[host]
	p.mu.Unlock()
	if found && time.Now().Before(entry.expires) {
		return entry.ips, nil
	}
	var ips []netip.Addr
	ttl := 60 * time.Second
	for _, kind := range []string{"A", "AAAA"} {
		target, err := url.Parse(p.dohURL)
		if err != nil {
			return nil, proxyFailure()
		}
		query := target.Query()
		query.Set("name", host)
		query.Set("type", kind)
		target.RawQuery = query.Encode()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
		if err != nil {
			return nil, proxyFailure()
		}
		req.Header.Set("Accept", "application/dns-json")
		resp, err := p.doh.Do(req)
		if err != nil {
			return nil, proxyFailure()
		}
		var answer struct {
			Status int
			Answer []struct {
				Type int
				TTL  uint32
				Data string
			}
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
		resp.Body.Close()
		if err != nil || len(body) > 1<<20 || resp.StatusCode != http.StatusOK || json.Unmarshal(body, &answer) != nil || answer.Status != 0 {
			return nil, proxyFailure()
		}
		for _, record := range answer.Answer {
			if record.Type != 1 && record.Type != 28 {
				continue
			}
			ip, err := netip.ParseAddr(record.Data)
			if err != nil {
				return nil, proxyFailure()
			}
			ips = append(ips, ip.Unmap())
			ttl = min(ttl, time.Duration(record.TTL)*time.Second)
		}
	}
	if len(ips) == 0 {
		return nil, proxyFailure()
	}
	if err := checkAddresses(ips, false); err != nil {
		return nil, err
	}
	slices.SortStableFunc(ips, func(a, b netip.Addr) int {
		if a.Is4() == b.Is4() {
			return 0
		}
		if a.Is4() {
			return -1
		}
		return 1
	})
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.cache) >= 256 {
		var oldest string
		var sequence uint64
		for name, entry := range p.cache {
			if oldest == "" || entry.sequence < sequence {
				oldest, sequence = name, entry.sequence
			}
		}
		delete(p.cache, oldest)
	}
	p.sequence++
	p.cache[host] = dnsCacheEntry{ips: ips, expires: time.Now().Add(ttl), sequence: p.sequence}
	return ips, nil
}

func (p *proxyTransport) connect(ctx context.Context, network, address string) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, network, p.url.Host)
	if err != nil {
		return nil, proxyFailure()
	}
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	ok := false
	defer func() {
		stop()
		if !ok {
			conn.Close()
		}
	}()
	if deadline, has := ctx.Deadline(); has {
		_ = conn.SetDeadline(deadline)
	}
	req := &http.Request{Method: http.MethodConnect, URL: &url.URL{Opaque: address}, Host: address, Header: make(http.Header)}
	if p.url.User != nil {
		password, _ := p.url.User.Password()
		req.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(p.url.User.Username()+":"+password)))
	}
	if err := req.Write(conn); err != nil {
		return nil, proxyFailure()
	}
	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, req)
	if err != nil {
		return nil, proxyFailure()
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, proxyFailure()
	}
	if !stop() || ctx.Err() != nil {
		return nil, proxyFailure()
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		return nil, proxyFailure()
	}
	ok = true
	return &bufferedConn{Conn: conn, reader: reader}, nil
}

// Preserve bytes a proxy may have already sent after the CONNECT headers.
type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedConn) Read(b []byte) (int, error) { return c.reader.Read(b) }
