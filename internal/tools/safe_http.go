package tools

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultHTTPTimeout      = 15 * time.Second
	defaultHTTPMaxBodyBytes = int64(512 * 1024)
	defaultHTTPMaxRedirects = 5
	defaultHTTPUserAgent    = "Xarlatan/0.2"
)

// HTTPErrorCode classifies failures without requiring callers to parse text.
type HTTPErrorCode string

const (
	HTTPPolicyViolation   HTTPErrorCode = "policy_violation"
	HTTPNetworkFailure    HTTPErrorCode = "network_failure"
	HTTPContextCancelled  HTTPErrorCode = "context_cancelled"
	HTTPTimeout           HTTPErrorCode = "timeout"
	HTTPRedirectLimit     HTTPErrorCode = "redirect_limit"
	HTTPStatusFailure     HTTPErrorCode = "http_status"
	HTTPBodyLimitExceeded HTTPErrorCode = "body_limit"
	HTTPContentTypeDenied HTTPErrorCode = "content_type"
)

// HTTPError is the typed error returned by SafeHTTPClient.
type HTTPError struct {
	Code       HTTPErrorCode
	URL        string
	StatusCode int
	Err        error
}

func (e *HTTPError) Error() string {
	if e == nil {
		return "HTTP error"
	}
	parts := []string{string(e.Code)}
	if e.URL != "" {
		parts = append(parts, e.URL)
	}
	if e.StatusCode != 0 {
		parts = append(parts, fmt.Sprintf("status=%d", e.StatusCode))
	}
	if e.Err != nil {
		parts = append(parts, e.Err.Error())
	}
	return strings.Join(parts, ": ")
}

func (e *HTTPError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// IsHTTPErrorCode reports whether err contains an HTTPError with the given code.
func IsHTTPErrorCode(err error, code HTTPErrorCode) bool {
	var target *HTTPError
	return errors.As(err, &target) && target.Code == code
}

// SafeHTTPConfig contains the fixed security bounds for outbound web tools.
type SafeHTTPConfig struct {
	Timeout      time.Duration
	MaxBodyBytes int64
	MaxRedirects int
	UserAgent    string
}

// DefaultSafeHTTPConfig returns the production defaults used by all web tools.
func DefaultSafeHTTPConfig() SafeHTTPConfig {
	return SafeHTTPConfig{
		Timeout:      defaultHTTPTimeout,
		MaxBodyBytes: defaultHTTPMaxBodyBytes,
		MaxRedirects: defaultHTTPMaxRedirects,
		UserAgent:    defaultHTTPUserAgent,
	}
}

type ipResolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

type dialContextFunc func(context.Context, string, string) (net.Conn, error)

// SafeHTTPClient validates every effective destination before connecting.
type SafeHTTPClient struct {
	config   SafeHTTPConfig
	resolver ipResolver
	dial     dialContextFunc
	client   *http.Client
}

// SafeHTTPResponse is a bounded response returned by SafeHTTPClient.
type SafeHTTPResponse struct {
	Body        []byte
	StatusCode  int
	ContentType string
	FinalURL    string
	Truncated   bool
}

// NewSafeHTTPClient builds a production client with controlled DNS and dialing.
func NewSafeHTTPClient(config SafeHTTPConfig) (*SafeHTTPClient, error) {
	dialer := &net.Dialer{
		Timeout:   5 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	return newSafeHTTPClient(config, net.DefaultResolver, dialer.DialContext)
}

func newSafeHTTPClient(config SafeHTTPConfig, resolver ipResolver, dial dialContextFunc) (*SafeHTTPClient, error) {
	if config.Timeout <= 0 {
		return nil, fmt.Errorf("HTTP timeout must be greater than zero")
	}
	if config.MaxBodyBytes <= 0 {
		return nil, fmt.Errorf("HTTP max body bytes must be greater than zero")
	}
	if config.MaxRedirects < 0 {
		return nil, fmt.Errorf("HTTP max redirects must not be negative")
	}
	if strings.TrimSpace(config.UserAgent) == "" {
		config.UserAgent = defaultHTTPUserAgent
	}
	if resolver == nil {
		return nil, fmt.Errorf("HTTP resolver is nil")
	}
	if dial == nil {
		return nil, fmt.Errorf("HTTP dialer is nil")
	}

	safe := &SafeHTTPClient{
		config:   config,
		resolver: resolver,
		dial:     dial,
	}
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           safe.dialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          20,
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: config.Timeout,
		ExpectContinueTimeout: time.Second,
	}
	safe.client = &http.Client{
		Timeout:       config.Timeout,
		Transport:     transport,
		CheckRedirect: safe.checkRedirect,
	}
	return safe, nil
}

var defaultSafeHTTPClient = mustSafeHTTPClient(DefaultSafeHTTPConfig())

func mustSafeHTTPClient(config SafeHTTPConfig) *SafeHTTPClient {
	client, err := NewSafeHTTPClient(config)
	if err != nil {
		panic(err)
	}
	return client
}

// Get performs a bounded GET after validating URL, DNS, dial target and redirects.
func (c *SafeHTTPClient) Get(
	ctx context.Context,
	rawURL string,
	headers map[string]string,
	maxBytes int64,
) (SafeHTTPResponse, error) {
	if c == nil || c.client == nil {
		return SafeHTTPResponse{}, &HTTPError{
			Code: HTTPNetworkFailure,
			URL:  rawURL,
			Err:  fmt.Errorf("safe HTTP client is not initialized"),
		}
	}
	parsed, err := validateOutboundURL(rawURL)
	if err != nil {
		return SafeHTTPResponse{}, err
	}
	if maxBytes <= 0 || maxBytes > c.config.MaxBodyBytes {
		maxBytes = c.config.MaxBodyBytes
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return SafeHTTPResponse{}, &HTTPError{Code: HTTPPolicyViolation, URL: rawURL, Err: err}
	}
	req.Header.Set("User-Agent", c.config.UserAgent)
	req.Header.Set("Accept", "text/html,text/plain,application/json,application/xml;q=0.9")
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return SafeHTTPResponse{}, classifyHTTPClientError(ctx, rawURL, err)
	}
	defer resp.Body.Close()

	finalURL := rawURL
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return SafeHTTPResponse{}, &HTTPError{
			Code:       HTTPStatusFailure,
			URL:        finalURL,
			StatusCode: resp.StatusCode,
			Err:        fmt.Errorf("unexpected HTTP status"),
		}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return SafeHTTPResponse{}, classifyHTTPClientError(ctx, finalURL, err)
	}
	truncated := int64(len(body)) > maxBytes
	if truncated {
		body = body[:maxBytes]
	}

	contentType, err := allowedResponseContentType(resp.Header.Get("Content-Type"), body)
	if err != nil {
		return SafeHTTPResponse{}, &HTTPError{
			Code: HTTPContentTypeDenied,
			URL:  finalURL,
			Err:  err,
		}
	}

	return SafeHTTPResponse{
		Body:        body,
		StatusCode:  resp.StatusCode,
		ContentType: contentType,
		FinalURL:    finalURL,
		Truncated:   truncated,
	}, nil
}

