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
	haproxyBackendACLPath  = "/services/haproxy/backend/acl"
	haproxyBackendACLsPath = "/services/haproxy/backend/acls"
)

var (
	_ resource.Resource                = (*haproxyBackendACLResource)(nil)
	_ resource.ResourceWithConfigure   = (*haproxyBackendACLResource)(nil)
	_ resource.ResourceWithImportState = (*haproxyBackendACLResource)(nil)
)

type backendACLAttributeKind string

const (
	backendACLAttributeBool   backendACLAttributeKind = "bool"
	backendACLAttributeString backendACLAttributeKind = "string"
)

type backendACLAttribute struct {
	Name        string
	JSONName    string
	Kind        backendACLAttributeKind
	Description string
}

var haproxyBackendACLAttributes = []backendACLAttribute{
	{Name: "expression", JSONName: "expression", Kind: backendACLAttributeString, Description: "ACL expression used by pfSense HAProxy, such as host_matches, path_starts_with, source_ip, traffic_is_ssl, ssl_sni_matches, or custom."},
	{Name: "value", JSONName: "value", Kind: backendACLAttributeString, Description: "Expression value. pfREST allows an empty string for expressions that do not need a value."},
	{Name: "casesensitive", JSONName: "casesensitive", Kind: backendACLAttributeBool, Description: "Enable case-sensitive matching for this ACL."},
	{Name: "not", JSONName: "not", Kind: backendACLAttributeBool, Description: "Invert this ACL match."},
}

var haproxyBackendACLExpressions = map[string]struct{}{
	"host_starts_with":    {},
	"host_ends_with":      {},
	"host_matches":        {},
	"host_regex":          {},
	"host_contains":       {},
	"path_starts_with":    {},
	"path_ends_with":      {},
	"path_matches":        {},
	"path_regex":          {},
	"path_contains":       {},
	"path_dir":            {},
	"url_parameter":       {},
	"ssl_c_verify_code":   {},
	"ssl_c_verify":        {},
	"ssl_c_ca_commonname": {},
	"source_ip":           {},
	"backendservercount":  {},
	"traffic_is_http":     {},
	"traffic_is_ssl":      {},
	"ssl_sni_matches":     {},
	"ssl_sni_contains":    {},
	"ssl_sni_starts_with": {},
	"ssl_sni_ends_with":   {},
	"ssl_sni_regex":       {},
	"custom":              {},
}

type haproxyBackendACLResource struct {
	client *pfsense.Client
}

type haproxyBackendACLModel struct {
	ID            types.String `tfsdk:"id"`
	BackendName   types.String `tfsdk:"backend_name"`
	Name          types.String `tfsdk:"name"`
	Expression    types.String `tfsdk:"expression"`
	Value         types.String `tfsdk:"value"`
	CaseSensitive types.Bool   `tfsdk:"casesensitive"`
	Not           types.Bool   `tfsdk:"not"`
	Position      types.Int64  `tfsdk:"position"`
}

func newHaproxyBackendACLResource() resource.Resource {
	return &haproxyBackendACLResource{}
}

func (r *haproxyBackendACLResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "pfsense_haproxy_backend_acl"
}

func (r *haproxyBackendACLResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = resourceschema.Schema{
		Description: "Manages a pfSense HAProxy backend ACL as an ordered child of a pfsense_haproxy_backend. Terraform uses backend name plus ACL name as the stable ID and resolves pfSense's current backend/ACL object IDs before writes because pfSense object IDs may not be durable.",
		Attributes:  haproxyBackendACLResourceSchemaAttributes(),
	}
}

