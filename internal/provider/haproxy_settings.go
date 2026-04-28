package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/d3vi1/terraform-provider-pfsense-restapi-haproxy/internal/pfsense"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	datasourceschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const (
	haproxySettingsID   = "settings"
	haproxySettingsPath = "/services/haproxy/settings"
)

var (
	_ datasource.DataSource              = (*haproxySettingsDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*haproxySettingsDataSource)(nil)
	_ resource.Resource                  = (*haproxySettingsResource)(nil)
	_ resource.ResourceWithConfigure     = (*haproxySettingsResource)(nil)
	_ resource.ResourceWithImportState   = (*haproxySettingsResource)(nil)
)

type settingsAttributeKind string

const (
	settingsAttributeBool   settingsAttributeKind = "bool"
	settingsAttributeInt64  settingsAttributeKind = "int64"
	settingsAttributeString settingsAttributeKind = "string"
)

type settingsAttribute struct {
	Name        string
	JSONName    string
	Kind        settingsAttributeKind
	Description string
	Sensitive   bool
}

var haproxySettingsAttributes = []settingsAttribute{
	{Name: "enable", JSONName: "enable", Kind: settingsAttributeBool, Description: "Enable or disable HAProxy on the firewall."},
	{Name: "maxconn", JSONName: "maxconn", Kind: settingsAttributeInt64, Description: "Maximum per-process number of concurrent connections. Null leaves the pfSense package default in place."},
	{Name: "nbthread", JSONName: "nbthread", Kind: settingsAttributeInt64, Description: "Number of HAProxy threads to start per process."},
	{Name: "terminate_on_reload", JSONName: "terminate_on_reload", Kind: settingsAttributeBool, Description: "Immediately stop old HAProxy processes on reload."},
	{Name: "hard_stop_after", JSONName: "hard_stop_after", Kind: settingsAttributeString, Description: "Maximum time allowed for a clean soft stop, such as 30s, 15m, 3h, or 1d."},
	{Name: "carpdev", JSONName: "carpdev", Kind: settingsAttributeString, Description: "CARP interface IP to monitor so HAProxy runs only on the MASTER firewall."},
	{Name: "localstatsport", JSONName: "localstatsport", Kind: settingsAttributeInt64, Description: "Internal port used for the HAProxy stats tab. Null disables local stats."},
	{Name: "localstats_refreshtime", JSONName: "localstats_refreshtime", Kind: settingsAttributeInt64, Description: "Local stats refresh interval in seconds."},
	{Name: "localstats_sticktable_refreshtime", JSONName: "localstats_sticktable_refreshtime", Kind: settingsAttributeInt64, Description: "Stick table stats refresh interval in seconds."},
	{Name: "remotesyslog", JSONName: "remotesyslog", Kind: settingsAttributeString, Description: "Remote syslog destination for HAProxy logs, or /var/run/log for local pfSense logs."},
	{Name: "logfacility", JSONName: "logfacility", Kind: settingsAttributeString, Description: "Syslog facility used by HAProxy."},
	{Name: "loglevel", JSONName: "loglevel", Kind: settingsAttributeString, Description: "Minimum HAProxy log level to emit."},
	{Name: "log_send_hostname", JSONName: "log-send-hostname", Kind: settingsAttributeString, Description: "Hostname included in HAProxy syslog headers. Empty uses the system hostname."},
	{Name: "resolver_retries", JSONName: "resolver_retries", Kind: settingsAttributeInt64, Description: "Number of DNS queries HAProxy sends before giving up."},
	{Name: "resolver_timeoutretry", JSONName: "resolver_timeoutretry", Kind: settingsAttributeString, Description: "Time between DNS retry queries when no response is received."},
	{Name: "resolver_holdvalid", JSONName: "resolver_holdvalid", Kind: settingsAttributeString, Description: "Interval between successive name resolutions after a valid answer."},
	{Name: "email_level", JSONName: "email_level", Kind: settingsAttributeString, Description: "Maximum log level for SMTP alerts. Empty disables email alerts."},
	{Name: "email_myhostname", JSONName: "email_myhostname", Kind: settingsAttributeString, Description: "Hostname HAProxy uses as the email origin."},
	{Name: "email_from", JSONName: "email_from", Kind: settingsAttributeString, Description: "Sender email address for HAProxy SMTP alerts."},
	{Name: "email_to", JSONName: "email_to", Kind: settingsAttributeString, Description: "Recipient email address for HAProxy SMTP alerts."},
	{Name: "sslcompatibilitymode", JSONName: "sslcompatibilitymode", Kind: settingsAttributeString, Description: "SSL/TLS compatibility mode: auto, modern, intermediate, or old."},
	{Name: "ssldefaultdhparam", JSONName: "ssldefaultdhparam", Kind: settingsAttributeInt64, Description: "Default Diffie-Hellman parameter size."},
	{Name: "advanced", JSONName: "advanced", Kind: settingsAttributeString, Description: "Base64-encoded additional HAProxy global settings. This may contain sensitive material.", Sensitive: true},
	{Name: "enablesync", JSONName: "enablesync", Kind: settingsAttributeBool, Description: "Include HAProxy configuration in pfSense HA sync when configured."},
}

