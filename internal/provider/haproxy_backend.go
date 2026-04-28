package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"

	"github.com/d3vi1/terraform-provider-pfsense-restapi-haproxy/internal/pfsense"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const (
	haproxyBackendPath  = "/services/haproxy/backend"
	haproxyBackendsPath = "/services/haproxy/backends"
)

var (
	_ resource.Resource                = (*haproxyBackendResource)(nil)
	_ resource.ResourceWithConfigure   = (*haproxyBackendResource)(nil)
	_ resource.ResourceWithImportState = (*haproxyBackendResource)(nil)
)

type backendAttributeKind string

const (
	backendAttributeBool   backendAttributeKind = "bool"
	backendAttributeInt64  backendAttributeKind = "int64"
	backendAttributeString backendAttributeKind = "string"
)

type backendAttribute struct {
	Name        string
	JSONName    string
	Kind        backendAttributeKind
	Description string
}

var haproxyBackendAttributes = []backendAttribute{
	{Name: "balance", JSONName: "balance", Kind: backendAttributeString, Description: "Load-balancing algorithm for this backend, such as roundrobin, static-rr, leastconn, source, or uri. Empty uses the pfSense HAProxy package default."},
	{Name: "connection_timeout", JSONName: "connection_timeout", Kind: backendAttributeInt64, Description: "Connection timeout in milliseconds. Null leaves the pfSense HAProxy package default in place."},
	{Name: "server_timeout", JSONName: "server_timeout", Kind: backendAttributeInt64, Description: "Server data timeout in milliseconds. Null leaves the pfSense HAProxy package default in place."},
	{Name: "retries", JSONName: "retries", Kind: backendAttributeInt64, Description: "Retry attempts after a backend server connection failure."},
	{Name: "check_type", JSONName: "check_type", Kind: backendAttributeString, Description: "Backend health check method, such as none, Basic, HTTP, LDAP, MySQL, PostgreSQL, Redis, SMTP, ESMTP, or SSL."},
	{Name: "checkinter", JSONName: "checkinter", Kind: backendAttributeInt64, Description: "Health check interval in milliseconds when check_type is not none."},
	{Name: "log_health_checks", JSONName: "log_health_checks", Kind: backendAttributeBool, Description: "Enable or disable logging health check status changes."},
	{Name: "httpcheck_method", JSONName: "httpcheck_method", Kind: backendAttributeString, Description: "HTTP method used for HTTP health checks."},
	{Name: "monitor_uri", JSONName: "monitor_uri", Kind: backendAttributeString, Description: "URI used for HTTP health checks."},
	{Name: "monitor_httpversion", JSONName: "monitor_httpversion", Kind: backendAttributeString, Description: "HTTP version used for HTTP health checks."},
	{Name: "agent_checks", JSONName: "agent_checks", Kind: backendAttributeBool, Description: "Enable or disable HAProxy agent checks for this backend."},
	{Name: "agent_port", JSONName: "agent_port", Kind: backendAttributeString, Description: "TCP port used for HAProxy agent checks. Required by pfREST when agent_checks is true."},
	{Name: "agent_inter", JSONName: "agent_inter", Kind: backendAttributeInt64, Description: "Interval in milliseconds between HAProxy agent checks."},
	{Name: "persist_cookie_enabled", JSONName: "persist_cookie_enabled", Kind: backendAttributeBool, Description: "Enable or disable cookie-based persistence."},
	{Name: "persist_cookie_name", JSONName: "persist_cookie_name", Kind: backendAttributeString, Description: "Cookie name used for persistence. Required by pfREST when persist_cookie_enabled is true."},
	{Name: "persist_cookie_mode", JSONName: "persist_cookie_mode", Kind: backendAttributeString, Description: "HAProxy cookie persistence mode."},
}

type haproxyBackendResource struct {
	client *pfsense.Client
}

