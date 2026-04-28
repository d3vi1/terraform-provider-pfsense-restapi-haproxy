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
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const (
	haproxyBackendActionPath  = "/services/haproxy/backend/action"
	haproxyBackendActionsPath = "/services/haproxy/backend/actions"
)

var (
	_ resource.Resource                = (*haproxyBackendActionResource)(nil)
	_ resource.ResourceWithConfigure   = (*haproxyBackendActionResource)(nil)
	_ resource.ResourceWithImportState = (*haproxyBackendActionResource)(nil)
)

type backendActionAttribute struct {
	Name           string
	JSONName       string
	InternalSuffix string
	Description    string
}

var haproxyBackendActionAttributes = []backendActionAttribute{
	{Name: "server", JSONName: "server", InternalSuffix: "server", Description: "Backend server selected by use_server."},
	{Name: "customaction", JSONName: "customaction", InternalSuffix: "customaction", Description: "Custom HAProxy action text used by custom."},
	{Name: "deny_status", JSONName: "deny_status", InternalSuffix: "deny_status", Description: "HTTP status used by http-request_deny or http-request_tarpit."},
	{Name: "realm", JSONName: "realm", InternalSuffix: "realm", Description: "Authentication realm used by http-request_auth."},
	{Name: "rule", JSONName: "rule", InternalSuffix: "rule", Description: "Redirect rule used by http-request_redirect."},
	{Name: "lua_function", JSONName: "lua_function", InternalSuffix: "lua-function", Description: "Lua function or service used by Lua/use-service actions."},
	{Name: "name", JSONName: "name", InternalSuffix: "name", Description: "Header name used by header mutation actions."},
	{Name: "fmt", JSONName: "fmt", InternalSuffix: "fmt", Description: "HAProxy fmt value used by add/set header and set method/path/query/URI actions."},
	{Name: "find", JSONName: "find", InternalSuffix: "find", Description: "Value or regular expression to find for replace actions."},
	{Name: "replace", JSONName: "replace", InternalSuffix: "replace", Description: "Replacement value for replace actions."},
	{Name: "path", JSONName: "path", InternalSuffix: "path", Description: "Path matcher used by http-request_replace-path."},
	{Name: "status", JSONName: "status", InternalSuffix: "status", Description: "HTTP status used by set-status actions."},
	{Name: "reason", JSONName: "reason", InternalSuffix: "reason", Description: "HTTP reason phrase used by set-status actions."},
}

var haproxyBackendActionChoices = map[string]struct{}{
	"use_server":                         {},
	"custom":                             {},
	"http-request_allow":                 {},
	"http-request_deny":                  {},
	"http-request_tarpit":                {},
	"http-request_auth":                  {},
	"http-request_redirect":              {},
	"http-request_lua":                   {},
	"http-request_use-service":           {},
	"http-request_add-header":            {},
	"http-request_set-header":            {},
	"http-request_del-header":            {},
	"http-request_replace-header":        {},
	"http-request_replace-path":          {},
	"http-request_replace-value":         {},
	"http-request_set-method":            {},
	"http-request_set-path":              {},
	"http-request_set-query":             {},
	"http-request_set-uri":               {},
	"http-response_allow":                {},
	"http-response_deny":                 {},
	"http-response_lua":                  {},
	"http-response_add-header":           {},
	"http-response_set-header":           {},
	"http-response_del-header":           {},
	"http-response_replace-header":       {},
	"http-response_replace-value":        {},
	"http-response_set-status":           {},
	"http-after-response_add-header":     {},
	"http-after-response_set-header":     {},
	"http-after-response_del-header":     {},
	"http-after-response_replace-header": {},
	"http-after-response_replace-value":  {},
	"http-after-response_set-status":     {},
	"tcp-request_connection_accept":      {},
	"tcp-request_connection_reject":      {},
	"tcp-request_content_accept":         {},
	"tcp-request_content_reject":         {},
	"tcp-request_content_lua":            {},
	"tcp-request_content_use-service":    {},
	"tcp-response_content_accept":        {},
	"tcp-response_content_close":         {},
	"tcp-response_content_reject":        {},
	"tcp-response_content_lua":           {},
}

