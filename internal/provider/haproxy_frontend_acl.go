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
	haproxyFrontendACLPath  = "/services/haproxy/frontend/acl"
	haproxyFrontendACLsPath = "/services/haproxy/frontend/acls"
)

var (
	_ resource.Resource                = (*haproxyFrontendACLResource)(nil)
	_ resource.ResourceWithConfigure   = (*haproxyFrontendACLResource)(nil)
	_ resource.ResourceWithImportState = (*haproxyFrontendACLResource)(nil)
)

type frontendACLAttributeKind string

const (
	frontendACLAttributeBool   frontendACLAttributeKind = "bool"
	frontendACLAttributeString frontendACLAttributeKind = "string"
)

type frontendACLAttribute struct {
	Name        string
	JSONName    string
	Kind        frontendACLAttributeKind
	Description string
}

var haproxyFrontendACLAttributes = []frontendACLAttribute{
	{Name: "expression", JSONName: "expression", Kind: frontendACLAttributeString, Description: "ACL expression used by pfSense HAProxy, such as host_matches, path_starts_with, source_ip, traffic_is_ssl, ssl_sni_matches, or custom."},
	{Name: "value", JSONName: "value", Kind: frontendACLAttributeString, Description: "Expression value. pfREST allows an empty string for expressions that do not need a value."},
	{Name: "casesensitive", JSONName: "casesensitive", Kind: frontendACLAttributeBool, Description: "Enable case-sensitive matching for this ACL."},
	{Name: "not", JSONName: "not", Kind: frontendACLAttributeBool, Description: "Invert this ACL match."},
}

var haproxyFrontendACLExpressions = map[string]struct{}{
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

type haproxyFrontendACLResource struct {
	client *pfsense.Client
}

type haproxyFrontendACLModel struct {
	ID            types.String `tfsdk:"id"`
	FrontendName  types.String `tfsdk:"frontend_name"`
	Name          types.String `tfsdk:"name"`
	Expression    types.String `tfsdk:"expression"`
	Value         types.String `tfsdk:"value"`
	CaseSensitive types.Bool   `tfsdk:"casesensitive"`
	Not           types.Bool   `tfsdk:"not"`
	Position      types.Int64  `tfsdk:"position"`
}

func newHaproxyFrontendACLResource() resource.Resource {
	return &haproxyFrontendACLResource{}
}

func (r *haproxyFrontendACLResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "pfsense_haproxy_frontend_acl"
}

func (r *haproxyFrontendACLResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = resourceschema.Schema{
		Description: "Manages a pfSense HAProxy frontend ACL as an ordered child of a pfsense_haproxy_frontend. Terraform uses frontend name plus ACL name as the stable ID and resolves pfSense's current frontend/ACL object IDs before writes because pfSense object IDs may not be durable.",
		Attributes:  haproxyFrontendACLResourceSchemaAttributes(),
	}
}

func (r *haproxyFrontendACLResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *haproxyFrontendACLResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing pfSense client", "The provider was not configured before creating pfsense_haproxy_frontend_acl.")
		return
	}

	var plan haproxyFrontendACLModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	keys, err := validateHaproxyFrontendACLPlan(plan)
	if err != nil {
		resp.Diagnostics.AddError("Invalid HAProxy frontend ACL", err.Error())
		return
	}

	_, parentID, found, err := findHaproxyFrontendByName(ctx, r.client, keys.frontendName)
	if err != nil {
		resp.Diagnostics.AddError("Find parent HAProxy frontend before create failed", frontendACLLookupErrorDetail(keys, err))
		return
	}
	if !found {
		resp.Diagnostics.AddError("Create HAProxy frontend ACL failed", fmt.Sprintf("Parent frontend %q was not found on pfSense. Create or import pfsense_haproxy_frontend.%s before managing child ACLs.", keys.frontendName, keys.frontendName))
		return
	}

	_, _, found, err = findHaproxyFrontendACLByName(ctx, r.client, parentID, keys.frontendName, keys.name)
	if err != nil {
		resp.Diagnostics.AddError("Check existing HAProxy frontend ACL failed", frontendACLLookupErrorDetail(keys, err))
		return
	}
	if found {
		resp.Diagnostics.AddError(
			"HAProxy frontend ACL already exists",
			fmt.Sprintf("A pfSense HAProxy frontend ACL named %q already exists under frontend %q. Import it with `terraform import pfsense_haproxy_frontend_acl.<name> %s` before managing it.", keys.name, keys.frontendName, haproxyFrontendACLTerraformID(keys.frontendName, keys.name)),
		)
		return
	}

	if err := r.client.Post(ctx, haproxyFrontendACLPath, buildHaproxyFrontendACLCreatePayload(plan, parentID, keys), nil); err != nil {
		resp.Diagnostics.AddError("Create HAProxy frontend ACL failed", err.Error())
		return
	}

	acl, _, found, err := findHaproxyFrontendACLByName(ctx, r.client, parentID, keys.frontendName, keys.name)
	if err != nil {
		resp.Diagnostics.AddError("Read HAProxy frontend ACL after create failed", frontendACLLookupErrorDetail(keys, err))
		return
	}
	if !found {
		resp.Diagnostics.AddError(
			"Read HAProxy frontend ACL after create failed",
			fmt.Sprintf("Created ACL %q under frontend %q but GET %s did not return it. Confirm the live UAT child response shape and natural-key filtering before relying on this resource.", keys.name, keys.frontendName, haproxyFrontendACLsPath),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, acl)...)
}