type haproxyBackendModel struct {
	ID                   types.String `tfsdk:"id"`
	Name                 types.String `tfsdk:"name"`
	Balance              types.String `tfsdk:"balance"`
	ConnectionTimeout    types.Int64  `tfsdk:"connection_timeout"`
	ServerTimeout        types.Int64  `tfsdk:"server_timeout"`
	Retries              types.Int64  `tfsdk:"retries"`
	CheckType            types.String `tfsdk:"check_type"`
	CheckInterval        types.Int64  `tfsdk:"checkinter"`
	LogHealthChecks      types.Bool   `tfsdk:"log_health_checks"`
	HTTPCheckMethod      types.String `tfsdk:"httpcheck_method"`
	MonitorURI           types.String `tfsdk:"monitor_uri"`
	MonitorHTTPVersion   types.String `tfsdk:"monitor_httpversion"`
	AgentChecks          types.Bool   `tfsdk:"agent_checks"`
	AgentPort            types.String `tfsdk:"agent_port"`
	AgentInterval        types.Int64  `tfsdk:"agent_inter"`
	PersistCookieEnabled types.Bool   `tfsdk:"persist_cookie_enabled"`
	PersistCookieName    types.String `tfsdk:"persist_cookie_name"`
	PersistCookieMode    types.String `tfsdk:"persist_cookie_mode"`
}

func newHaproxyBackendResource() resource.Resource {
	return &haproxyBackendResource{}
}

func (r *haproxyBackendResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "pfsense_haproxy_backend"
}

func (r *haproxyBackendResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = resourceschema.Schema{
		Description: "Manages a pfSense HAProxy backend through pfSense-pkg-RESTAPI. Terraform uses the backend name as the stable ID and resolves pfSense's current object ID before writes because pfSense object IDs may not be durable.",
		Attributes:  haproxyBackendResourceSchemaAttributes(),
	}
}

func (r *haproxyBackendResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *haproxyBackendResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing pfSense client", "The provider was not configured before creating pfsense_haproxy_backend.")
		return
	}

	var plan haproxyBackendModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name, err := haproxyBackendName(plan.Name)
	if err != nil {
		resp.Diagnostics.AddError("Invalid HAProxy backend name", err.Error())
		return
	}

	_, _, found, err := findHaproxyBackendByName(ctx, r.client, name)
	if err != nil {
		resp.Diagnostics.AddError("Check existing HAProxy backend failed", backendLookupErrorDetail(name, err))
		return
	}
	if found {
		resp.Diagnostics.AddError(
			"HAProxy backend already exists",
			fmt.Sprintf("A pfSense HAProxy backend named %q already exists. Import it with `terraform import pfsense_haproxy_backend.<name> %s` before managing it.", name, name),
		)
		return
	}

	if err := r.client.Post(ctx, haproxyBackendPath, buildHaproxyBackendCreatePayload(plan, name), nil); err != nil {
		resp.Diagnostics.AddError("Create HAProxy backend failed", err.Error())
		return
	}

	backend, _, found, err := findHaproxyBackendByName(ctx, r.client, name)
	if err != nil {
		resp.Diagnostics.AddError("Read HAProxy backend after create failed", backendLookupErrorDetail(name, err))
		return
	}
	if !found {
		resp.Diagnostics.AddError(
			"Read HAProxy backend after create failed",
			fmt.Sprintf("Created backend %q but GET %s did not return it. Confirm the live UAT /services/haproxy/backends response shape and natural-key filtering before relying on this resource.", name, haproxyBackendsPath),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, backend)...)
}

func (r *haproxyBackendResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing pfSense client", "The provider was not configured before reading pfsense_haproxy_backend.")
		return
	}

	var state haproxyBackendModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name, err := haproxyBackendStateName(state)
	if err != nil {
		resp.Diagnostics.AddError("Invalid HAProxy backend state", err.Error())
		return
	}

	backend, _, found, err := findHaproxyBackendByName(ctx, r.client, name)
	if err != nil {
		resp.Diagnostics.AddError("Read HAProxy backend failed", backendLookupErrorDetail(name, err))
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, backend)...)
}

