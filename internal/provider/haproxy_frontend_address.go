package provider

import (
	"context"
	"fmt"
	"net/netip"
	"net/url"
	"strconv"
	"strings"

	"github.com/d3vi1/terraform-provider-pfsense-restapi-haproxy/internal/pfsense"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const (
	haproxyFrontendAddressPath   = "/services/haproxy/frontend/address"
	haproxyFrontendAddressesPath = "/services/haproxy/frontend/addresses"
)

var (
	_ resource.Resource                = (*haproxyFrontendAddressResource)(nil)
	_ resource.ResourceWithConfigure   = (*haproxyFrontendAddressResource)(nil)
	_ resource.ResourceWithImportState = (*haproxyFrontendAddressResource)(nil)
	_ resource.ResourceWithModifyPlan  = (*haproxyFrontendAddressResource)(nil)
)

type frontendAddressAttributeKind string

const (
	frontendAddressAttributeBool frontendAddressAttributeKind = "bool"
)

type frontendAddressAttribute struct {
	Name        string
	JSONName    string
	Kind        frontendAddressAttributeKind
	Description string
}

var haproxyFrontendAddressAttributes = []frontendAddressAttribute{
	{Name: "extaddr_ssl", JSONName: "extaddr_ssl", Kind: frontendAddressAttributeBool, Description: "Enable or disable SSL/TLS offloading for this frontend address."},
}

type haproxyFrontendAddressResource struct {
	client *pfsense.Client
}

type haproxyFrontendAddressModel struct {
	ID            types.String `tfsdk:"id"`
	FrontendName  types.String `tfsdk:"frontend_name"`
	Extaddr       types.String `tfsdk:"extaddr"`
	ExtaddrCustom types.String `tfsdk:"extaddr_custom"`
	ExtaddrPort   types.Int64  `tfsdk:"extaddr_port"`
	ExtaddrSSL    types.Bool   `tfsdk:"extaddr_ssl"`
}

func newHaproxyFrontendAddressResource() resource.Resource {
	return &haproxyFrontendAddressResource{}
}

func (r *haproxyFrontendAddressResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "pfsense_haproxy_frontend_address"
}

func (r *haproxyFrontendAddressResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = resourceschema.Schema{
		Description: "Manages a pfSense HAProxy frontend bind/listen address as a child of pfsense_haproxy_frontend. Terraform uses frontend name, listen address, custom address, and port as the stable ID and resolves pfSense's current frontend/address object IDs before writes because pfSense object IDs may not be durable.",
		Attributes:  haproxyFrontendAddressResourceSchemaAttributes(),
	}
}

func (r *haproxyFrontendAddressResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *haproxyFrontendAddressResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}

	var plan haproxyFrontendAddressModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if plan.Extaddr.IsNull() || plan.Extaddr.IsUnknown() || plan.ExtaddrCustom.IsNull() || plan.ExtaddrCustom.IsUnknown() {
		return
	}

	extaddr, err := haproxyFrontendAddressExtaddr(plan.Extaddr)
	if err != nil {
		resp.Diagnostics.AddError("Invalid HAProxy frontend address", err.Error())
		return
	}
	normalizedCustom, err := haproxyFrontendAddressExtaddrCustom(plan.ExtaddrCustom, extaddr)
	if err != nil {
		resp.Diagnostics.AddError("Invalid HAProxy frontend address", err.Error())
		return
	}
	if plan.ExtaddrCustom.IsNull() || plan.ExtaddrCustom.IsUnknown() || plan.ExtaddrCustom.ValueString() == normalizedCustom {
		return
	}

	plan.ExtaddrCustom = types.StringValue(normalizedCustom)
	resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
}

