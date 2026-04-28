package provider

import (
	"context"
	"fmt"
	"net/url"
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
	haproxyFrontendPath  = "/services/haproxy/frontend"
	haproxyFrontendsPath = "/services/haproxy/frontends"
)

var (
	_ resource.Resource                = (*haproxyFrontendResource)(nil)
	_ resource.ResourceWithConfigure   = (*haproxyFrontendResource)(nil)
	_ resource.ResourceWithImportState = (*haproxyFrontendResource)(nil)
)

type frontendAttributeKind string

const (
	frontendAttributeBool   frontendAttributeKind = "bool"
	frontendAttributeInt64  frontendAttributeKind = "int64"
	frontendAttributeString frontendAttributeKind = "string"
)

type frontendAttribute struct {
	Name        string
	JSONName    string
	Kind        frontendAttributeKind
	Description string
}

var haproxyFrontendAttributes = []frontendAttribute{
	{Name: "descr", JSONName: "descr", Kind: frontendAttributeString, Description: "Description for this HAProxy frontend."},
	{Name: "status", JSONName: "status", Kind: frontendAttributeString, Description: "Activation status for this frontend: active or disabled."},
	{Name: "max_connections", JSONName: "max_connections", Kind: frontendAttributeInt64, Description: "Maximum number of connections allowed by this frontend. Null leaves the pfSense HAProxy package default in place."},
	{Name: "backend_serverpool", JSONName: "backend_serverpool", Kind: frontendAttributeString, Description: "Default backend to use for this frontend."},
	{Name: "socket_stats", JSONName: "socket_stats", Kind: frontendAttributeBool, Description: "Enable or disable separate statistics for each socket."},
	{Name: "dontlognull", JSONName: "dontlognull", Kind: frontendAttributeBool, Description: "Enable or disable logging connections with no data transferred."},
	{Name: "dontlog_normal", JSONName: "dontlog_normal", Kind: frontendAttributeBool, Description: "Enable or disable only logging anomalous connections."},
	{Name: "log_separate_errors", JSONName: "log_separate_errors", Kind: frontendAttributeBool, Description: "Enable or disable logging potentially interesting information at error level."},
	{Name: "log_detailed", JSONName: "log_detailed", Kind: frontendAttributeBool, Description: "Enable or disable detailed frontend logging."},
	{Name: "client_timeout", JSONName: "client_timeout", Kind: frontendAttributeInt64, Description: "Client data timeout in milliseconds. Null leaves the pfSense HAProxy package default in place."},
	{Name: "forwardfor", JSONName: "forwardfor", Kind: frontendAttributeBool, Description: "Enable or disable the HTTP X-Forwarded-For header. Only valid for HTTP frontends."},
	{Name: "httpclose", JSONName: "httpclose", Kind: frontendAttributeString, Description: "HTTP connection mode: http-keep-alive, http-tunnel, httpclose, http-server-close, or forceclose."},
}

type haproxyFrontendResource struct {
	client *pfsense.Client
}

type haproxyFrontendModel struct {
	ID                types.String `tfsdk:"id"`
	Name              types.String `tfsdk:"name"`
	Type              types.String `tfsdk:"type"`
	Description       types.String `tfsdk:"descr"`
	Status            types.String `tfsdk:"status"`
	MaxConnections    types.Int64  `tfsdk:"max_connections"`
	BackendServerpool types.String `tfsdk:"backend_serverpool"`
	SocketStats       types.Bool   `tfsdk:"socket_stats"`
	DontLogNull       types.Bool   `tfsdk:"dontlognull"`
	DontLogNormal     types.Bool   `tfsdk:"dontlog_normal"`
	LogSeparateErrors types.Bool   `tfsdk:"log_separate_errors"`
	LogDetailed       types.Bool   `tfsdk:"log_detailed"`
	ClientTimeout     types.Int64  `tfsdk:"client_timeout"`
	ForwardFor        types.Bool   `tfsdk:"forwardfor"`
	HTTPClose         types.String `tfsdk:"httpclose"`
}

func newHaproxyFrontendResource() resource.Resource {
	return &haproxyFrontendResource{}
}