func (r *haproxyBackendResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing pfSense client", "The provider was not configured before updating pfsense_haproxy_backend.")
		return
	}

	var plan, prior haproxyBackendModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name, err := haproxyBackendName(plan.Name)
	if err != nil {
		resp.Diagnostics.AddError("Invalid HAProxy backend name", err.Error())
		return
	}
	priorName, err := haproxyBackendStateName(prior)
	if err != nil {
		resp.Diagnostics.AddError("Invalid HAProxy backend prior state", err.Error())
		return
	}
	if name != priorName {
		resp.Diagnostics.AddError("Renaming HAProxy backends is not supported", "The backend name is the Terraform natural key. Change the name by creating a new resource and deleting the old one.")
		return
	}

	_, apiID, found, err := findHaproxyBackendByName(ctx, r.client, name)
	if err != nil {
		resp.Diagnostics.AddError("Find HAProxy backend before update failed", backendLookupErrorDetail(name, err))
		return
	}
	if !found {
		resp.Diagnostics.AddError("Update HAProxy backend failed", fmt.Sprintf("Backend %q was not found on pfSense. Recreate it or remove it from Terraform state.", name))
		return
	}

	patch := buildHaproxyBackendPatch(plan, prior, apiID)
	if len(patch) > 1 {
		if err := r.client.Patch(ctx, haproxyBackendPath, patch, nil); err != nil {
			resp.Diagnostics.AddError("Update HAProxy backend failed", err.Error())
			return
		}
	}

	backend, _, found, err := findHaproxyBackendByName(ctx, r.client, name)
	if err != nil {
		resp.Diagnostics.AddError("Read HAProxy backend after update failed", backendLookupErrorDetail(name, err))
		return
	}
	if !found {
		resp.Diagnostics.AddError("Read HAProxy backend after update failed", fmt.Sprintf("Backend %q was not returned by GET %s after PATCH.", name, haproxyBackendsPath))
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, backend)...)
}

func (r *haproxyBackendResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing pfSense client", "The provider was not configured before deleting pfsense_haproxy_backend.")
		return
	}

	var state haproxyBackendModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name, err := haproxyBackendStateName(state)
	if err != nil {
		resp.Diagnostics.AddError("Invalid HAProxy backend state", err.Error())
		return
	}

	_, apiID, found, err := findHaproxyBackendByName(ctx, r.client, name)
	if err != nil {
		resp.Diagnostics.AddError("Find HAProxy backend before delete failed", backendLookupErrorDetail(name, err))
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	if err := r.client.Delete(ctx, haproxyBackendDeletePath(apiID), nil); err != nil {
		resp.Diagnostics.AddError("Delete HAProxy backend failed", err.Error())
		return
	}

	resp.State.RemoveResource(ctx)
}

func (r *haproxyBackendResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	name := strings.TrimSpace(req.ID)
	if name == "" {
		resp.Diagnostics.AddError("Invalid HAProxy backend import ID", "Import pfsense_haproxy_backend with the backend name.")
		return
	}

	model := nullHaproxyBackendModel()
	model.ID = types.StringValue(name)
	model.Name = types.StringValue(name)
	resp.Diagnostics.Append(resp.State.Set(ctx, model)...)
}

func haproxyBackendResourceSchemaAttributes() map[string]resourceschema.Attribute {
	attributes := map[string]resourceschema.Attribute{
		"id": resourceschema.StringAttribute{
			Computed:    true,
			Description: "Stable Terraform ID for the backend. This is the backend name, not the pfSense object ID.",
		},
		"name": resourceschema.StringAttribute{
			Required:    true,
			Description: "Unique HAProxy backend name. pfSense restricts names to letters, numbers, dot, hyphen, and underscore. Terraform treats this as the natural key and requires replacement if it changes.",
			PlanModifiers: []planmodifier.String{
				stringplanmodifier.RequiresReplace(),
			},
		},
	}

	for _, attribute := range haproxyBackendAttributes {
		switch attribute.Kind {
		case backendAttributeBool:
			attributes[attribute.Name] = resourceschema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: attribute.Description,
			}
		case backendAttributeInt64:
			attributes[attribute.Name] = resourceschema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Description: attribute.Description,
			}
		case backendAttributeString:
			attributes[attribute.Name] = resourceschema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: attribute.Description,
			}
		}
	}

	return attributes
}

