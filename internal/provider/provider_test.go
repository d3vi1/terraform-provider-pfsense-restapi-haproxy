package provider

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	providerfw "github.com/hashicorp/terraform-plugin-framework/provider"
	providerschema "github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflogtest"
)

func TestResolveConfigAPIKeyFromProvider(t *testing.T) {
	clearProviderEnv(t)

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

func TestResolveConfigEnvironmentFallbackWithExplicitNulls(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv("PFSENSE_ENDPOINT", " https://pfsense-env.example.com/ ")
	t.Setenv("PFSENSE_API_KEY", " env-secret ")
	t.Setenv("PFSENSE_INSECURE_TLS", "true")
	t.Setenv("PFSENSE_TIMEOUT", "45s")

	resolved, diags := resolveConfig(providerConfig{
		Endpoint:    types.StringNull(),
		APIKey:      types.StringNull(),
		InsecureTLS: types.BoolNull(),
		Timeout:     types.StringNull(),
	})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %#v", diags)
	}
	if resolved.Endpoint != "https://pfsense-env.example.com" {
		t.Fatalf("endpoint = %q", resolved.Endpoint)
	}
	if resolved.APIKey != "env-secret" {
		t.Fatalf("api key = %q", resolved.APIKey)
	}
	if !resolved.InsecureTLS {
		t.Fatalf("insecure tls not resolved from environment")
	}
	if resolved.Timeout != 45*time.Second {
		t.Fatalf("timeout = %s", resolved.Timeout)
	}
}

func TestResolveConfigProviderValuesOverrideEnvironment(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv("PFSENSE_ENDPOINT", "https://pfsense-env.example.com")
	t.Setenv("PFSENSE_API_KEY", "env-secret")
	t.Setenv("PFSENSE_INSECURE_TLS", "true")
	t.Setenv("PFSENSE_TIMEOUT", "45s")

	resolved, diags := resolveConfig(providerConfig{
		Endpoint:    types.StringValue("https://pfsense-provider.example.com/"),
		APIKey:      types.StringValue("provider-secret"),
		InsecureTLS: types.BoolValue(false),
		Timeout:     types.StringValue("5s"),
	})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %#v", diags)
	}
	if resolved.Endpoint != "https://pfsense-provider.example.com" {
		t.Fatalf("endpoint = %q", resolved.Endpoint)
	}
	if resolved.APIKey != "provider-secret" {
		t.Fatalf("api key = %q", resolved.APIKey)
	}
	if resolved.InsecureTLS {
		t.Fatalf("insecure tls should use provider value")
	}
	if resolved.Timeout != 5*time.Second {
		t.Fatalf("timeout = %s", resolved.Timeout)
	}
}

func TestResolveConfigRequiresAuth(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv("PFSENSE_ENDPOINT", "https://pfsense.example.com")

	_, diags := resolveConfig(providerConfig{})
	if !diags.HasError() {
		t.Fatalf("expected authentication diagnostic")
	}
}

func TestResolveConfigUsernamePasswordFromEnv(t *testing.T) {
	clearProviderEnv(t)
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

func TestResolveConfigRejectsInvalidBoolEnv(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv("PFSENSE_ENDPOINT", "https://pfsense.example.com")
	t.Setenv("PFSENSE_API_KEY", "secret")
	t.Setenv("PFSENSE_INSECURE_TLS", "not-a-bool")

	_, diags := resolveConfig(providerConfig{})
	if !diags.HasError() {
		t.Fatalf("expected invalid boolean diagnostic")
	}
}

func TestResolveConfigRejectsInvalidTimeout(t *testing.T) {
	for _, timeout := range []string{"not-a-duration", "0s", "-1s"} {
		t.Run(timeout, func(t *testing.T) {
			clearProviderEnv(t)

			_, diags := resolveConfig(providerConfig{
				Endpoint: types.StringValue("https://pfsense.example.com"),
				APIKey:   types.StringValue("secret"),
				Timeout:  types.StringValue(timeout),
			})
			if !diags.HasError() {
				t.Fatalf("expected invalid timeout diagnostic")
			}
		})
	}
}

func TestResolveConfigMultipleAuthenticationWarning(t *testing.T) {
	clearProviderEnv(t)

	_, diags := resolveConfig(providerConfig{
		Endpoint: types.StringValue("https://pfsense.example.com"),
		APIKey:   types.StringValue("secret"),
		Username: types.StringValue("api-user"),
		Password: types.StringValue("api-pass"),
	})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %#v", diags)
	}
	if diags.WarningsCount() != 1 {
		t.Fatalf("warnings count = %d", diags.WarningsCount())
	}
}