func (r *haproxyFrontendResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "pfsense_haproxy_frontend"
}

func (r *haproxyFrontendResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = resourceschema.Schema{
		Description: "Manages a top-level pfSense HAProxy frontend through pfSense-pkg-RESTAPI. Terraform uses the frontend name as the stable ID and resolves pfSense's current object ID before update/delete because pfSense object IDs may not be durable.",
		Attributes:  haproxyFrontendResourceSchemaAttributes(),
	}
}

func (r *haproxyFrontendResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *haproxyFrontendResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing pfSense client", "The provider was not configured before creating pfsense_haproxy_frontend.")
		return
	}

	var plan haproxyFrontendModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	normalized, err := validateHaproxyFrontendPlan(plan)
	if err != nil {
		resp.Diagnostics.AddError("Invalid HAProxy frontend", err.Error())
		return
	}

	_, _, found, err := lookupHaproxyFrontendByName(ctx, r.client, normalized.name, false)
	if err != nil {
		resp.Diagnostics.AddError("Check existing HAProxy frontend failed", frontendReadLookupErrorDetail(normalized.name, err))
		return
	}
	if found {
		resp.Diagnostics.AddError(
			"HAProxy frontend already exists",
			fmt.Sprintf("A pfSense HAProxy frontend named %q already exists. Import it with `terraform import pfsense_haproxy_frontend.<name> %s` before managing it.", normalized.name, normalized.name),
		)
		return
	}

	if err := r.client.Post(ctx, haproxyFrontendPath, buildHaproxyFrontendCreatePayload(plan, normalized), nil); err != nil {
		resp.Diagnostics.AddError("Create HAProxy frontend failed", err.Error())
		return
	}

	frontend, _, found, err := lookupHaproxyFrontendByName(ctx, r.client, normalized.name, false)
	if err != nil {
		resp.Diagnostics.AddError("Read HAProxy frontend after create failed", frontendReadLookupErrorDetail(normalized.name, err))
		return
	}
	if !found {
		resp.Diagnostics.AddError(
			"Read HAProxy frontend after create failed",
			fmt.Sprintf("Created frontend %q but GET %s did not return it. Confirm the live UAT /services/haproxy/frontends response shape and natural-key filtering before relying on this resource.", normalized.name, haproxyFrontendsPath),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, frontend)...)
}

func (r *haproxyFrontendResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing pfSense client", "The provider was not configured before reading pfsense_haproxy_frontend.")
		return
	}

	var state haproxyFrontendModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name, err := haproxyFrontendStateName(state)
	if err != nil {
		resp.Diagnostics.AddError("Invalid HAProxy frontend state", err.Error())
		return
	}

	frontend, _, found, err := lookupHaproxyFrontendByName(ctx, r.client, name, false)
	if err != nil {
		resp.Diagnostics.AddError("Read HAProxy frontend failed", frontendReadLookupErrorDetail(name, err))
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, frontend)...)
}

func (r *haproxyFrontendResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing pfSense client", "The provider was not configured before updating pfsense_haproxy_frontend.")
		return
	}

	var plan, prior haproxyFrontendModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}

	normalized, err := validateHaproxyFrontendPlan(plan)
	if err != nil {
		resp.Diagnostics.AddError("Invalid HAProxy frontend", err.Error())
		return
	}
	priorName, err := haproxyFrontendStateName(prior)
	if err != nil {
		resp.Diagnostics.AddError("Invalid HAProxy frontend prior state", err.Error())
		return
	}
	if normalized.name != priorName {
		resp.Diagnostics.AddError("Renaming HAProxy frontends is not supported", "The frontend name is the Terraform natural key. Change the name by creating a new resource and deleting the old one.")
		return
	}

	_, apiID, found, err := findHaproxyFrontendByName(ctx, r.client, normalized.name)
	if err != nil {
		resp.Diagnostics.AddError("Find HAProxy frontend before update failed", frontendLookupErrorDetail(normalized.name, err))
		return
	}
	if !found {
		resp.Diagnostics.AddError("Update HAProxy frontend failed", fmt.Sprintf("Frontend %q was not found on pfSense. Recreate it or remove it from Terraform state.", normalized.name))
		return
	}

	patch := buildHaproxyFrontendPatch(plan, prior, apiID)
	if len(patch) > 1 {
		if err := r.client.Patch(ctx, haproxyFrontendPath, patch, nil); err != nil {
			resp.Diagnostics.AddError("Update HAProxy frontend failed", err.Error())
			return
		}
	}

	frontend, _, found, err := findHaproxyFrontendByName(ctx, r.client, normalized.name)
	if err != nil {
		resp.Diagnostics.AddError("Read HAProxy frontend after update failed", frontendLookupErrorDetail(normalized.name, err))
		return
	}
	if !found {
		resp.Diagnostics.AddError("Read HAProxy frontend after update failed", fmt.Sprintf("Frontend %q was not returned by GET %s after PATCH.", normalized.name, haproxyFrontendsPath))
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, frontend)...)
}