func (r *haproxyFrontendAddressResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing pfSense client", "The provider was not configured before creating pfsense_haproxy_frontend_address.")
		return
	}

	var plan haproxyFrontendAddressModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	normalized, err := validateHaproxyFrontendAddressPlan(plan)
	if err != nil {
		resp.Diagnostics.AddError("Invalid HAProxy frontend address", err.Error())
		return
	}

	_, parentID, found, err := findHaproxyFrontendByName(ctx, r.client, normalized.frontendName)
	if err != nil {
		resp.Diagnostics.AddError("Find parent HAProxy frontend before create failed", frontendAddressParentLookupErrorDetail(normalized.frontendName, err))
		return
	}
	if !found {
		resp.Diagnostics.AddError("Create HAProxy frontend address failed", fmt.Sprintf("Parent frontend %q was not found on pfSense. Create or import pfsense_haproxy_frontend.%s before managing child addresses.", normalized.frontendName, normalized.frontendName))
		return
	}

	_, _, found, err = findHaproxyFrontendAddress(ctx, r.client, parentID, normalized)
	if err != nil {
		resp.Diagnostics.AddError("Check existing HAProxy frontend address failed", frontendAddressLookupErrorDetail(normalized, err))
		return
	}
	if found {
		resp.Diagnostics.AddError(
			"HAProxy frontend address already exists",
			fmt.Sprintf("A pfSense HAProxy frontend address %q already exists under frontend %q. Import it with `terraform import pfsense_haproxy_frontend_address.<name> %s` before managing it.", normalized.addressLabel(), normalized.frontendName, haproxyFrontendAddressTerraformID(normalized)),
		)
		return
	}

	if err := r.client.Post(ctx, haproxyFrontendAddressPath, buildHaproxyFrontendAddressCreatePayload(plan, parentID, normalized), nil); err != nil {
		resp.Diagnostics.AddError("Create HAProxy frontend address failed", err.Error())
		return
	}

	address, _, found, err := findHaproxyFrontendAddress(ctx, r.client, parentID, normalized)
	if err != nil {
		resp.Diagnostics.AddError("Read HAProxy frontend address after create failed", frontendAddressLookupErrorDetail(normalized, err))
		return
	}
	if !found {
		resp.Diagnostics.AddError(
			"Read HAProxy frontend address after create failed",
			fmt.Sprintf("Created address %q under frontend %q but GET %s did not return it. Confirm the live UAT child response shape and natural-key filtering before relying on this resource.", normalized.addressLabel(), normalized.frontendName, haproxyFrontendAddressesPath),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, address)...)
}

func (r *haproxyFrontendAddressResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing pfSense client", "The provider was not configured before reading pfsense_haproxy_frontend_address.")
		return
	}

	var state haproxyFrontendAddressModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	keys, err := haproxyFrontendAddressStateKeys(state)
	if err != nil {
		resp.Diagnostics.AddError("Invalid HAProxy frontend address state", err.Error())
		return
	}

	_, parentID, found, err := findHaproxyFrontendByName(ctx, r.client, keys.frontendName)
	if err != nil {
		resp.Diagnostics.AddError("Find parent HAProxy frontend failed", frontendAddressParentLookupErrorDetail(keys.frontendName, err))
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	address, _, found, err := findHaproxyFrontendAddress(ctx, r.client, parentID, keys)
	if err != nil {
		resp.Diagnostics.AddError("Read HAProxy frontend address failed", frontendAddressLookupErrorDetail(keys, err))
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, address)...)
}