func (r *haproxyFrontendACLResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing pfSense client", "The provider was not configured before reading pfsense_haproxy_frontend_acl.")
		return
	}

	var state haproxyFrontendACLModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	keys, err := haproxyFrontendACLStateKeys(state)
	if err != nil {
		resp.Diagnostics.AddError("Invalid HAProxy frontend ACL state", err.Error())
		return
	}

	_, parentID, found, err := findHaproxyFrontendByName(ctx, r.client, keys.frontendName)
	if err != nil {
		resp.Diagnostics.AddError("Find parent HAProxy frontend failed", frontendACLLookupErrorDetail(keys, err))
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	acl, _, found, err := findHaproxyFrontendACLByName(ctx, r.client, parentID, keys.frontendName, keys.name)
	if err != nil {
		resp.Diagnostics.AddError("Read HAProxy frontend ACL failed", frontendACLLookupErrorDetail(keys, err))
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, acl)...)
}

func (r *haproxyFrontendACLResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing pfSense client", "The provider was not configured before updating pfsense_haproxy_frontend_acl.")
		return
	}

	var plan, prior haproxyFrontendACLModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}

	keys, err := validateHaproxyFrontendACLPlan(plan)
	if err != nil {
		resp.Diagnostics.AddError("Invalid HAProxy frontend ACL", err.Error())
		return
	}
	priorKeys, err := haproxyFrontendACLStateKeys(prior)
	if err != nil {
		resp.Diagnostics.AddError("Invalid HAProxy frontend ACL prior state", err.Error())
		return
	}
	if keys.frontendName != priorKeys.frontendName || keys.name != priorKeys.name {
		resp.Diagnostics.AddError("Renaming HAProxy frontend ACLs is not supported", "The frontend name and ACL name form the Terraform natural key. Change either value by creating a new resource and deleting the old one.")
		return
	}

	_, parentID, found, err := findHaproxyFrontendByName(ctx, r.client, keys.frontendName)
	if err != nil {
		resp.Diagnostics.AddError("Find parent HAProxy frontend before update failed", frontendACLLookupErrorDetail(keys, err))
		return
	}
	if !found {
		resp.Diagnostics.AddError("Update HAProxy frontend ACL failed", fmt.Sprintf("Parent frontend %q was not found on pfSense. Recreate it or remove the child ACL from Terraform state.", keys.frontendName))
		return
	}

	_, aclID, found, err := findHaproxyFrontendACLByName(ctx, r.client, parentID, keys.frontendName, keys.name)
	if err != nil {
		resp.Diagnostics.AddError("Find HAProxy frontend ACL before update failed", frontendACLLookupErrorDetail(keys, err))
		return
	}
	if !found {
		resp.Diagnostics.AddError("Update HAProxy frontend ACL failed", fmt.Sprintf("ACL %q under frontend %q was not found on pfSense. Recreate it or remove it from Terraform state.", keys.name, keys.frontendName))
		return
	}

	patch := buildHaproxyFrontendACLPatch(plan, prior, parentID, aclID)
	if len(patch) > 2 {
		if err := r.client.Patch(ctx, haproxyFrontendACLPath, patch, nil); err != nil {
			resp.Diagnostics.AddError("Update HAProxy frontend ACL failed", err.Error())
			return
		}
	}

	acl, _, found, err := findHaproxyFrontendACLByName(ctx, r.client, parentID, keys.frontendName, keys.name)
	if err != nil {
		resp.Diagnostics.AddError("Read HAProxy frontend ACL after update failed", frontendACLLookupErrorDetail(keys, err))
		return
	}
	if !found {
		resp.Diagnostics.AddError("Read HAProxy frontend ACL after update failed", fmt.Sprintf("ACL %q under frontend %q was not returned by GET %s after PATCH.", keys.name, keys.frontendName, haproxyFrontendACLsPath))
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, acl)...)
}

