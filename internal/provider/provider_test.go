package provider

import (
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestResolveConfigAPIKeyFromProvider(t *testing.T) {
	t.Setenv("PFSENSE_ENDPOINT", "")
	t.Setenv("PFSENSE_API_KEY", "")

	resolved, diags := resolveConfig(providerConfig{
		Endpoint:    types.StringValue("https://pfsense.example.com/"),
		APIKey:      types.StringValue("secret"),
		InsecureTLS: types.BoolValue(true),
		Timeout:     types.StringValue("15s"),
	})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %#v", diags)
	}
	if resolved.Endpoint != "https://pfsense.example.com" {
		t.Fatalf("endpoint = %q", resolved.Endpoint)
	}
	if resolved.APIKey != "secret" {
		t.Fatalf("api key not resolved")
	}
	if !resolved.InsecureTLS {
		t.Fatalf("insecure tls not resolved")
	}
	if resolved.Timeout != 15*time.Second {
		t.Fatalf("timeout = %s", resolved.Timeout)
	}
}

func TestResolveConfigRequiresAuth(t *testing.T) {
	t.Setenv("PFSENSE_ENDPOINT", "https://pfsense.example.com")
	t.Setenv("PFSENSE_API_KEY", "")
	t.Setenv("PFSENSE_USERNAME", "")
	t.Setenv("PFSENSE_PASSWORD", "")

	_, diags := resolveConfig(providerConfig{})
	if !diags.HasError() {
		t.Fatalf("expected authentication diagnostic")
	}
}

func TestResolveConfigUsernamePasswordFromEnv(t *testing.T) {
	t.Setenv("PFSENSE_ENDPOINT", "https://pfsense.example.com")
	t.Setenv("PFSENSE_USERNAME", "api-user")
	t.Setenv("PFSENSE_PASSWORD", "api-pass")
	t.Setenv("PFSENSE_TIMEOUT", "45s")

	resolved, diags := resolveConfig(providerConfig{})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %#v", diags)
	}
	if resolved.Username != "api-user" || resolved.Password != "api-pass" {
		t.Fatalf("username/password not resolved")
	}
	if resolved.Timeout != 45*time.Second {
		t.Fatalf("timeout = %s", resolved.Timeout)
	}
}