func (r *haproxyFrontendAddressResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing pfSense client", "The provider was not configured before updating pfsense_haproxy_frontend_address.")
		return
	}

	var plan, prior haproxyFrontendAddressModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}

	normalized, err := validateHaproxyFrontendAddressPlan(plan)
	if err != nil {
		resp.Diagnostics.AddError("Invalid HAProxy frontend address", err.Error())
		return
	}
	priorKeys, err := haproxyFrontendAddressStateKeys(prior)
	if err != nil {
		resp.Diagnostics.AddError("Invalid HAProxy frontend address prior state", err.Error())
		return
	}
	if !normalized.sameNaturalKey(priorKeys) {
		resp.Diagnostics.AddError("Renaming HAProxy frontend addresses is not supported", "The frontend name, listen address, custom address, and port form the Terraform natural key. Change any natural-key value by creating a new resource and deleting the old one.")
		return
	}

	_, parentID, found, err := findHaproxyFrontendByName(ctx, r.client, normalized.frontendName)
	if err != nil {
		resp.Diagnostics.AddError("Find parent HAProxy frontend before update failed", frontendAddressParentLookupErrorDetail(normalized.frontendName, err))
		return
	}
	if !found {
		resp.Diagnostics.AddError("Update HAProxy frontend address failed", fmt.Sprintf("Parent frontend %q was not found on pfSense. Recreate it or remove the child address from Terraform state.", normalized.frontendName))
		return
	}

	_, addressID, found, err := findHaproxyFrontendAddress(ctx, r.client, parentID, normalized)
	if err != nil {
		resp.Diagnostics.AddError("Find HAProxy frontend address before update failed", frontendAddressLookupErrorDetail(normalized, err))
		return
	}
	if !found {
		resp.Diagnostics.AddError("Update HAProxy frontend address failed", fmt.Sprintf("Address %q under frontend %q was not found on pfSense. Recreate it or remove it from Terraform state.", normalized.addressLabel(), normalized.frontendName))
		return
	}

	patch := buildHaproxyFrontendAddressPatch(plan, prior, parentID, addressID)
	if len(patch) > 2 {
		if err := r.client.Patch(ctx, haproxyFrontendAddressPath, patch, nil); err != nil {
			resp.Diagnostics.AddError("Update HAProxy frontend address failed", err.Error())
			return
		}
	}

	address, _, found, err := findHaproxyFrontendAddress(ctx, r.client, parentID, normalized)
	if err != nil {
		resp.Diagnostics.AddError("Read HAProxy frontend address after update failed", frontendAddressLookupErrorDetail(normalized, err))
		return
	}
	if !found {
		resp.Diagnostics.AddError("Read HAProxy frontend address after update failed", fmt.Sprintf("Address %q under frontend %q was not returned by GET %s after PATCH.", normalized.addressLabel(), normalized.frontendName, haproxyFrontendAddressesPath))
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, address)...)
}

