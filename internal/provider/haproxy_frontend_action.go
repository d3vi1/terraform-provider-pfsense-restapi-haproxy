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
	haproxyFrontendActionPath  = "/services/haproxy/frontend/action"
	haproxyFrontendActionsPath = "/services/haproxy/frontend/actions"
)

var (
	_ resource.Resource                = (*haproxyFrontendActionResource)(nil)
	_ resource.ResourceWithConfigure   = (*haproxyFrontendActionResource)(nil)
	_ resource.ResourceWithImportState = (*haproxyFrontendActionResource)(nil)
)

type frontendActionAttribute struct {
	Name           string
	JSONName       string
	InternalSuffix string
	Description    string
}

var haproxyFrontendActionAttributes = []frontendActionAttribute{
	{Name: "backend", JSONName: "backend", InternalSuffix: "backend", Description: "Backend selected by use_backend."},
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

var haproxyFrontendActionChoices = map[string]struct{}{
	"use_backend":                        {},
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

var haproxyFrontendActionRequiredFields = map[string][]string{
	"use_backend":                        {"backend"},
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

type haproxyFrontendActionResource struct {
	client *pfsense.Client
}

type haproxyFrontendActionModel struct {
	ID           types.String `tfsdk:"id"`
	FrontendName types.String `tfsdk:"frontend_name"`
	Key          types.String `tfsdk:"key"`
	Action       types.String `tfsdk:"action"`
	ACL          types.String `tfsdk:"acl"`
	Backend      types.String `tfsdk:"backend"`
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

type haproxyFrontendActionKeys struct {
	frontendName string
	key          string
}

type haproxyFrontendActionPayload struct {
	action string
	acl    string
	fields map[string]string
}

func newHaproxyFrontendActionResource() resource.Resource {
	return &haproxyFrontendActionResource{}
}

func (r *haproxyFrontendActionResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "pfsense_haproxy_frontend_action"
}

func (r *haproxyFrontendActionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = resourceschema.Schema{
		Description: "Manages a pfSense HAProxy frontend action as an ordered child of a pfsense_haproxy_frontend. pfSense frontend actions do not have a stable name, so Terraform uses frontend_name/key as the resource ID and re-identifies the live child by exact normalized action payload. The key is never sent to pfSense.",
		Attributes:  haproxyFrontendActionResourceSchemaAttributes(),
	}
}

func (r *haproxyFrontendActionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *haproxyFrontendActionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing pfSense client", "The provider was not configured before creating pfsense_haproxy_frontend_action.")
		return
	}

	var plan haproxyFrontendActionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	keys, payload, err := validateHaproxyFrontendActionPlan(plan)
	if err != nil {
		resp.Diagnostics.AddError("Invalid HAProxy frontend action", err.Error())
		return
	}

	_, parentID, found, err := findHaproxyFrontendByName(ctx, r.client, keys.frontendName)
	if err != nil {
		resp.Diagnostics.AddError("Find parent HAProxy frontend before create failed", frontendActionLookupErrorDetail(keys, err))
		return
	}
	if !found {
		resp.Diagnostics.AddError("Create HAProxy frontend action failed", fmt.Sprintf("Parent frontend %q was not found on pfSense. Create or import pfsense_haproxy_frontend.%s before managing child actions.", keys.frontendName, keys.frontendName))
		return
	}

	_, _, found, err = findHaproxyFrontendAction(ctx, r.client, parentID, keys, payload)
	if err != nil {
		resp.Diagnostics.AddError("Check existing HAProxy frontend action failed", frontendActionLookupErrorDetail(keys, err))
		return
	}
	if found {
		resp.Diagnostics.AddError(
			"HAProxy frontend action already exists",
			fmt.Sprintf("A pfSense HAProxy frontend action with the same normalized payload already exists under frontend %q. Choose a unique action payload or import the existing action with `terraform import pfsense_haproxy_frontend_action.<name> %s` after cleaning up duplicates.", keys.frontendName, haproxyFrontendActionTerraformID(keys)),
		)
		return
	}

	if err := r.client.Post(ctx, haproxyFrontendActionPath, buildHaproxyFrontendActionCreatePayload(plan, parentID, payload), nil); err != nil {
		resp.Diagnostics.AddError("Create HAProxy frontend action failed", err.Error())
		return
	}

	action, _, found, err := findHaproxyFrontendAction(ctx, r.client, parentID, keys, payload)
	if err != nil {
		resp.Diagnostics.AddError("Read HAProxy frontend action after create failed", frontendActionLookupErrorDetail(keys, err))
		return
	}
	if !found {
		resp.Diagnostics.AddError(
			"Read HAProxy frontend action after create failed",
			fmt.Sprintf("Created frontend action %q under frontend %q but GET %s did not return a unique payload match. Confirm the live UAT child response shape before relying on this resource.", keys.key, keys.frontendName, haproxyFrontendActionsPath),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, action)...)
}

func (r *haproxyFrontendActionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing pfSense client", "The provider was not configured before reading pfsense_haproxy_frontend_action.")
		return
	}

	var state haproxyFrontendActionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	keys, payload, hasPayload, err := haproxyFrontendActionStateKeys(state)
	if err != nil {
		resp.Diagnostics.AddError("Invalid HAProxy frontend action state", err.Error())
		return
	}

	_, parentID, found, err := findHaproxyFrontendByName(ctx, r.client, keys.frontendName)
	if err != nil {
		resp.Diagnostics.AddError("Find parent HAProxy frontend failed", frontendActionLookupErrorDetail(keys, err))
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	if !hasPayload {
		state.ID = types.StringValue(haproxyFrontendActionTerraformID(keys))
		state.FrontendName = types.StringValue(keys.frontendName)
		state.Key = types.StringValue(keys.key)
		resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
		return
	}

	action, _, found, err := findHaproxyFrontendAction(ctx, r.client, parentID, keys, payload)
	if err != nil {
		resp.Diagnostics.AddError("Read HAProxy frontend action failed", frontendActionLookupErrorDetail(keys, err))
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, action)...)
}

func (r *haproxyFrontendActionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing pfSense client", "The provider was not configured before updating pfsense_haproxy_frontend_action.")
		return
	}

	var plan, prior haproxyFrontendActionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}

	keys, planPayload, err := validateHaproxyFrontendActionPlan(plan)
	if err != nil {
		resp.Diagnostics.AddError("Invalid HAProxy frontend action", err.Error())
		return
	}
	priorKeys, priorPayload, priorHasPayload, err := haproxyFrontendActionStateKeys(prior)
	if err != nil {
		resp.Diagnostics.AddError("Invalid HAProxy frontend action prior state", err.Error())
		return
	}
	if keys.frontendName != priorKeys.frontendName || keys.key != priorKeys.key {
		resp.Diagnostics.AddError("Renaming HAProxy frontend actions is not supported", "The frontend name and key form the Terraform natural key. Change either value by creating a new resource and deleting the old one.")
		return
	}

	_, parentID, found, err := findHaproxyFrontendByName(ctx, r.client, keys.frontendName)
	if err != nil {
		resp.Diagnostics.AddError("Find parent HAProxy frontend before update failed", frontendActionLookupErrorDetail(keys, err))
		return
	}
	if !found {
		resp.Diagnostics.AddError("Update HAProxy frontend action failed", fmt.Sprintf("Parent frontend %q was not found on pfSense. Recreate it or remove the child action from Terraform state.", keys.frontendName))
		return
	}

	lookupPayload := priorPayload
	if !priorHasPayload {
		lookupPayload = planPayload
	}
	currentAction, actionID, found, err := findHaproxyFrontendAction(ctx, r.client, parentID, keys, lookupPayload)
	if err != nil {
		resp.Diagnostics.AddError("Find HAProxy frontend action before update failed", frontendActionLookupErrorDetail(keys, err))
		return
	}
	if !found {
		resp.Diagnostics.AddError("Update HAProxy frontend action failed", fmt.Sprintf("Action %q under frontend %q was not found on pfSense. Recreate it or remove it from Terraform state.", keys.key, keys.frontendName))
		return
	}

	if priorHasPayload {
		patch := buildHaproxyFrontendActionPatch(plan, prior, parentID, actionID, planPayload)
		if len(patch) > 2 {
			if err := r.client.Patch(ctx, haproxyFrontendActionPath, patch, nil); err != nil {
				resp.Diagnostics.AddError("Update HAProxy frontend action failed", err.Error())
				return
			}
		}
	} else if !plan.Position.IsNull() && !plan.Position.IsUnknown() && !plan.Position.Equal(currentAction.Position) {
		patch := map[string]any{
			"parent_id": parentID,
			"id":        actionID,
			"placement": plan.Position.ValueInt64(),
		}
		if err := r.client.Patch(ctx, haproxyFrontendActionPath, patch, nil); err != nil {
			resp.Diagnostics.AddError("Update HAProxy frontend action failed", err.Error())
			return
		}
	}

	action, _, found, err := findHaproxyFrontendAction(ctx, r.client, parentID, keys, planPayload)
	if err != nil {
		resp.Diagnostics.AddError("Read HAProxy frontend action after update failed", frontendActionLookupErrorDetail(keys, err))
		return
	}
	if !found {
		resp.Diagnostics.AddError("Read HAProxy frontend action after update failed", fmt.Sprintf("Action %q under frontend %q was not returned by GET %s after PATCH.", keys.key, keys.frontendName, haproxyFrontendActionsPath))
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, action)...)
}

