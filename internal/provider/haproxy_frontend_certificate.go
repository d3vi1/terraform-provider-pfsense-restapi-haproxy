package provider

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"unicode"

	"github.com/d3vi1/terraform-provider-pfsense-restapi-haproxy/internal/pfsense"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const (
	haproxyFrontendCertificatePath  = "/services/haproxy/frontend/certificate"
	haproxyFrontendCertificatesPath = "/services/haproxy/frontend/certificates"
)

var (
	_ resource.Resource                = (*haproxyFrontendCertificateResource)(nil)
	_ resource.ResourceWithConfigure   = (*haproxyFrontendCertificateResource)(nil)
	_ resource.ResourceWithImportState = (*haproxyFrontendCertificateResource)(nil)
)

type haproxyFrontendCertificateResource struct {
	client *pfsense.Client
}

type haproxyFrontendCertificateModel struct {
	ID             types.String `tfsdk:"id"`
	FrontendName   types.String `tfsdk:"frontend_name"`
	SSLCertificate types.String `tfsdk:"ssl_certificate"`
}

func newHaproxyFrontendCertificateResource() resource.Resource {
	return &haproxyFrontendCertificateResource{}
}

func (r *haproxyFrontendCertificateResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "pfsense_haproxy_frontend_certificate"
}

func (r *haproxyFrontendCertificateResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = resourceschema.Schema{
		Description: "Attaches an existing pfSense certificate reference to a pfsense_haproxy_frontend. Terraform stores only the frontend name and certificate reference as the stable ID and resolves pfSense's current frontend/certificate object IDs before writes because pfSense object IDs may not be durable.",
		Attributes:  haproxyFrontendCertificateResourceSchemaAttributes(),
	}
}

func (r *haproxyFrontendCertificateResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *haproxyFrontendCertificateResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing pfSense client", "The provider was not configured before creating pfsense_haproxy_frontend_certificate.")
		return
	}

	var plan haproxyFrontendCertificateModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	keys, err := validateHaproxyFrontendCertificatePlan(plan)
	if err != nil {
		resp.Diagnostics.AddError("Invalid HAProxy frontend certificate", err.Error())
		return
	}

	_, parentID, found, err := findHaproxyFrontendByName(ctx, r.client, keys.frontendName)
	if err != nil {
		resp.Diagnostics.AddError("Find parent HAProxy frontend before create failed", frontendCertificateParentLookupErrorDetail(keys.frontendName, err))
		return
	}
	if !found {
		resp.Diagnostics.AddError("Create HAProxy frontend certificate failed", fmt.Sprintf("Parent frontend %q was not found on pfSense. Create or import pfsense_haproxy_frontend.%s before managing child certificate attachments.", keys.frontendName, keys.frontendName))
		return
	}

	_, _, found, err = findHaproxyFrontendCertificate(ctx, r.client, parentID, keys)
	if err != nil {
		resp.Diagnostics.AddError("Check existing HAProxy frontend certificate failed", frontendCertificateLookupErrorDetail(keys, err))
		return
	}
	if found {
		resp.Diagnostics.AddError(
			"HAProxy frontend certificate already exists",
			fmt.Sprintf("A pfSense HAProxy frontend certificate attachment for %q already exists under frontend %q. Import it with `terraform import pfsense_haproxy_frontend_certificate.<name> %s` before managing it.", keys.sslCertificate, keys.frontendName, haproxyFrontendCertificateTerraformID(keys)),
		)
		return
	}

	if err := r.client.Post(ctx, haproxyFrontendCertificatePath, buildHaproxyFrontendCertificateCreatePayload(parentID, keys), nil); err != nil {
		resp.Diagnostics.AddError("Create HAProxy frontend certificate failed", err.Error())
		return
	}

	certificate, _, found, err := findHaproxyFrontendCertificate(ctx, r.client, parentID, keys)
	if err != nil {
		resp.Diagnostics.AddError("Read HAProxy frontend certificate after create failed", frontendCertificateLookupErrorDetail(keys, err))
		return
	}
	if !found {
		resp.Diagnostics.AddError(
			"Read HAProxy frontend certificate after create failed",
			fmt.Sprintf("Created certificate attachment %q under frontend %q but GET %s did not return it. Confirm the live UAT child response shape and natural-key filtering before relying on this resource.", keys.sslCertificate, keys.frontendName, haproxyFrontendCertificatesPath),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, certificate)...)
}

func (r *haproxyFrontendCertificateResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing pfSense client", "The provider was not configured before reading pfsense_haproxy_frontend_certificate.")
		return
	}

	var state haproxyFrontendCertificateModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	keys, err := haproxyFrontendCertificateStateKeys(state)
	if err != nil {
		resp.Diagnostics.AddError("Invalid HAProxy frontend certificate state", err.Error())
		return
	}

	_, parentID, found, err := findHaproxyFrontendByName(ctx, r.client, keys.frontendName)
	if err != nil {
		resp.Diagnostics.AddError("Find parent HAProxy frontend failed", frontendCertificateParentLookupErrorDetail(keys.frontendName, err))
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	certificate, _, found, err := findHaproxyFrontendCertificate(ctx, r.client, parentID, keys)
	if err != nil {
		resp.Diagnostics.AddError("Read HAProxy frontend certificate failed", frontendCertificateLookupErrorDetail(keys, err))
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, certificate)...)
}

func (r *haproxyFrontendCertificateResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError(
		"Updating HAProxy frontend certificates is not supported",
		"frontend_name and ssl_certificate form the Terraform natural key and both require replacement. Create a new pfsense_haproxy_frontend_certificate resource and delete the old one instead of patching certificate attachments.",
	)
}

func (r *haproxyFrontendCertificateResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing pfSense client", "The provider was not configured before deleting pfsense_haproxy_frontend_certificate.")
		return
	}

	var state haproxyFrontendCertificateModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	keys, err := haproxyFrontendCertificateStateKeys(state)
	if err != nil {
		resp.Diagnostics.AddError("Invalid HAProxy frontend certificate state", err.Error())
		return
	}

	_, parentID, found, err := findHaproxyFrontendByName(ctx, r.client, keys.frontendName)
	if err != nil {
		resp.Diagnostics.AddError("Find parent HAProxy frontend before delete failed", frontendCertificateParentLookupErrorDetail(keys.frontendName, err))
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	_, certificateID, found, err := findHaproxyFrontendCertificate(ctx, r.client, parentID, keys)
	if err != nil {
		resp.Diagnostics.AddError("Find HAProxy frontend certificate before delete failed", frontendCertificateLookupErrorDetail(keys, err))
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	if err := r.client.Delete(ctx, haproxyFrontendCertificateDeletePath(parentID, certificateID), nil); err != nil {
		resp.Diagnostics.AddError("Delete HAProxy frontend certificate failed", err.Error())
		return
	}

	resp.State.RemoveResource(ctx)
}

func (r *haproxyFrontendCertificateResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	keys, err := parseHaproxyFrontendCertificateImportID(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Invalid HAProxy frontend certificate import ID", err.Error())
		return
	}

	model := nullHaproxyFrontendCertificateModel()
	model.ID = types.StringValue(haproxyFrontendCertificateTerraformID(keys))
	model.FrontendName = types.StringValue(keys.frontendName)
	model.SSLCertificate = types.StringValue(keys.sslCertificate)
	resp.Diagnostics.Append(resp.State.Set(ctx, model)...)
}

func haproxyFrontendCertificateResourceSchemaAttributes() map[string]resourceschema.Attribute {
	return map[string]resourceschema.Attribute{
		"id": resourceschema.StringAttribute{
			Computed:    true,
			Description: "Stable Terraform ID for the frontend certificate attachment in frontend_name/ssl_certificate form. This is not the pfSense child object ID.",
		},
		"frontend_name": resourceschema.StringAttribute{
			Required:    true,
			Description: "Name of the parent pfsense_haproxy_frontend. Terraform resolves the current pfSense frontend object ID by this natural key before every frontend certificate write.",
			PlanModifiers: []planmodifier.String{
				stringplanmodifier.RequiresReplace(),
			},
		},
		"ssl_certificate": resourceschema.StringAttribute{
			Required:    true,
			Description: "Existing pfSense certificate reference to attach to the frontend. This must be a reference/name only, not PEM certificate or private key material. Terraform treats this as part of the natural key and requires replacement if it changes.",
			PlanModifiers: []planmodifier.String{
				stringplanmodifier.RequiresReplace(),
			},
		},
	}
}

type haproxyFrontendCertificateKeys struct {
	frontendName   string
	sslCertificate string
}

func validateHaproxyFrontendCertificatePlan(model haproxyFrontendCertificateModel) (haproxyFrontendCertificateKeys, error) {
	frontendName, err := haproxyFrontendCertificateFrontendName(model.FrontendName)
	if err != nil {
		return haproxyFrontendCertificateKeys{}, err
	}
	sslCertificate, err := haproxyFrontendCertificateSSLCertificate(model.SSLCertificate)
	if err != nil {
		return haproxyFrontendCertificateKeys{}, err
	}

	return haproxyFrontendCertificateKeys{frontendName: frontendName, sslCertificate: sslCertificate}, nil
}

func haproxyFrontendCertificateFrontendName(value types.String) (string, error) {
	name, err := haproxyFrontendName(value)
	if err != nil {
		return "", fmt.Errorf("frontend_name %s", err.Error())
	}

	return name, nil
}

func haproxyFrontendCertificateSSLCertificate(value types.String) (string, error) {
	if value.IsNull() || value.IsUnknown() {
		return "", fmt.Errorf("ssl_certificate is required")
	}

	sslCertificate := value.ValueString()
	if strings.TrimSpace(sslCertificate) == "" {
		return "", fmt.Errorf("ssl_certificate must not be empty")
	}
	if strings.TrimSpace(sslCertificate) != sslCertificate {
		return "", fmt.Errorf("ssl_certificate must not contain leading or trailing whitespace")
	}
	if strings.IndexFunc(sslCertificate, unicode.IsSpace) >= 0 {
		return "", fmt.Errorf("ssl_certificate must not contain whitespace")
	}
	if strings.Contains(sslCertificate, "/") {
		return "", fmt.Errorf("ssl_certificate must not contain /")
	}

	upper := strings.ToUpper(sslCertificate)
	if strings.Contains(upper, "-----BEGIN") ||
		strings.Contains(upper, "-----END") ||
		strings.Contains(upper, "BEGIN CERTIFICATE") ||
		strings.Contains(upper, "END CERTIFICATE") ||
		strings.Contains(upper, "PRIVATE KEY") {
		return "", fmt.Errorf("ssl_certificate must be an existing pfSense certificate reference, not certificate or private key material")
	}

	return sslCertificate, nil
}

func haproxyFrontendCertificateStateKeys(model haproxyFrontendCertificateModel) (haproxyFrontendCertificateKeys, error) {
	if !model.FrontendName.IsNull() && !model.FrontendName.IsUnknown() &&
		!model.SSLCertificate.IsNull() && !model.SSLCertificate.IsUnknown() {
		return validateHaproxyFrontendCertificatePlan(model)
	}
	if !model.ID.IsNull() && !model.ID.IsUnknown() {
		return parseHaproxyFrontendCertificateImportID(model.ID.ValueString())
	}

	return haproxyFrontendCertificateKeys{}, fmt.Errorf("state is missing frontend_name and ssl_certificate")
}

func parseHaproxyFrontendCertificateImportID(id string) (haproxyFrontendCertificateKeys, error) {
	trimmed := strings.TrimSpace(id)
	parts := strings.Split(trimmed, "/")
	if len(parts) != 2 {
		return haproxyFrontendCertificateKeys{}, fmt.Errorf("import pfsense_haproxy_frontend_certificate with ID frontend_name/ssl_certificate")
	}
	for _, part := range parts {
		if part == "" {
			return haproxyFrontendCertificateKeys{}, fmt.Errorf("import pfsense_haproxy_frontend_certificate with ID frontend_name/ssl_certificate")
		}
	}

	frontendName, err := haproxyFrontendCertificateFrontendName(types.StringValue(parts[0]))
	if err != nil {
		return haproxyFrontendCertificateKeys{}, err
	}
	sslCertificate, err := haproxyFrontendCertificateSSLCertificate(types.StringValue(parts[1]))
	if err != nil {
		return haproxyFrontendCertificateKeys{}, err
	}

	return haproxyFrontendCertificateKeys{frontendName: frontendName, sslCertificate: sslCertificate}, nil
}

func buildHaproxyFrontendCertificateCreatePayload(parentID string, keys haproxyFrontendCertificateKeys) map[string]any {
	return map[string]any{
		"parent_id":       parentID,
		"ssl_certificate": keys.sslCertificate,
	}
}

func findHaproxyFrontendCertificate(ctx context.Context, client *pfsense.Client, parentID string, keys haproxyFrontendCertificateKeys) (haproxyFrontendCertificateModel, string, bool, error) {
	var raw any
	if err := client.Get(ctx, haproxyFrontendCertificatesQueryPath(parentID, keys), &raw); err != nil {
		return haproxyFrontendCertificateModel{}, "", false, err
	}

	payloads, err := haproxyFrontendCertificatePayloadList(raw)
	if err != nil {
		return haproxyFrontendCertificateModel{}, "", false, err
	}

	var matched map[string]any
	for _, payload := range payloads {
		payloadMatches, err := haproxyFrontendCertificatePayloadMatches(payload, parentID, keys)
		if err != nil {
			return haproxyFrontendCertificateModel{}, "", false, err
		}
		if !payloadMatches {
			continue
		}
		if matched != nil {
			return haproxyFrontendCertificateModel{}, "", false, fmt.Errorf("multiple HAProxy frontend certificate attachments for %q were returned under frontend %q; certificate references must be unique within a frontend for Terraform management", keys.sslCertificate, keys.frontendName)
		}
		matched = payload
	}

	if matched == nil {
		return haproxyFrontendCertificateModel{}, "", false, nil
	}

	apiID, err := apiRequiredScalarStringWithLabel(matched, "HAProxy frontend certificate", "id")
	if err != nil {
		return haproxyFrontendCertificateModel{}, "", false, fmt.Errorf("%w; confirm UAT returns child object IDs from GET %s before using delete", err, haproxyFrontendCertificatesPath)
	}
	model, err := haproxyFrontendCertificateModelFromAPI(matched, keys.frontendName)
	if err != nil {
		return haproxyFrontendCertificateModel{}, "", false, err
	}

	return model, apiID, true, nil
}

func haproxyFrontendCertificatePayloadMatches(payload map[string]any, parentID string, keys haproxyFrontendCertificateKeys) (bool, error) {
	if value, name, ok := apiValue(payload, "parent_id"); ok && value != nil {
		payloadParentID, err := scalarStringValue("HAProxy frontend certificate", name, value)
		if err != nil {
			return false, err
		}
		if payloadParentID != parentID {
			return false, nil
		}
	}

	sslCertificateValue, err := apiRequiredStringWithLabel(payload, "HAProxy frontend certificate", "ssl_certificate")
	if err != nil {
		return false, err
	}
	sslCertificate, err := haproxyFrontendCertificateSSLCertificate(types.StringValue(sslCertificateValue))
	if err != nil {
		return false, fmt.Errorf("HAProxy frontend certificate ssl_certificate %s", err.Error())
	}

	return sslCertificate == keys.sslCertificate, nil
}

func haproxyFrontendCertificatePayloadList(raw any) ([]map[string]any, error) {
	if raw == nil {
		return nil, nil
	}

	switch typed := raw.(type) {
	case []any:
		payloads := make([]map[string]any, 0, len(typed))
		for index, item := range typed {
			payload, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("HAProxy frontend certificates response item %d has unsupported type %T", index, item)
			}
			payloads = append(payloads, payload)
		}
		return payloads, nil
	case []map[string]any:
		return typed, nil
	case map[string]any:
		return []map[string]any{typed}, nil
	default:
		return nil, fmt.Errorf("HAProxy frontend certificates response has unsupported type %T; confirm the live UAT /services/haproxy/frontend/certificates schema", raw)
	}
}

