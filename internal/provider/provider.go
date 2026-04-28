package provider

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/d3vi1/terraform-provider-pfsense-restapi-haproxy/internal/pfsense"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var _ provider.Provider = (*haproxyProvider)(nil)

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &haproxyProvider{version: version}
	}
}

type haproxyProvider struct {
	version string
}

type providerConfig struct {
	Endpoint    types.String `tfsdk:"endpoint"`
	APIKey      types.String `tfsdk:"api_key"`
	Username    types.String `tfsdk:"username"`
	Password    types.String `tfsdk:"password"`
	InsecureTLS types.Bool   `tfsdk:"insecure_tls"`
	Timeout     types.String `tfsdk:"timeout"`
}

type resolvedConfig struct {
	Endpoint    string
	APIKey      string
	Username    string
	Password    string
	InsecureTLS bool
	Timeout     time.Duration
}

func (p *haproxyProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "pfsense-haproxy"
	resp.Version = p.version
}

func (p *haproxyProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Provider for managing pfSense HAProxy package configuration through pfSense-pkg-RESTAPI.",
		Attributes: map[string]schema.Attribute{
			"endpoint": schema.StringAttribute{
				Description: "pfSense REST API endpoint, for example https://192.168.51.254.",
				Optional:    true,
			},
			"api_key": schema.StringAttribute{
				Description: "pfSense REST API key. Preferred for automation.",
				Optional:    true,
				Sensitive:   true,
			},
			"username": schema.StringAttribute{
				Description: "pfSense username. Use only when API key authentication is unavailable.",
				Optional:    true,
			},
			"password": schema.StringAttribute{
				Description: "pfSense password. Use only when API key authentication is unavailable.",
				Optional:    true,
				Sensitive:   true,
			},
			"insecure_tls": schema.BoolAttribute{
				Description: "Skip TLS certificate verification. Intended for lab/self-signed pfSense deployments.",
				Optional:    true,
			},
			"timeout": schema.StringAttribute{
				Description: "HTTP client timeout, for example 30s.",
				Optional:    true,
			},
		},
	}
}