func (r *haproxyFrontendResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing pfSense client", "The provider was not configured before deleting pfsense_haproxy_frontend.")
		return
	}

	var state haproxyFrontendModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name, err := haproxyFrontendStateName(state)
	if err != nil {
		resp.Diagnostics.AddError("Invalid HAProxy frontend state", err.Error())
		return
	}

	_, apiID, found, err := findHaproxyFrontendByName(ctx, r.client, name)
	if err != nil {
		resp.Diagnostics.AddError("Find HAProxy frontend before delete failed", frontendLookupErrorDetail(name, err))
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	if err := r.client.Delete(ctx, haproxyFrontendDeletePath(apiID), nil); err != nil {
		resp.Diagnostics.AddError("Delete HAProxy frontend failed", err.Error())
		return
	}

	resp.State.RemoveResource(ctx)
}

func (r *haproxyFrontendResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	name, err := haproxyFrontendName(types.StringValue(strings.TrimSpace(req.ID)))
	if err != nil {
		resp.Diagnostics.AddError("Invalid HAProxy frontend import ID", fmt.Sprintf("Import pfsense_haproxy_frontend with the frontend name: %s.", err.Error()))
		return
	}

	model := nullHaproxyFrontendModel()
	model.ID = types.StringValue(name)
	model.Name = types.StringValue(name)
	resp.Diagnostics.Append(resp.State.Set(ctx, model)...)
}

func haproxyFrontendResourceSchemaAttributes() map[string]resourceschema.Attribute {
	attributes := map[string]resourceschema.Attribute{
		"id": resourceschema.StringAttribute{
			Computed:    true,
			Description: "Stable Terraform ID for the frontend. This is the frontend name, not the pfSense object ID.",
		},
		"name": resourceschema.StringAttribute{
			Required:    true,
			Description: "Unique HAProxy frontend name. Names must contain at least two letters, numbers, dots, hyphens, or underscores. Terraform treats this as the natural key and requires replacement if it changes.",
			PlanModifiers: []planmodifier.String{
				stringplanmodifier.RequiresReplace(),
			},
		},
		"type": resourceschema.StringAttribute{
			Required:    true,
			Description: "Frontend processing type. This resource currently supports http and tcp; https is deferred until certificate ownership is modeled.",
		},
	}

	for _, attribute := range haproxyFrontendAttributes {
		switch attribute.Kind {
		case frontendAttributeBool:
			attributes[attribute.Name] = resourceschema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: attribute.Description,
			}
		case frontendAttributeInt64:
			attributes[attribute.Name] = resourceschema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Description: attribute.Description,
			}
		case frontendAttributeString:
			attributes[attribute.Name] = resourceschema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: attribute.Description,
			}
		}
	}

	return attributes
}

type haproxyFrontendKeys struct {
	name         string
	frontendType string
}

