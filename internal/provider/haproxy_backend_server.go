package provider

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/d3vi1/terraform-provider-pfsense-restapi-haproxy/internal/pfsense"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	datasourceschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const (
	haproxyBackendServerPath  = "/services/haproxy/backend/server"
	haproxyBackendServersPath = "/services/haproxy/backend/servers"
)

var (
	_ datasource.DataSource              = (*haproxyBackendServerDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*haproxyBackendServerDataSource)(nil)
	_ resource.Resource                  = (*haproxyBackendServerResource)(nil)
	_ resource.ResourceWithConfigure     = (*haproxyBackendServerResource)(nil)
	_ resource.ResourceWithImportState   = (*haproxyBackendServerResource)(nil)

	haproxyNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
)

type backendServerAttributeKind string

const (
	backendServerAttributeBool   backendServerAttributeKind = "bool"
	backendServerAttributeInt64  backendServerAttributeKind = "int64"
	backendServerAttributeString backendServerAttributeKind = "string"
)

type backendServerAttribute struct {
	Name        string
	JSONName    string
	Kind        backendServerAttributeKind
	Description string
}

var haproxyBackendServerAttributes = []backendServerAttribute{
	{Name: "address", JSONName: "address", Kind: backendServerAttributeString, Description: "Hostname or IP address for this backend server. Hostnames are resolved by HAProxy at service startup."},
	{Name: "port", JSONName: "port", Kind: backendServerAttributeInt64, Description: "TCP port to forward to on this backend server."},
	{Name: "status", JSONName: "status", Kind: backendServerAttributeString, Description: "Eligibility status for this backend server: active, backup, disabled, or inactive."},
	{Name: "weight", JSONName: "weight", Kind: backendServerAttributeInt64, Description: "Load-balancing weight for this backend server. pfREST accepts values from 0 through 256."},
	{Name: "ssl", JSONName: "ssl", Kind: backendServerAttributeBool, Description: "Use SSL/TLS when forwarding to this backend server."},
	{Name: "sslserververify", JSONName: "sslserververify", Kind: backendServerAttributeBool, Description: "Verify the backend server SSL/TLS certificate when forwarding with SSL/TLS."},
}

type haproxyBackendServerResource struct {
	client *pfsense.Client
}

type haproxyBackendServerDataSource struct {
	client *pfsense.Client
}

type haproxyBackendServerModel struct {
	ID              types.String `tfsdk:"id"`
	BackendName     types.String `tfsdk:"backend_name"`
	Name            types.String `tfsdk:"name"`
	Address         types.String `tfsdk:"address"`
	Port            types.Int64  `tfsdk:"port"`
	Status          types.String `tfsdk:"status"`
	Weight          types.Int64  `tfsdk:"weight"`
	SSL             types.Bool   `tfsdk:"ssl"`
	SSLServerVerify types.Bool   `tfsdk:"sslserververify"`
	ServerID        types.Int64  `tfsdk:"serverid"`
}

func newHaproxyBackendServerDataSource() datasource.DataSource {
	return &haproxyBackendServerDataSource{}
}

func newHaproxyBackendServerResource() resource.Resource {
	return &haproxyBackendServerResource{}
}

func (d *haproxyBackendServerDataSource) Metadata(_ context.Context, _ datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "pfsense_haproxy_backend_server"
}

func (d *haproxyBackendServerDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = datasourceschema.Schema{
		Description: "Looks up a pfSense HAProxy backend server by exact parent backend name and server name without exposing pfSense's transient REST child object ID.",
		Attributes:  haproxyBackendServerDataSourceSchemaAttributes(),
	}
}