type haproxySettingsDataSource struct {
	client *pfsense.Client
}

type haproxySettingsResource struct {
	client *pfsense.Client
}

type haproxySettingsModel struct {
	ID                              types.String `tfsdk:"id"`
	Enable                          types.Bool   `tfsdk:"enable"`
	Maxconn                         types.Int64  `tfsdk:"maxconn"`
	Nbthread                        types.Int64  `tfsdk:"nbthread"`
	TerminateOnReload               types.Bool   `tfsdk:"terminate_on_reload"`
	HardStopAfter                   types.String `tfsdk:"hard_stop_after"`
	Carpdev                         types.String `tfsdk:"carpdev"`
	LocalStatsPort                  types.Int64  `tfsdk:"localstatsport"`
	LocalStatsRefreshTime           types.Int64  `tfsdk:"localstats_refreshtime"`
	LocalStatsStickTableRefreshTime types.Int64  `tfsdk:"localstats_sticktable_refreshtime"`
	RemoteSyslog                    types.String `tfsdk:"remotesyslog"`
	LogFacility                     types.String `tfsdk:"logfacility"`
	LogLevel                        types.String `tfsdk:"loglevel"`
	LogSendHostname                 types.String `tfsdk:"log_send_hostname"`
	ResolverRetries                 types.Int64  `tfsdk:"resolver_retries"`
	ResolverTimeoutRetry            types.String `tfsdk:"resolver_timeoutretry"`
	ResolverHoldValid               types.String `tfsdk:"resolver_holdvalid"`
	EmailLevel                      types.String `tfsdk:"email_level"`
	EmailMyHostname                 types.String `tfsdk:"email_myhostname"`
	EmailFrom                       types.String `tfsdk:"email_from"`
	EmailTo                         types.String `tfsdk:"email_to"`
	SSLCompatibilityMode            types.String `tfsdk:"sslcompatibilitymode"`
	SSLDefaultDHParam               types.Int64  `tfsdk:"ssldefaultdhparam"`
	Advanced                        types.String `tfsdk:"advanced"`
	EnableSync                      types.Bool   `tfsdk:"enablesync"`
}

func newHaproxySettingsDataSource() datasource.DataSource {
	return &haproxySettingsDataSource{}
}

func newHaproxySettingsResource() resource.Resource {
	return &haproxySettingsResource{}
}

func (d *haproxySettingsDataSource) Metadata(_ context.Context, _ datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "pfsense_haproxy_settings"
}

func (d *haproxySettingsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = datasourceschema.Schema{
		Description: "Reads the singleton pfSense HAProxy package settings object.",
		Attributes:  haproxySettingsDataSourceSchemaAttributes(),
	}
}

func (d *haproxySettingsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*pfsense.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected data source configure type", fmt.Sprintf("Expected *pfsense.Client, got %T.", req.ProviderData))
		return
	}

	d.client = client
}