var haproxyBackendActionRequiredFields = map[string][]string{
	"use_server":                         {"server"},
	"custom":                             {"customaction"},
	"http-request_deny":                  {"deny_status"},
	"http-request_tarpit":                {"deny_status"},
	"http-request_auth":                  {"realm"},
	"http-request_redirect":              {"rule"},
	"http-request_lua":                   {"lua_function"},
	"http-request_use-service":           {"lua_function"},
	"http-response_lua":                  {"lua_function"},
	"tcp-request_content_lua":            {"lua_function"},
	"tcp-request_content_use-service":    {"lua_function"},
	"tcp-response_content_lua":           {"lua_function"},
	"http-request_add-header":            {"name", "fmt"},
	"http-request_set-header":            {"name", "fmt"},
	"http-request_del-header":            {"name"},
	"http-request_replace-header":        {"name", "find", "replace"},
	"http-request_replace-value":         {"name", "find", "replace"},
	"http-response_add-header":           {"name", "fmt"},
	"http-response_set-header":           {"name", "fmt"},
	"http-response_del-header":           {"name"},
	"http-response_replace-header":       {"name", "find", "replace"},
	"http-response_replace-value":        {"name", "find", "replace"},
	"http-after-response_add-header":     {"name", "fmt"},
	"http-after-response_set-header":     {"name", "fmt"},
	"http-after-response_del-header":     {"name"},
	"http-after-response_replace-header": {"name", "find", "replace"},
	"http-after-response_replace-value":  {"name", "find", "replace"},
	"http-request_replace-path":          {"find", "replace", "path"},
	"http-request_set-method":            {"fmt"},
	"http-request_set-path":              {"fmt"},
	"http-request_set-query":             {"fmt"},
	"http-request_set-uri":               {"fmt"},
	"http-response_set-status":           {"status", "reason"},
	"http-after-response_set-status":     {"status", "reason"},
}

type haproxyBackendActionResource struct {
	client *pfsense.Client
}

type haproxyBackendActionModel struct {
	ID           types.String `tfsdk:"id"`
	BackendName  types.String `tfsdk:"backend_name"`
	Key          types.String `tfsdk:"key"`
	Action       types.String `tfsdk:"action"`
	ACL          types.String `tfsdk:"acl"`
	Server       types.String `tfsdk:"server"`
	CustomAction types.String `tfsdk:"customaction"`
	DenyStatus   types.String `tfsdk:"deny_status"`
	Realm        types.String `tfsdk:"realm"`
	Rule         types.String `tfsdk:"rule"`
	LuaFunction  types.String `tfsdk:"lua_function"`
	Name         types.String `tfsdk:"name"`
	Fmt          types.String `tfsdk:"fmt"`
	Find         types.String `tfsdk:"find"`
	Replace      types.String `tfsdk:"replace"`
	Path         types.String `tfsdk:"path"`
	Status       types.String `tfsdk:"status"`
	Reason       types.String `tfsdk:"reason"`
	Position     types.Int64  `tfsdk:"position"`
}

type haproxyBackendActionKeys struct {
	backendName string
	key         string
}

type haproxyBackendActionPayload struct {
	action string
	acl    string
	fields map[string]string
}

func newHaproxyBackendActionResource() resource.Resource {
	return &haproxyBackendActionResource{}
}

func (r *haproxyBackendActionResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "pfsense_haproxy_backend_action"
}

func (r *haproxyBackendActionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = resourceschema.Schema{
		Description: "Manages a pfSense HAProxy backend action as an ordered child of a pfsense_haproxy_backend. pfSense backend actions do not have a stable name, so Terraform uses backend_name/key as the resource ID and re-identifies the live child by exact normalized action payload. The key is never sent to pfSense.",
		Attributes:  haproxyBackendActionResourceSchemaAttributes(),
	}
}

func (r *haproxyBackendActionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *haproxyBackendActionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing pfSense client", "The provider was not configured before creating pfsense_haproxy_backend_action.")
		return
	}

	var plan haproxyBackendActionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	keys, payload, err := validateHaproxyBackendActionPlan(plan)
	if err != nil {
		resp.Diagnostics.AddError("Invalid HAProxy backend action", err.Error())
		return
	}

	_, parentID, found, err := findHaproxyBackendByName(ctx, r.client, keys.backendName)
	if err != nil {
		resp.Diagnostics.AddError("Find parent HAProxy backend before create failed", backendActionLookupErrorDetail(keys, err))
		return
	}
	if !found {
		resp.Diagnostics.AddError("Create HAProxy backend action failed", fmt.Sprintf("Parent backend %q was not found on pfSense. Create or import pfsense_haproxy_backend.%s before managing child actions.", keys.backendName, keys.backendName))
		return
	}

	_, _, found, err = findHaproxyBackendAction(ctx, r.client, parentID, keys, payload)
	if err != nil {
		resp.Diagnostics.AddError("Check existing HAProxy backend action failed", backendActionLookupErrorDetail(keys, err))
		return
	}
	if found {
		resp.Diagnostics.AddError(
			"HAProxy backend action already exists",
			fmt.Sprintf("A pfSense HAProxy backend action with the same normalized payload already exists under backend %q. Choose a unique action payload or import the existing action with `terraform import pfsense_haproxy_backend_action.<name> %s` after cleaning up duplicates.", keys.backendName, haproxyBackendActionTerraformID(keys)),
		)
		return
	}

	if err := r.client.Post(ctx, haproxyBackendActionPath, buildHaproxyBackendActionCreatePayload(plan, parentID, payload), nil); err != nil {
		resp.Diagnostics.AddError("Create HAProxy backend action failed", err.Error())
		return
	}

	action, _, found, err := findHaproxyBackendAction(ctx, r.client, parentID, keys, payload)
	if err != nil {
		resp.Diagnostics.AddError("Read HAProxy backend action after create failed", backendActionLookupErrorDetail(keys, err))
		return
	}
	if !found {
		resp.Diagnostics.AddError(
			"Read HAProxy backend action after create failed",
			fmt.Sprintf("Created backend action %q under backend %q but GET %s did not return a unique payload match. Confirm the live UAT child response shape before relying on this resource.", keys.key, keys.backendName, haproxyBackendActionsPath),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, action)...)
}