func classifyHTTPClientError(ctx context.Context, rawURL string, err error) error {
	var typed *HTTPError
	if errors.As(err, &typed) {
		return typed
	}
	if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		return &HTTPError{Code: HTTPContextCancelled, URL: rawURL, Err: context.Canceled}
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return &HTTPError{Code: HTTPTimeout, URL: rawURL, Err: context.DeadlineExceeded}
	}
	return &HTTPError{Code: HTTPNetworkFailure, URL: rawURL, Err: err}
}

func validateOutboundURL(rawURL string) (*url.URL, error) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return nil, &HTTPError{Code: HTTPPolicyViolation, URL: rawURL, Err: fmt.Errorf("URL is empty")}
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return nil, &HTTPError{Code: HTTPPolicyViolation, URL: rawURL, Err: err}
	}
	if u.Opaque != "" || !u.IsAbs() || u.Host == "" {
		return nil, &HTTPError{
			Code: HTTPPolicyViolation,
			URL:  rawURL,
			Err:  fmt.Errorf("URL must be absolute and include a host"),
		}
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, &HTTPError{
			Code: HTTPPolicyViolation,
			URL:  rawURL,
			Err:  fmt.Errorf("unsupported URL scheme %q", u.Scheme),
		}
	}
	if u.User != nil {
		return nil, &HTTPError{
			Code: HTTPPolicyViolation,
			URL:  rawURL,
			Err:  fmt.Errorf("embedded URL credentials are not allowed"),
		}
	}
	hostname := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if hostname == "" {
		return nil, &HTTPError{Code: HTTPPolicyViolation, URL: rawURL, Err: fmt.Errorf("URL host is empty")}
	}
	if strings.Contains(hostname, "%") {
		return nil, &HTTPError{Code: HTTPPolicyViolation, URL: rawURL, Err: fmt.Errorf("IPv6 zones are not allowed")}
	}
	if hostname == "localhost" || strings.HasSuffix(hostname, ".localhost") {
		return nil, &HTTPError{Code: HTTPPolicyViolation, URL: rawURL, Err: fmt.Errorf("localhost is blocked")}
	}
	if port := u.Port(); port != "" {
		value, parseErr := strconv.Atoi(port)
		if parseErr != nil || value < 1 || value > 65535 {
			return nil, &HTTPError{Code: HTTPPolicyViolation, URL: rawURL, Err: fmt.Errorf("invalid port %q", port)}
		}
	}
	if literal := net.ParseIP(hostname); literal != nil {
		if reason := blockedOutboundIPReason(literal); reason != "" {
			return nil, &HTTPError{Code: HTTPPolicyViolation, URL: rawURL, Err: fmt.Errorf("blocked IP: %s", reason)}
		}
	}
	return u, nil
}