func buildHaproxyBackendCreatePayload(plan haproxyBackendModel, name string) map[string]any {
	payload := map[string]any{
		"name": name,
	}
	values := plan.attrValues()

	for _, attribute := range haproxyBackendAttributes {
		planned := values[attribute.Name]
		if planned.IsNull() || planned.IsUnknown() {
			continue
		}
		payload[attribute.JSONName] = backendTerraformValueToJSON(attribute.Kind, planned)
	}

	return payload
}

func buildHaproxyBackendPatch(plan haproxyBackendModel, prior haproxyBackendModel, apiID string) map[string]any {
	patch := map[string]any{
		"id": apiID,
	}
	planValues := plan.attrValues()
	priorValues := prior.attrValues()

	for _, attribute := range haproxyBackendAttributes {
		planned := planValues[attribute.Name]
		if planned.IsUnknown() {
			continue
		}
		if planned.Equal(priorValues[attribute.Name]) {
			continue
		}

		patch[attribute.JSONName] = backendTerraformValueToJSON(attribute.Kind, planned)
	}

	return patch
}

func findHaproxyBackendByName(ctx context.Context, client *pfsense.Client, name string) (haproxyBackendModel, string, bool, error) {
	var raw any
	if err := client.Get(ctx, haproxyBackendsQueryPath(name), &raw); err != nil {
		return haproxyBackendModel{}, "", false, err
	}

	payloads, err := haproxyBackendPayloadList(raw)
	if err != nil {
		return haproxyBackendModel{}, "", false, err
	}

	var matched map[string]any
	for _, payload := range payloads {
		candidateName, err := apiRequiredStringWithLabel(payload, "HAProxy backend", "name")
		if err != nil {
			return haproxyBackendModel{}, "", false, err
		}
		if candidateName != name {
			continue
		}
		if matched != nil {
			return haproxyBackendModel{}, "", false, fmt.Errorf("multiple HAProxy backends named %q were returned; backend names must be unique for Terraform natural-key management", name)
		}
		matched = payload
	}

	if matched == nil {
		return haproxyBackendModel{}, "", false, nil
	}

	apiID, err := apiRequiredScalarStringWithLabel(matched, "HAProxy backend", "id")
	if err != nil {
		return haproxyBackendModel{}, "", false, fmt.Errorf("%w; confirm UAT returns object IDs from GET %s before using update/delete", err, haproxyBackendsPath)
	}
	model, err := haproxyBackendModelFromAPI(matched)
	if err != nil {
		return haproxyBackendModel{}, "", false, err
	}

	return model, apiID, true, nil
}

func haproxyBackendPayloadList(raw any) ([]map[string]any, error) {
	if raw == nil {
		return nil, nil
	}

	switch typed := raw.(type) {
	case []any:
		payloads := make([]map[string]any, 0, len(typed))
		for index, item := range typed {
			payload, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("HAProxy backends response item %d has unsupported type %T", index, item)
			}
			payloads = append(payloads, payload)
		}
		return payloads, nil
	case []map[string]any:
		return typed, nil
	case map[string]any:
		return []map[string]any{typed}, nil
	default:
		return nil, fmt.Errorf("HAProxy backends response has unsupported type %T; confirm the live UAT /services/haproxy/backends schema", raw)
	}
}

