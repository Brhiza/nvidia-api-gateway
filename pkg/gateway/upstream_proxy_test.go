package gateway

import (
	"context"
	"net/http"
	"testing"
)

func TestBuildUpstreamProxyTransportInherit(t *testing.T) {
	transport, mode, err := buildUpstreamProxyTransport("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mode != upstreamProxyModeInherit {
		t.Fatalf("mode = %v, want %v", mode, upstreamProxyModeInherit)
	}
	if transport != nil {
		t.Fatal("expected inherit mode to return nil transport")
	}
}

func TestBuildUpstreamProxyTransportDirect(t *testing.T) {
	transport, mode, err := buildUpstreamProxyTransport("direct")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mode != upstreamProxyModeDirect {
		t.Fatalf("mode = %v, want %v", mode, upstreamProxyModeDirect)
	}
	if transport == nil {
		t.Fatal("expected direct transport")
	}
	if transport.Proxy != nil {
		t.Fatal("expected direct transport to disable proxy function")
	}
}

func TestBuildUpstreamProxyTransportHTTPProxy(t *testing.T) {
	transport, mode, err := buildUpstreamProxyTransport("http://proxy.example.com:8080")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mode != upstreamProxyModeProxy {
		t.Fatalf("mode = %v, want %v", mode, upstreamProxyModeProxy)
	}
	if transport == nil {
		t.Fatal("expected proxy transport")
	}
	if transport.Proxy == nil {
		t.Fatal("expected proxy function to be set")
	}
	req, err := http.NewRequest(http.MethodGet, "https://integrate.api.nvidia.com/v1/models", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	proxyURL, err := transport.Proxy(req)
	if err != nil {
		t.Fatalf("resolve proxy URL: %v", err)
	}
	if proxyURL == nil || proxyURL.String() != "http://proxy.example.com:8080" {
		t.Fatalf("proxy URL = %v, want http://proxy.example.com:8080", proxyURL)
	}
}

func TestBuildUpstreamProxyTransportSOCKS5Family(t *testing.T) {
	for _, raw := range []string{"socks5://proxy.example.com:1080", "socks5h://proxy.example.com:1080"} {
		transport, mode, err := buildUpstreamProxyTransport(raw)
		if err != nil {
			t.Fatalf("unexpected error for %s: %v", raw, err)
		}
		if mode != upstreamProxyModeProxy {
			t.Fatalf("mode = %v, want %v", mode, upstreamProxyModeProxy)
		}
		if transport == nil {
			t.Fatalf("expected transport for %s", raw)
		}
		if transport.Proxy != nil {
			t.Fatalf("expected SOCKS transport to bypass HTTP proxy function for %s", raw)
		}
		if transport.DialContext == nil {
			t.Fatalf("expected SOCKS transport to install custom dialer for %s", raw)
		}
	}
}

func TestResolveSOCKS5TargetResolvesLocally(t *testing.T) {
	lookups := 0
	resolved, err := resolveSOCKS5Target(context.Background(), "socks5", "example.com:443", func(_ context.Context, network, host string) ([]string, error) {
		lookups++
		if network != "ip" {
			t.Fatalf("network = %q, want ip", network)
		}
		if host != "example.com" {
			t.Fatalf("host = %q, want example.com", host)
		}
		return []string{"1.2.3.4"}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved != "1.2.3.4:443" {
		t.Fatalf("resolved = %q, want 1.2.3.4:443", resolved)
	}
	if lookups != 1 {
		t.Fatalf("lookups = %d, want 1", lookups)
	}
}

func TestResolveSOCKS5HTargetKeepsRemoteDNS(t *testing.T) {
	lookups := 0
	resolved, err := resolveSOCKS5Target(context.Background(), "socks5h", "example.com:443", func(context.Context, string, string) ([]string, error) {
		lookups++
		return []string{"1.2.3.4"}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved != "example.com:443" {
		t.Fatalf("resolved = %q, want example.com:443", resolved)
	}
	if lookups != 0 {
		t.Fatalf("expected no local DNS lookup for socks5h, got %d", lookups)
	}
}