func (d *haproxyBackendServerDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *haproxyBackendServerDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		resp.Diagnostics.AddError("Missing pfSense client", "The provider was not configured before reading pfsense_haproxy_backend_server.")
		return
	}

	var config haproxyBackendServerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	backendName, err := haproxyBackendServerBackendName(config.BackendName)
	if err != nil {
		resp.Diagnostics.AddError("Invalid HAProxy backend server lookup", err.Error())
		return
	}
	name, err := haproxyBackendServerName(config.Name)
	if err != nil {
		resp.Diagnostics.AddError("Invalid HAProxy backend server lookup", err.Error())
		return
	}

	_, parentID, found, err := findHaproxyBackendByName(ctx, d.client, backendName)
	if err != nil {
		resp.Diagnostics.AddError("Find parent HAProxy backend failed", backendServerParentLookupErrorDetail(backendName, name, err))
		return
	}
	if !found {
		resp.Diagnostics.AddError(
			"HAProxy backend not found",
			fmt.Sprintf("No parent pfSense HAProxy backend named %q was returned by GET %s.", backendName, haproxyBackendsPath),
		)
		return
	}

	server, _, found, err := lookupHaproxyBackendServerByName(ctx, d.client, parentID, backendName, name, false)
	if err != nil {
		resp.Diagnostics.AddError("Read HAProxy backend server failed", backendServerDataSourceLookupErrorDetail(backendName, name, err))
		return
	}
	if !found {
		resp.Diagnostics.AddError(
			"HAProxy backend server not found",
			fmt.Sprintf("No pfSense HAProxy backend server named %q was returned under backend %q by GET %s.", name, backendName, haproxyBackendServersPath),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, server)...)
}

func (r *haproxyBackendServerResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "pfsense_haproxy_backend_server"
}

func (r *haproxyBackendServerResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = resourceschema.Schema{
		Description: "Manages a pfSense HAProxy backend server as a child of a pfsense_haproxy_backend. Terraform uses backend name plus server name as the stable ID and resolves pfSense's current backend/server object IDs before writes because pfSense object IDs may not be durable.",
		Attributes:  haproxyBackendServerResourceSchemaAttributes(),
	}
}

func (r *haproxyBackendServerResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *haproxyBackendServerResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing pfSense client", "The provider was not configured before creating pfsense_haproxy_backend_server.")
		return
	}

	var plan haproxyBackendServerModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	normalized, err := validateHaproxyBackendServerPlan(plan)
	if err != nil {
		resp.Diagnostics.AddError("Invalid HAProxy backend server", err.Error())
		return
	}

	_, parentID, found, err := findHaproxyBackendByName(ctx, r.client, normalized.backendName)
	if err != nil {
		resp.Diagnostics.AddError("Find parent HAProxy backend before create failed", backendLookupErrorDetail(normalized.backendName, err))
		return
	}
	if !found {
		resp.Diagnostics.AddError("Create HAProxy backend server failed", fmt.Sprintf("Parent backend %q was not found on pfSense. Create or import pfsense_haproxy_backend.%s before managing child servers.", normalized.backendName, normalized.backendName))
		return
	}

	_, _, found, err = findHaproxyBackendServerByName(ctx, r.client, parentID, normalized.backendName, normalized.name)
	if err != nil {
		resp.Diagnostics.AddError("Check existing HAProxy backend server failed", backendServerLookupErrorDetail(normalized.backendName, normalized.name, err))
		return
	}
	if found {
		resp.Diagnostics.AddError(
			"HAProxy backend server already exists",
			fmt.Sprintf("A pfSense HAProxy backend server named %q already exists under backend %q. Import it with `terraform import pfsense_haproxy_backend_server.<name> %s` before managing it.", normalized.name, normalized.backendName, haproxyBackendServerTerraformID(normalized.backendName, normalized.name)),
		)
		return
	}

	if err := r.client.Post(ctx, haproxyBackendServerPath, buildHaproxyBackendServerCreatePayload(plan, parentID, normalized), nil); err != nil {
		resp.Diagnostics.AddError("Create HAProxy backend server failed", err.Error())
		return
	}

	server, _, found, err := findHaproxyBackendServerByName(ctx, r.client, parentID, normalized.backendName, normalized.name)
	if err != nil {
		resp.Diagnostics.AddError("Read HAProxy backend server after create failed", backendServerLookupErrorDetail(normalized.backendName, normalized.name, err))
		return
	}
	if !found {
		resp.Diagnostics.AddError(
			"Read HAProxy backend server after create failed",
			fmt.Sprintf("Created server %q under backend %q but GET %s did not return it. Confirm the live UAT child response shape and natural-key filtering before relying on this resource.", normalized.name, normalized.backendName, haproxyBackendServersPath),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, server)...)
}