func haproxyBackendModelFromAPI(payload map[string]any) (haproxyBackendModel, error) {
	backend := nullHaproxyBackendModel()

	name, err := apiRequiredStringWithLabel(payload, "HAProxy backend", "name")
	if err != nil {
		return backend, err
	}
	backend.ID = types.StringValue(name)
	backend.Name = types.StringValue(name)

	if backend.Balance, err = apiStringWithLabel(payload, "HAProxy backend", "balance"); err != nil {
		return backend, err
	}
	if backend.ConnectionTimeout, err = apiInt64WithLabel(payload, "HAProxy backend", "connection_timeout"); err != nil {
		return backend, err
	}
	if backend.ServerTimeout, err = apiInt64WithLabel(payload, "HAProxy backend", "server_timeout"); err != nil {
		return backend, err
	}
	if backend.Retries, err = apiInt64WithLabel(payload, "HAProxy backend", "retries"); err != nil {
		return backend, err
	}
	if backend.CheckType, err = apiStringWithLabel(payload, "HAProxy backend", "check_type"); err != nil {
		return backend, err
	}
	if backend.CheckInterval, err = apiInt64WithLabel(payload, "HAProxy backend", "checkinter"); err != nil {
		return backend, err
	}
	if backend.LogHealthChecks, err = apiBoolWithLabel(payload, "HAProxy backend", "log_health_checks"); err != nil {
		return backend, err
	}
	if backend.HTTPCheckMethod, err = apiStringWithLabel(payload, "HAProxy backend", "httpcheck_method"); err != nil {
		return backend, err
	}
	if backend.MonitorURI, err = apiStringWithLabel(payload, "HAProxy backend", "monitor_uri"); err != nil {
		return backend, err
	}
	if backend.MonitorHTTPVersion, err = apiStringWithLabel(payload, "HAProxy backend", "monitor_httpversion"); err != nil {
		return backend, err
	}
	if backend.AgentChecks, err = apiBoolWithLabel(payload, "HAProxy backend", "agent_checks"); err != nil {
		return backend, err
	}
	if backend.AgentPort, err = apiScalarStringWithLabel(payload, "HAProxy backend", "agent_port"); err != nil {
		return backend, err
	}
	if backend.AgentInterval, err = apiInt64WithLabel(payload, "HAProxy backend", "agent_inter"); err != nil {
		return backend, err
	}
	if backend.PersistCookieEnabled, err = apiBoolWithLabel(payload, "HAProxy backend", "persist_cookie_enabled"); err != nil {
		return backend, err
	}
	if backend.PersistCookieName, err = apiStringWithLabel(payload, "HAProxy backend", "persist_cookie_name"); err != nil {
		return backend, err
	}
	if backend.PersistCookieMode, err = apiStringWithLabel(payload, "HAProxy backend", "persist_cookie_mode"); err != nil {
		return backend, err
	}

	return backend, nil
}

func nullHaproxyBackendModel() haproxyBackendModel {
	return haproxyBackendModel{
		ID:                   types.StringNull(),
		Name:                 types.StringNull(),
		Balance:              types.StringNull(),
		ConnectionTimeout:    types.Int64Null(),
		ServerTimeout:        types.Int64Null(),
		Retries:              types.Int64Null(),
		CheckType:            types.StringNull(),
		CheckInterval:        types.Int64Null(),
		LogHealthChecks:      types.BoolNull(),
		HTTPCheckMethod:      types.StringNull(),
		MonitorURI:           types.StringNull(),
		MonitorHTTPVersion:   types.StringNull(),
		AgentChecks:          types.BoolNull(),
		AgentPort:            types.StringNull(),
		AgentInterval:        types.Int64Null(),
		PersistCookieEnabled: types.BoolNull(),
		PersistCookieName:    types.StringNull(),
		PersistCookieMode:    types.StringNull(),
	}
}

func (m haproxyBackendModel) attrValues() map[string]attr.Value {
	return map[string]attr.Value{
		"balance":                m.Balance,
		"connection_timeout":     m.ConnectionTimeout,
		"server_timeout":         m.ServerTimeout,
		"retries":                m.Retries,
		"check_type":             m.CheckType,
		"checkinter":             m.CheckInterval,
		"log_health_checks":      m.LogHealthChecks,
		"httpcheck_method":       m.HTTPCheckMethod,
		"monitor_uri":            m.MonitorURI,
		"monitor_httpversion":    m.MonitorHTTPVersion,
		"agent_checks":           m.AgentChecks,
		"agent_port":             m.AgentPort,
		"agent_inter":            m.AgentInterval,
		"persist_cookie_enabled": m.PersistCookieEnabled,
		"persist_cookie_name":    m.PersistCookieName,
		"persist_cookie_mode":    m.PersistCookieMode,
	}
}