func (r *haproxyBackendActionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing pfSense client", "The provider was not configured before reading pfsense_haproxy_backend_action.")
		return
	}

	var state haproxyBackendActionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	keys, payload, hasPayload, err := haproxyBackendActionStateKeys(state)
	if err != nil {
		resp.Diagnostics.AddError("Invalid HAProxy backend action state", err.Error())
		return
	}

	_, parentID, found, err := findHaproxyBackendByName(ctx, r.client, keys.backendName)
	if err != nil {
		resp.Diagnostics.AddError("Find parent HAProxy backend failed", backendActionLookupErrorDetail(keys, err))
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	if !hasPayload {
		state.ID = types.StringValue(haproxyBackendActionTerraformID(keys))
		state.BackendName = types.StringValue(keys.backendName)
		state.Key = types.StringValue(keys.key)
		resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
		return
	}

	action, _, found, err := findHaproxyBackendAction(ctx, r.client, parentID, keys, payload)
	if err != nil {
		resp.Diagnostics.AddError("Read HAProxy backend action failed", backendActionLookupErrorDetail(keys, err))
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, action)...)
}

func (r *haproxyBackendActionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing pfSense client", "The provider was not configured before updating pfsense_haproxy_backend_action.")
		return
	}

	var plan, prior haproxyBackendActionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}

	keys, planPayload, err := validateHaproxyBackendActionPlan(plan)
	if err != nil {
		resp.Diagnostics.AddError("Invalid HAProxy backend action", err.Error())
		return
	}
	priorKeys, priorPayload, priorHasPayload, err := haproxyBackendActionStateKeys(prior)
	if err != nil {
		resp.Diagnostics.AddError("Invalid HAProxy backend action prior state", err.Error())
		return
	}
	if keys.backendName != priorKeys.backendName || keys.key != priorKeys.key {
		resp.Diagnostics.AddError("Renaming HAProxy backend actions is not supported", "The backend name and key form the Terraform natural key. Change either value by creating a new resource and deleting the old one.")
		return
	}

	_, parentID, found, err := findHaproxyBackendByName(ctx, r.client, keys.backendName)
	if err != nil {
		resp.Diagnostics.AddError("Find parent HAProxy backend before update failed", backendActionLookupErrorDetail(keys, err))
		return
	}
	if !found {
		resp.Diagnostics.AddError("Update HAProxy backend action failed", fmt.Sprintf("Parent backend %q was not found on pfSense. Recreate it or remove the child action from Terraform state.", keys.backendName))
		return
	}

	lookupPayload := priorPayload
	if !priorHasPayload {
		lookupPayload = planPayload
	}
	_, actionID, found, err := findHaproxyBackendAction(ctx, r.client, parentID, keys, lookupPayload)
	if err != nil {
		resp.Diagnostics.AddError("Find HAProxy backend action before update failed", backendActionLookupErrorDetail(keys, err))
		return
	}
	if !found {
		resp.Diagnostics.AddError("Update HAProxy backend action failed", fmt.Sprintf("Action %q under backend %q was not found on pfSense. Recreate it or remove it from Terraform state.", keys.key, keys.backendName))
		return
	}

	if priorHasPayload {
		patch := buildHaproxyBackendActionPatch(plan, prior, parentID, actionID, planPayload)
		if len(patch) > 2 {
			if err := r.client.Patch(ctx, haproxyBackendActionPath, patch, nil); err != nil {
				resp.Diagnostics.AddError("Update HAProxy backend action failed", err.Error())
				return
			}
		}
	}

	action, _, found, err := findHaproxyBackendAction(ctx, r.client, parentID, keys, planPayload)
	if err != nil {
		resp.Diagnostics.AddError("Read HAProxy backend action after update failed", backendActionLookupErrorDetail(keys, err))
		return
	}
	if !found {
		resp.Diagnostics.AddError("Read HAProxy backend action after update failed", fmt.Sprintf("Action %q under backend %q was not returned by GET %s after PATCH.", keys.key, keys.backendName, haproxyBackendActionsPath))
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, action)...)
}