func (r *haproxyBackendACLResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *haproxyBackendACLResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing pfSense client", "The provider was not configured before creating pfsense_haproxy_backend_acl.")
		return
	}

	var plan haproxyBackendACLModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	keys, err := validateHaproxyBackendACLPlan(plan)
	if err != nil {
		resp.Diagnostics.AddError("Invalid HAProxy backend ACL", err.Error())
		return
	}

	_, parentID, found, err := findHaproxyBackendByName(ctx, r.client, keys.backendName)
	if err != nil {
		resp.Diagnostics.AddError("Find parent HAProxy backend before create failed", backendACLLookupErrorDetail(keys, err))
		return
	}
	if !found {
		resp.Diagnostics.AddError("Create HAProxy backend ACL failed", fmt.Sprintf("Parent backend %q was not found on pfSense. Create or import pfsense_haproxy_backend.%s before managing child ACLs.", keys.backendName, keys.backendName))
		return
	}

	_, _, found, err = findHaproxyBackendACLByName(ctx, r.client, parentID, keys.backendName, keys.name)
	if err != nil {
		resp.Diagnostics.AddError("Check existing HAProxy backend ACL failed", backendACLLookupErrorDetail(keys, err))
		return
	}
	if found {
		resp.Diagnostics.AddError(
			"HAProxy backend ACL already exists",
			fmt.Sprintf("A pfSense HAProxy backend ACL named %q already exists under backend %q. Import it with `terraform import pfsense_haproxy_backend_acl.<name> %s` before managing it.", keys.name, keys.backendName, haproxyBackendACLTerraformID(keys.backendName, keys.name)),
		)
		return
	}

	if err := r.client.Post(ctx, haproxyBackendACLPath, buildHaproxyBackendACLCreatePayload(plan, parentID, keys), nil); err != nil {
		resp.Diagnostics.AddError("Create HAProxy backend ACL failed", err.Error())
		return
	}

	acl, _, found, err := findHaproxyBackendACLByName(ctx, r.client, parentID, keys.backendName, keys.name)
	if err != nil {
		resp.Diagnostics.AddError("Read HAProxy backend ACL after create failed", backendACLLookupErrorDetail(keys, err))
		return
	}
	if !found {
		resp.Diagnostics.AddError(
			"Read HAProxy backend ACL after create failed",
			fmt.Sprintf("Created ACL %q under backend %q but GET %s did not return it. Confirm the live UAT child response shape and natural-key filtering before relying on this resource.", keys.name, keys.backendName, haproxyBackendACLsPath),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, acl)...)
}

func (r *haproxyBackendACLResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing pfSense client", "The provider was not configured before reading pfsense_haproxy_backend_acl.")
		return
	}

	var state haproxyBackendACLModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	keys, err := haproxyBackendACLStateKeys(state)
	if err != nil {
		resp.Diagnostics.AddError("Invalid HAProxy backend ACL state", err.Error())
		return
	}

	_, parentID, found, err := findHaproxyBackendByName(ctx, r.client, keys.backendName)
	if err != nil {
		resp.Diagnostics.AddError("Find parent HAProxy backend failed", backendACLLookupErrorDetail(keys, err))
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	acl, _, found, err := findHaproxyBackendACLByName(ctx, r.client, parentID, keys.backendName, keys.name)
	if err != nil {
		resp.Diagnostics.AddError("Read HAProxy backend ACL failed", backendACLLookupErrorDetail(keys, err))
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, acl)...)
}

