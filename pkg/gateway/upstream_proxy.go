package gateway

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	xproxy "golang.org/x/net/proxy"
)

type upstreamProxyMode int

const (
	upstreamProxyModeInherit upstreamProxyMode = iota
	upstreamProxyModeDirect
	upstreamProxyModeProxy
	upstreamProxyModeInvalid
)

type upstreamProxySetting struct {
	Raw  string
	Mode upstreamProxyMode
	URL  *url.URL
}

type hostLookupFunc func(ctx context.Context, network, host string) ([]string, error)

var defaultHostLookup hostLookupFunc = func(ctx context.Context, _ string, host string) ([]string, error) {
	return net.DefaultResolver.LookupHost(ctx, host)
}

func parseUpstreamProxySetting(raw string) (upstreamProxySetting, error) {
	trimmed := strings.TrimSpace(raw)
	setting := upstreamProxySetting{Raw: trimmed}

	if trimmed == "" {
		setting.Mode = upstreamProxyModeInherit
		return setting, nil
	}
	if strings.EqualFold(trimmed, "direct") || strings.EqualFold(trimmed, "none") {
		setting.Mode = upstreamProxyModeDirect
		return setting, nil
	}

	parsedURL, err := url.Parse(trimmed)
	if err != nil {
		setting.Mode = upstreamProxyModeInvalid
		return setting, fmt.Errorf("parse proxy URL failed: %w", err)
	}
	if strings.TrimSpace(parsedURL.Scheme) == "" || strings.TrimSpace(parsedURL.Host) == "" {
		setting.Mode = upstreamProxyModeInvalid
		return setting, fmt.Errorf("proxy URL missing scheme/host")
	}

	scheme := strings.ToLower(strings.TrimSpace(parsedURL.Scheme))
	switch scheme {
	case "http", "https", "socks5", "socks5h":
		parsedURL.Scheme = scheme
		setting.Mode = upstreamProxyModeProxy
		setting.URL = parsedURL
		return setting, nil
	default:
		setting.Mode = upstreamProxyModeInvalid
		return setting, fmt.Errorf("unsupported proxy scheme: %s", parsedURL.Scheme)
	}
}

func validateUpstreamProxySetting(raw string) error {
	_, err := parseUpstreamProxySetting(raw)
	return err
}

func cloneDefaultTransport() *http.Transport {
	if transport, ok := http.DefaultTransport.(*http.Transport); ok && transport != nil {
		return transport.Clone()
	}
	return &http.Transport{}
}

func buildUpstreamProxyTransport(raw string) (*http.Transport, upstreamProxyMode, error) {
	setting, err := parseUpstreamProxySetting(raw)
	if err != nil {
		return nil, setting.Mode, err
	}

	switch setting.Mode {
	case upstreamProxyModeInherit:
		return nil, setting.Mode, nil
	case upstreamProxyModeDirect:
		transport := cloneDefaultTransport()
		transport.Proxy = nil
		return transport, setting.Mode, nil
	case upstreamProxyModeProxy:
		transport := cloneDefaultTransport()
		switch setting.URL.Scheme {
		case "http", "https":
			transport.Proxy = http.ProxyURL(setting.URL)
			return transport, setting.Mode, nil
		case "socks5", "socks5h":
			dialContext, err := buildSOCKS5DialContext(setting.URL)
			if err != nil {
				return nil, setting.Mode, err
			}
			transport.Proxy = nil
			transport.DialContext = dialContext
			return transport, setting.Mode, nil
		}
	}

	return nil, setting.Mode, nil
}

func buildSOCKS5DialContext(proxyURL *url.URL) (func(context.Context, string, string) (net.Conn, error), error) {
	if proxyURL == nil {
		return nil, fmt.Errorf("proxy URL is nil")
	}
	var authCfg *xproxy.Auth
	if proxyURL.User != nil {
		username := proxyURL.User.Username()
		password, _ := proxyURL.User.Password()
		if username != "" || password != "" {
			authCfg = &xproxy.Auth{User: username, Password: password}
		}
	}
	forward := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
	dialer, err := xproxy.SOCKS5("tcp", proxyURL.Host, authCfg, forward)
	if err != nil {
		return nil, fmt.Errorf("create SOCKS5 dialer failed: %w", err)
	}
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		target, err := resolveSOCKS5Target(ctx, proxyURL.Scheme, address, defaultHostLookup)
		if err != nil {
			return nil, err
		}
		if ctxDialer, ok := dialer.(xproxy.ContextDialer); ok {
			return ctxDialer.DialContext(ctx, network, target)
		}
		return dialer.Dial(network, target)
	}, nil
}

func resolveSOCKS5Target(ctx context.Context, scheme, address string, lookup hostLookupFunc) (string, error) {
	if strings.ToLower(strings.TrimSpace(scheme)) != "socks5" {
		return address, nil
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return "", err
	}
	if net.ParseIP(host) != nil {
		return address, nil
	}
	if lookup == nil {
		lookup = defaultHostLookup
	}
	addrs, err := lookup(ctx, "ip", host)
	if err != nil {
		return "", err
	}
	if len(addrs) == 0 {
		return "", fmt.Errorf("no ip address resolved for %s", host)
	}
	return net.JoinHostPort(addrs[0], port), nil
}
