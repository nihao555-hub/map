package web

import "testing"

func TestProxiedMediaURL(t *testing.T) {
	in := "https://lh3.googleusercontent.com/p/abc=w400"
	got := ProxiedMediaURL(in)
	if got == in || got == "" {
		t.Fatalf("expected proxied url, got %q", got)
	}
	if got[:len("/api/v1/media?u=")] != "/api/v1/media?u=" {
		t.Fatalf("got %q", got)
	}
	// 非白名单原样
	if ProxiedMediaURL("https://evil.example/x.png") != "https://evil.example/x.png" {
		t.Fatal("non-allowlisted should pass through")
	}
	if ProxiedMediaURL("") != "" {
		t.Fatal("empty")
	}
}

func TestFirstProxyLine(t *testing.T) {
	raw := "# comment\n\nsocks5://u:p@xray:1180\nsocks5://u:p@xray:1181\n"
	if got := firstProxyLine(raw); got != "socks5://u:p@xray:1180" {
		t.Fatalf("got %q", got)
	}
}