func (p *haproxyProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config providerConfig
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resolved, diags := resolveConfig(config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	logInsecureTLSWarning(ctx, resolved.InsecureTLS)

	client := pfsense.NewClient(pfsense.Config{
		Endpoint:    resolved.Endpoint,
		APIKey:      resolved.APIKey,
		Username:    resolved.Username,
		Password:    resolved.Password,
		InsecureTLS: resolved.InsecureTLS,
		Timeout:     resolved.Timeout,
	})

	resp.DataSourceData = client
	resp.ResourceData = client
}

func (p *haproxyProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{}
}

func (p *haproxyProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{}
}

func resolveConfig(config providerConfig) (resolvedConfig, diag.Diagnostics) {
	var diags diag.Diagnostics

	endpoint, d := stringOrEnv(config.Endpoint, "endpoint", "PFSENSE_ENDPOINT")
	endpointHasError := d.HasError()
	diags.Append(d...)
	apiKey, d := stringOrEnv(config.APIKey, "api_key", "PFSENSE_API_KEY")
	apiKeyHasError := d.HasError()
	diags.Append(d...)
	username, d := stringOrEnv(config.Username, "username", "PFSENSE_USERNAME")
	usernameHasError := d.HasError()
	diags.Append(d...)
	password, d := stringOrEnv(config.Password, "password", "PFSENSE_PASSWORD")
	passwordHasError := d.HasError()
	diags.Append(d...)
	insecureTLS, d := boolOrEnv(config.InsecureTLS, "PFSENSE_INSECURE_TLS")
	diags.Append(d...)

	timeout, d := durationOrEnv(config.Timeout, "PFSENSE_TIMEOUT", 30*time.Second)
	diags.Append(d...)

	endpoint = normalizeEndpoint(endpoint)
	if endpoint == "" && !endpointHasError {
		diags.AddError("Missing endpoint", "Set endpoint in the provider configuration or PFSENSE_ENDPOINT environment variable.")
	} else if endpoint != "" {
		diags.Append(validateEndpoint(endpoint)...)
	}

	if !apiKeyHasError && !usernameHasError && !passwordHasError {
		if apiKey == "" {
			switch {
			case username == "" && password == "":
				diags.AddError("Missing authentication", "Set api_key/PFSENSE_API_KEY or username/password via provider configuration or environment variables.")
			case username == "":
				diags.AddError("Incomplete username/password authentication", "Set username/PFSENSE_USERNAME when password/PFSENSE_PASSWORD is configured, or use api_key/PFSENSE_API_KEY.")
			case password == "":
				diags.AddError("Incomplete username/password authentication", "Set password/PFSENSE_PASSWORD when username/PFSENSE_USERNAME is configured, or use api_key/PFSENSE_API_KEY.")
			}
		}
		if apiKey != "" && (username != "" || password != "") {
			diags.AddWarning("Multiple authentication methods configured", "api_key takes precedence over username/password authentication.")
		}
	}

	return resolvedConfig{
		Endpoint:    endpoint,
		APIKey:      apiKey,
		Username:    username,
		Password:    password,
		InsecureTLS: insecureTLS,
		Timeout:     timeout,
	}, diags
}

func stringOrEnv(value types.String, attrName string, envName string) (string, diag.Diagnostics) {
	var diags diag.Diagnostics
	if value.IsUnknown() {
		diags.AddError("Invalid configuration", fmt.Sprintf("%s is unknown", attrName))
		return "", diags
	}
	if !value.IsNull() {
		return strings.TrimSpace(value.ValueString()), diags
	}
	return strings.TrimSpace(os.Getenv(envName)), diags
}

func boolOrEnv(value types.Bool, envName string) (bool, diag.Diagnostics) {
	var diags diag.Diagnostics
	if value.IsUnknown() {
		diags.AddError("Invalid configuration", fmt.Sprintf("%s is unknown", envName))
		return false, diags
	}
	if !value.IsNull() {
		return value.ValueBool(), diags
	}
	raw := strings.TrimSpace(os.Getenv(envName))
	if raw == "" {
		return false, diags
	}
	parsed, err := strconv.ParseBool(raw)
	if err != nil {
		diags.AddError("Invalid boolean", fmt.Sprintf("%s=%q is not a valid boolean", envName, raw))
		return false, diags
	}
	return parsed, diags
}

func durationOrEnv(value types.String, envName string, fallback time.Duration) (time.Duration, diag.Diagnostics) {
	var diags diag.Diagnostics
	if value.IsUnknown() {
		diags.AddError("Invalid timeout", "timeout is unknown")
		return fallback, diags
	}
	raw := ""
	if !value.IsNull() {
		raw = value.ValueString()
	} else {
		raw = os.Getenv(envName)
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback, diags
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		diags.AddError("Invalid timeout", fmt.Sprintf("%q is not a valid duration", raw))
		return fallback, diags
	}
	if parsed <= 0 {
		diags.AddError("Invalid timeout", "timeout must be greater than zero.")
		return fallback, diags
	}
	return parsed, diags
}

func normalizeEndpoint(endpoint string) string {
	return strings.TrimRight(strings.TrimSpace(endpoint), "/")
}

func validateEndpoint(endpoint string) diag.Diagnostics {
	var diags diag.Diagnostics

	parsed, err := url.Parse(endpoint)
	if err != nil {
		diags.AddError("Invalid endpoint", fmt.Sprintf("endpoint must be a valid absolute http(s) URL: %v", err))
		return diags
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		diags.AddError("Invalid endpoint", "endpoint must be an absolute http(s) URL, for example https://pfsense.example.com.")
		return diags
	}
	switch parsed.Scheme {
	case "http", "https":
	default:
		diags.AddError("Invalid endpoint", fmt.Sprintf("endpoint scheme must be http or https, got %q.", parsed.Scheme))
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		diags.AddError("Invalid endpoint", "endpoint must not include a query string or fragment.")
	}

	return diags
}

func logInsecureTLSWarning(ctx context.Context, insecureTLS bool) {
	if insecureTLS {
		tflog.Warn(ctx, "TLS certificate verification is disabled")
	}
}