func (d *haproxySettingsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		resp.Diagnostics.AddError("Missing pfSense client", "The provider was not configured before reading pfsense_haproxy_settings.")
		return
	}

	settings, err := readHaproxySettings(ctx, d.client)
	if err != nil {
		resp.Diagnostics.AddError("Read HAProxy settings failed", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, settings)...)
}

func (r *haproxySettingsResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "pfsense_haproxy_settings"
}

func (r *haproxySettingsResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = resourceschema.Schema{
		Description: "Manages selected scalar fields from the singleton pfSense HAProxy package settings object. The resource is import-first and never creates or deletes the remote settings object.",
		Attributes:  haproxySettingsResourceSchemaAttributes(),
	}
}

func (r *haproxySettingsResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*pfsense.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected resource configure type", fmt.Sprintf("Expected *pfsense.Client, got %T.", req.ProviderData))
		return
	}

	r.client = client
}

func (r *haproxySettingsResource) Create(_ context.Context, _ resource.CreateRequest, resp *resource.CreateResponse) {
	resp.Diagnostics.AddError(
		"Import required for HAProxy settings",
		"pfsense_haproxy_settings is a singleton settings object that already exists on pfSense. Import it with ID \"settings\" before managing selected fields. Create makes no pfSense REST API write.",
	)
}

func (r *haproxySettingsResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing pfSense client", "The provider was not configured before reading pfsense_haproxy_settings.")
		return
	}

	var state haproxySettingsModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !state.ID.IsNull() && !state.ID.IsUnknown() && state.ID.ValueString() != haproxySettingsID {
		resp.Diagnostics.AddError("Invalid HAProxy settings ID", fmt.Sprintf("Expected ID %q, got %q.", haproxySettingsID, state.ID.ValueString()))
		return
	}

	settings, err := readHaproxySettings(ctx, r.client)
	if err != nil {
		resp.Diagnostics.AddError("Read HAProxy settings failed", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, settings)...)
}

func (r *haproxySettingsResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing pfSense client", "The provider was not configured before updating pfsense_haproxy_settings.")
		return
	}

	var plan, prior haproxySettingsModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !plan.ID.IsNull() && !plan.ID.IsUnknown() && plan.ID.ValueString() != haproxySettingsID {
		resp.Diagnostics.AddError("Invalid HAProxy settings ID", fmt.Sprintf("Expected ID %q, got %q.", haproxySettingsID, plan.ID.ValueString()))
		return
	}
	if !prior.ID.IsNull() && !prior.ID.IsUnknown() && prior.ID.ValueString() != haproxySettingsID {
		resp.Diagnostics.AddError("Invalid HAProxy settings ID", fmt.Sprintf("Expected ID %q, got %q.", haproxySettingsID, prior.ID.ValueString()))
		return
	}
	if resp.Diagnostics.HasError() {
		return
	}

	patch := buildHaproxySettingsPatch(plan, prior)
	if len(patch) > 0 {
		if err := r.client.Patch(ctx, haproxySettingsPath, patch, nil); err != nil {
			resp.Diagnostics.AddError("Update HAProxy settings failed", err.Error())
			return
		}
	}

	settings, err := readHaproxySettings(ctx, r.client)
	if err != nil {
		resp.Diagnostics.AddError("Read HAProxy settings after update failed", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, settings)...)
}

func (r *haproxySettingsResource) Delete(ctx context.Context, _ resource.DeleteRequest, resp *resource.DeleteResponse) {
	resp.State.RemoveResource(ctx)
}

func (r *haproxySettingsResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if req.ID != haproxySettingsID {
		resp.Diagnostics.AddError("Invalid HAProxy settings import ID", fmt.Sprintf("Import pfsense_haproxy_settings with the fixed ID %q.", haproxySettingsID))
		return
	}

	settings := nullHaproxySettingsModel()
	settings.ID = types.StringValue(haproxySettingsID)
	resp.Diagnostics.Append(resp.State.Set(ctx, settings)...)
}