func backendTerraformValueToJSON(kind backendAttributeKind, value attr.Value) any {
	if value.IsNull() {
		return nil
	}

	switch kind {
	case backendAttributeBool:
		return value.(types.Bool).ValueBool()
	case backendAttributeInt64:
		return value.(types.Int64).ValueInt64()
	case backendAttributeString:
		return value.(types.String).ValueString()
	default:
		return nil
	}
}

func haproxyBackendName(value types.String) (string, error) {
	if value.IsNull() || value.IsUnknown() {
		return "", fmt.Errorf("name is required")
	}
	name := value.ValueString()
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", fmt.Errorf("name must not be empty")
	}
	if trimmed != name {
		return "", fmt.Errorf("name must not contain leading or trailing whitespace")
	}

	return name, nil
}

func haproxyBackendStateName(model haproxyBackendModel) (string, error) {
	if !model.Name.IsNull() && !model.Name.IsUnknown() {
		return haproxyBackendName(model.Name)
	}
	if !model.ID.IsNull() && !model.ID.IsUnknown() {
		return haproxyBackendName(model.ID)
	}

	return "", fmt.Errorf("state is missing backend name")
}

func haproxyBackendsQueryPath(name string) string {
	values := url.Values{}
	values.Set("name", name)
	return haproxyBackendsPath + "?" + values.Encode()
}

func haproxyBackendDeletePath(apiID string) string {
	values := url.Values{}
	values.Set("id", apiID)
	return haproxyBackendPath + "?" + values.Encode()
}

func backendLookupErrorDetail(name string, err error) string {
	return fmt.Sprintf("%s. Confirm GET %s is available on UAT, returns a list of backend objects with stable name fields, and includes the transient pfSense object id required for update/delete. Backend name: %q.", err.Error(), haproxyBackendsPath, name)
}

func apiRequiredStringWithLabel(payload map[string]any, label string, names ...string) (string, error) {
	value, name, ok := apiValue(payload, names...)
	if !ok || value == nil {
		return "", fmt.Errorf("%s response did not include required string field %q", label, names[0])
	}

	typed, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s field %q has unsupported string type %T", label, name, value)
	}
	if strings.TrimSpace(typed) == "" {
		return "", fmt.Errorf("%s field %q must not be empty", label, name)
	}

	return typed, nil
}

func apiRequiredScalarStringWithLabel(payload map[string]any, label string, names ...string) (string, error) {
	value, name, ok := apiValue(payload, names...)
	if !ok || value == nil {
		return "", fmt.Errorf("%s response did not include required scalar field %q", label, names[0])
	}

	return scalarStringValue(label, name, value)
}

func apiScalarStringWithLabel(payload map[string]any, label string, names ...string) (types.String, error) {
	value, name, ok := apiValue(payload, names...)
	if !ok || value == nil {
		return types.StringNull(), nil
	}

	scalar, err := scalarStringValue(label, name, value)
	if err != nil {
		return types.StringNull(), err
	}
	if scalar == "" {
		return types.StringNull(), nil
	}

	return types.StringValue(scalar), nil
}

func scalarStringValue(label string, name string, value any) (string, error) {
	switch typed := value.(type) {
	case string:
		return typed, nil
	case float64:
		if math.Trunc(typed) != typed {
			return "", fmt.Errorf("%s field %q is %v, not an integer scalar", label, name, typed)
		}
		return strconv.FormatInt(int64(typed), 10), nil
	case json.Number:
		intValue, err := typed.Int64()
		if err != nil {
			return "", fmt.Errorf("%s field %q is %q, not an integer scalar: %w", label, name, typed.String(), err)
		}
		return strconv.FormatInt(intValue, 10), nil
	default:
		return "", fmt.Errorf("%s field %q has unsupported scalar type %T", label, name, value)
	}
}