func (r *haproxyBackendServerResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing pfSense client", "The provider was not configured before reading pfsense_haproxy_backend_server.")
		return
	}

	var state haproxyBackendServerModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	keys, err := haproxyBackendServerStateKeys(state)
	if err != nil {
		resp.Diagnostics.AddError("Invalid HAProxy backend server state", err.Error())
		return
	}

	_, parentID, found, err := findHaproxyBackendByName(ctx, r.client, keys.backendName)
	if err != nil {
		resp.Diagnostics.AddError("Find parent HAProxy backend failed", backendLookupErrorDetail(keys.backendName, err))
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	server, _, found, err := findHaproxyBackendServerByName(ctx, r.client, parentID, keys.backendName, keys.name)
	if err != nil {
		resp.Diagnostics.AddError("Read HAProxy backend server failed", backendServerLookupErrorDetail(keys.backendName, keys.name, err))
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, server)...)
}

func (r *haproxyBackendServerResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing pfSense client", "The provider was not configured before updating pfsense_haproxy_backend_server.")
		return
	}

	var plan, prior haproxyBackendServerModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}

	normalized, err := validateHaproxyBackendServerPlan(plan)
	if err != nil {
		resp.Diagnostics.AddError("Invalid HAProxy backend server", err.Error())
		return
	}
	priorKeys, err := haproxyBackendServerStateKeys(prior)
	if err != nil {
		resp.Diagnostics.AddError("Invalid HAProxy backend server prior state", err.Error())
		return
	}
	if normalized.backendName != priorKeys.backendName || normalized.name != priorKeys.name {
		resp.Diagnostics.AddError("Renaming HAProxy backend servers is not supported", "The backend name and server name form the Terraform natural key. Change either value by creating a new resource and deleting the old one.")
		return
	}

	_, parentID, found, err := findHaproxyBackendByName(ctx, r.client, normalized.backendName)
	if err != nil {
		resp.Diagnostics.AddError("Find parent HAProxy backend before update failed", backendLookupErrorDetail(normalized.backendName, err))
		return
	}
	if !found {
		resp.Diagnostics.AddError("Update HAProxy backend server failed", fmt.Sprintf("Parent backend %q was not found on pfSense. Recreate it or remove the child server from Terraform state.", normalized.backendName))
		return
	}

	_, serverID, found, err := findHaproxyBackendServerByName(ctx, r.client, parentID, normalized.backendName, normalized.name)
	if err != nil {
		resp.Diagnostics.AddError("Find HAProxy backend server before update failed", backendServerLookupErrorDetail(normalized.backendName, normalized.name, err))
		return
	}
	if !found {
		resp.Diagnostics.AddError("Update HAProxy backend server failed", fmt.Sprintf("Server %q under backend %q was not found on pfSense. Recreate it or remove it from Terraform state.", normalized.name, normalized.backendName))
		return
	}

	patch := buildHaproxyBackendServerPatch(plan, prior, parentID, serverID)
	if len(patch) > 2 {
		if err := r.client.Patch(ctx, haproxyBackendServerPath, patch, nil); err != nil {
			resp.Diagnostics.AddError("Update HAProxy backend server failed", err.Error())
			return
		}
	}

	server, _, found, err := findHaproxyBackendServerByName(ctx, r.client, parentID, normalized.backendName, normalized.name)
	if err != nil {
		resp.Diagnostics.AddError("Read HAProxy backend server after update failed", backendServerLookupErrorDetail(normalized.backendName, normalized.name, err))
		return
	}
	if !found {
		resp.Diagnostics.AddError("Read HAProxy backend server after update failed", fmt.Sprintf("Server %q under backend %q was not returned by GET %s after PATCH.", normalized.name, normalized.backendName, haproxyBackendServersPath))
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, server)...)
}