func (r *haproxyFrontendAddressResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing pfSense client", "The provider was not configured before deleting pfsense_haproxy_frontend_address.")
		return
	}

	var state haproxyFrontendAddressModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	keys, err := haproxyFrontendAddressStateKeys(state)
	if err != nil {
		resp.Diagnostics.AddError("Invalid HAProxy frontend address state", err.Error())
		return
	}

	_, parentID, found, err := findHaproxyFrontendByName(ctx, r.client, keys.frontendName)
	if err != nil {
		resp.Diagnostics.AddError("Find parent HAProxy frontend before delete failed", frontendAddressParentLookupErrorDetail(keys.frontendName, err))
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	_, addressID, found, err := findHaproxyFrontendAddress(ctx, r.client, parentID, keys)
	if err != nil {
		resp.Diagnostics.AddError("Find HAProxy frontend address before delete failed", frontendAddressLookupErrorDetail(keys, err))
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	if err := r.client.Delete(ctx, haproxyFrontendAddressDeletePath(parentID, addressID), nil); err != nil {
		resp.Diagnostics.AddError("Delete HAProxy frontend address failed", err.Error())
		return
	}

	resp.State.RemoveResource(ctx)
}

func (r *haproxyFrontendAddressResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	keys, err := parseHaproxyFrontendAddressImportID(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Invalid HAProxy frontend address import ID", err.Error())
		return
	}

	model := nullHaproxyFrontendAddressModel()
	model.ID = types.StringValue(haproxyFrontendAddressTerraformID(keys))
	model.FrontendName = types.StringValue(keys.frontendName)
	model.Extaddr = types.StringValue(keys.extaddr)
	model.ExtaddrCustom = types.StringValue(keys.extaddrCustom)
	model.ExtaddrPort = types.Int64Value(keys.extaddrPort)
	resp.Diagnostics.Append(resp.State.Set(ctx, model)...)
}

func haproxyFrontendAddressResourceSchemaAttributes() map[string]resourceschema.Attribute {
	return map[string]resourceschema.Attribute{
		"id": resourceschema.StringAttribute{
			Computed:    true,
			Description: "Stable Terraform ID for the frontend address in frontend_name/extaddr/extaddr_custom_or_-/extaddr_port form. This is not the pfSense object ID.",
		},
		"frontend_name": resourceschema.StringAttribute{
			Required:    true,
			Description: "Name of the parent pfsense_haproxy_frontend. Terraform resolves the current pfSense frontend object ID by this natural key before every frontend address write.",
			PlanModifiers: []planmodifier.String{
				stringplanmodifier.RequiresReplace(),
			},
		},
		"extaddr": resourceschema.StringAttribute{
			Required:    true,
			Description: "External address selector for this frontend bind. Valid values are custom, any_ipv4, any_ipv6, localhost_ipv4, and localhost_ipv6. Terraform treats this as part of the natural key and requires replacement if it changes.",
			PlanModifiers: []planmodifier.String{
				stringplanmodifier.RequiresReplace(),
			},
		},
		"extaddr_custom": resourceschema.StringAttribute{
			Optional:    true,
			Computed:    true,
			Default:     stringdefault.StaticString(""),
			Description: "Custom IPv4 or IPv6 listen address. Required when extaddr is custom and must be unset for built-in extaddr selectors. Terraform treats this as part of the natural key and requires replacement if it changes.",
			PlanModifiers: []planmodifier.String{
				stringplanmodifier.RequiresReplace(),
			},
		},
		"extaddr_port": resourceschema.Int64Attribute{
			Required:    true,
			Description: "TCP port to bind for this frontend address. Terraform treats this as part of the natural key and requires replacement if it changes.",
			PlanModifiers: []planmodifier.Int64{
				int64planmodifier.RequiresReplace(),
			},
		},
		"extaddr_ssl": resourceschema.BoolAttribute{
			Optional:    true,
			Computed:    true,
			Description: "Enable or disable SSL/TLS offloading for this frontend address.",
		},
	}
}

type haproxyFrontendAddressKeys struct {
	frontendName  string
	extaddr       string
	extaddrCustom string
	extaddrPort   int64
}

func (k haproxyFrontendAddressKeys) sameNaturalKey(other haproxyFrontendAddressKeys) bool {
	return k.frontendName == other.frontendName &&
		k.extaddr == other.extaddr &&
		k.extaddrCustom == other.extaddrCustom &&
		k.extaddrPort == other.extaddrPort
}

func (k haproxyFrontendAddressKeys) addressLabel() string {
	if k.extaddrCustom != "" {
		return fmt.Sprintf("%s:%d", k.extaddrCustom, k.extaddrPort)
	}

	return fmt.Sprintf("%s:%d", k.extaddr, k.extaddrPort)
}

func validateHaproxyFrontendAddressPlan(model haproxyFrontendAddressModel) (haproxyFrontendAddressKeys, error) {
	frontendName, err := haproxyFrontendAddressFrontendName(model.FrontendName)
	if err != nil {
		return haproxyFrontendAddressKeys{}, err
	}
	extaddr, err := haproxyFrontendAddressExtaddr(model.Extaddr)
	if err != nil {
		return haproxyFrontendAddressKeys{}, err
	}
	extaddrCustom, err := haproxyFrontendAddressExtaddrCustom(model.ExtaddrCustom, extaddr)
	if err != nil {
		return haproxyFrontendAddressKeys{}, err
	}
	extaddrPort, err := haproxyFrontendAddressPort(model.ExtaddrPort)
	if err != nil {
		return haproxyFrontendAddressKeys{}, err
	}

	return haproxyFrontendAddressKeys{
		frontendName:  frontendName,
		extaddr:       extaddr,
		extaddrCustom: extaddrCustom,
		extaddrPort:   extaddrPort,
	}, nil
}

func haproxyFrontendAddressFrontendName(value types.String) (string, error) {
	name, err := haproxyFrontendName(value)
	if err != nil {
		return "", fmt.Errorf("frontend_name %s", err.Error())
	}

	return name, nil
}

func haproxyFrontendAddressExtaddr(value types.String) (string, error) {
	if value.IsNull() || value.IsUnknown() {
		return "", fmt.Errorf("extaddr is required")
	}
	extaddr := value.ValueString()
	if strings.TrimSpace(extaddr) != extaddr || extaddr == "" {
		return "", fmt.Errorf("extaddr must be one of custom, any_ipv4, any_ipv6, localhost_ipv4, or localhost_ipv6")
	}
	if strings.Contains(extaddr, "/") {
		return "", fmt.Errorf("extaddr must not contain /")
	}
	switch extaddr {
	case "custom", "any_ipv4", "any_ipv6", "localhost_ipv4", "localhost_ipv6":
		return extaddr, nil
	default:
		return "", fmt.Errorf("extaddr must be one of custom, any_ipv4, any_ipv6, localhost_ipv4, or localhost_ipv6")
	}
}

func haproxyFrontendAddressExtaddrCustom(value types.String, extaddr string) (string, error) {
	if value.IsUnknown() {
		return "", fmt.Errorf("extaddr_custom must not be unknown")
	}
	if value.IsNull() {
		if extaddr == "custom" {
			return "", fmt.Errorf("extaddr_custom is required when extaddr is custom")
		}
		return "", nil
	}

	extaddrCustom := value.ValueString()
	trimmed := strings.TrimSpace(extaddrCustom)
	if extaddrCustom != trimmed {
		return "", fmt.Errorf("extaddr_custom must not contain leading or trailing whitespace")
	}
	if extaddrCustom == "" {
		if extaddr == "custom" {
			return "", fmt.Errorf("extaddr_custom is required when extaddr is custom")
		}
		return "", nil
	}
	if strings.Contains(extaddrCustom, "/") {
		return "", fmt.Errorf("extaddr_custom must not contain /")
	}
	if extaddrCustom == "-" {
		return "", fmt.Errorf("extaddr_custom must not be - because - is reserved in Terraform import IDs")
	}
	if extaddr != "custom" {
		return "", fmt.Errorf("extaddr_custom is only valid when extaddr is custom")
	}
	parsedCustom, err := netip.ParseAddr(extaddrCustom)
	if err != nil {
		return "", fmt.Errorf("extaddr_custom must be a valid IPv4 or IPv6 address: %w", err)
	}

	return parsedCustom.String(), nil
}

func haproxyFrontendAddressPort(value types.Int64) (int64, error) {
	if value.IsNull() || value.IsUnknown() {
		return 0, fmt.Errorf("extaddr_port is required")
	}
	port := value.ValueInt64()
	if port < 1 || port > 65535 {
		return 0, fmt.Errorf("extaddr_port must be between 1 and 65535")
	}

	return port, nil
}

func haproxyFrontendAddressStateKeys(model haproxyFrontendAddressModel) (haproxyFrontendAddressKeys, error) {
	if !model.FrontendName.IsNull() && !model.FrontendName.IsUnknown() &&
		!model.Extaddr.IsNull() && !model.Extaddr.IsUnknown() &&
		!model.ExtaddrPort.IsNull() && !model.ExtaddrPort.IsUnknown() {
		frontendName, err := haproxyFrontendAddressFrontendName(model.FrontendName)
		if err != nil {
			return haproxyFrontendAddressKeys{}, err
		}
		extaddr, err := haproxyFrontendAddressExtaddr(model.Extaddr)
		if err != nil {
			return haproxyFrontendAddressKeys{}, err
		}
		extaddrCustom, err := haproxyFrontendAddressExtaddrCustom(model.ExtaddrCustom, extaddr)
		if err != nil {
			return haproxyFrontendAddressKeys{}, err
		}
		extaddrPort, err := haproxyFrontendAddressPort(model.ExtaddrPort)
		if err != nil {
			return haproxyFrontendAddressKeys{}, err
		}

		return haproxyFrontendAddressKeys{frontendName: frontendName, extaddr: extaddr, extaddrCustom: extaddrCustom, extaddrPort: extaddrPort}, nil
	}
	if !model.ID.IsNull() && !model.ID.IsUnknown() {
		return parseHaproxyFrontendAddressImportID(model.ID.ValueString())
	}

	return haproxyFrontendAddressKeys{}, fmt.Errorf("state is missing frontend_name, extaddr, and extaddr_port")
}

func parseHaproxyFrontendAddressImportID(id string) (haproxyFrontendAddressKeys, error) {
	trimmed := strings.TrimSpace(id)
	parts := strings.Split(trimmed, "/")
	if len(parts) != 4 {
		return haproxyFrontendAddressKeys{}, fmt.Errorf("import pfsense_haproxy_frontend_address with ID frontend_name/extaddr/extaddr_custom_or_-/extaddr_port")
	}
	for _, part := range parts {
		if part == "" {
			return haproxyFrontendAddressKeys{}, fmt.Errorf("import pfsense_haproxy_frontend_address with ID frontend_name/extaddr/extaddr_custom_or_-/extaddr_port")
		}
	}

	frontendName, err := haproxyFrontendAddressFrontendName(types.StringValue(parts[0]))
	if err != nil {
		return haproxyFrontendAddressKeys{}, err
	}
	extaddr, err := haproxyFrontendAddressExtaddr(types.StringValue(parts[1]))
	if err != nil {
		return haproxyFrontendAddressKeys{}, err
	}
	extaddrCustom := parts[2]
	if extaddrCustom == "-" {
		extaddrCustom = ""
	}
	normalizedCustom, err := haproxyFrontendAddressExtaddrCustom(types.StringValue(extaddrCustom), extaddr)
	if err != nil {
		return haproxyFrontendAddressKeys{}, err
	}
	extaddrPort, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil {
		return haproxyFrontendAddressKeys{}, fmt.Errorf("extaddr_port must be between 1 and 65535")
	}
	normalizedPort, err := haproxyFrontendAddressPort(types.Int64Value(extaddrPort))
	if err != nil {
		return haproxyFrontendAddressKeys{}, err
	}

	return haproxyFrontendAddressKeys{frontendName: frontendName, extaddr: extaddr, extaddrCustom: normalizedCustom, extaddrPort: normalizedPort}, nil
}

func buildHaproxyFrontendAddressCreatePayload(plan haproxyFrontendAddressModel, parentID string, normalized haproxyFrontendAddressKeys) map[string]any {
	payload := map[string]any{
		"parent_id":      parentID,
		"extaddr":        normalized.extaddr,
		"extaddr_custom": normalized.extaddrCustom,
		"extaddr_port":   normalized.extaddrPort,
	}
	values := plan.attrValues()

	for _, attribute := range haproxyFrontendAddressAttributes {
		planned := values[attribute.Name]
		if planned.IsNull() || planned.IsUnknown() {
			continue
		}
		payload[attribute.JSONName] = frontendAddressTerraformValueToJSON(attribute.Kind, planned)
	}

	return payload
}

func buildHaproxyFrontendAddressPatch(plan haproxyFrontendAddressModel, prior haproxyFrontendAddressModel, parentID string, addressID string) map[string]any {
	patch := map[string]any{
		"parent_id": parentID,
		"id":        addressID,
	}
	planValues := plan.attrValues()
	priorValues := prior.attrValues()

	for _, attribute := range haproxyFrontendAddressAttributes {
		planned := planValues[attribute.Name]
		if planned.IsUnknown() {
			continue
		}
		if planned.Equal(priorValues[attribute.Name]) {
			continue
		}

		patch[attribute.JSONName] = frontendAddressTerraformValueToJSON(attribute.Kind, planned)
	}

	return patch
}

func findHaproxyFrontendAddress(ctx context.Context, client *pfsense.Client, parentID string, keys haproxyFrontendAddressKeys) (haproxyFrontendAddressModel, string, bool, error) {
	return lookupHaproxyFrontendAddress(ctx, client, parentID, keys, true)
}

func lookupHaproxyFrontendAddress(ctx context.Context, client *pfsense.Client, parentID string, keys haproxyFrontendAddressKeys, requireAPIID bool) (haproxyFrontendAddressModel, string, bool, error) {
	var raw any
	if err := client.Get(ctx, haproxyFrontendAddressesQueryPath(parentID, keys), &raw); err != nil {
		return haproxyFrontendAddressModel{}, "", false, err
	}

	payloads, err := haproxyFrontendAddressPayloadList(raw)
	if err != nil {
		return haproxyFrontendAddressModel{}, "", false, err
	}

	var matched map[string]any
	for _, payload := range payloads {
		payloadMatches, err := haproxyFrontendAddressPayloadMatches(payload, keys)
		if err != nil {
			return haproxyFrontendAddressModel{}, "", false, err
		}
		if !payloadMatches {
			continue
		}
		if matched != nil {
			return haproxyFrontendAddressModel{}, "", false, fmt.Errorf("multiple HAProxy frontend addresses matching %q were returned under frontend %q; address natural keys must be unique within a frontend for Terraform management", keys.addressLabel(), keys.frontendName)
		}
		matched = payload
	}

	if matched == nil {
		return haproxyFrontendAddressModel{}, "", false, nil
	}

	apiID := ""
	if requireAPIID {
		var err error
		apiID, err = apiRequiredScalarStringWithLabel(matched, "HAProxy frontend address", "id")
		if err != nil {
			return haproxyFrontendAddressModel{}, "", false, fmt.Errorf("%w; confirm UAT returns child object IDs from GET %s before using update/delete", err, haproxyFrontendAddressesPath)
		}
	}
	model, err := haproxyFrontendAddressModelFromAPI(matched, keys.frontendName)
	if err != nil {
		return haproxyFrontendAddressModel{}, "", false, err
	}

	return model, apiID, true, nil
}

func haproxyFrontendAddressPayloadMatches(payload map[string]any, keys haproxyFrontendAddressKeys) (bool, error) {
	extaddrValue, err := apiRequiredStringWithLabel(payload, "HAProxy frontend address", "extaddr")
	if err != nil {
		return false, err
	}
	extaddr, err := haproxyFrontendAddressExtaddr(types.StringValue(extaddrValue))
	if err != nil {
		return false, fmt.Errorf("HAProxy frontend address extaddr %s", err.Error())
	}
	if extaddr != keys.extaddr {
		return false, nil
	}
	extaddrCustom, err := haproxyFrontendAddressAPIExtaddrCustom(payload, extaddr)
	if err != nil {
		return false, err
	}
	if extaddrCustom != keys.extaddrCustom {
		return false, nil
	}
	extaddrPort, err := apiRequiredInt64WithLabel(payload, "HAProxy frontend address", "extaddr_port")
	if err != nil {
		return false, err
	}

	return extaddrPort == keys.extaddrPort, nil
}

func haproxyFrontendAddressPayloadList(raw any) ([]map[string]any, error) {
	if raw == nil {
		return nil, nil
	}

	switch typed := raw.(type) {
	case []any:
		payloads := make([]map[string]any, 0, len(typed))
		for index, item := range typed {
			payload, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("HAProxy frontend addresses response item %d has unsupported type %T", index, item)
			}
			payloads = append(payloads, payload)
		}
		return payloads, nil
	case []map[string]any:
		return typed, nil
	case map[string]any:
		return []map[string]any{typed}, nil
	default:
		return nil, fmt.Errorf("HAProxy frontend addresses response has unsupported type %T; confirm the live UAT /services/haproxy/frontend/addresses schema", raw)
	}
}