func blockedOutboundIPReason(ip net.IP) string {
	if ip == nil {
		return "invalid address"
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	if !ip.IsGlobalUnicast() {
		return "address is not global unicast"
	}
	if ip.IsPrivate() {
		return "private address"
	}
	if ipInCIDR(ip, "100.64.0.0/10") {
		return "carrier-grade NAT address"
	}
	for _, blocked := range []string{
		"169.254.169.254",
		"169.254.170.2",
		"100.100.100.200",
		"192.0.0.192",
		"fd00:ec2::254",
	} {
		if ip.Equal(net.ParseIP(blocked)) {
			return "metadata service address"
		}
	}
	return ""
}

func ipInCIDR(ip net.IP, rawCIDR string) bool {
	_, network, err := net.ParseCIDR(rawCIDR)
	return err == nil && network.Contains(ip)
}

func (c *SafeHTTPClient) resolveAllowedIPs(ctx context.Context, hostname string) ([]net.IP, error) {
	if literal := net.ParseIP(hostname); literal != nil {
		if reason := blockedOutboundIPReason(literal); reason != "" {
			return nil, &HTTPError{Code: HTTPPolicyViolation, URL: hostname, Err: fmt.Errorf("blocked IP: %s", reason)}
		}
		return []net.IP{literal}, nil
	}

	addresses, err := c.resolver.LookupIPAddr(ctx, hostname)
	if err != nil {
		return nil, classifyHTTPClientError(ctx, hostname, err)
	}
	if len(addresses) == 0 {
		return nil, &HTTPError{Code: HTTPNetworkFailure, URL: hostname, Err: fmt.Errorf("DNS returned no addresses")}
	}

	seen := make(map[string]struct{}, len(addresses))
	resolved := make([]net.IP, 0, len(addresses))
	for _, address := range addresses {
		ip := address.IP
		if reason := blockedOutboundIPReason(ip); reason != "" {
			return nil, &HTTPError{
				Code: HTTPPolicyViolation,
				URL:  hostname,
				Err:  fmt.Errorf("DNS resolved to blocked IP %s: %s", ip, reason),
			}
		}
		key := ip.String()
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		resolved = append(resolved, ip)
	}
	return resolved, nil
}

func (c *SafeHTTPClient) dialContext(ctx context.Context, network, address string) (net.Conn, error) {
	hostname, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, &HTTPError{Code: HTTPPolicyViolation, URL: address, Err: err}
	}
	addresses, err := c.resolveAllowedIPs(ctx, hostname)
	if err != nil {
		return nil, err
	}

	var failures []error
	for _, ip := range addresses {
		conn, dialErr := c.dial(ctx, network, net.JoinHostPort(ip.String(), port))
		if dialErr == nil {
			return conn, nil
		}
		if ctx.Err() != nil {
			return nil, classifyHTTPClientError(ctx, address, dialErr)
		}
		failures = append(failures, dialErr)
	}
	return nil, &HTTPError{
		Code: HTTPNetworkFailure,
		URL:  address,
		Err:  errors.Join(failures...),
	}
}

func (c *SafeHTTPClient) checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) > c.config.MaxRedirects {
		return &HTTPError{
			Code: HTTPRedirectLimit,
			URL:  req.URL.String(),
			Err:  fmt.Errorf("maximum redirects exceeded"),
		}
	}
	if _, err := validateOutboundURL(req.URL.String()); err != nil {
		return err
	}
	if len(via) > 0 && !sameAuthority(via[len(via)-1].URL, req.URL) {
		for _, header := range []string{
			"Authorization",
			"Proxy-Authorization",
			"Cookie",
			"X-Subscription-Token",
			"X-Api-Key",
			"Api-Key",
		} {
			req.Header.Del(header)
		}
	}
	return nil
}

func sameAuthority(left, right *url.URL) bool {
	if left == nil || right == nil {
		return false
	}
	return strings.EqualFold(left.Hostname(), right.Hostname()) && effectivePort(left) == effectivePort(right)
}

func effectivePort(u *url.URL) string {
	if u == nil {
		return ""
	}
	if port := u.Port(); port != "" {
		return port
	}
	if strings.EqualFold(u.Scheme, "https") {
		return "443"
	}
	if strings.EqualFold(u.Scheme, "http") {
		return "80"
	}
	return ""
}

func allowedResponseContentType(raw string, body []byte) (string, error) {
	if strings.TrimSpace(raw) == "" {
		raw = http.DetectContentType(body)
	}
	mediaType, _, err := mime.ParseMediaType(raw)
	if err != nil {
		return "", fmt.Errorf("invalid content type %q: %w", raw, err)
	}
	mediaType = strings.ToLower(mediaType)
	if strings.HasPrefix(mediaType, "text/") ||
		mediaType == "application/json" ||
		mediaType == "application/xml" ||
		mediaType == "application/xhtml+xml" ||
		mediaType == "application/javascript" ||
		mediaType == "application/x-ndjson" ||
		strings.HasSuffix(mediaType, "+json") ||
		strings.HasSuffix(mediaType, "+xml") {
		return mediaType, nil
	}
	return "", fmt.Errorf("content type %q is not allowed", mediaType)
}