func (r *haproxyBackendServerResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing pfSense client", "The provider was not configured before deleting pfsense_haproxy_backend_server.")
		return
	}

	var state haproxyBackendServerModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	keys, err := haproxyBackendServerStateKeys(state)
	if err != nil {
		resp.Diagnostics.AddError("Invalid HAProxy backend server state", err.Error())
		return
	}

	_, parentID, found, err := findHaproxyBackendByName(ctx, r.client, keys.backendName)
	if err != nil {
		resp.Diagnostics.AddError("Find parent HAProxy backend before delete failed", backendLookupErrorDetail(keys.backendName, err))
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	_, serverID, found, err := findHaproxyBackendServerByName(ctx, r.client, parentID, keys.backendName, keys.name)
	if err != nil {
		resp.Diagnostics.AddError("Find HAProxy backend server before delete failed", backendServerLookupErrorDetail(keys.backendName, keys.name, err))
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	if err := r.client.Delete(ctx, haproxyBackendServerDeletePath(parentID, serverID), nil); err != nil {
		resp.Diagnostics.AddError("Delete HAProxy backend server failed", err.Error())
		return
	}

	resp.State.RemoveResource(ctx)
}

func (r *haproxyBackendServerResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	keys, err := parseHaproxyBackendServerImportID(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Invalid HAProxy backend server import ID", err.Error())
		return
	}

	model := nullHaproxyBackendServerModel()
	model.ID = types.StringValue(haproxyBackendServerTerraformID(keys.backendName, keys.name))
	model.BackendName = types.StringValue(keys.backendName)
	model.Name = types.StringValue(keys.name)
	resp.Diagnostics.Append(resp.State.Set(ctx, model)...)
}

func haproxyBackendServerDataSourceSchemaAttributes() map[string]datasourceschema.Attribute {
	return map[string]datasourceschema.Attribute{
		"id": datasourceschema.StringAttribute{
			Computed:    true,
			Description: "Stable Terraform ID for the backend server in backend_name/name form. This is not the pfSense object ID.",
		},
		"backend_name": datasourceschema.StringAttribute{
			Required:    true,
			Description: "Name of the parent pfsense_haproxy_backend to look up. The parent is resolved by exact name so its current pfSense object ID can be used only for the child query.",
		},
		"name": datasourceschema.StringAttribute{
			Required:    true,
			Description: "Unique HAProxy backend server name within the parent backend. The data source requires an exact server name match.",
		},
		"address": datasourceschema.StringAttribute{
			Computed:    true,
			Description: "Hostname or IP address for this backend server. Hostnames are resolved by HAProxy at service startup.",
		},
		"port": datasourceschema.Int64Attribute{
			Computed:    true,
			Description: "TCP port to forward to on this backend server.",
		},
		"status": datasourceschema.StringAttribute{
			Computed:    true,
			Description: "Eligibility status for this backend server: active, backup, disabled, or inactive.",
		},
		"weight": datasourceschema.Int64Attribute{
			Computed:    true,
			Description: "Load-balancing weight for this backend server. pfREST accepts values from 0 through 256.",
		},
		"ssl": datasourceschema.BoolAttribute{
			Computed:    true,
			Description: "Use SSL/TLS when forwarding to this backend server.",
		},
		"sslserververify": datasourceschema.BoolAttribute{
			Computed:    true,
			Description: "Verify the backend server SSL/TLS certificate when forwarding with SSL/TLS.",
		},
		"serverid": datasourceschema.Int64Attribute{
			Computed:    true,
			Description: "Read-only HAProxy backend server ID assigned by pfSense for internal use.",
		},
	}
}

func haproxyBackendServerResourceSchemaAttributes() map[string]resourceschema.Attribute {
	return map[string]resourceschema.Attribute{
		"id": resourceschema.StringAttribute{
			Computed:    true,
			Description: "Stable Terraform ID for the backend server in backend_name/name form. This is not the pfSense object ID.",
		},
		"backend_name": resourceschema.StringAttribute{
			Required:    true,
			Description: "Name of the parent pfsense_haproxy_backend. Terraform resolves the current pfSense backend object ID by this natural key before every backend server write.",
			PlanModifiers: []planmodifier.String{
				stringplanmodifier.RequiresReplace(),
			},
		},
		"name": resourceschema.StringAttribute{
			Required:    true,
			Description: "Unique HAProxy backend server name within the parent backend. pfSense restricts names to letters, numbers, dot, hyphen, and underscore. Terraform treats this as part of the natural key and requires replacement if it changes.",
			PlanModifiers: []planmodifier.String{
				stringplanmodifier.RequiresReplace(),
			},
		},
		"address": resourceschema.StringAttribute{
			Required:    true,
			Description: "Hostname or IP address for this backend server. Hostnames are resolved by HAProxy at service startup.",
		},
		"port": resourceschema.Int64Attribute{
			Required:    true,
			Description: "TCP port to forward to on this backend server.",
		},
		"status": resourceschema.StringAttribute{
			Optional:    true,
			Computed:    true,
			Description: "Eligibility status for this backend server: active, backup, disabled, or inactive.",
		},
		"weight": resourceschema.Int64Attribute{
			Optional:    true,
			Computed:    true,
			Description: "Load-balancing weight for this backend server. pfREST accepts values from 0 through 256.",
		},
		"ssl": resourceschema.BoolAttribute{
			Optional:    true,
			Computed:    true,
			Description: "Use SSL/TLS when forwarding to this backend server.",
		},
		"sslserververify": resourceschema.BoolAttribute{
			Optional:    true,
			Computed:    true,
			Description: "Verify the backend server SSL/TLS certificate when forwarding with SSL/TLS.",
		},
		"serverid": resourceschema.Int64Attribute{
			Computed:    true,
			Description: "Read-only HAProxy backend server ID assigned by pfSense for internal use.",
		},
	}
}

type haproxyBackendServerKeys struct {
	backendName string
	name        string
	address     string
	port        int64
}

func validateHaproxyBackendServerPlan(model haproxyBackendServerModel) (haproxyBackendServerKeys, error) {
	backendName, err := haproxyBackendServerBackendName(model.BackendName)
	if err != nil {
		return haproxyBackendServerKeys{}, err
	}
	name, err := haproxyBackendServerName(model.Name)
	if err != nil {
		return haproxyBackendServerKeys{}, err
	}
	address, err := haproxyBackendServerAddress(model.Address)
	if err != nil {
		return haproxyBackendServerKeys{}, err
	}
	port, err := haproxyBackendServerPort(model.Port)
	if err != nil {
		return haproxyBackendServerKeys{}, err
	}
	if err := validateHaproxyBackendServerOptionalFields(model); err != nil {
		return haproxyBackendServerKeys{}, err
	}

	return haproxyBackendServerKeys{
		backendName: backendName,
		name:        name,
		address:     address,
		port:        port,
	}, nil
}

func validateHaproxyBackendServerOptionalFields(model haproxyBackendServerModel) error {
	if !model.Status.IsNull() && !model.Status.IsUnknown() {
		switch model.Status.ValueString() {
		case "active", "backup", "disabled", "inactive":
		default:
			return fmt.Errorf("status must be one of active, backup, disabled, or inactive")
		}
	}
	if !model.Weight.IsNull() && !model.Weight.IsUnknown() {
		weight := model.Weight.ValueInt64()
		if weight < 0 || weight > 256 {
			return fmt.Errorf("weight must be between 0 and 256")
		}
	}

	return nil
}

func haproxyBackendServerBackendName(value types.String) (string, error) {
	name, err := haproxyBackendName(value)
	if err != nil {
		return "", fmt.Errorf("backend_name %s", err.Error())
	}

	return name, nil
}

func haproxyBackendServerName(value types.String) (string, error) {
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

func haproxyBackendServerAddress(value types.String) (string, error) {
	if value.IsNull() || value.IsUnknown() {
		return "", fmt.Errorf("address is required")
	}
	address := value.ValueString()
	trimmed := strings.TrimSpace(address)
	if trimmed == "" {
		return "", fmt.Errorf("address must not be empty")
	}
	if trimmed != address {
		return "", fmt.Errorf("address must not contain leading or trailing whitespace")
	}

	return address, nil
}

func haproxyBackendServerPort(value types.Int64) (int64, error) {
	if value.IsNull() || value.IsUnknown() {
		return 0, fmt.Errorf("port is required")
	}
	port := value.ValueInt64()
	if port < 1 || port > 65535 {
		return 0, fmt.Errorf("port must be between 1 and 65535")
	}

	return port, nil
}

func haproxyBackendServerStateKeys(model haproxyBackendServerModel) (haproxyBackendServerKeys, error) {
	if !model.BackendName.IsNull() && !model.BackendName.IsUnknown() && !model.Name.IsNull() && !model.Name.IsUnknown() {
		backendName, err := haproxyBackendServerBackendName(model.BackendName)
		if err != nil {
			return haproxyBackendServerKeys{}, err
		}
		name, err := haproxyBackendServerName(model.Name)
		if err != nil {
			return haproxyBackendServerKeys{}, err
		}
		return haproxyBackendServerKeys{backendName: backendName, name: name}, nil
	}
	if !model.ID.IsNull() && !model.ID.IsUnknown() {
		return parseHaproxyBackendServerImportID(model.ID.ValueString())
	}

	return haproxyBackendServerKeys{}, fmt.Errorf("state is missing backend_name and name")
}

func parseHaproxyBackendServerImportID(id string) (haproxyBackendServerKeys, error) {
	trimmed := strings.TrimSpace(id)
	backendName, serverName, ok := strings.Cut(trimmed, "/")
	if !ok || backendName == "" || serverName == "" || strings.Contains(serverName, "/") {
		return haproxyBackendServerKeys{}, fmt.Errorf("import pfsense_haproxy_backend_server with ID backend_name/server_name")
	}

	backend, err := haproxyBackendServerBackendName(types.StringValue(backendName))
	if err != nil {
		return haproxyBackendServerKeys{}, err
	}
	server, err := haproxyBackendServerName(types.StringValue(serverName))
	if err != nil {
		return haproxyBackendServerKeys{}, err
	}

	return haproxyBackendServerKeys{backendName: backend, name: server}, nil
}

func buildHaproxyBackendServerCreatePayload(plan haproxyBackendServerModel, parentID string, normalized haproxyBackendServerKeys) map[string]any {
	payload := map[string]any{
		"parent_id": parentID,
		"name":      normalized.name,
		"address":   normalized.address,
		"port":      normalized.port,
	}
	values := plan.attrValues()

	for _, attribute := range haproxyBackendServerAttributes {
		if attribute.Name == "address" || attribute.Name == "port" {
			continue
		}
		planned := values[attribute.Name]
		if planned.IsNull() || planned.IsUnknown() {
			continue
		}
		payload[attribute.JSONName] = backendServerTerraformValueToJSON(attribute.Kind, planned)
	}

	return payload
}

func buildHaproxyBackendServerPatch(plan haproxyBackendServerModel, prior haproxyBackendServerModel, parentID string, serverID string) map[string]any {
	patch := map[string]any{
		"parent_id": parentID,
		"id":        serverID,
	}
	planValues := plan.attrValues()
	priorValues := prior.attrValues()

	for _, attribute := range haproxyBackendServerAttributes {
		planned := planValues[attribute.Name]
		if planned.IsUnknown() {
			continue
		}
		if planned.Equal(priorValues[attribute.Name]) {
			continue
		}

		patch[attribute.JSONName] = backendServerTerraformValueToJSON(attribute.Kind, planned)
	}

	return patch
}

func findHaproxyBackendServerByName(ctx context.Context, client *pfsense.Client, parentID string, backendName string, name string) (haproxyBackendServerModel, string, bool, error) {
	return lookupHaproxyBackendServerByName(ctx, client, parentID, backendName, name, true)
}

func lookupHaproxyBackendServerByName(ctx context.Context, client *pfsense.Client, parentID string, backendName string, name string, requireAPIID bool) (haproxyBackendServerModel, string, bool, error) {
	var raw any
	if err := client.Get(ctx, haproxyBackendServersQueryPath(parentID, name), &raw); err != nil {
		return haproxyBackendServerModel{}, "", false, err
	}

	payloads, err := haproxyBackendServerPayloadList(raw)
	if err != nil {
		return haproxyBackendServerModel{}, "", false, err
	}

	var matched map[string]any
	for _, payload := range payloads {
		candidateName, err := apiRequiredStringWithLabel(payload, "HAProxy backend server", "name")
		if err != nil {
			return haproxyBackendServerModel{}, "", false, err
		}
		if candidateName != name {
			continue
		}
		if matched != nil {
			return haproxyBackendServerModel{}, "", false, fmt.Errorf("multiple HAProxy backend servers named %q were returned under backend %q; server names must be unique within a backend for Terraform natural-key management", name, backendName)
		}
		matched = payload
	}

	if matched == nil {
		return haproxyBackendServerModel{}, "", false, nil
	}

	apiID := ""
	if requireAPIID {
		var err error
		apiID, err = apiRequiredScalarStringWithLabel(matched, "HAProxy backend server", "id")
		if err != nil {
			return haproxyBackendServerModel{}, "", false, fmt.Errorf("%w; confirm UAT returns child object IDs from GET %s before using update/delete", err, haproxyBackendServersPath)
		}
	}
	model, err := haproxyBackendServerModelFromAPI(matched, backendName)
	if err != nil {
		return haproxyBackendServerModel{}, "", false, err
	}

	return model, apiID, true, nil
}

func haproxyBackendServerPayloadList(raw any) ([]map[string]any, error) {
	if raw == nil {
		return nil, nil
	}

	switch typed := raw.(type) {
	case []any:
		payloads := make([]map[string]any, 0, len(typed))
		for index, item := range typed {
			payload, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("HAProxy backend servers response item %d has unsupported type %T", index, item)
			}
			payloads = append(payloads, payload)
		}
		return payloads, nil
	case []map[string]any:
		return typed, nil
	case map[string]any:
		return []map[string]any{typed}, nil
	default:
		return nil, fmt.Errorf("HAProxy backend servers response has unsupported type %T; confirm the live UAT /services/haproxy/backend/servers schema", raw)
	}
}

func haproxyBackendServerModelFromAPI(payload map[string]any, backendName string) (haproxyBackendServerModel, error) {
	server := nullHaproxyBackendServerModel()

	name, err := apiRequiredStringWithLabel(payload, "HAProxy backend server", "name")
	if err != nil {
		return server, err
	}
	address, err := apiRequiredStringWithLabel(payload, "HAProxy backend server", "address")
	if err != nil {
		return server, err
	}
	port, err := apiRequiredInt64WithLabel(payload, "HAProxy backend server", "port")
	if err != nil {
		return server, err
	}

	server.ID = types.StringValue(haproxyBackendServerTerraformID(backendName, name))
	server.BackendName = types.StringValue(backendName)
	server.Name = types.StringValue(name)
	server.Address = types.StringValue(address)
	server.Port = types.Int64Value(port)

	if server.Status, err = apiStringWithLabel(payload, "HAProxy backend server", "status"); err != nil {
		return server, err
	}
	if server.Weight, err = apiInt64WithLabel(payload, "HAProxy backend server", "weight"); err != nil {
		return server, err
	}
	if server.SSL, err = apiBoolWithLabel(payload, "HAProxy backend server", "ssl"); err != nil {
		return server, err
	}
	if server.SSLServerVerify, err = apiBoolWithLabel(payload, "HAProxy backend server", "sslserververify"); err != nil {
		return server, err
	}
	if server.ServerID, err = apiInt64WithLabel(payload, "HAProxy backend server", "serverid"); err != nil {
		return server, err
	}

	return server, nil
}

func apiRequiredInt64WithLabel(payload map[string]any, label string, names ...string) (int64, error) {
	value, err := apiInt64WithLabel(payload, label, names...)
	if err != nil {
		return 0, err
	}
	if value.IsNull() || value.IsUnknown() {
		return 0, fmt.Errorf("%s response did not include required integer field %q", label, names[0])
	}

	return value.ValueInt64(), nil
}

func nullHaproxyBackendServerModel() haproxyBackendServerModel {
	return haproxyBackendServerModel{
		ID:              types.StringNull(),
		BackendName:     types.StringNull(),
		Name:            types.StringNull(),
		Address:         types.StringNull(),
		Port:            types.Int64Null(),
		Status:          types.StringNull(),
		Weight:          types.Int64Null(),
		SSL:             types.BoolNull(),
		SSLServerVerify: types.BoolNull(),
		ServerID:        types.Int64Null(),
	}
}

func (m haproxyBackendServerModel) attrValues() map[string]attr.Value {
	return map[string]attr.Value{
		"address":         m.Address,
		"port":            m.Port,
		"status":          m.Status,
		"weight":          m.Weight,
		"ssl":             m.SSL,
		"sslserververify": m.SSLServerVerify,
	}
}

func backendServerTerraformValueToJSON(kind backendServerAttributeKind, value attr.Value) any {
	if value.IsNull() {
		return nil
	}

	switch kind {
	case backendServerAttributeBool:
		return value.(types.Bool).ValueBool()
	case backendServerAttributeInt64:
		return value.(types.Int64).ValueInt64()
	case backendServerAttributeString:
		return value.(types.String).ValueString()
	default:
		return nil
	}
}

func haproxyBackendServersQueryPath(parentID string, name string) string {
	values := url.Values{}
	values.Set("parent_id", parentID)
	values.Set("name", name)
	return haproxyBackendServersPath + "?" + values.Encode()
}

func haproxyBackendServerDeletePath(parentID string, serverID string) string {
	values := url.Values{}
	values.Set("parent_id", parentID)
	values.Set("id", serverID)
	return haproxyBackendServerPath + "?" + values.Encode()
}

func haproxyBackendServerTerraformID(backendName string, name string) string {
	return backendName + "/" + name
}

func backendServerLookupErrorDetail(backendName string, name string, err error) string {
	return fmt.Sprintf("%s. Confirm GET %s is available on UAT, accepts parent_id/name query filters, returns server objects with stable name fields, and includes the transient pfSense child object id required for update/delete. Backend name: %q. Server name: %q.", err.Error(), haproxyBackendServersPath, backendName, name)
}

func backendServerDataSourceLookupErrorDetail(backendName string, name string, err error) string {
	return fmt.Sprintf("%s. Confirm GET %s is available on UAT, accepts parent_id/name query filters, and returns server objects with stable name fields. Backend name: %q. Server name: %q.", err.Error(), haproxyBackendServersPath, backendName, name)
}

func backendServerParentLookupErrorDetail(backendName string, name string, err error) string {
	return fmt.Sprintf("%s. Confirm GET %s is available on UAT, returns one backend object with a stable name field, and includes the transient pfSense backend object id required to query child servers. Backend name: %q. Server name: %q.", err.Error(), haproxyBackendsPath, backendName, name)
}