func validateHaproxyFrontendPlan(model haproxyFrontendModel) (haproxyFrontendKeys, error) {
	name, err := haproxyFrontendName(model.Name)
	if err != nil {
		return haproxyFrontendKeys{}, err
	}
	frontendType, err := haproxyFrontendType(model.Type)
	if err != nil {
		return haproxyFrontendKeys{}, err
	}
	if err := validateHaproxyFrontendOptionalFields(model, frontendType); err != nil {
		return haproxyFrontendKeys{}, err
	}

	return haproxyFrontendKeys{name: name, frontendType: frontendType}, nil
}

func validateHaproxyFrontendOptionalFields(model haproxyFrontendModel, frontendType string) error {
	if !model.Status.IsNull() && !model.Status.IsUnknown() {
		switch model.Status.ValueString() {
		case "active", "disabled":
		default:
			return fmt.Errorf("status must be one of active or disabled")
		}
	}
	if !model.HTTPClose.IsNull() && !model.HTTPClose.IsUnknown() {
		switch model.HTTPClose.ValueString() {
		case "http-keep-alive", "http-tunnel", "httpclose", "http-server-close", "forceclose":
		default:
			return fmt.Errorf("httpclose must be one of http-keep-alive, http-tunnel, httpclose, http-server-close, or forceclose")
		}
	}
	if !model.MaxConnections.IsNull() && !model.MaxConnections.IsUnknown() && model.MaxConnections.ValueInt64() < 0 {
		return fmt.Errorf("max_connections must be non-negative")
	}
	if !model.ClientTimeout.IsNull() && !model.ClientTimeout.IsUnknown() && model.ClientTimeout.ValueInt64() < 0 {
		return fmt.Errorf("client_timeout must be non-negative")
	}
	if !model.ForwardFor.IsNull() && !model.ForwardFor.IsUnknown() && model.ForwardFor.ValueBool() && frontendType != "http" {
		return fmt.Errorf("forwardfor is only valid when type is http")
	}

	return nil
}

func haproxyFrontendName(value types.String) (string, error) {
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
	if len(name) < 2 {
		return "", fmt.Errorf("name must be at least 2 characters")
	}
	if strings.Contains(name, "/") {
		return "", fmt.Errorf("name must not contain /")
	}
	if !haproxyNamePattern.MatchString(name) {
		return "", fmt.Errorf("name may contain only letters, numbers, dot, hyphen, and underscore")
	}

	return name, nil
}

func haproxyFrontendType(value types.String) (string, error) {
	if value.IsNull() || value.IsUnknown() {
		return "", fmt.Errorf("type is required")
	}
	frontendType := value.ValueString()
	if strings.TrimSpace(frontendType) != frontendType || frontendType == "" {
		return "", fmt.Errorf("type must be http or tcp")
	}
	switch frontendType {
	case "http", "tcp":
		return frontendType, nil
	default:
		return "", fmt.Errorf("type must be http or tcp")
	}
}

func haproxyFrontendStateName(model haproxyFrontendModel) (string, error) {
	if !model.Name.IsNull() && !model.Name.IsUnknown() {
		return haproxyFrontendName(model.Name)
	}
	if !model.ID.IsNull() && !model.ID.IsUnknown() {
		return haproxyFrontendName(model.ID)
	}

	return "", fmt.Errorf("state is missing frontend name")
}

func buildHaproxyFrontendCreatePayload(plan haproxyFrontendModel, normalized haproxyFrontendKeys) map[string]any {
	payload := map[string]any{
		"name": normalized.name,
		"type": normalized.frontendType,
	}
	values := plan.attrValues()

	for _, attribute := range haproxyFrontendAttributes {
		planned := values[attribute.Name]
		if planned.IsNull() || planned.IsUnknown() {
			continue
		}
		payload[attribute.JSONName] = frontendTerraformValueToJSON(attribute.Kind, planned)
	}

	return payload
}

func buildHaproxyFrontendPatch(plan haproxyFrontendModel, prior haproxyFrontendModel, apiID string) map[string]any {
	patch := map[string]any{
		"id": apiID,
	}
	if !plan.Type.IsUnknown() && !plan.Type.Equal(prior.Type) {
		patch["type"] = plan.Type.ValueString()
	}

	planValues := plan.attrValues()
	priorValues := prior.attrValues()
	for _, attribute := range haproxyFrontendAttributes {
		planned := planValues[attribute.Name]
		if planned.IsUnknown() {
			continue
		}
		if planned.Equal(priorValues[attribute.Name]) {
			continue
		}

		patch[attribute.JSONName] = frontendTerraformValueToJSON(attribute.Kind, planned)
	}

	return patch
}