func (r *haproxyBackendACLResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing pfSense client", "The provider was not configured before updating pfsense_haproxy_backend_acl.")
		return
	}

	var plan, prior haproxyBackendACLModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}

	keys, err := validateHaproxyBackendACLPlan(plan)
	if err != nil {
		resp.Diagnostics.AddError("Invalid HAProxy backend ACL", err.Error())
		return
	}
	priorKeys, err := haproxyBackendACLStateKeys(prior)
	if err != nil {
		resp.Diagnostics.AddError("Invalid HAProxy backend ACL prior state", err.Error())
		return
	}
	if keys.backendName != priorKeys.backendName || keys.name != priorKeys.name {
		resp.Diagnostics.AddError("Renaming HAProxy backend ACLs is not supported", "The backend name and ACL name form the Terraform natural key. Change either value by creating a new resource and deleting the old one.")
		return
	}

	_, parentID, found, err := findHaproxyBackendByName(ctx, r.client, keys.backendName)
	if err != nil {
		resp.Diagnostics.AddError("Find parent HAProxy backend before update failed", backendACLLookupErrorDetail(keys, err))
		return
	}
	if !found {
		resp.Diagnostics.AddError("Update HAProxy backend ACL failed", fmt.Sprintf("Parent backend %q was not found on pfSense. Recreate it or remove the child ACL from Terraform state.", keys.backendName))
		return
	}

	_, aclID, found, err := findHaproxyBackendACLByName(ctx, r.client, parentID, keys.backendName, keys.name)
	if err != nil {
		resp.Diagnostics.AddError("Find HAProxy backend ACL before update failed", backendACLLookupErrorDetail(keys, err))
		return
	}
	if !found {
		resp.Diagnostics.AddError("Update HAProxy backend ACL failed", fmt.Sprintf("ACL %q under backend %q was not found on pfSense. Recreate it or remove it from Terraform state.", keys.name, keys.backendName))
		return
	}

	patch := buildHaproxyBackendACLPatch(plan, prior, parentID, aclID)
	if len(patch) > 2 {
		if err := r.client.Patch(ctx, haproxyBackendACLPath, patch, nil); err != nil {
			resp.Diagnostics.AddError("Update HAProxy backend ACL failed", err.Error())
			return
		}
	}

	acl, _, found, err := findHaproxyBackendACLByName(ctx, r.client, parentID, keys.backendName, keys.name)
	if err != nil {
		resp.Diagnostics.AddError("Read HAProxy backend ACL after update failed", backendACLLookupErrorDetail(keys, err))
		return
	}
	if !found {
		resp.Diagnostics.AddError("Read HAProxy backend ACL after update failed", fmt.Sprintf("ACL %q under backend %q was not returned by GET %s after PATCH.", keys.name, keys.backendName, haproxyBackendACLsPath))
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, acl)...)
}