func haproxySettingsDataSourceSchemaAttributes() map[string]datasourceschema.Attribute {
	attributes := map[string]datasourceschema.Attribute{
		"id": datasourceschema.StringAttribute{
			Computed:    true,
			Description: "Fixed singleton ID for the HAProxy settings object.",
		},
	}

	for _, attribute := range haproxySettingsAttributes {
		switch attribute.Kind {
		case settingsAttributeBool:
			attributes[attribute.Name] = datasourceschema.BoolAttribute{
				Computed:    true,
				Description: attribute.Description,
				Sensitive:   attribute.Sensitive,
			}
		case settingsAttributeInt64:
			attributes[attribute.Name] = datasourceschema.Int64Attribute{
				Computed:    true,
				Description: attribute.Description,
				Sensitive:   attribute.Sensitive,
			}
		case settingsAttributeString:
			attributes[attribute.Name] = datasourceschema.StringAttribute{
				Computed:    true,
				Description: attribute.Description,
				Sensitive:   attribute.Sensitive,
			}
		}
	}

	return attributes
}

func haproxySettingsResourceSchemaAttributes() map[string]resourceschema.Attribute {
	attributes := map[string]resourceschema.Attribute{
		"id": resourceschema.StringAttribute{
			Computed:    true,
			Description: "Fixed singleton ID for the HAProxy settings object. Import must use \"settings\".",
		},
	}

	for _, attribute := range haproxySettingsAttributes {
		switch attribute.Kind {
		case settingsAttributeBool:
			attributes[attribute.Name] = resourceschema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: attribute.Description,
				Sensitive:   attribute.Sensitive,
			}
		case settingsAttributeInt64:
			attributes[attribute.Name] = resourceschema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Description: attribute.Description,
				Sensitive:   attribute.Sensitive,
			}
		case settingsAttributeString:
			attributes[attribute.Name] = resourceschema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: attribute.Description,
				Sensitive:   attribute.Sensitive,
			}
		}
	}

	return attributes
}

func readHaproxySettings(ctx context.Context, client *pfsense.Client) (haproxySettingsModel, error) {
	var payload map[string]any
	if err := client.Get(ctx, haproxySettingsPath, &payload); err != nil {
		return haproxySettingsModel{}, err
	}

	return haproxySettingsModelFromAPI(payload)
}

func haproxySettingsModelFromAPI(payload map[string]any) (haproxySettingsModel, error) {
	settings := nullHaproxySettingsModel()
	settings.ID = types.StringValue(haproxySettingsID)

	var err error
	if settings.Enable, err = apiBool(payload, "enable"); err != nil {
		return settings, err
	}
	if settings.Maxconn, err = apiInt64(payload, "maxconn"); err != nil {
		return settings, err
	}
	if settings.Nbthread, err = apiInt64(payload, "nbthread"); err != nil {
		return settings, err
	}
	if settings.TerminateOnReload, err = apiBool(payload, "terminate_on_reload"); err != nil {
		return settings, err
	}
	if settings.HardStopAfter, err = apiString(payload, "hard_stop_after"); err != nil {
		return settings, err
	}
	if settings.Carpdev, err = apiString(payload, "carpdev"); err != nil {
		return settings, err
	}
	if settings.LocalStatsPort, err = apiInt64(payload, "localstatsport"); err != nil {
		return settings, err
	}
	if settings.LocalStatsRefreshTime, err = apiInt64(payload, "localstats_refreshtime"); err != nil {
		return settings, err
	}
	if settings.LocalStatsStickTableRefreshTime, err = apiInt64(payload, "localstats_sticktable_refreshtime"); err != nil {
		return settings, err
	}
	if settings.RemoteSyslog, err = apiString(payload, "remotesyslog"); err != nil {
		return settings, err
	}
	if settings.LogFacility, err = apiString(payload, "logfacility"); err != nil {
		return settings, err
	}
	if settings.LogLevel, err = apiString(payload, "loglevel"); err != nil {
		return settings, err
	}
	if settings.LogSendHostname, err = apiString(payload, "log-send-hostname", "log_send_hostname"); err != nil {
		return settings, err
	}
	if settings.ResolverRetries, err = apiInt64(payload, "resolver_retries"); err != nil {
		return settings, err
	}
	if settings.ResolverTimeoutRetry, err = apiString(payload, "resolver_timeoutretry"); err != nil {
		return settings, err
	}
	if settings.ResolverHoldValid, err = apiString(payload, "resolver_holdvalid"); err != nil {
		return settings, err
	}
	if settings.EmailLevel, err = apiString(payload, "email_level"); err != nil {
		return settings, err
	}
	if settings.EmailMyHostname, err = apiString(payload, "email_myhostname"); err != nil {
		return settings, err
	}
	if settings.EmailFrom, err = apiString(payload, "email_from"); err != nil {
		return settings, err
	}
	if settings.EmailTo, err = apiString(payload, "email_to"); err != nil {
		return settings, err
	}
	if settings.SSLCompatibilityMode, err = apiString(payload, "sslcompatibilitymode"); err != nil {
		return settings, err
	}
	if settings.SSLDefaultDHParam, err = apiInt64(payload, "ssldefaultdhparam"); err != nil {
		return settings, err
	}
	if settings.Advanced, err = apiString(payload, "advanced"); err != nil {
		return settings, err
	}
	if settings.EnableSync, err = apiBool(payload, "enablesync"); err != nil {
		return settings, err
	}

	return settings, nil
}