func findHaproxyFrontendByName(ctx context.Context, client *pfsense.Client, name string) (haproxyFrontendModel, string, bool, error) {
	return lookupHaproxyFrontendByName(ctx, client, name, true)
}

func lookupHaproxyFrontendByName(ctx context.Context, client *pfsense.Client, name string, requireAPIID bool) (haproxyFrontendModel, string, bool, error) {
	var raw any
	if err := client.Get(ctx, haproxyFrontendsQueryPath(name), &raw); err != nil {
		return haproxyFrontendModel{}, "", false, err
	}

	payloads, err := haproxyFrontendPayloadList(raw)
	if err != nil {
		return haproxyFrontendModel{}, "", false, err
	}

	var matched map[string]any
	for _, payload := range payloads {
		candidateName, err := apiRequiredStringWithLabel(payload, "HAProxy frontend", "name")
		if err != nil {
			return haproxyFrontendModel{}, "", false, err
		}
		if candidateName != name {
			continue
		}
		if matched != nil {
			return haproxyFrontendModel{}, "", false, fmt.Errorf("multiple HAProxy frontends named %q were returned; frontend names must be unique for Terraform natural-key management", name)
		}
		matched = payload
	}

	if matched == nil {
		return haproxyFrontendModel{}, "", false, nil
	}

	apiID := ""
	if requireAPIID {
		var err error
		apiID, err = apiRequiredScalarStringWithLabel(matched, "HAProxy frontend", "id")
		if err != nil {
			return haproxyFrontendModel{}, "", false, fmt.Errorf("%w; confirm UAT returns object IDs from GET %s before using update/delete", err, haproxyFrontendsPath)
		}
	}
	model, err := haproxyFrontendModelFromAPI(matched)
	if err != nil {
		return haproxyFrontendModel{}, "", false, err
	}

	return model, apiID, true, nil
}

func haproxyFrontendPayloadList(raw any) ([]map[string]any, error) {
	if raw == nil {
		return nil, nil
	}

	switch typed := raw.(type) {
	case []any:
		payloads := make([]map[string]any, 0, len(typed))
		for index, item := range typed {
			payload, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("HAProxy frontends response item %d has unsupported type %T", index, item)
			}
			payloads = append(payloads, payload)
		}
		return payloads, nil
	case []map[string]any:
		return typed, nil
	case map[string]any:
		return []map[string]any{typed}, nil
	default:
		return nil, fmt.Errorf("HAProxy frontends response has unsupported type %T; confirm the live UAT /services/haproxy/frontends schema", raw)
	}
}

