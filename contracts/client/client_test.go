package client

import (
	"context"
	"errors"
	"testing"
)

func TestConfigFromEnvAndNotConfigured(t *testing.T) {
	t.Setenv(EnvRPCURL, "")
	_, err := NewPolygonClientFromEnv()
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("expected not configured without env, got %v", err)
	}
}

func TestConfiguredPlaceholderStatus(t *testing.T) {
	c, err := NewPolygonClient(Config{
		RPCURL:            "http://127.0.0.1:8545",
		IdentityAddress:   "0x0000000000000000000000000000000000000001",
		CredentialAddress: "0x0000000000000000000000000000000000000002",
		GovernanceAddress: "0x0000000000000000000000000000000000000003",
		QCAddress:         "0x0000000000000000000000000000000000000004",
	})
	if err != nil {
		t.Fatal(err)
	}
	status, err := c.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !status.Configured || status.Backend != "polygon-placeholder" {
		t.Fatalf("unexpected status: %+v", status)
	}
}