func nullHaproxySettingsModel() haproxySettingsModel {
	return haproxySettingsModel{
		ID:                              types.StringNull(),
		Enable:                          types.BoolNull(),
		Maxconn:                         types.Int64Null(),
		Nbthread:                        types.Int64Null(),
		TerminateOnReload:               types.BoolNull(),
		HardStopAfter:                   types.StringNull(),
		Carpdev:                         types.StringNull(),
		LocalStatsPort:                  types.Int64Null(),
		LocalStatsRefreshTime:           types.Int64Null(),
		LocalStatsStickTableRefreshTime: types.Int64Null(),
		RemoteSyslog:                    types.StringNull(),
		LogFacility:                     types.StringNull(),
		LogLevel:                        types.StringNull(),
		LogSendHostname:                 types.StringNull(),
		ResolverRetries:                 types.Int64Null(),
		ResolverTimeoutRetry:            types.StringNull(),
		ResolverHoldValid:               types.StringNull(),
		EmailLevel:                      types.StringNull(),
		EmailMyHostname:                 types.StringNull(),
		EmailFrom:                       types.StringNull(),
		EmailTo:                         types.StringNull(),
		SSLCompatibilityMode:            types.StringNull(),
		SSLDefaultDHParam:               types.Int64Null(),
		Advanced:                        types.StringNull(),
		EnableSync:                      types.BoolNull(),
	}
}

func buildHaproxySettingsPatch(plan haproxySettingsModel, prior haproxySettingsModel) map[string]any {
	patch := make(map[string]any)
	planValues := plan.attrValues()
	priorValues := prior.attrValues()

	for _, attribute := range haproxySettingsAttributes {
		planned := planValues[attribute.Name]
		if planned.IsUnknown() {
			continue
		}
		if planned.Equal(priorValues[attribute.Name]) {
			continue
		}

		patch[attribute.JSONName] = terraformValueToJSON(attribute.Kind, planned)
	}

	return patch
}

func (m haproxySettingsModel) attrValues() map[string]attr.Value {
	return map[string]attr.Value{
		"enable":                            m.Enable,
		"maxconn":                           m.Maxconn,
		"nbthread":                          m.Nbthread,
		"terminate_on_reload":               m.TerminateOnReload,
		"hard_stop_after":                   m.HardStopAfter,
		"carpdev":                           m.Carpdev,
		"localstatsport":                    m.LocalStatsPort,
		"localstats_refreshtime":            m.LocalStatsRefreshTime,
		"localstats_sticktable_refreshtime": m.LocalStatsStickTableRefreshTime,
		"remotesyslog":                      m.RemoteSyslog,
		"logfacility":                       m.LogFacility,
		"loglevel":                          m.LogLevel,
		"log_send_hostname":                 m.LogSendHostname,
		"resolver_retries":                  m.ResolverRetries,
		"resolver_timeoutretry":             m.ResolverTimeoutRetry,
		"resolver_holdvalid":                m.ResolverHoldValid,
		"email_level":                       m.EmailLevel,
		"email_myhostname":                  m.EmailMyHostname,
		"email_from":                        m.EmailFrom,
		"email_to":                          m.EmailTo,
		"sslcompatibilitymode":              m.SSLCompatibilityMode,
		"ssldefaultdhparam":                 m.SSLDefaultDHParam,
		"advanced":                          m.Advanced,
		"enablesync":                        m.EnableSync,
	}
}