func (r *haproxyFrontendACLResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing pfSense client", "The provider was not configured before deleting pfsense_haproxy_frontend_acl.")
		return
	}

	var state haproxyFrontendACLModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	keys, err := haproxyFrontendACLStateKeys(state)
	if err != nil {
		resp.Diagnostics.AddError("Invalid HAProxy frontend ACL state", err.Error())
		return
	}

	_, parentID, found, err := findHaproxyFrontendByName(ctx, r.client, keys.frontendName)
	if err != nil {
		resp.Diagnostics.AddError("Find parent HAProxy frontend before delete failed", frontendACLLookupErrorDetail(keys, err))
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	_, aclID, found, err := findHaproxyFrontendACLByName(ctx, r.client, parentID, keys.frontendName, keys.name)
	if err != nil {
		resp.Diagnostics.AddError("Find HAProxy frontend ACL before delete failed", frontendACLLookupErrorDetail(keys, err))
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	if err := r.client.Delete(ctx, haproxyFrontendACLDeletePath(parentID, aclID), nil); err != nil {
		resp.Diagnostics.AddError("Delete HAProxy frontend ACL failed", err.Error())
		return
	}

	resp.State.RemoveResource(ctx)
}

func (r *haproxyFrontendACLResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	keys, err := parseHaproxyFrontendACLImportID(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Invalid HAProxy frontend ACL import ID", err.Error())
		return
	}

	model := nullHaproxyFrontendACLModel()
	model.ID = types.StringValue(haproxyFrontendACLTerraformID(keys.frontendName, keys.name))
	model.FrontendName = types.StringValue(keys.frontendName)
	model.Name = types.StringValue(keys.name)
	resp.Diagnostics.Append(resp.State.Set(ctx, model)...)
}

func haproxyFrontendACLResourceSchemaAttributes() map[string]resourceschema.Attribute {
	return map[string]resourceschema.Attribute{
		"id": resourceschema.StringAttribute{
			Computed:    true,
			Description: "Stable Terraform ID for the frontend ACL in frontend_name/name form. This is not the pfSense object ID.",
		},
		"frontend_name": resourceschema.StringAttribute{
			Required:    true,
			Description: "Name of the parent pfsense_haproxy_frontend. Terraform resolves the current pfSense frontend object ID by this natural key before every frontend ACL write.",
			PlanModifiers: []planmodifier.String{
				stringplanmodifier.RequiresReplace(),
			},
		},
		"name": resourceschema.StringAttribute{
			Required:    true,
			Description: "Unique HAProxy frontend ACL name within the parent frontend. pfSense restricts names to letters, numbers, dot, hyphen, and underscore. Terraform treats this as part of the natural key and requires replacement if it changes.",
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
			Description: "Zero-based ACL order within the frontend. When configured, Terraform sends pfREST's placement field on create and when the position changes.",
			PlanModifiers: []planmodifier.Int64{
				int64planmodifier.UseStateForUnknown(),
			},
		},
	}
}

type haproxyFrontendACLKeys struct {
	frontendName string
	name         string
}

func validateHaproxyFrontendACLPlan(model haproxyFrontendACLModel) (haproxyFrontendACLKeys, error) {
	frontendName, err := haproxyFrontendACLFrontendName(model.FrontendName)
	if err != nil {
		return haproxyFrontendACLKeys{}, err
	}
	name, err := haproxyFrontendACLName(model.Name)
	if err != nil {
		return haproxyFrontendACLKeys{}, err
	}
	if _, err := haproxyFrontendACLExpression(model.Expression); err != nil {
		return haproxyFrontendACLKeys{}, err
	}
	if err := haproxyFrontendACLValue(model.Value); err != nil {
		return haproxyFrontendACLKeys{}, err
	}
	if err := validateHaproxyFrontendACLPosition(model.Position); err != nil {
		return haproxyFrontendACLKeys{}, err
	}

	return haproxyFrontendACLKeys{frontendName: frontendName, name: name}, nil
}

func haproxyFrontendACLFrontendName(value types.String) (string, error) {
	name, err := haproxyFrontendName(value)
	if err != nil {
		return "", fmt.Errorf("frontend_name %s", err.Error())
	}

	return name, nil
}

func haproxyFrontendACLName(value types.String) (string, error) {
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

func haproxyFrontendACLExpression(value types.String) (string, error) {
	if value.IsNull() || value.IsUnknown() {
		return "", fmt.Errorf("expression is required")
	}
	expression := value.ValueString()
	if strings.TrimSpace(expression) != expression || expression == "" {
		return "", fmt.Errorf("expression must be one of the documented HAProxy frontend ACL expressions")
	}
	if _, ok := haproxyFrontendACLExpressions[expression]; !ok {
		return "", fmt.Errorf("expression must be one of the documented HAProxy frontend ACL expressions")
	}

	return expression, nil
}

func haproxyFrontendACLValue(value types.String) error {
	if value.IsNull() || value.IsUnknown() {
		return fmt.Errorf("value is required")
	}

	return nil
}

func validateHaproxyFrontendACLPosition(value types.Int64) error {
	if value.IsNull() || value.IsUnknown() {
		return nil
	}
	if value.ValueInt64() < 0 {
		return fmt.Errorf("position must be zero or greater")
	}

	return nil
}

func haproxyFrontendACLStateKeys(model haproxyFrontendACLModel) (haproxyFrontendACLKeys, error) {
	if !model.FrontendName.IsNull() && !model.FrontendName.IsUnknown() && !model.Name.IsNull() && !model.Name.IsUnknown() {
		frontendName, err := haproxyFrontendACLFrontendName(model.FrontendName)
		if err != nil {
			return haproxyFrontendACLKeys{}, err
		}
		name, err := haproxyFrontendACLName(model.Name)
		if err != nil {
			return haproxyFrontendACLKeys{}, err
		}
		return haproxyFrontendACLKeys{frontendName: frontendName, name: name}, nil
	}
	if !model.ID.IsNull() && !model.ID.IsUnknown() {
		return parseHaproxyFrontendACLImportID(model.ID.ValueString())
	}

	return haproxyFrontendACLKeys{}, fmt.Errorf("state is missing frontend_name and name")
}

func parseHaproxyFrontendACLImportID(id string) (haproxyFrontendACLKeys, error) {
	trimmed := strings.TrimSpace(id)
	frontendName, aclName, ok := strings.Cut(trimmed, "/")
	if !ok || frontendName == "" || aclName == "" || strings.Contains(aclName, "/") {
		return haproxyFrontendACLKeys{}, fmt.Errorf("import pfsense_haproxy_frontend_acl with ID frontend_name/acl_name")
	}

	frontend, err := haproxyFrontendACLFrontendName(types.StringValue(frontendName))
	if err != nil {
		return haproxyFrontendACLKeys{}, err
	}
	acl, err := haproxyFrontendACLName(types.StringValue(aclName))
	if err != nil {
		return haproxyFrontendACLKeys{}, err
	}

	return haproxyFrontendACLKeys{frontendName: frontend, name: acl}, nil
}

func buildHaproxyFrontendACLCreatePayload(plan haproxyFrontendACLModel, parentID string, keys haproxyFrontendACLKeys) map[string]any {
	expression, _ := haproxyFrontendACLExpression(plan.Expression)
	payload := map[string]any{
		"parent_id":  parentID,
		"name":       keys.name,
		"expression": expression,
		"value":      plan.Value.ValueString(),
	}
	values := plan.attrValues()

	for _, attribute := range haproxyFrontendACLAttributes {
		if attribute.Name == "expression" || attribute.Name == "value" {
			continue
		}
		planned := values[attribute.Name]
		if planned.IsNull() || planned.IsUnknown() {
			continue
		}
		payload[attribute.JSONName] = frontendACLTerraformValueToJSON(attribute.Kind, planned)
	}
	if !plan.Position.IsNull() && !plan.Position.IsUnknown() {
		payload["placement"] = plan.Position.ValueInt64()
	}

	return payload
}

func buildHaproxyFrontendACLPatch(plan haproxyFrontendACLModel, prior haproxyFrontendACLModel, parentID string, aclID string) map[string]any {
	patch := map[string]any{
		"parent_id": parentID,
		"id":        aclID,
	}
	planValues := plan.attrValues()
	priorValues := prior.attrValues()

	for _, attribute := range haproxyFrontendACLAttributes {
		planned := planValues[attribute.Name]
		if planned.IsUnknown() {
			continue
		}
		if planned.Equal(priorValues[attribute.Name]) {
			continue
		}
		patch[attribute.JSONName] = frontendACLTerraformValueToJSON(attribute.Kind, planned)
	}
	if !plan.Position.IsNull() && !plan.Position.IsUnknown() && !plan.Position.Equal(prior.Position) {
		patch["placement"] = plan.Position.ValueInt64()
	}

	return patch
}

func findHaproxyFrontendACLByName(ctx context.Context, client *pfsense.Client, parentID string, frontendName string, name string) (haproxyFrontendACLModel, string, bool, error) {
	var raw any
	if err := client.Get(ctx, haproxyFrontendACLsQueryPath(parentID, name), &raw); err != nil {
		return haproxyFrontendACLModel{}, "", false, err
	}

	payloads, err := haproxyFrontendACLPayloadList(raw)
	if err != nil {
		return haproxyFrontendACLModel{}, "", false, err
	}

	var matched map[string]any
	for _, payload := range payloads {
		candidateName, err := apiRequiredStringWithLabel(payload, "HAProxy frontend ACL", "name")
		if err != nil {
			return haproxyFrontendACLModel{}, "", false, err
		}
		if candidateName != name {
			continue
		}
		if matched != nil {
			return haproxyFrontendACLModel{}, "", false, fmt.Errorf("multiple HAProxy frontend ACLs named %q were returned under frontend %q; ACL names must be unique within a frontend for Terraform natural-key management", name, frontendName)
		}
		matched = payload
	}

	if matched == nil {
		return haproxyFrontendACLModel{}, "", false, nil
	}

	apiID, err := apiRequiredScalarStringWithLabel(matched, "HAProxy frontend ACL", "id")
	if err != nil {
		return haproxyFrontendACLModel{}, "", false, fmt.Errorf("%w; confirm UAT returns child object IDs from GET %s before using update/delete", err, haproxyFrontendACLsPath)
	}
	position, err := haproxyFrontendACLPosition(ctx, client, parentID, frontendName, name)
	if err != nil {
		return haproxyFrontendACLModel{}, "", false, err
	}
	model, err := haproxyFrontendACLModelFromAPI(matched, frontendName, position)
	if err != nil {
		return haproxyFrontendACLModel{}, "", false, err
	}

	return model, apiID, true, nil
}

func haproxyFrontendACLPosition(ctx context.Context, client *pfsense.Client, parentID string, frontendName string, name string) (int64, error) {
	var raw any
	if err := client.Get(ctx, haproxyFrontendACLsQueryPath(parentID, ""), &raw); err != nil {
		return 0, err
	}

	payloads, err := haproxyFrontendACLPayloadList(raw)
	if err != nil {
		return 0, err
	}

	position := int64(-1)
	for index, payload := range payloads {
		candidateName, err := apiRequiredStringWithLabel(payload, "HAProxy frontend ACL", "name")
		if err != nil {
			return 0, err
		}
		if candidateName != name {
			continue
		}
		if position >= 0 {
			return 0, fmt.Errorf("multiple HAProxy frontend ACLs named %q were returned under frontend %q; ACL names must be unique within a frontend for Terraform natural-key management", name, frontendName)
		}
		position = int64(index)
	}
	if position < 0 {
		return 0, fmt.Errorf("HAProxy frontend ACL %q under frontend %q was returned by filtered lookup but not by full ordered lookup", name, frontendName)
	}

	return position, nil
}

func haproxyFrontendACLPayloadList(raw any) ([]map[string]any, error) {
	if raw == nil {
		return nil, nil
	}

	switch typed := raw.(type) {
	case []any:
		payloads := make([]map[string]any, 0, len(typed))
		for index, item := range typed {
			payload, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("HAProxy frontend ACLs response item %d has unsupported type %T", index, item)
			}
			payloads = append(payloads, payload)
		}
		return payloads, nil
	case []map[string]any:
		return typed, nil
	case map[string]any:
		return []map[string]any{typed}, nil
	default:
		return nil, fmt.Errorf("HAProxy frontend ACLs response has unsupported type %T; confirm the live UAT /services/haproxy/frontend/acls schema", raw)
	}
}

func haproxyFrontendACLModelFromAPI(payload map[string]any, frontendName string, position int64) (haproxyFrontendACLModel, error) {
	acl := nullHaproxyFrontendACLModel()

	nameValue, err := apiRequiredStringWithLabel(payload, "HAProxy frontend ACL", "name")
	if err != nil {
		return acl, err
	}
	name, err := haproxyFrontendACLName(types.StringValue(nameValue))
	if err != nil {
		return acl, fmt.Errorf("HAProxy frontend ACL name %s", err.Error())
	}
	expressionValue, err := apiRequiredStringWithLabel(payload, "HAProxy frontend ACL", "expression")
	if err != nil {
		return acl, err
	}
	expression, err := haproxyFrontendACLExpression(types.StringValue(expressionValue))
	if err != nil {
		return acl, fmt.Errorf("HAProxy frontend ACL expression %s", err.Error())
	}
	value, err := apiRequiredStringAllowEmptyWithLabel(payload, "HAProxy frontend ACL", "value")
	if err != nil {
		return acl, err
	}

	acl.ID = types.StringValue(haproxyFrontendACLTerraformID(frontendName, name))
	acl.FrontendName = types.StringValue(frontendName)
	acl.Name = types.StringValue(name)
	acl.Expression = types.StringValue(expression)
	acl.Value = types.StringValue(value)
	acl.Position = types.Int64Value(position)

	if acl.CaseSensitive, err = apiBoolDefaultFalseWithLabel(payload, "HAProxy frontend ACL", "casesensitive"); err != nil {
		return acl, err
	}
	if acl.Not, err = apiBoolDefaultFalseWithLabel(payload, "HAProxy frontend ACL", "not"); err != nil {
		return acl, err
	}

	return acl, nil
}

func nullHaproxyFrontendACLModel() haproxyFrontendACLModel {
	return haproxyFrontendACLModel{
		ID:            types.StringNull(),
		FrontendName:  types.StringNull(),
		Name:          types.StringNull(),
		Expression:    types.StringNull(),
		Value:         types.StringNull(),
		CaseSensitive: types.BoolNull(),
		Not:           types.BoolNull(),
		Position:      types.Int64Null(),
	}
}

func (m haproxyFrontendACLModel) attrValues() map[string]attr.Value {
	return map[string]attr.Value{
		"expression":    m.Expression,
		"value":         m.Value,
		"casesensitive": m.CaseSensitive,
		"not":           m.Not,
	}
}

func frontendACLTerraformValueToJSON(kind frontendACLAttributeKind, value attr.Value) any {
	if value.IsNull() {
		return nil
	}

	switch kind {
	case frontendACLAttributeBool:
		return value.(types.Bool).ValueBool()
	case frontendACLAttributeString:
		return value.(types.String).ValueString()
	default:
		return nil
	}
}

func haproxyFrontendACLsQueryPath(parentID string, name string) string {
	values := url.Values{}
	values.Set("parent_id", parentID)
	if name != "" {
		values.Set("name", name)
	}
	return haproxyFrontendACLsPath + "?" + values.Encode()
}

func haproxyFrontendACLDeletePath(parentID string, aclID string) string {
	values := url.Values{}
	values.Set("parent_id", parentID)
	values.Set("id", aclID)
	return haproxyFrontendACLPath + "?" + values.Encode()
}

func haproxyFrontendACLTerraformID(frontendName string, name string) string {
	return frontendName + "/" + name
}

func frontendACLLookupErrorDetail(keys haproxyFrontendACLKeys, err error) string {
	return fmt.Sprintf("%s. Confirm GET %s is available on UAT, accepts parent_id/name query filters, returns ordered ACL objects with stable name fields, and includes the transient pfSense child object id required for update/delete. Frontend name: %q. ACL name: %q.", err.Error(), haproxyFrontendACLsPath, keys.frontendName, keys.name)
}