func haproxyFrontendCertificateModelFromAPI(payload map[string]any, frontendName string) (haproxyFrontendCertificateModel, error) {
	certificate := nullHaproxyFrontendCertificateModel()

	sslCertificateValue, err := apiRequiredStringWithLabel(payload, "HAProxy frontend certificate", "ssl_certificate")
	if err != nil {
		return certificate, err
	}
	sslCertificate, err := haproxyFrontendCertificateSSLCertificate(types.StringValue(sslCertificateValue))
	if err != nil {
		return certificate, fmt.Errorf("HAProxy frontend certificate ssl_certificate %s", err.Error())
	}

	keys := haproxyFrontendCertificateKeys{frontendName: frontendName, sslCertificate: sslCertificate}
	certificate.ID = types.StringValue(haproxyFrontendCertificateTerraformID(keys))
	certificate.FrontendName = types.StringValue(frontendName)
	certificate.SSLCertificate = types.StringValue(sslCertificate)

	return certificate, nil
}

func nullHaproxyFrontendCertificateModel() haproxyFrontendCertificateModel {
	return haproxyFrontendCertificateModel{
		ID:             types.StringNull(),
		FrontendName:   types.StringNull(),
		SSLCertificate: types.StringNull(),
	}
}

func haproxyFrontendCertificatesQueryPath(parentID string, keys haproxyFrontendCertificateKeys) string {
	values := url.Values{}
	values.Set("parent_id", parentID)
	values.Set("ssl_certificate", keys.sslCertificate)
	return haproxyFrontendCertificatesPath + "?" + values.Encode()
}

func haproxyFrontendCertificateDeletePath(parentID string, certificateID string) string {
	values := url.Values{}
	values.Set("parent_id", parentID)
	values.Set("id", certificateID)
	return haproxyFrontendCertificatePath + "?" + values.Encode()
}

func haproxyFrontendCertificateTerraformID(keys haproxyFrontendCertificateKeys) string {
	return keys.frontendName + "/" + keys.sslCertificate
}

func frontendCertificateLookupErrorDetail(keys haproxyFrontendCertificateKeys, err error) string {
	return fmt.Sprintf("%s. Confirm GET %s is available on UAT, accepts parent_id and ssl_certificate query filters, returns frontend certificate attachment objects with stable ssl_certificate fields, and includes the transient pfSense child object id required for delete. Frontend name: %q. Certificate reference: %q.", err.Error(), haproxyFrontendCertificatesPath, keys.frontendName, keys.sslCertificate)
}

func frontendCertificateParentLookupErrorDetail(frontendName string, err error) string {
	return fmt.Sprintf("%s. Confirm GET %s is available on UAT, returns one frontend object with a stable name field, and includes the transient pfSense frontend object id required to query child certificate attachments. Frontend name: %q.", err.Error(), haproxyFrontendsPath, frontendName)
}
