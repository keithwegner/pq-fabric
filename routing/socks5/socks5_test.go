package socks5

import (
	"context"
	"strings"
	"testing"
)

func TestProxyHandleRejectsUnsupportedAndDisallowed(t *testing.T) {
	proxy := Proxy{
		Allow: func(destination string) bool {
			return destination == "local-echo:7000"
		},
		Dialer: DialFunc(func(_ context.Context, destination string, payload []byte) ([]byte, error) {
			return append([]byte("ok:"), payload...), nil
		}),
	}
	if _, err := proxy.Handle(context.Background(), Request{Command: 0x02, Destination: "local-echo:7000"}); err == nil {
		t.Fatal("expected unsupported command to fail")
	}
	if _, err := proxy.Handle(context.Background(), Request{Command: cmdConnect, Destination: "example.com:80"}); err == nil || !strings.Contains(err.Error(), "rejected") {
		t.Fatalf("expected disallowed destination rejection, got %v", err)
	}
	response, err := proxy.Handle(context.Background(), Request{Command: cmdConnect, Destination: "local-echo:7000", Payload: []byte("hello")})
	if err != nil {
		t.Fatal(err)
	}
	if string(response) != "ok:hello" {
		t.Fatalf("unexpected response %q", string(response))
	}
}