func haproxyFrontendModelFromAPI(payload map[string]any) (haproxyFrontendModel, error) {
	frontend := nullHaproxyFrontendModel()

	name, err := apiRequiredStringWithLabel(payload, "HAProxy frontend", "name")
	if err != nil {
		return frontend, err
	}
	frontendType, err := apiRequiredStringWithLabel(payload, "HAProxy frontend", "type")
	if err != nil {
		return frontend, err
	}

	frontend.ID = types.StringValue(name)
	frontend.Name = types.StringValue(name)
	frontend.Type = types.StringValue(frontendType)

	if frontend.Description, err = apiStringWithLabel(payload, "HAProxy frontend", "descr"); err != nil {
		return frontend, err
	}
	if frontend.Status, err = apiStringWithLabel(payload, "HAProxy frontend", "status"); err != nil {
		return frontend, err
	}
	if frontend.MaxConnections, err = apiInt64WithLabel(payload, "HAProxy frontend", "max_connections"); err != nil {
		return frontend, err
	}
	if frontend.BackendServerpool, err = apiStringWithLabel(payload, "HAProxy frontend", "backend_serverpool"); err != nil {
		return frontend, err
	}
	if frontend.SocketStats, err = apiBoolWithLabel(payload, "HAProxy frontend", "socket_stats"); err != nil {
		return frontend, err
	}
	if frontend.DontLogNull, err = apiBoolWithLabel(payload, "HAProxy frontend", "dontlognull"); err != nil {
		return frontend, err
	}
	if frontend.DontLogNormal, err = apiBoolWithLabel(payload, "HAProxy frontend", "dontlog_normal"); err != nil {
		return frontend, err
	}
	if frontend.LogSeparateErrors, err = apiBoolWithLabel(payload, "HAProxy frontend", "log_separate_errors"); err != nil {
		return frontend, err
	}
	if frontend.LogDetailed, err = apiBoolWithLabel(payload, "HAProxy frontend", "log_detailed"); err != nil {
		return frontend, err
	}
	if frontend.ClientTimeout, err = apiInt64WithLabel(payload, "HAProxy frontend", "client_timeout"); err != nil {
		return frontend, err
	}
	if frontend.ForwardFor, err = apiBoolWithLabel(payload, "HAProxy frontend", "forwardfor"); err != nil {
		return frontend, err
	}
	if frontend.HTTPClose, err = apiStringWithLabel(payload, "HAProxy frontend", "httpclose"); err != nil {
		return frontend, err
	}

	return frontend, nil
}

func nullHaproxyFrontendModel() haproxyFrontendModel {
	return haproxyFrontendModel{
		ID:                types.StringNull(),
		Name:              types.StringNull(),
		Type:              types.StringNull(),
		Description:       types.StringNull(),
		Status:            types.StringNull(),
		MaxConnections:    types.Int64Null(),
		BackendServerpool: types.StringNull(),
		SocketStats:       types.BoolNull(),
		DontLogNull:       types.BoolNull(),
		DontLogNormal:     types.BoolNull(),
		LogSeparateErrors: types.BoolNull(),
		LogDetailed:       types.BoolNull(),
		ClientTimeout:     types.Int64Null(),
		ForwardFor:        types.BoolNull(),
		HTTPClose:         types.StringNull(),
	}
}

func (m haproxyFrontendModel) attrValues() map[string]attr.Value {
	return map[string]attr.Value{
		"descr":               m.Description,
		"status":              m.Status,
		"max_connections":     m.MaxConnections,
		"backend_serverpool":  m.BackendServerpool,
		"socket_stats":        m.SocketStats,
		"dontlognull":         m.DontLogNull,
		"dontlog_normal":      m.DontLogNormal,
		"log_separate_errors": m.LogSeparateErrors,
		"log_detailed":        m.LogDetailed,
		"client_timeout":      m.ClientTimeout,
		"forwardfor":          m.ForwardFor,
		"httpclose":           m.HTTPClose,
	}
}

func frontendTerraformValueToJSON(kind frontendAttributeKind, value attr.Value) any {
	if value.IsNull() {
		return nil
	}

	switch kind {
	case frontendAttributeBool:
		return value.(types.Bool).ValueBool()
	case frontendAttributeInt64:
		return value.(types.Int64).ValueInt64()
	case frontendAttributeString:
		return value.(types.String).ValueString()
	default:
		return nil
	}
}

func haproxyFrontendsQueryPath(name string) string {
	values := url.Values{}
	values.Set("name", name)
	return haproxyFrontendsPath + "?" + values.Encode()
}

func haproxyFrontendDeletePath(apiID string) string {
	values := url.Values{}
	values.Set("id", apiID)
	return haproxyFrontendPath + "?" + values.Encode()
}

func frontendLookupErrorDetail(name string, err error) string {
	return fmt.Sprintf("%s. Confirm GET %s is available on UAT, returns a list of frontend objects with stable name fields, and includes the transient pfSense object id required for update/delete. Frontend name: %q.", err.Error(), haproxyFrontendsPath, name)
}

func frontendReadLookupErrorDetail(name string, err error) string {
	return fmt.Sprintf("%s. Confirm GET %s is available on UAT and returns a list of frontend objects with stable name fields. Frontend name: %q.", err.Error(), haproxyFrontendsPath, name)
}