func (r *haproxyBackendActionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing pfSense client", "The provider was not configured before deleting pfsense_haproxy_backend_action.")
		return
	}

	var state haproxyBackendActionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	keys, payload, hasPayload, err := haproxyBackendActionStateKeys(state)
	if err != nil {
		resp.Diagnostics.AddError("Invalid HAProxy backend action state", err.Error())
		return
	}

	_, parentID, found, err := findHaproxyBackendByName(ctx, r.client, keys.backendName)
	if err != nil {
		resp.Diagnostics.AddError("Find parent HAProxy backend before delete failed", backendActionLookupErrorDetail(keys, err))
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	if !hasPayload {
		resp.Diagnostics.AddError("Delete HAProxy backend action failed", "The action state does not include enough payload fields to identify the pfSense child action. Add the full resource configuration, refresh state, then delete it.")
		return
	}

	_, actionID, found, err := findHaproxyBackendAction(ctx, r.client, parentID, keys, payload)
	if err != nil {
		resp.Diagnostics.AddError("Find HAProxy backend action before delete failed", backendActionLookupErrorDetail(keys, err))
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	if err := r.client.Delete(ctx, haproxyBackendActionDeletePath(parentID, actionID), nil); err != nil {
		resp.Diagnostics.AddError("Delete HAProxy backend action failed", err.Error())
		return
	}

	resp.State.RemoveResource(ctx)
}

func (r *haproxyBackendActionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	keys, err := parseHaproxyBackendActionImportID(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Invalid HAProxy backend action import ID", err.Error())
		return
	}

	model := nullHaproxyBackendActionModel()
	model.ID = types.StringValue(haproxyBackendActionTerraformID(keys))
	model.BackendName = types.StringValue(keys.backendName)
	model.Key = types.StringValue(keys.key)
	resp.Diagnostics.Append(resp.State.Set(ctx, model)...)
}

func haproxyBackendActionResourceSchemaAttributes() map[string]resourceschema.Attribute {
	attributes := map[string]resourceschema.Attribute{
		"id": resourceschema.StringAttribute{
			Computed:    true,
			Description: "Stable Terraform ID for the backend action in backend_name/key form. This is not the pfSense object ID, and key is not sent to pfSense.",
		},
		"backend_name": resourceschema.StringAttribute{
			Required:    true,
			Description: "Name of the parent pfsense_haproxy_backend. Terraform resolves the current pfSense backend object ID by this natural key before every backend action write.",
			PlanModifiers: []planmodifier.String{
				stringplanmodifier.RequiresReplace(),
			},
		},
		"key": resourceschema.StringAttribute{
			Required:    true,
			Description: "Terraform-only identity for this anonymous pfSense backend action. The key may contain only letters, numbers, dot, hyphen, and underscore, must not contain slash, and is never sent to pfSense.",
			PlanModifiers: []planmodifier.String{
				stringplanmodifier.RequiresReplace(),
			},
		},
		"action": resourceschema.StringAttribute{
			Required:    true,
			Description: "HAProxy backend action type, such as use_server, custom, http-request_deny, http-request_set-header, http-response_set-status, or tcp-request_content_lua.",
		},
		"acl": resourceschema.StringAttribute{
			Required:    true,
			Description: "ACL condition string for this action. It must identify the backend ACL condition used by pfSense.",
		},
		"position": resourceschema.Int64Attribute{
			Optional:    true,
			Computed:    true,
			Description: "Zero-based action order within the backend. When configured, Terraform sends pfREST's placement field on create and when the position changes.",
			PlanModifiers: []planmodifier.Int64{
				int64planmodifier.UseStateForUnknown(),
			},
		},
	}
	for _, attribute := range haproxyBackendActionAttributes {
		attributes[attribute.Name] = resourceschema.StringAttribute{
			Optional:    true,
			Description: attribute.Description,
		}
	}

	return attributes
}

func validateHaproxyBackendActionPlan(model haproxyBackendActionModel) (haproxyBackendActionKeys, haproxyBackendActionPayload, error) {
	keys, err := haproxyBackendActionKeysFromModel(model)
	if err != nil {
		return haproxyBackendActionKeys{}, haproxyBackendActionPayload{}, err
	}
	payload, err := haproxyBackendActionPayloadFromModel(model, true)
	if err != nil {
		return haproxyBackendActionKeys{}, haproxyBackendActionPayload{}, err
	}
	if err := validateHaproxyBackendActionPosition(model.Position); err != nil {
		return haproxyBackendActionKeys{}, haproxyBackendActionPayload{}, err
	}

	return keys, payload, nil
}

func haproxyBackendActionStateKeys(model haproxyBackendActionModel) (haproxyBackendActionKeys, haproxyBackendActionPayload, bool, error) {
	keys, err := haproxyBackendActionKeysFromModel(model)
	if err != nil {
		return haproxyBackendActionKeys{}, haproxyBackendActionPayload{}, false, err
	}
	payload, hasPayload, err := haproxyBackendActionOptionalPayloadFromModel(model)
	if err != nil {
		return haproxyBackendActionKeys{}, haproxyBackendActionPayload{}, false, err
	}

	return keys, payload, hasPayload, nil
}

func haproxyBackendActionKeysFromModel(model haproxyBackendActionModel) (haproxyBackendActionKeys, error) {
	if !model.BackendName.IsNull() && !model.BackendName.IsUnknown() && !model.Key.IsNull() && !model.Key.IsUnknown() {
		backendName, err := haproxyBackendActionBackendName(model.BackendName)
		if err != nil {
			return haproxyBackendActionKeys{}, err
		}
		key, err := haproxyBackendActionKey(model.Key)
		if err != nil {
			return haproxyBackendActionKeys{}, err
		}
		return haproxyBackendActionKeys{backendName: backendName, key: key}, nil
	}
	if !model.ID.IsNull() && !model.ID.IsUnknown() {
		return parseHaproxyBackendActionImportID(model.ID.ValueString())
	}

	return haproxyBackendActionKeys{}, fmt.Errorf("state is missing backend_name and key")
}

func haproxyBackendActionBackendName(value types.String) (string, error) {
	name, err := haproxyBackendName(value)
	if err != nil {
		return "", fmt.Errorf("backend_name %s", err.Error())
	}

	return name, nil
}

func haproxyBackendActionKey(value types.String) (string, error) {
	if value.IsNull() || value.IsUnknown() {
		return "", fmt.Errorf("key is required")
	}
	key := value.ValueString()
	trimmed := strings.TrimSpace(key)
	if trimmed == "" {
		return "", fmt.Errorf("key must not be empty")
	}
	if trimmed != key {
		return "", fmt.Errorf("key must not contain leading or trailing whitespace")
	}
	if strings.Contains(key, "/") {
		return "", fmt.Errorf("key must not contain /")
	}
	if !haproxyNamePattern.MatchString(key) {
		return "", fmt.Errorf("key may contain only letters, numbers, dot, hyphen, and underscore")
	}

	return key, nil
}

func haproxyBackendActionOptionalPayloadFromModel(model haproxyBackendActionModel) (haproxyBackendActionPayload, bool, error) {
	if model.Action.IsNull() || model.Action.IsUnknown() || model.ACL.IsNull() || model.ACL.IsUnknown() {
		return haproxyBackendActionPayload{}, false, nil
	}

	payload, err := haproxyBackendActionPayloadFromModel(model, false)
	return payload, true, err
}

func haproxyBackendActionPayloadFromModel(model haproxyBackendActionModel, require bool) (haproxyBackendActionPayload, error) {
	action, err := haproxyBackendActionAction(model.Action)
	if err != nil {
		return haproxyBackendActionPayload{}, err
	}
	acl, err := haproxyBackendActionACL(model.ACL)
	if err != nil {
		return haproxyBackendActionPayload{}, err
	}

	required := haproxyBackendActionRequiredFieldSet(action)
	fields := make(map[string]string, len(required))
	values := model.attrValues()
	for _, attribute := range haproxyBackendActionAttributes {
		value := values[attribute.Name]
		_, isRequired := required[attribute.Name]
		if isRequired {
			fieldValue, err := haproxyBackendActionConditionalField(attribute.Name, value)
			if err != nil {
				return haproxyBackendActionPayload{}, err
			}
			fields[attribute.Name] = fieldValue
			continue
		}
		if value.IsUnknown() {
			continue
		}
		if !value.IsNull() {
			return haproxyBackendActionPayload{}, fmt.Errorf("%s is not applicable when action is %q; leave irrelevant action fields null", attribute.Name, action)
		}
	}
	if require && len(fields) != len(required) {
		return haproxyBackendActionPayload{}, fmt.Errorf("action %q is missing required conditional fields", action)
	}

	return haproxyBackendActionPayload{action: action, acl: acl, fields: fields}, nil
}

func haproxyBackendActionAction(value types.String) (string, error) {
	if value.IsNull() || value.IsUnknown() {
		return "", fmt.Errorf("action is required")
	}
	action := value.ValueString()
	if strings.TrimSpace(action) != action || action == "" {
		return "", fmt.Errorf("action must be one of the documented HAProxy backend action choices")
	}
	if _, ok := haproxyBackendActionChoices[action]; !ok {
		return "", fmt.Errorf("action %q is not supported by the pfREST HAProxyBackendAction model", action)
	}

	return action, nil
}

func haproxyBackendActionACL(value types.String) (string, error) {
	if value.IsNull() || value.IsUnknown() {
		return "", fmt.Errorf("acl is required")
	}
	acl := value.ValueString()
	trimmed := strings.TrimSpace(acl)
	if trimmed == "" {
		return "", fmt.Errorf("acl must not be empty")
	}
	if trimmed != acl {
		return "", fmt.Errorf("acl must not contain leading or trailing whitespace")
	}

	return acl, nil
}

func haproxyBackendActionConditionalField(name string, value attr.Value) (string, error) {
	if value.IsNull() || value.IsUnknown() {
		return "", fmt.Errorf("%s is required for this action", name)
	}

	stringValue := value.(types.String).ValueString()
	if strings.TrimSpace(stringValue) == "" {
		return "", fmt.Errorf("%s must not be empty", name)
	}

	return stringValue, nil
}

func validateHaproxyBackendActionPosition(value types.Int64) error {
	if value.IsNull() || value.IsUnknown() {
		return nil
	}
	if value.ValueInt64() < 0 {
		return fmt.Errorf("position must be zero or greater")
	}

	return nil
}

func parseHaproxyBackendActionImportID(id string) (haproxyBackendActionKeys, error) {
	trimmed := strings.TrimSpace(id)
	backendName, key, ok := strings.Cut(trimmed, "/")
	if !ok || backendName == "" || key == "" || strings.Contains(key, "/") {
		return haproxyBackendActionKeys{}, fmt.Errorf("import pfsense_haproxy_backend_action with ID backend_name/key")
	}

	backend, err := haproxyBackendActionBackendName(types.StringValue(backendName))
	if err != nil {
		return haproxyBackendActionKeys{}, err
	}
	normalizedKey, err := haproxyBackendActionKey(types.StringValue(key))
	if err != nil {
		return haproxyBackendActionKeys{}, err
	}

	return haproxyBackendActionKeys{backendName: backend, key: normalizedKey}, nil
}

func buildHaproxyBackendActionCreatePayload(plan haproxyBackendActionModel, parentID string, payload haproxyBackendActionPayload) map[string]any {
	request := haproxyBackendActionPayloadToAPI(payload)
	request["parent_id"] = parentID
	if !plan.Position.IsNull() && !plan.Position.IsUnknown() {
		request["placement"] = plan.Position.ValueInt64()
	}

	return request
}

func buildHaproxyBackendActionPatch(plan haproxyBackendActionModel, prior haproxyBackendActionModel, parentID string, actionID string, planPayload haproxyBackendActionPayload) map[string]any {
	patch := map[string]any{
		"parent_id": parentID,
		"id":        actionID,
	}
	if !plan.Action.Equal(prior.Action) {
		patch["action"] = planPayload.action
		for name, value := range haproxyBackendActionPayloadToAPI(planPayload) {
			if name == "action" || name == "acl" {
				continue
			}
			patch[name] = value
		}
	}
	if !plan.ACL.Equal(prior.ACL) {
		patch["acl"] = planPayload.acl
	}

	planValues := plan.attrValues()
	priorValues := prior.attrValues()
	required := haproxyBackendActionRequiredFieldSet(planPayload.action)
	for _, attribute := range haproxyBackendActionAttributes {
		if _, ok := required[attribute.Name]; !ok {
			continue
		}
		planned := planValues[attribute.Name]
		if planned.IsUnknown() {
			continue
		}
		if planned.Equal(priorValues[attribute.Name]) && plan.Action.Equal(prior.Action) {
			continue
		}
		patch[attribute.JSONName] = planned.(types.String).ValueString()
	}
	if !plan.Position.IsNull() && !plan.Position.IsUnknown() && !plan.Position.Equal(prior.Position) {
		patch["placement"] = plan.Position.ValueInt64()
	}

	return patch
}

func findHaproxyBackendAction(ctx context.Context, client *pfsense.Client, parentID string, keys haproxyBackendActionKeys, desired haproxyBackendActionPayload) (haproxyBackendActionModel, string, bool, error) {
	var raw any
	if err := client.Get(ctx, haproxyBackendActionsQueryPath(parentID), &raw); err != nil {
		return haproxyBackendActionModel{}, "", false, err
	}

	payloads, err := haproxyBackendActionPayloadList(raw)
	if err != nil {
		return haproxyBackendActionModel{}, "", false, err
	}

	var matched map[string]any
	var matchedPosition int64
	for index, payload := range payloads {
		candidate, err := haproxyBackendActionPayloadFromAPI(payload)
		if err != nil {
			continue
		}
		if !candidate.equal(desired) {
			continue
		}
		if matched != nil {
			return haproxyBackendActionModel{}, "", false, fmt.Errorf("multiple HAProxy backend actions matching key %q payload were returned under backend %q; make the action payload unique or clean up duplicate pfSense actions before import/management", keys.key, keys.backendName)
		}
		matched = payload
		matchedPosition = int64(index)
	}

	if matched == nil {
		return haproxyBackendActionModel{}, "", false, nil
	}

	apiID, err := apiRequiredScalarStringWithLabel(matched, "HAProxy backend action", "id")
	if err != nil {
		return haproxyBackendActionModel{}, "", false, fmt.Errorf("%w; confirm UAT returns child object IDs from GET %s before using update/delete", err, haproxyBackendActionsPath)
	}
	model, err := haproxyBackendActionModelFromAPI(matched, keys, matchedPosition)
	if err != nil {
		return haproxyBackendActionModel{}, "", false, err
	}

	return model, apiID, true, nil
}

func haproxyBackendActionPayloadFromAPI(payload map[string]any) (haproxyBackendActionPayload, error) {
	actionValue, err := apiRequiredStringWithLabel(payload, "HAProxy backend action", "action")
	if err != nil {
		return haproxyBackendActionPayload{}, err
	}
	action, err := haproxyBackendActionAction(types.StringValue(actionValue))
	if err != nil {
		return haproxyBackendActionPayload{}, fmt.Errorf("HAProxy backend action action %s", err.Error())
	}
	aclValue, err := apiRequiredStringWithLabel(payload, "HAProxy backend action", "acl")
	if err != nil {
		return haproxyBackendActionPayload{}, err
	}
	acl, err := haproxyBackendActionACL(types.StringValue(aclValue))
	if err != nil {
		return haproxyBackendActionPayload{}, fmt.Errorf("HAProxy backend action acl %s", err.Error())
	}

	required := haproxyBackendActionRequiredFieldSet(action)
	fields := make(map[string]string, len(required))
	for _, attribute := range haproxyBackendActionAttributes {
		if _, ok := required[attribute.Name]; !ok {
			continue
		}
		fieldValue, err := apiRequiredActionStringWithLabel(payload, "HAProxy backend action", action, attribute)
		if err != nil {
			return haproxyBackendActionPayload{}, err
		}
		fields[attribute.Name] = fieldValue
	}

	return haproxyBackendActionPayload{action: action, acl: acl, fields: fields}, nil
}

func apiRequiredActionStringWithLabel(payload map[string]any, label string, action string, attribute backendActionAttribute) (string, error) {
	internalName := action + attribute.InternalSuffix
	value, name, ok := apiValue(payload, attribute.JSONName, internalName)
	if !ok || value == nil {
		return "", fmt.Errorf("%s response did not include required string field %q", label, attribute.JSONName)
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

func haproxyBackendActionPayloadList(raw any) ([]map[string]any, error) {
	if raw == nil {
		return nil, nil
	}

	switch typed := raw.(type) {
	case []any:
		payloads := make([]map[string]any, 0, len(typed))
		for index, item := range typed {
			payload, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("HAProxy backend actions response item %d has unsupported type %T", index, item)
			}
			payloads = append(payloads, payload)
		}
		return payloads, nil
	case []map[string]any:
		return typed, nil
	case map[string]any:
		return []map[string]any{typed}, nil
	default:
		return nil, fmt.Errorf("HAProxy backend actions response has unsupported type %T; confirm the live UAT /services/haproxy/backend/actions schema", raw)
	}
}

func haproxyBackendActionModelFromAPI(payload map[string]any, keys haproxyBackendActionKeys, position int64) (haproxyBackendActionModel, error) {
	actionModel := nullHaproxyBackendActionModel()
	normalized, err := haproxyBackendActionPayloadFromAPI(payload)
	if err != nil {
		return actionModel, err
	}

	actionModel.ID = types.StringValue(haproxyBackendActionTerraformID(keys))
	actionModel.BackendName = types.StringValue(keys.backendName)
	actionModel.Key = types.StringValue(keys.key)
	actionModel.Action = types.StringValue(normalized.action)
	actionModel.ACL = types.StringValue(normalized.acl)
	actionModel.Position = types.Int64Value(position)
	actionModel.setPayloadFields(normalized)

	return actionModel, nil
}

func nullHaproxyBackendActionModel() haproxyBackendActionModel {
	return haproxyBackendActionModel{
		ID:           types.StringNull(),
		BackendName:  types.StringNull(),
		Key:          types.StringNull(),
		Action:       types.StringNull(),
		ACL:          types.StringNull(),
		Server:       types.StringNull(),
		CustomAction: types.StringNull(),
		DenyStatus:   types.StringNull(),
		Realm:        types.StringNull(),
		Rule:         types.StringNull(),
		LuaFunction:  types.StringNull(),
		Name:         types.StringNull(),
		Fmt:          types.StringNull(),
		Find:         types.StringNull(),
		Replace:      types.StringNull(),
		Path:         types.StringNull(),
		Status:       types.StringNull(),
		Reason:       types.StringNull(),
		Position:     types.Int64Null(),
	}
}

func (m haproxyBackendActionModel) attrValues() map[string]attr.Value {
	return map[string]attr.Value{
		"server":       m.Server,
		"customaction": m.CustomAction,
		"deny_status":  m.DenyStatus,
		"realm":        m.Realm,
		"rule":         m.Rule,
		"lua_function": m.LuaFunction,
		"name":         m.Name,
		"fmt":          m.Fmt,
		"find":         m.Find,
		"replace":      m.Replace,
		"path":         m.Path,
		"status":       m.Status,
		"reason":       m.Reason,
	}
}

func (m *haproxyBackendActionModel) setPayloadFields(payload haproxyBackendActionPayload) {
	for name, value := range payload.fields {
		switch name {
		case "server":
			m.Server = types.StringValue(value)
		case "customaction":
			m.CustomAction = types.StringValue(value)
		case "deny_status":
			m.DenyStatus = types.StringValue(value)
		case "realm":
			m.Realm = types.StringValue(value)
		case "rule":
			m.Rule = types.StringValue(value)
		case "lua_function":
			m.LuaFunction = types.StringValue(value)
		case "name":
			m.Name = types.StringValue(value)
		case "fmt":
			m.Fmt = types.StringValue(value)
		case "find":
			m.Find = types.StringValue(value)
		case "replace":
			m.Replace = types.StringValue(value)
		case "path":
			m.Path = types.StringValue(value)
		case "status":
			m.Status = types.StringValue(value)
		case "reason":
			m.Reason = types.StringValue(value)
		}
	}
}

func haproxyBackendActionPayloadToAPI(payload haproxyBackendActionPayload) map[string]any {
	request := map[string]any{
		"action": payload.action,
		"acl":    payload.acl,
	}
	for _, attribute := range haproxyBackendActionAttributes {
		if value, ok := payload.fields[attribute.Name]; ok {
			request[attribute.JSONName] = value
		}
	}

	return request
}

func haproxyBackendActionRequiredFieldSet(action string) map[string]struct{} {
	fields := haproxyBackendActionRequiredFields[action]
	result := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		result[field] = struct{}{}
	}

	return result
}

func (p haproxyBackendActionPayload) equal(other haproxyBackendActionPayload) bool {
	if p.action != other.action || p.acl != other.acl || len(p.fields) != len(other.fields) {
		return false
	}
	for name, value := range p.fields {
		if other.fields[name] != value {
			return false
		}
	}

	return true
}

func haproxyBackendActionsQueryPath(parentID string) string {
	values := url.Values{}
	values.Set("parent_id", parentID)
	return haproxyBackendActionsPath + "?" + values.Encode()
}

func haproxyBackendActionDeletePath(parentID string, actionID string) string {
	values := url.Values{}
	values.Set("parent_id", parentID)
	values.Set("id", actionID)
	return haproxyBackendActionPath + "?" + values.Encode()
}

func haproxyBackendActionTerraformID(keys haproxyBackendActionKeys) string {
	return keys.backendName + "/" + keys.key
}

func backendActionLookupErrorDetail(keys haproxyBackendActionKeys, err error) string {
	return fmt.Sprintf("%s. Confirm GET %s is available on UAT, accepts parent_id query filters, returns ordered backend action objects with action payload fields, and includes the transient pfSense child object id required for update/delete. Backend name: %q. Terraform key: %q.", err.Error(), haproxyBackendActionsPath, keys.backendName, keys.key)
}