func haproxyFrontendAddressModelFromAPI(payload map[string]any, frontendName string) (haproxyFrontendAddressModel, error) {
	address := nullHaproxyFrontendAddressModel()

	extaddrValue, err := apiRequiredStringWithLabel(payload, "HAProxy frontend address", "extaddr")
	if err != nil {
		return address, err
	}
	extaddr, err := haproxyFrontendAddressExtaddr(types.StringValue(extaddrValue))
	if err != nil {
		return address, fmt.Errorf("HAProxy frontend address extaddr %s", err.Error())
	}
	extaddrCustom, err := haproxyFrontendAddressAPIExtaddrCustom(payload, extaddr)
	if err != nil {
		return address, err
	}
	extaddrPort, err := apiRequiredInt64WithLabel(payload, "HAProxy frontend address", "extaddr_port")
	if err != nil {
		return address, err
	}
	keys := haproxyFrontendAddressKeys{frontendName: frontendName, extaddr: extaddr, extaddrCustom: extaddrCustom, extaddrPort: extaddrPort}

	address.ID = types.StringValue(haproxyFrontendAddressTerraformID(keys))
	address.FrontendName = types.StringValue(frontendName)
	address.Extaddr = types.StringValue(extaddr)
	address.ExtaddrCustom = types.StringValue(extaddrCustom)
	address.ExtaddrPort = types.Int64Value(extaddrPort)

	if address.ExtaddrSSL, err = apiBoolWithLabel(payload, "HAProxy frontend address", "extaddr_ssl"); err != nil {
		return address, err
	}

	return address, nil
}

