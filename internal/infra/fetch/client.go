package fetch

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"time"

	"github.com/bogeia/vane/internal/config"
	"github.com/bogeia/vane/internal/infra/retry"
)

const maxBody = 5 << 20

type proxyRequestContextKey struct{}

type Result struct {
	Body       []byte
	Header     http.Header
	StatusCode int
	URL        string
}

type Client struct {
	client        *http.Client
	allowLoopback bool
	cfg           config.Fetch
	proxy         *proxyTransport
}

// New only permits loopback for local test servers when explicitly requested.
func New(allowLoopback bool, cfg config.Fetch) *Client {
	c := &Client{allowLoopback: allowLoopback, cfg: cfg}
	if cfg.ProxyURL != "" {
		c.proxy = newProxyTransport(cfg.ProxyURL, cfg.RequestTimeout)
	}
	transport := &http.Transport{Proxy: nil, ForceAttemptHTTP2: true, IdleConnTimeout: 90 * time.Second}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if c.proxy != nil {
			// Transport detaches dial cancellation; retain the caller budget for DoH and CONNECT.
			if requestCtx, ok := ctx.Value(proxyRequestContextKey{}).(context.Context); ok {
				ctx = requestCtx
			}
		}
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := c.resolve(ctx, host)
		if err != nil {
			return nil, err
		}
		var last error
		for _, ip := range ips {
			// Dial the checked literal address; never resolve the hostname a second time here.
			var connection net.Conn
			var err error
			if c.proxy != nil {
				connection, err = c.proxy.connect(ctx, network, net.JoinHostPort(ip.String(), port))
			} else {
				connection, err = (&net.Dialer{Timeout: cfg.RequestTimeout}).DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			}
			if err == nil {
				return connection, nil
			}
			last = err
		}
		return nil, last
	}
	c.client = &http.Client{
		Transport: transport, Timeout: cfg.RequestTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 5 {
				return retry.Permanent(fmt.Errorf("too many redirects"))
			}
			return c.checkURL(req.Context(), req.URL)
		},
	}
	return c
}

func (c *Client) Get(ctx context.Context, rawURL string, headers http.Header) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, retry.Budget(c.cfg.RequestTimeout, retry.Policy(c.cfg.Retry)))
	defer cancel()
	return retry.Do(ctx, retry.Policy(c.cfg.Retry), func() (Result, error) { return c.get(ctx, rawURL, headers) })
}

func (c *Client) get(ctx context.Context, rawURL string, headers http.Header) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, c.cfg.RequestTimeout)
	defer cancel()
	requestCtx := ctx
	if c.proxy != nil {
		requestCtx = context.WithValue(ctx, proxyRequestContextKey{}, ctx)
	}
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return Result{}, retry.Permanent(fmt.Errorf("invalid fetch URL"))
	}
	if err := c.checkURL(ctx, req.URL); err != nil {
		if ctx.Err() != nil {
			return Result{}, ctx.Err()
		}
		if retry.IsPermanent(err) {
			return Result{}, retry.Permanent(fmt.Errorf("fetch request rejected"))
		}
		if retry.Network(err) {
			return Result{}, fmt.Errorf("fetch request failed")
		}
		return Result{}, retry.Permanent(fmt.Errorf("fetch request failed"))
	}
	req.Header = headers.Clone()
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	req.Header.Set("User-Agent", "Vane/0.1 RSS collector")
	resp, err := c.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return Result{}, ctx.Err()
		}
		if retry.IsPermanent(err) {
			return Result{}, retry.Permanent(fmt.Errorf("fetch request rejected"))
		}
		if retry.Network(err) {
			return Result{}, fmt.Errorf("fetch request failed")
		}
		return Result{}, retry.Permanent(fmt.Errorf("fetch request failed"))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotModified && (resp.StatusCode < 200 || resp.StatusCode >= 300) {
		return Result{}, retry.HTTPError(fmt.Errorf("fetch returned HTTP %d", resp.StatusCode), resp.StatusCode, resp.Header)
	}
	if resp.ContentLength > maxBody {
		return Result{}, retry.Permanent(fmt.Errorf("response exceeds 5 MB"))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		if ctx.Err() != nil {
			return Result{}, ctx.Err()
		}
		if retry.IsPermanent(err) {
			return Result{}, retry.Permanent(fmt.Errorf("fetch request rejected"))
		}
		if retry.Network(err) {
			return Result{}, fmt.Errorf("fetch request failed")
		}
		return Result{}, retry.Permanent(fmt.Errorf("fetch request failed"))
	}
	if len(body) > maxBody {
		return Result{}, retry.Permanent(fmt.Errorf("response exceeds 5 MB"))
	}
	return Result{Body: body, Header: resp.Header.Clone(), StatusCode: resp.StatusCode, URL: resp.Request.URL.String()}, nil
}

func (c *Client) checkURL(ctx context.Context, target *url.URL) error {
	if (target.Scheme != "http" && target.Scheme != "https") || target.Hostname() == "" || target.User != nil {
		return retry.Permanent(fmt.Errorf("fetch URL must be absolute HTTP(S) without credentials"))
	}
	_, err := c.resolve(ctx, target.Hostname())
	return err
}

var deniedNetworks = []netip.Prefix{
	netip.MustParsePrefix("::/96"), netip.MustParsePrefix("100::/64"), netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("fec0::/10"), netip.MustParsePrefix("64:ff9b::/96"), netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("2002::/16"), netip.MustParsePrefix("2001::/32"),
}

func (c *Client) resolve(ctx context.Context, host string) ([]netip.Addr, error) {
	if c.proxy != nil {
		if ip, err := netip.ParseAddr(host); err == nil {
			ips := []netip.Addr{ip.Unmap()}
			return ips, checkAddresses(ips, false)
		}
		return c.proxy.resolve(ctx, host)
	}
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	return ips, checkAddresses(ips, c.allowLoopback)
}

func checkAddresses(ips []netip.Addr, allowLoopback bool) error {
	if len(ips) == 0 {
		return retry.Permanent(fmt.Errorf("fetch host has no addresses"))
	}
	for _, ip := range ips {
		ip = ip.Unmap()
		if allowLoopback && ip.IsLoopback() {
			continue
		}
		blocked := !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.Zone() != ""
		for _, prefix := range deniedNetworks {
			blocked = blocked || prefix.Contains(ip)
		}
		if blocked {
			return retry.Permanent(fmt.Errorf("fetch address is not public"))
		}
	}
	return nil
}