func (r *haproxyFrontendActionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing pfSense client", "The provider was not configured before deleting pfsense_haproxy_frontend_action.")
		return
	}

	var state haproxyFrontendActionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	keys, payload, hasPayload, err := haproxyFrontendActionStateKeys(state)
	if err != nil {
		resp.Diagnostics.AddError("Invalid HAProxy frontend action state", err.Error())
		return
	}

	_, parentID, found, err := findHaproxyFrontendByName(ctx, r.client, keys.frontendName)
	if err != nil {
		resp.Diagnostics.AddError("Find parent HAProxy frontend before delete failed", frontendActionLookupErrorDetail(keys, err))
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	if !hasPayload {
		resp.Diagnostics.AddError("Delete HAProxy frontend action failed", "The action state does not include enough payload fields to identify the pfSense child action. Add the full resource configuration, refresh state, then delete it.")
		return
	}

	_, actionID, found, err := findHaproxyFrontendAction(ctx, r.client, parentID, keys, payload)
	if err != nil {
		resp.Diagnostics.AddError("Find HAProxy frontend action before delete failed", frontendActionLookupErrorDetail(keys, err))
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	if err := r.client.Delete(ctx, haproxyFrontendActionDeletePath(parentID, actionID), nil); err != nil {
		resp.Diagnostics.AddError("Delete HAProxy frontend action failed", err.Error())
		return
	}

	resp.State.RemoveResource(ctx)
}

func (r *haproxyFrontendActionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	keys, err := parseHaproxyFrontendActionImportID(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Invalid HAProxy frontend action import ID", err.Error())
		return
	}

	model := nullHaproxyFrontendActionModel()
	model.ID = types.StringValue(haproxyFrontendActionTerraformID(keys))
	model.FrontendName = types.StringValue(keys.frontendName)
	model.Key = types.StringValue(keys.key)
	resp.Diagnostics.Append(resp.State.Set(ctx, model)...)
}

func haproxyFrontendActionResourceSchemaAttributes() map[string]resourceschema.Attribute {
	attributes := map[string]resourceschema.Attribute{
		"id": resourceschema.StringAttribute{
			Computed:    true,
			Description: "Stable Terraform ID for the frontend action in frontend_name/key form. This is not the pfSense object ID, and key is not sent to pfSense.",
		},
		"frontend_name": resourceschema.StringAttribute{
			Required:    true,
			Description: "Name of the parent pfsense_haproxy_frontend. Terraform resolves the current pfSense frontend object ID by this natural key before every frontend action write.",
			PlanModifiers: []planmodifier.String{
				stringplanmodifier.RequiresReplace(),
			},
		},
		"key": resourceschema.StringAttribute{
			Required:    true,
			Description: "Terraform-only identity for this anonymous pfSense frontend action. The key may contain only letters, numbers, dot, hyphen, and underscore, must not contain slash, and is never sent to pfSense.",
			PlanModifiers: []planmodifier.String{
				stringplanmodifier.RequiresReplace(),
			},
		},
		"action": resourceschema.StringAttribute{
			Required:    true,
			Description: "HAProxy frontend action type, such as use_backend, custom, http-request_deny, http-request_set-header, http-response_set-status, or tcp-request_content_lua.",
		},
		"acl": resourceschema.StringAttribute{
			Required:    true,
			Description: "ACL condition string for this action. It must identify the frontend ACL condition used by pfSense.",
		},
		"position": resourceschema.Int64Attribute{
			Optional:    true,
			Computed:    true,
			Description: "Zero-based action order within the frontend. When configured, Terraform sends pfREST's placement field on create and when the position changes.",
			PlanModifiers: []planmodifier.Int64{
				int64planmodifier.UseStateForUnknown(),
			},
		},
	}
	for _, attribute := range haproxyFrontendActionAttributes {
		attributes[attribute.Name] = resourceschema.StringAttribute{
			Optional:    true,
			Description: attribute.Description,
		}
	}

	return attributes
}

func validateHaproxyFrontendActionPlan(model haproxyFrontendActionModel) (haproxyFrontendActionKeys, haproxyFrontendActionPayload, error) {
	keys, err := haproxyFrontendActionKeysFromModel(model)
	if err != nil {
		return haproxyFrontendActionKeys{}, haproxyFrontendActionPayload{}, err
	}
	payload, err := haproxyFrontendActionPayloadFromModel(model, true)
	if err != nil {
		return haproxyFrontendActionKeys{}, haproxyFrontendActionPayload{}, err
	}
	if err := validateHaproxyFrontendActionPosition(model.Position); err != nil {
		return haproxyFrontendActionKeys{}, haproxyFrontendActionPayload{}, err
	}

	return keys, payload, nil
}

func haproxyFrontendActionStateKeys(model haproxyFrontendActionModel) (haproxyFrontendActionKeys, haproxyFrontendActionPayload, bool, error) {
	keys, err := haproxyFrontendActionKeysFromModel(model)
	if err != nil {
		return haproxyFrontendActionKeys{}, haproxyFrontendActionPayload{}, false, err
	}
	payload, hasPayload, err := haproxyFrontendActionOptionalPayloadFromModel(model)
	if err != nil {
		return haproxyFrontendActionKeys{}, haproxyFrontendActionPayload{}, false, err
	}

	return keys, payload, hasPayload, nil
}

func haproxyFrontendActionKeysFromModel(model haproxyFrontendActionModel) (haproxyFrontendActionKeys, error) {
	if !model.FrontendName.IsNull() && !model.FrontendName.IsUnknown() && !model.Key.IsNull() && !model.Key.IsUnknown() {
		frontendName, err := haproxyFrontendActionFrontendName(model.FrontendName)
		if err != nil {
			return haproxyFrontendActionKeys{}, err
		}
		key, err := haproxyFrontendActionKey(model.Key)
		if err != nil {
			return haproxyFrontendActionKeys{}, err
		}
		return haproxyFrontendActionKeys{frontendName: frontendName, key: key}, nil
	}
	if !model.ID.IsNull() && !model.ID.IsUnknown() {
		return parseHaproxyFrontendActionImportID(model.ID.ValueString())
	}

	return haproxyFrontendActionKeys{}, fmt.Errorf("state is missing frontend_name and key")
}

func haproxyFrontendActionFrontendName(value types.String) (string, error) {
	name, err := haproxyFrontendName(value)
	if err != nil {
		return "", fmt.Errorf("frontend_name %s", err.Error())
	}

	return name, nil
}

func haproxyFrontendActionKey(value types.String) (string, error) {
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

func haproxyFrontendActionOptionalPayloadFromModel(model haproxyFrontendActionModel) (haproxyFrontendActionPayload, bool, error) {
	if model.Action.IsNull() || model.Action.IsUnknown() || model.ACL.IsNull() || model.ACL.IsUnknown() {
		return haproxyFrontendActionPayload{}, false, nil
	}

	payload, err := haproxyFrontendActionPayloadFromModel(model, false)
	return payload, true, err
}

func haproxyFrontendActionPayloadFromModel(model haproxyFrontendActionModel, require bool) (haproxyFrontendActionPayload, error) {
	action, err := haproxyFrontendActionAction(model.Action)
	if err != nil {
		return haproxyFrontendActionPayload{}, err
	}
	acl, err := haproxyFrontendActionACL(model.ACL)
	if err != nil {
		return haproxyFrontendActionPayload{}, err
	}

	required := haproxyFrontendActionRequiredFieldSet(action)
	fields := make(map[string]string, len(required))
	values := model.attrValues()
	for _, attribute := range haproxyFrontendActionAttributes {
		value := values[attribute.Name]
		_, isRequired := required[attribute.Name]
		if isRequired {
			fieldValue, err := haproxyFrontendActionConditionalField(attribute.Name, value)
			if err != nil {
				return haproxyFrontendActionPayload{}, err
			}
			fields[attribute.Name] = fieldValue
			continue
		}
		if value.IsUnknown() {
			continue
		}
		if !value.IsNull() {
			return haproxyFrontendActionPayload{}, fmt.Errorf("%s is not applicable when action is %q; leave irrelevant action fields null", attribute.Name, action)
		}
	}
	if require && len(fields) != len(required) {
		return haproxyFrontendActionPayload{}, fmt.Errorf("action %q is missing required conditional fields", action)
	}

	return haproxyFrontendActionPayload{action: action, acl: acl, fields: fields}, nil
}

func haproxyFrontendActionAction(value types.String) (string, error) {
	if value.IsNull() || value.IsUnknown() {
		return "", fmt.Errorf("action is required")
	}
	action := value.ValueString()
	if strings.TrimSpace(action) != action || action == "" {
		return "", fmt.Errorf("action must be one of the documented HAProxy frontend action choices")
	}
	if _, ok := haproxyFrontendActionChoices[action]; !ok {
		return "", fmt.Errorf("action %q is not supported by the pfREST HAProxyFrontendAction model", action)
	}

	return action, nil
}

func haproxyFrontendActionACL(value types.String) (string, error) {
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

func haproxyFrontendActionConditionalField(name string, value attr.Value) (string, error) {
	if value.IsNull() || value.IsUnknown() {
		return "", fmt.Errorf("%s is required for this action", name)
	}

	stringValue := value.(types.String).ValueString()
	if strings.TrimSpace(stringValue) == "" {
		return "", fmt.Errorf("%s must not be empty", name)
	}

	return stringValue, nil
}

func validateHaproxyFrontendActionPosition(value types.Int64) error {
	if value.IsNull() || value.IsUnknown() {
		return nil
	}
	if value.ValueInt64() < 0 {
		return fmt.Errorf("position must be zero or greater")
	}

	return nil
}

func parseHaproxyFrontendActionImportID(id string) (haproxyFrontendActionKeys, error) {
	trimmed := strings.TrimSpace(id)
	frontendName, key, ok := strings.Cut(trimmed, "/")
	if !ok || frontendName == "" || key == "" || strings.Contains(key, "/") {
		return haproxyFrontendActionKeys{}, fmt.Errorf("import pfsense_haproxy_frontend_action with ID frontend_name/key")
	}

	frontend, err := haproxyFrontendActionFrontendName(types.StringValue(frontendName))
	if err != nil {
		return haproxyFrontendActionKeys{}, err
	}
	normalizedKey, err := haproxyFrontendActionKey(types.StringValue(key))
	if err != nil {
		return haproxyFrontendActionKeys{}, err
	}

	return haproxyFrontendActionKeys{frontendName: frontend, key: normalizedKey}, nil
}

func buildHaproxyFrontendActionCreatePayload(plan haproxyFrontendActionModel, parentID string, payload haproxyFrontendActionPayload) map[string]any {
	request := haproxyFrontendActionPayloadToAPI(payload)
	request["parent_id"] = parentID
	if !plan.Position.IsNull() && !plan.Position.IsUnknown() {
		request["placement"] = plan.Position.ValueInt64()
	}

	return request
}

func buildHaproxyFrontendActionPatch(plan haproxyFrontendActionModel, prior haproxyFrontendActionModel, parentID string, actionID string, planPayload haproxyFrontendActionPayload) map[string]any {
	patch := map[string]any{
		"parent_id": parentID,
		"id":        actionID,
	}
	if !plan.Action.Equal(prior.Action) {
		patch["action"] = planPayload.action
		for name, value := range haproxyFrontendActionPayloadToAPI(planPayload) {
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
	required := haproxyFrontendActionRequiredFieldSet(planPayload.action)
	for _, attribute := range haproxyFrontendActionAttributes {
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

func findHaproxyFrontendAction(ctx context.Context, client *pfsense.Client, parentID string, keys haproxyFrontendActionKeys, desired haproxyFrontendActionPayload) (haproxyFrontendActionModel, string, bool, error) {
	var raw any
	if err := client.Get(ctx, haproxyFrontendActionsQueryPath(parentID), &raw); err != nil {
		return haproxyFrontendActionModel{}, "", false, err
	}

	payloads, err := haproxyFrontendActionPayloadList(raw)
	if err != nil {
		return haproxyFrontendActionModel{}, "", false, err
	}

	var matched map[string]any
	var matchedPosition int64
	for index, payload := range payloads {
		candidate, err := haproxyFrontendActionPayloadFromAPI(payload)
		if err != nil {
			continue
		}
		if !candidate.equal(desired) {
			continue
		}
		if matched != nil {
			return haproxyFrontendActionModel{}, "", false, fmt.Errorf("multiple HAProxy frontend actions matching key %q payload were returned under frontend %q; make the action payload unique or clean up duplicate pfSense actions before import/management", keys.key, keys.frontendName)
		}
		matched = payload
		matchedPosition = int64(index)
	}

	if matched == nil {
		return haproxyFrontendActionModel{}, "", false, nil
	}

	apiID, err := apiRequiredScalarStringWithLabel(matched, "HAProxy frontend action", "id")
	if err != nil {
		return haproxyFrontendActionModel{}, "", false, fmt.Errorf("%w; confirm UAT returns child object IDs from GET %s before using update/delete", err, haproxyFrontendActionsPath)
	}
	model, err := haproxyFrontendActionModelFromAPI(matched, keys, matchedPosition)
	if err != nil {
		return haproxyFrontendActionModel{}, "", false, err
	}

	return model, apiID, true, nil
}

func haproxyFrontendActionPayloadFromAPI(payload map[string]any) (haproxyFrontendActionPayload, error) {
	actionValue, err := apiRequiredStringWithLabel(payload, "HAProxy frontend action", "action")
	if err != nil {
		return haproxyFrontendActionPayload{}, err
	}
	action, err := haproxyFrontendActionAction(types.StringValue(actionValue))
	if err != nil {
		return haproxyFrontendActionPayload{}, fmt.Errorf("HAProxy frontend action action %s", err.Error())
	}
	aclValue, err := apiRequiredStringWithLabel(payload, "HAProxy frontend action", "acl")
	if err != nil {
		return haproxyFrontendActionPayload{}, err
	}
	acl, err := haproxyFrontendActionACL(types.StringValue(aclValue))
	if err != nil {
		return haproxyFrontendActionPayload{}, fmt.Errorf("HAProxy frontend action acl %s", err.Error())
	}

	required := haproxyFrontendActionRequiredFieldSet(action)
	fields := make(map[string]string, len(required))
	for _, attribute := range haproxyFrontendActionAttributes {
		if _, ok := required[attribute.Name]; !ok {
			continue
		}
		fieldValue, err := apiRequiredFrontendActionStringWithLabel(payload, "HAProxy frontend action", action, attribute)
		if err != nil {
			return haproxyFrontendActionPayload{}, err
		}
		fields[attribute.Name] = fieldValue
	}

	return haproxyFrontendActionPayload{action: action, acl: acl, fields: fields}, nil
}

func apiRequiredFrontendActionStringWithLabel(payload map[string]any, label string, action string, attribute frontendActionAttribute) (string, error) {
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

func haproxyFrontendActionPayloadList(raw any) ([]map[string]any, error) {
	if raw == nil {
		return nil, nil
	}

	switch typed := raw.(type) {
	case []any:
		payloads := make([]map[string]any, 0, len(typed))
		for index, item := range typed {
			payload, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("HAProxy frontend actions response item %d has unsupported type %T", index, item)
			}
			payloads = append(payloads, payload)
		}
		return payloads, nil
	case []map[string]any:
		return typed, nil
	case map[string]any:
		return []map[string]any{typed}, nil
	default:
		return nil, fmt.Errorf("HAProxy frontend actions response has unsupported type %T; confirm the live UAT /services/haproxy/frontend/actions schema", raw)
	}
}

func haproxyFrontendActionModelFromAPI(payload map[string]any, keys haproxyFrontendActionKeys, position int64) (haproxyFrontendActionModel, error) {
	actionModel := nullHaproxyFrontendActionModel()
	normalized, err := haproxyFrontendActionPayloadFromAPI(payload)
	if err != nil {
		return actionModel, err
	}

	actionModel.ID = types.StringValue(haproxyFrontendActionTerraformID(keys))
	actionModel.FrontendName = types.StringValue(keys.frontendName)
	actionModel.Key = types.StringValue(keys.key)
	actionModel.Action = types.StringValue(normalized.action)
	actionModel.ACL = types.StringValue(normalized.acl)
	actionModel.Position = types.Int64Value(position)
	actionModel.setPayloadFields(normalized)

	return actionModel, nil
}

func nullHaproxyFrontendActionModel() haproxyFrontendActionModel {
	return haproxyFrontendActionModel{
		ID:           types.StringNull(),
		FrontendName: types.StringNull(),
		Key:          types.StringNull(),
		Action:       types.StringNull(),
		ACL:          types.StringNull(),
		Backend:      types.StringNull(),
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

func (m haproxyFrontendActionModel) attrValues() map[string]attr.Value {
	return map[string]attr.Value{
		"backend":      m.Backend,
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

func (m *haproxyFrontendActionModel) setPayloadFields(payload haproxyFrontendActionPayload) {
	for name, value := range payload.fields {
		switch name {
		case "backend":
			m.Backend = types.StringValue(value)
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

func haproxyFrontendActionPayloadToAPI(payload haproxyFrontendActionPayload) map[string]any {
	request := map[string]any{
		"action": payload.action,
		"acl":    payload.acl,
	}
	for _, attribute := range haproxyFrontendActionAttributes {
		if value, ok := payload.fields[attribute.Name]; ok {
			request[attribute.JSONName] = value
		}
	}

	return request
}

func haproxyFrontendActionRequiredFieldSet(action string) map[string]struct{} {
	fields := haproxyFrontendActionRequiredFields[action]
	result := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		result[field] = struct{}{}
	}

	return result
}

func (p haproxyFrontendActionPayload) equal(other haproxyFrontendActionPayload) bool {
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

func haproxyFrontendActionsQueryPath(parentID string) string {
	values := url.Values{}
	values.Set("parent_id", parentID)
	return haproxyFrontendActionsPath + "?" + values.Encode()
}

func haproxyFrontendActionDeletePath(parentID string, actionID string) string {
	values := url.Values{}
	values.Set("parent_id", parentID)
	values.Set("id", actionID)
	return haproxyFrontendActionPath + "?" + values.Encode()
}

func haproxyFrontendActionTerraformID(keys haproxyFrontendActionKeys) string {
	return keys.frontendName + "/" + keys.key
}

func frontendActionLookupErrorDetail(keys haproxyFrontendActionKeys, err error) string {
	return fmt.Sprintf("%s. Confirm GET %s is available on UAT, accepts parent_id query filters, returns ordered frontend action objects with action payload fields, and includes the transient pfSense child object id required for update/delete. Frontend name: %q. Terraform key: %q.", err.Error(), haproxyFrontendActionsPath, keys.frontendName, keys.key)
}