func haproxyFrontendAddressAPIExtaddrCustom(payload map[string]any, extaddr string) (string, error) {
	extaddrCustomValue, err := apiStringWithLabel(payload, "HAProxy frontend address", "extaddr_custom")
	if err != nil {
		return "", err
	}
	if extaddrCustomValue.IsNull() || extaddrCustomValue.IsUnknown() {
		return haproxyFrontendAddressExtaddrCustom(types.StringValue(""), extaddr)
	}

	return haproxyFrontendAddressExtaddrCustom(extaddrCustomValue, extaddr)
}

func nullHaproxyFrontendAddressModel() haproxyFrontendAddressModel {
	return haproxyFrontendAddressModel{
		ID:            types.StringNull(),
		FrontendName:  types.StringNull(),
		Extaddr:       types.StringNull(),
		ExtaddrCustom: types.StringNull(),
		ExtaddrPort:   types.Int64Null(),
		ExtaddrSSL:    types.BoolNull(),
	}
}

func (m haproxyFrontendAddressModel) attrValues() map[string]attr.Value {
	return map[string]attr.Value{
		"extaddr_ssl": m.ExtaddrSSL,
	}
}

func frontendAddressTerraformValueToJSON(kind frontendAddressAttributeKind, value attr.Value) any {
	if value.IsNull() {
		return nil
	}

	switch kind {
	case frontendAddressAttributeBool:
		return value.(types.Bool).ValueBool()
	default:
		return nil
	}
}