func terraformValueToJSON(kind settingsAttributeKind, value attr.Value) any {
	if value.IsNull() {
		return nil
	}

	switch kind {
	case settingsAttributeBool:
		return value.(types.Bool).ValueBool()
	case settingsAttributeInt64:
		return value.(types.Int64).ValueInt64()
	case settingsAttributeString:
		return value.(types.String).ValueString()
	default:
		return nil
	}
}

func apiBool(payload map[string]any, names ...string) (types.Bool, error) {
	value, name, ok := apiValue(payload, names...)
	if !ok || value == nil {
		return types.BoolNull(), nil
	}

	switch typed := value.(type) {
	case bool:
		return types.BoolValue(typed), nil
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "1", "true", "yes", "on":
			return types.BoolValue(true), nil
		case "0", "false", "no", "off", "":
			return types.BoolValue(false), nil
		default:
			return types.BoolNull(), fmt.Errorf("HAProxy settings field %q is %q, not a boolean", name, typed)
		}
	case float64:
		if typed == 0 {
			return types.BoolValue(false), nil
		}
		if typed == 1 {
			return types.BoolValue(true), nil
		}
		return types.BoolNull(), fmt.Errorf("HAProxy settings field %q is %v, not a boolean", name, typed)
	case json.Number:
		intValue, err := typed.Int64()
		if err != nil {
			return types.BoolNull(), fmt.Errorf("HAProxy settings field %q is %q, not a boolean: %w", name, typed.String(), err)
		}
		if intValue == 0 {
			return types.BoolValue(false), nil
		}
		if intValue == 1 {
			return types.BoolValue(true), nil
		}
		return types.BoolNull(), fmt.Errorf("HAProxy settings field %q is %q, not a boolean", name, typed.String())
	default:
		return types.BoolNull(), fmt.Errorf("HAProxy settings field %q has unsupported boolean type %T", name, value)
	}
}

func apiInt64(payload map[string]any, names ...string) (types.Int64, error) {
	value, name, ok := apiValue(payload, names...)
	if !ok || value == nil {
		return types.Int64Null(), nil
	}

	switch typed := value.(type) {
	case float64:
		if math.Trunc(typed) != typed {
			return types.Int64Null(), fmt.Errorf("HAProxy settings field %q is %v, not an integer", name, typed)
		}
		return types.Int64Value(int64(typed)), nil
	case json.Number:
		intValue, err := typed.Int64()
		if err != nil {
			return types.Int64Null(), fmt.Errorf("HAProxy settings field %q is %q, not an integer: %w", name, typed.String(), err)
		}
		return types.Int64Value(intValue), nil
	case string:
		if strings.TrimSpace(typed) == "" {
			return types.Int64Null(), nil
		}
		intValue, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		if err != nil {
			return types.Int64Null(), fmt.Errorf("HAProxy settings field %q is %q, not an integer: %w", name, typed, err)
		}
		return types.Int64Value(intValue), nil
	default:
		return types.Int64Null(), fmt.Errorf("HAProxy settings field %q has unsupported integer type %T", name, value)
	}
}

func apiString(payload map[string]any, names ...string) (types.String, error) {
	value, name, ok := apiValue(payload, names...)
	if !ok || value == nil {
		return types.StringNull(), nil
	}

	typed, ok := value.(string)
	if !ok {
		return types.StringNull(), fmt.Errorf("HAProxy settings field %q has unsupported string type %T", name, value)
	}

	return types.StringValue(typed), nil
}

func apiValue(payload map[string]any, names ...string) (any, string, bool) {
	for _, name := range names {
		value, ok := payload[name]
		if ok {
			return value, name, true
		}
	}

	return nil, "", false
}