func (r *haproxyBackendACLResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing pfSense client", "The provider was not configured before deleting pfsense_haproxy_backend_acl.")
		return
	}

	var state haproxyBackendACLModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	keys, err := haproxyBackendACLStateKeys(state)
	if err != nil {
		resp.Diagnostics.AddError("Invalid HAProxy backend ACL state", err.Error())
		return
	}

	_, parentID, found, err := findHaproxyBackendByName(ctx, r.client, keys.backendName)
	if err != nil {
		resp.Diagnostics.AddError("Find parent HAProxy backend before delete failed", backendACLLookupErrorDetail(keys, err))
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	_, aclID, found, err := findHaproxyBackendACLByName(ctx, r.client, parentID, keys.backendName, keys.name)
	if err != nil {
		resp.Diagnostics.AddError("Find HAProxy backend ACL before delete failed", backendACLLookupErrorDetail(keys, err))
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	if err := r.client.Delete(ctx, haproxyBackendACLDeletePath(parentID, aclID), nil); err != nil {
		resp.Diagnostics.AddError("Delete HAProxy backend ACL failed", err.Error())
		return
	}

	resp.State.RemoveResource(ctx)
}

func (r *haproxyBackendACLResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	keys, err := parseHaproxyBackendACLImportID(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Invalid HAProxy backend ACL import ID", err.Error())
		return
	}

	model := nullHaproxyBackendACLModel()
	model.ID = types.StringValue(haproxyBackendACLTerraformID(keys.backendName, keys.name))
	model.BackendName = types.StringValue(keys.backendName)
	model.Name = types.StringValue(keys.name)
	resp.Diagnostics.Append(resp.State.Set(ctx, model)...)
}

func haproxyBackendACLResourceSchemaAttributes() map[string]resourceschema.Attribute {
	return map[string]resourceschema.Attribute{
		"id": resourceschema.StringAttribute{
			Computed:    true,
			Description: "Stable Terraform ID for the backend ACL in backend_name/name form. This is not the pfSense object ID.",
		},
		"backend_name": resourceschema.StringAttribute{
			Required:    true,
			Description: "Name of the parent pfsense_haproxy_backend. Terraform resolves the current pfSense backend object ID by this natural key before every backend ACL write.",
			PlanModifiers: []planmodifier.String{
				stringplanmodifier.RequiresReplace(),
			},
		},
		"name": resourceschema.StringAttribute{
			Required:    true,
			Description: "Unique HAProxy backend ACL name within the parent backend. pfSense restricts names to letters, numbers, dot, hyphen, and underscore. Terraform treats this as part of the natural key and requires replacement if it changes.",
			PlanModifiers: []planmodifier.String{
				stringplanmodifier.RequiresReplace(),
			},
		},
		"expression": resourceschema.StringAttribute{
			Required:    true,
			Description: "ACL expression. Supported values include host/path/SNI matches, source_ip, backendservercount, traffic_is_http, traffic_is_ssl, and custom.",
		},
		"value": resourceschema.StringAttribute{
			Required:    true,
			Description: "ACL expression value. Empty string is allowed for pfREST expressions that do not require a value.",
		},
		"casesensitive": resourceschema.BoolAttribute{
			Optional:    true,
			Computed:    true,
			Description: "Enable case-sensitive matching. pfSense stores true as yes.",
		},
		"not": resourceschema.BoolAttribute{
			Optional:    true,
			Computed:    true,
			Description: "Invert this ACL match. pfSense stores true as yes.",
		},
		"position": resourceschema.Int64Attribute{
			Optional:    true,
			Computed:    true,
			Description: "Zero-based ACL order within the backend. When configured, Terraform sends pfREST's placement field on create and when the position changes.",
			PlanModifiers: []planmodifier.Int64{
				int64planmodifier.UseStateForUnknown(),
			},
		},
	}
}

type haproxyBackendACLKeys struct {
	backendName string
	name        string
}

func validateHaproxyBackendACLPlan(model haproxyBackendACLModel) (haproxyBackendACLKeys, error) {
	backendName, err := haproxyBackendACLBackendName(model.BackendName)
	if err != nil {
		return haproxyBackendACLKeys{}, err
	}
	name, err := haproxyBackendACLName(model.Name)
	if err != nil {
		return haproxyBackendACLKeys{}, err
	}
	if _, err := haproxyBackendACLExpression(model.Expression); err != nil {
		return haproxyBackendACLKeys{}, err
	}
	if err := haproxyBackendACLValue(model.Value); err != nil {
		return haproxyBackendACLKeys{}, err
	}
	if err := validateHaproxyBackendACLPosition(model.Position); err != nil {
		return haproxyBackendACLKeys{}, err
	}

	return haproxyBackendACLKeys{backendName: backendName, name: name}, nil
}

func haproxyBackendACLBackendName(value types.String) (string, error) {
	name, err := haproxyBackendName(value)
	if err != nil {
		return "", fmt.Errorf("backend_name %s", err.Error())
	}

	return name, nil
}

func haproxyBackendACLName(value types.String) (string, error) {
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
	if !haproxyNamePattern.MatchString(name) {
		return "", fmt.Errorf("name may contain only letters, numbers, dot, hyphen, and underscore")
	}

	return name, nil
}

func haproxyBackendACLExpression(value types.String) (string, error) {
	if value.IsNull() || value.IsUnknown() {
		return "", fmt.Errorf("expression is required")
	}
	expression := value.ValueString()
	if strings.TrimSpace(expression) != expression || expression == "" {
		return "", fmt.Errorf("expression must be one of the documented HAProxy backend ACL expressions")
	}
	if _, ok := haproxyBackendACLExpressions[expression]; !ok {
		return "", fmt.Errorf("expression must be one of the documented HAProxy backend ACL expressions")
	}

	return expression, nil
}

func haproxyBackendACLValue(value types.String) error {
	if value.IsNull() || value.IsUnknown() {
		return fmt.Errorf("value is required")
	}

	return nil
}

func validateHaproxyBackendACLPosition(value types.Int64) error {
	if value.IsNull() || value.IsUnknown() {
		return nil
	}
	if value.ValueInt64() < 0 {
		return fmt.Errorf("position must be zero or greater")
	}

	return nil
}

func haproxyBackendACLStateKeys(model haproxyBackendACLModel) (haproxyBackendACLKeys, error) {
	if !model.BackendName.IsNull() && !model.BackendName.IsUnknown() && !model.Name.IsNull() && !model.Name.IsUnknown() {
		backendName, err := haproxyBackendACLBackendName(model.BackendName)
		if err != nil {
			return haproxyBackendACLKeys{}, err
		}
		name, err := haproxyBackendACLName(model.Name)
		if err != nil {
			return haproxyBackendACLKeys{}, err
		}
		return haproxyBackendACLKeys{backendName: backendName, name: name}, nil
	}
	if !model.ID.IsNull() && !model.ID.IsUnknown() {
		return parseHaproxyBackendACLImportID(model.ID.ValueString())
	}

	return haproxyBackendACLKeys{}, fmt.Errorf("state is missing backend_name and name")
}

func parseHaproxyBackendACLImportID(id string) (haproxyBackendACLKeys, error) {
	trimmed := strings.TrimSpace(id)
	backendName, aclName, ok := strings.Cut(trimmed, "/")
	if !ok || backendName == "" || aclName == "" || strings.Contains(aclName, "/") {
		return haproxyBackendACLKeys{}, fmt.Errorf("import pfsense_haproxy_backend_acl with ID backend_name/acl_name")
	}

	backend, err := haproxyBackendACLBackendName(types.StringValue(backendName))
	if err != nil {
		return haproxyBackendACLKeys{}, err
	}
	acl, err := haproxyBackendACLName(types.StringValue(aclName))
	if err != nil {
		return haproxyBackendACLKeys{}, err
	}

	return haproxyBackendACLKeys{backendName: backend, name: acl}, nil
}

func buildHaproxyBackendACLCreatePayload(plan haproxyBackendACLModel, parentID string, keys haproxyBackendACLKeys) map[string]any {
	expression, _ := haproxyBackendACLExpression(plan.Expression)
	payload := map[string]any{
		"parent_id":  parentID,
		"name":       keys.name,
		"expression": expression,
		"value":      plan.Value.ValueString(),
	}
	values := plan.attrValues()

	for _, attribute := range haproxyBackendACLAttributes {
		if attribute.Name == "expression" || attribute.Name == "value" {
			continue
		}
		planned := values[attribute.Name]
		if planned.IsNull() || planned.IsUnknown() {
			continue
		}
		payload[attribute.JSONName] = backendACLTerraformValueToJSON(attribute.Kind, planned)
	}
	if !plan.Position.IsNull() && !plan.Position.IsUnknown() {
		payload["placement"] = plan.Position.ValueInt64()
	}

	return payload
}

func buildHaproxyBackendACLPatch(plan haproxyBackendACLModel, prior haproxyBackendACLModel, parentID string, aclID string) map[string]any {
	patch := map[string]any{
		"parent_id": parentID,
		"id":        aclID,
	}
	planValues := plan.attrValues()
	priorValues := prior.attrValues()

	for _, attribute := range haproxyBackendACLAttributes {
		planned := planValues[attribute.Name]
		if planned.IsUnknown() {
			continue
		}
		if planned.Equal(priorValues[attribute.Name]) {
			continue
		}
		patch[attribute.JSONName] = backendACLTerraformValueToJSON(attribute.Kind, planned)
	}
	if !plan.Position.IsNull() && !plan.Position.IsUnknown() && !plan.Position.Equal(prior.Position) {
		patch["placement"] = plan.Position.ValueInt64()
	}

	return patch
}

func findHaproxyBackendACLByName(ctx context.Context, client *pfsense.Client, parentID string, backendName string, name string) (haproxyBackendACLModel, string, bool, error) {
	var raw any
	if err := client.Get(ctx, haproxyBackendACLsQueryPath(parentID, name), &raw); err != nil {
		return haproxyBackendACLModel{}, "", false, err
	}

	payloads, err := haproxyBackendACLPayloadList(raw)
	if err != nil {
		return haproxyBackendACLModel{}, "", false, err
	}

	var matched map[string]any
	for _, payload := range payloads {
		candidateName, err := apiRequiredStringWithLabel(payload, "HAProxy backend ACL", "name")
		if err != nil {
			return haproxyBackendACLModel{}, "", false, err
		}
		if candidateName != name {
			continue
		}
		if matched != nil {
			return haproxyBackendACLModel{}, "", false, fmt.Errorf("multiple HAProxy backend ACLs named %q were returned under backend %q; ACL names must be unique within a backend for Terraform natural-key management", name, backendName)
		}
		matched = payload
	}

	if matched == nil {
		return haproxyBackendACLModel{}, "", false, nil
	}

	apiID, err := apiRequiredScalarStringWithLabel(matched, "HAProxy backend ACL", "id")
	if err != nil {
		return haproxyBackendACLModel{}, "", false, fmt.Errorf("%w; confirm UAT returns child object IDs from GET %s before using update/delete", err, haproxyBackendACLsPath)
	}
	position, err := haproxyBackendACLPosition(ctx, client, parentID, backendName, name)
	if err != nil {
		return haproxyBackendACLModel{}, "", false, err
	}
	model, err := haproxyBackendACLModelFromAPI(matched, backendName, position)
	if err != nil {
		return haproxyBackendACLModel{}, "", false, err
	}

	return model, apiID, true, nil
}

func haproxyBackendACLPosition(ctx context.Context, client *pfsense.Client, parentID string, backendName string, name string) (int64, error) {
	var raw any
	if err := client.Get(ctx, haproxyBackendACLsQueryPath(parentID, ""), &raw); err != nil {
		return 0, err
	}

	payloads, err := haproxyBackendACLPayloadList(raw)
	if err != nil {
		return 0, err
	}

	position := int64(-1)
	for index, payload := range payloads {
		candidateName, err := apiRequiredStringWithLabel(payload, "HAProxy backend ACL", "name")
		if err != nil {
			return 0, err
		}
		if candidateName != name {
			continue
		}
		if position >= 0 {
			return 0, fmt.Errorf("multiple HAProxy backend ACLs named %q were returned under backend %q; ACL names must be unique within a backend for Terraform natural-key management", name, backendName)
		}
		position = int64(index)
	}
	if position < 0 {
		return 0, fmt.Errorf("HAProxy backend ACL %q under backend %q was returned by filtered lookup but not by full ordered lookup", name, backendName)
	}

	return position, nil
}

func haproxyBackendACLPayloadList(raw any) ([]map[string]any, error) {
	if raw == nil {
		return nil, nil
	}

	switch typed := raw.(type) {
	case []any:
		payloads := make([]map[string]any, 0, len(typed))
		for index, item := range typed {
			payload, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("HAProxy backend ACLs response item %d has unsupported type %T", index, item)
			}
			payloads = append(payloads, payload)
		}
		return payloads, nil
	case []map[string]any:
		return typed, nil
	case map[string]any:
		return []map[string]any{typed}, nil
	default:
		return nil, fmt.Errorf("HAProxy backend ACLs response has unsupported type %T; confirm the live UAT /services/haproxy/backend/acls schema", raw)
	}
}