func haproxyFrontendAddressesQueryPath(parentID string, keys haproxyFrontendAddressKeys) string {
	values := url.Values{}
	values.Set("parent_id", parentID)
	values.Set("extaddr", keys.extaddr)
	values.Set("extaddr_custom", keys.extaddrCustom)
	values.Set("extaddr_port", strconv.FormatInt(keys.extaddrPort, 10))
	return haproxyFrontendAddressesPath + "?" + values.Encode()
}

func haproxyFrontendAddressDeletePath(parentID string, addressID string) string {
	values := url.Values{}
	values.Set("parent_id", parentID)
	values.Set("id", addressID)
	return haproxyFrontendAddressPath + "?" + values.Encode()
}

func haproxyFrontendAddressTerraformID(keys haproxyFrontendAddressKeys) string {
	custom := keys.extaddrCustom
	if custom == "" {
		custom = "-"
	}

	return keys.frontendName + "/" + keys.extaddr + "/" + custom + "/" + strconv.FormatInt(keys.extaddrPort, 10)
}

func frontendAddressLookupErrorDetail(keys haproxyFrontendAddressKeys, err error) string {
	return fmt.Sprintf("%s. Confirm GET %s is available on UAT, accepts parent_id/extaddr/extaddr_custom/extaddr_port query filters, returns frontend address objects with stable extaddr fields, and includes the transient pfSense child object id required for update/delete. Frontend name: %q. Address: %q.", err.Error(), haproxyFrontendAddressesPath, keys.frontendName, keys.addressLabel())
}

func frontendAddressParentLookupErrorDetail(frontendName string, err error) string {
	return fmt.Sprintf("%s. Confirm GET %s is available on UAT, returns one frontend object with a stable name field, and includes the transient pfSense frontend object id required to query child addresses. Frontend name: %q.", err.Error(), haproxyFrontendsPath, frontendName)
}