func TestResolveConfigPartialUsernamePasswordRequiresBoth(t *testing.T) {
	tests := map[string]providerConfig{
		"username only": {
			Endpoint: types.StringValue("https://pfsense.example.com"),
			Username: types.StringValue("api-user"),
		},
		"password only": {
			Endpoint: types.StringValue("https://pfsense.example.com"),
			Password: types.StringValue("api-pass"),
		},
	}

	for name, config := range tests {
		t.Run(name, func(t *testing.T) {
			clearProviderEnv(t)

			_, diags := resolveConfig(config)
			if !diags.HasError() {
				t.Fatalf("expected partial username/password diagnostic")
			}
		})
	}
}

func TestResolveConfigRejectsInvalidEndpoint(t *testing.T) {
	for _, endpoint := range []string{
		"pfsense.example.com",
		"ftp://pfsense.example.com",
		"https://",
		"://pfsense.example.com",
		"https://user:pass@pfsense.example.com",
		"https://pfsense.example.com?query=1",
		"https://pfsense.example.com#fragment",
	} {
		t.Run(endpoint, func(t *testing.T) {
			clearProviderEnv(t)

			_, diags := resolveConfig(providerConfig{
				Endpoint: types.StringValue(endpoint),
				APIKey:   types.StringValue("secret"),
			})
			if !diags.HasError() {
				t.Fatalf("expected invalid endpoint diagnostic")
			}
		})
	}
}

func TestResolveConfigUnknownValuesAreDiagnostics(t *testing.T) {
	tests := map[string]providerConfig{
		"endpoint": {
			Endpoint: types.StringUnknown(),
			APIKey:   types.StringValue("secret"),
		},
		"api_key": {
			Endpoint: types.StringValue("https://pfsense.example.com"),
			APIKey:   types.StringUnknown(),
		},
		"insecure_tls": {
			Endpoint:    types.StringValue("https://pfsense.example.com"),
			APIKey:      types.StringValue("secret"),
			InsecureTLS: types.BoolUnknown(),
		},
		"timeout": {
			Endpoint: types.StringValue("https://pfsense.example.com"),
			APIKey:   types.StringValue("secret"),
			Timeout:  types.StringUnknown(),
		},
	}

	for name, config := range tests {
		t.Run(name, func(t *testing.T) {
			clearProviderEnv(t)

			_, diags := resolveConfig(config)
			if !diags.HasError() {
				t.Fatalf("expected unknown value diagnostic")
			}
		})
	}
}

func TestProviderSchemaSensitiveAttributes(t *testing.T) {
	schema := providerSchema(t)
	expectedSensitivity := map[string]bool{
		"endpoint":     false,
		"api_key":      true,
		"username":     false,
		"password":     true,
		"insecure_tls": false,
		"timeout":      false,
	}

	for name, expected := range expectedSensitivity {
		attr, ok := schema.Attributes[name]
		if !ok {
			t.Fatalf("schema missing %q", name)
		}
		if attr.IsSensitive() != expected {
			t.Fatalf("%s sensitivity = %t", name, attr.IsSensitive())
		}
	}
}

func TestProviderSchemaDoesNotExposeAutoApplyYet(t *testing.T) {
	schema := providerSchema(t)
	if _, ok := schema.Attributes["auto_apply"]; ok {
		t.Fatalf("auto_apply is reserved and should not be exposed until apply semantics are implemented")
	}
}

func TestLogInsecureTLSWarning(t *testing.T) {
	var output bytes.Buffer
	ctx := tflogtest.RootLogger(context.Background(), &output)

	logInsecureTLSWarning(ctx, true)
	got := output.String()
	if !strings.Contains(got, `"@level":"warn"`) {
		t.Fatalf("expected warn log, got %q", got)
	}
	if !strings.Contains(got, `"@message":"TLS certificate verification is disabled"`) {
		t.Fatalf("expected insecure TLS warning message, got %q", got)
	}

	output.Reset()
	logInsecureTLSWarning(ctx, false)
	if output.Len() != 0 {
		t.Fatalf("expected no log when insecure_tls is false, got %q", output.String())
	}
}

func clearProviderEnv(t *testing.T) {
	t.Helper()

	for _, name := range []string{
		"PFSENSE_ENDPOINT",
		"PFSENSE_API_KEY",
		"PFSENSE_USERNAME",
		"PFSENSE_PASSWORD",
		"PFSENSE_INSECURE_TLS",
		"PFSENSE_TIMEOUT",
	} {
		t.Setenv(name, "")
	}
}

func providerSchema(t *testing.T) providerschema.Schema {
	t.Helper()

	var resp providerfw.SchemaResponse
	(&haproxyProvider{}).Schema(context.Background(), providerfw.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected schema diagnostics: %#v", resp.Diagnostics)
	}

	return resp.Schema
}