func haproxyBackendACLModelFromAPI(payload map[string]any, backendName string, position int64) (haproxyBackendACLModel, error) {
	acl := nullHaproxyBackendACLModel()

	nameValue, err := apiRequiredStringWithLabel(payload, "HAProxy backend ACL", "name")
	if err != nil {
		return acl, err
	}
	name, err := haproxyBackendACLName(types.StringValue(nameValue))
	if err != nil {
		return acl, fmt.Errorf("HAProxy backend ACL name %s", err.Error())
	}
	expressionValue, err := apiRequiredStringWithLabel(payload, "HAProxy backend ACL", "expression")
	if err != nil {
		return acl, err
	}
	expression, err := haproxyBackendACLExpression(types.StringValue(expressionValue))
	if err != nil {
		return acl, fmt.Errorf("HAProxy backend ACL expression %s", err.Error())
	}
	value, err := apiRequiredStringAllowEmptyWithLabel(payload, "HAProxy backend ACL", "value")
	if err != nil {
		return acl, err
	}

	acl.ID = types.StringValue(haproxyBackendACLTerraformID(backendName, name))
	acl.BackendName = types.StringValue(backendName)
	acl.Name = types.StringValue(name)
	acl.Expression = types.StringValue(expression)
	acl.Value = types.StringValue(value)
	acl.Position = types.Int64Value(position)

	if acl.CaseSensitive, err = apiBoolDefaultFalseWithLabel(payload, "HAProxy backend ACL", "casesensitive"); err != nil {
		return acl, err
	}
	if acl.Not, err = apiBoolDefaultFalseWithLabel(payload, "HAProxy backend ACL", "not"); err != nil {
		return acl, err
	}

	return acl, nil
}

func apiRequiredStringAllowEmptyWithLabel(payload map[string]any, label string, names ...string) (string, error) {
	value, name, ok := apiValue(payload, names...)
	if !ok || value == nil {
		return "", fmt.Errorf("%s response did not include required string field %q", label, names[0])
	}

	typed, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s field %q has unsupported string type %T", label, name, value)
	}

	return typed, nil
}

func apiBoolDefaultFalseWithLabel(payload map[string]any, label string, names ...string) (types.Bool, error) {
	value, err := apiBoolWithLabel(payload, label, names...)
	if err != nil {
		return types.BoolNull(), err
	}
	if value.IsNull() || value.IsUnknown() {
		return types.BoolValue(false), nil
	}

	return value, nil
}

func nullHaproxyBackendACLModel() haproxyBackendACLModel {
	return haproxyBackendACLModel{
		ID:            types.StringNull(),
		BackendName:   types.StringNull(),
		Name:          types.StringNull(),
		Expression:    types.StringNull(),
		Value:         types.StringNull(),
		CaseSensitive: types.BoolNull(),
		Not:           types.BoolNull(),
		Position:      types.Int64Null(),
	}
}

func (m haproxyBackendACLModel) attrValues() map[string]attr.Value {
	return map[string]attr.Value{
		"expression":    m.Expression,
		"value":         m.Value,
		"casesensitive": m.CaseSensitive,
		"not":           m.Not,
	}
}

func backendACLTerraformValueToJSON(kind backendACLAttributeKind, value attr.Value) any {
	if value.IsNull() {
		return nil
	}

	switch kind {
	case backendACLAttributeBool:
		return value.(types.Bool).ValueBool()
	case backendACLAttributeString:
		return value.(types.String).ValueString()
	default:
		return nil
	}
}

func haproxyBackendACLsQueryPath(parentID string, name string) string {
	values := url.Values{}
	values.Set("parent_id", parentID)
	if name != "" {
		values.Set("name", name)
	}
	return haproxyBackendACLsPath + "?" + values.Encode()
}

func haproxyBackendACLDeletePath(parentID string, aclID string) string {
	values := url.Values{}
	values.Set("parent_id", parentID)
	values.Set("id", aclID)
	return haproxyBackendACLPath + "?" + values.Encode()
}

func haproxyBackendACLTerraformID(backendName string, name string) string {
	return backendName + "/" + name
}

func backendACLLookupErrorDetail(keys haproxyBackendACLKeys, err error) string {
	return fmt.Sprintf("%s. Confirm GET %s is available on UAT, accepts parent_id/name query filters, returns ordered ACL objects with stable name fields, and includes the transient pfSense child object id required for update/delete. Backend name: %q. ACL name: %q.", err.Error(), haproxyBackendACLsPath, keys.backendName, keys.name)
}
