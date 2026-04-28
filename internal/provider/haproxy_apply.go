package provider

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/d3vi1/terraform-provider-pfsense-restapi-haproxy/internal/pfsense"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	datasourceschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

const (
	haproxyApplyID                  = "apply"
	haproxyApplyPath                = "/services/haproxy/apply"
	defaultHaproxyApplyTimeout      = 2 * time.Minute
	defaultHaproxyApplyPollInterval = 2 * time.Second
)

var (
	_ datasource.DataSource              = (*haproxyApplyDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*haproxyApplyDataSource)(nil)
	_ resource.Resource                  = (*haproxyApplyResource)(nil)
	_ resource.ResourceWithConfigure     = (*haproxyApplyResource)(nil)
	_ resource.ResourceWithImportState   = (*haproxyApplyResource)(nil)
)

type haproxyApplyDataSource struct {
	client *pfsense.Client
}

type haproxyApplyResource struct {
	client *pfsense.Client
}

type haproxyApplyStatusModel struct {
	ID           types.String `tfsdk:"id"`
	Applied      types.Bool   `tfsdk:"applied"`
	Pending      types.Bool   `tfsdk:"pending"`
	Status       types.String `tfsdk:"status"`
	StatusDetail types.String `tfsdk:"status_detail"`
}

type haproxyApplyResourceModel struct {
	ID           types.String `tfsdk:"id"`
	Triggers     types.Map    `tfsdk:"triggers"`
	Timeout      types.String `tfsdk:"timeout"`
	PollInterval types.String `tfsdk:"poll_interval"`
	Applied      types.Bool   `tfsdk:"applied"`
	Pending      types.Bool   `tfsdk:"pending"`
	Status       types.String `tfsdk:"status"`
	StatusDetail types.String `tfsdk:"status_detail"`
}

type haproxyApplyStatus struct {
	Applied bool
}

type haproxyApplyPendingError struct {
	Timeout  time.Duration
	Attempts int
}

func (e *haproxyApplyPendingError) Error() string {
	return fmt.Sprintf(
		"HAProxy apply remained pending after %s (%d status checks); pfSense still reports applied=false, which usually means /var/run/haproxy.conf.dirty exists. Inspect HAProxy configuration validation output and pfSense REST API or HAProxy logs, then retry pfsense_haproxy_apply.",
		e.Timeout,
		e.Attempts,
	)
}

func newHaproxyApplyDataSource() datasource.DataSource {
	return &haproxyApplyDataSource{}
}

func newHaproxyApplyResource() resource.Resource {
	return &haproxyApplyResource{}
}

func (d *haproxyApplyDataSource) Metadata(_ context.Context, _ datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "pfsense_haproxy_apply"
}

func (d *haproxyApplyDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = datasourceschema.Schema{
		Description: "Reads the current pfSense HAProxy apply status from GET /services/haproxy/apply without triggering an apply.",
		Attributes:  haproxyApplyStatusDataSourceSchemaAttributes(),
	}
}

func (d *haproxyApplyDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *haproxyApplyDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		resp.Diagnostics.AddError("Missing pfSense client", "The provider was not configured before reading pfsense_haproxy_apply.")
		return
	}

	status, err := readHaproxyApplyStatus(ctx, d.client)
	if err != nil {
		resp.Diagnostics.AddError("Read HAProxy apply status failed", applyStatusErrorDetail(err))
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, haproxyApplyStatusModelFromStatus(status))...)
}

func (r *haproxyApplyResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "pfsense_haproxy_apply"
}

func (r *haproxyApplyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = resourceschema.Schema{
		Description: "Explicitly applies pending pfSense HAProxy package changes with POST /services/haproxy/apply and bounded polling against GET /services/haproxy/apply. Other HAProxy resources do not trigger hidden apply/reload operations.",
		Attributes:  haproxyApplyResourceSchemaAttributes(),
	}
}

func (r *haproxyApplyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *haproxyApplyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing pfSense client", "The provider was not configured before creating pfsense_haproxy_apply.")
		return
	}

	var plan haproxyApplyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !validHaproxyApplyID(plan.ID, &resp.Diagnostics) {
		return
	}

	timeout, pollInterval, diags := haproxyApplyPollingConfig(plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	status, err := applyAndWaitForHaproxy(ctx, r.client, timeout, pollInterval)
	if err != nil {
		resp.Diagnostics.AddError("Apply HAProxy changes failed", applyOperationErrorDetail(err))
		return
	}

	tflog.Info(ctx, "HAProxy apply completed", map[string]any{"status": "done"})
	resp.Diagnostics.Append(resp.State.Set(ctx, haproxyApplyResourceModelFromStatus(plan, status, timeout, pollInterval))...)
}

func (r *haproxyApplyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing pfSense client", "The provider was not configured before reading pfsense_haproxy_apply.")
		return
	}

	var state haproxyApplyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !validHaproxyApplyID(state.ID, &resp.Diagnostics) {
		return
	}

	timeout, pollInterval, diags := haproxyApplyPollingConfig(state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	status, err := readHaproxyApplyStatus(ctx, r.client)
	if err != nil {
		resp.Diagnostics.AddError("Read HAProxy apply status failed", applyStatusErrorDetail(err))
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, haproxyApplyResourceModelFromStatus(state, status, timeout, pollInterval))...)
}

func (r *haproxyApplyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing pfSense client", "The provider was not configured before updating pfsense_haproxy_apply.")
		return
	}

	var plan, prior haproxyApplyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !validHaproxyApplyID(plan.ID, &resp.Diagnostics) || !validHaproxyApplyID(prior.ID, &resp.Diagnostics) {
		return
	}

	timeout, pollInterval, diags := haproxyApplyPollingConfig(plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	var status haproxyApplyStatus
	var err error
	if haproxyApplyTriggersChanged(plan.Triggers, prior.Triggers) {
		status, err = applyAndWaitForHaproxy(ctx, r.client, timeout, pollInterval)
		if err != nil {
			resp.Diagnostics.AddError("Apply HAProxy changes failed", applyOperationErrorDetail(err))
			return
		}
		tflog.Info(ctx, "HAProxy apply completed", map[string]any{"status": "done"})
	} else {
		status, err = readHaproxyApplyStatus(ctx, r.client)
		if err != nil {
			resp.Diagnostics.AddError("Read HAProxy apply status failed", applyStatusErrorDetail(err))
			return
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, haproxyApplyResourceModelFromStatus(plan, status, timeout, pollInterval))...)
}

func (r *haproxyApplyResource) Delete(ctx context.Context, _ resource.DeleteRequest, resp *resource.DeleteResponse) {
	resp.State.RemoveResource(ctx)
}

func (r *haproxyApplyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if req.ID != haproxyApplyID {
		resp.Diagnostics.AddError("Invalid HAProxy apply import ID", fmt.Sprintf("Import pfsense_haproxy_apply with the fixed ID %q.", haproxyApplyID))
		return
	}

	model := nullHaproxyApplyResourceModel()
	model.ID = types.StringValue(haproxyApplyID)
	resp.Diagnostics.Append(resp.State.Set(ctx, model)...)
}

func haproxyApplyStatusDataSourceSchemaAttributes() map[string]datasourceschema.Attribute {
	return map[string]datasourceschema.Attribute{
		"id": datasourceschema.StringAttribute{
			Computed:    true,
			Description: "Fixed singleton ID for the HAProxy apply endpoint.",
		},
		"applied": datasourceschema.BoolAttribute{
			Computed:    true,
			Description: "True when pfSense reports all HAProxy configuration changes have been applied.",
		},
		"pending": datasourceschema.BoolAttribute{
			Computed:    true,
			Description: "True when pfSense reports HAProxy configuration changes are still pending.",
		},
		"status": datasourceschema.StringAttribute{
			Computed:    true,
			Description: "Normalized apply status: done when applied is true, pending when applied is false.",
		},
		"status_detail": datasourceschema.StringAttribute{
			Computed:    true,
			Description: "Human-readable status detail with next-step guidance for pending or completed apply state.",
		},
	}
}

func haproxyApplyResourceSchemaAttributes() map[string]resourceschema.Attribute {
	return map[string]resourceschema.Attribute{
		"id": resourceschema.StringAttribute{
			Computed:    true,
			Description: "Fixed singleton ID for the HAProxy apply endpoint. Import must use \"apply\".",
		},
		"triggers": resourceschema.MapAttribute{
			Optional:    true,
			ElementType: types.StringType,
			Description: "User-controlled string values that cause a new POST /services/haproxy/apply during resource update when changed. Reference managed HAProxy resources here to make apply explicit and repeatable.",
		},
		"timeout": resourceschema.StringAttribute{
			Optional:    true,
			Computed:    true,
			Description: "Maximum time to poll GET /services/haproxy/apply after POST before failing while pending. Defaults to 2m.",
		},
		"poll_interval": resourceschema.StringAttribute{
			Optional:    true,
			Computed:    true,
			Description: "Interval between HAProxy apply status checks while pending. Defaults to 2s.",
		},
		"applied": resourceschema.BoolAttribute{
			Computed:    true,
			Description: "True when pfSense reports all HAProxy configuration changes have been applied.",
		},
		"pending": resourceschema.BoolAttribute{
			Computed:    true,
			Description: "True when pfSense reports HAProxy configuration changes are still pending.",
		},
		"status": resourceschema.StringAttribute{
			Computed:    true,
			Description: "Normalized apply status: done when applied is true, pending when applied is false.",
		},
		"status_detail": resourceschema.StringAttribute{
			Computed:    true,
			Description: "Human-readable status detail with next-step guidance for pending or completed apply state.",
		},
	}
}

func readHaproxyApplyStatus(ctx context.Context, client *pfsense.Client) (haproxyApplyStatus, error) {
	var payload map[string]any
	if err := client.Get(ctx, haproxyApplyPath, &payload); err != nil {
		return haproxyApplyStatus{}, err
	}

	return haproxyApplyStatusFromAPI(payload)
}

func applyAndWaitForHaproxy(ctx context.Context, client *pfsense.Client, timeout time.Duration, pollInterval time.Duration) (haproxyApplyStatus, error) {
	if err := client.Post(ctx, haproxyApplyPath, map[string]any{"async": true}, nil); err != nil {
		return haproxyApplyStatus{}, fmt.Errorf("start HAProxy apply with POST %s: %w", haproxyApplyPath, err)
	}

	return pollHaproxyApplyStatus(ctx, client, timeout, pollInterval)
}

func pollHaproxyApplyStatus(ctx context.Context, client *pfsense.Client, timeout time.Duration, pollInterval time.Duration) (haproxyApplyStatus, error) {
	deadline := time.Now().Add(timeout)
	attempts := 0

	for {
		status, err := readHaproxyApplyStatus(ctx, client)
		attempts++
		if err != nil {
			return haproxyApplyStatus{}, fmt.Errorf("read HAProxy apply status with GET %s: %w", haproxyApplyPath, err)
		}
		if status.Applied {
			return status, nil
		}
		if !time.Now().Before(deadline) {
			return status, &haproxyApplyPendingError{
				Timeout:  timeout,
				Attempts: attempts,
			}
		}

		sleepFor := pollInterval
		if remaining := time.Until(deadline); remaining < sleepFor {
			sleepFor = remaining
		}

		timer := time.NewTimer(sleepFor)
		select {
		case <-timer.C:
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return haproxyApplyStatus{}, fmt.Errorf("HAProxy apply polling canceled: %w", ctx.Err())
		}
	}
}

func haproxyApplyStatusFromAPI(payload map[string]any) (haproxyApplyStatus, error) {
	applied, err := apiBoolWithLabel(payload, "HAProxy apply", "applied")
	if err != nil {
		return haproxyApplyStatus{}, err
	}
	if applied.IsNull() || applied.IsUnknown() {
		return haproxyApplyStatus{}, fmt.Errorf("HAProxy apply response did not include required boolean field %q; confirm the live UAT /services/haproxy/apply schema before relying on apply automation", "applied")
	}

	return haproxyApplyStatus{Applied: applied.ValueBool()}, nil
}

func haproxyApplyStatusModelFromStatus(status haproxyApplyStatus) haproxyApplyStatusModel {
	statusText, detail := haproxyApplyStatusText(status)

	return haproxyApplyStatusModel{
		ID:           types.StringValue(haproxyApplyID),
		Applied:      types.BoolValue(status.Applied),
		Pending:      types.BoolValue(!status.Applied),
		Status:       types.StringValue(statusText),
		StatusDetail: types.StringValue(detail),
	}
}

func haproxyApplyResourceModelFromStatus(config haproxyApplyResourceModel, status haproxyApplyStatus, timeout time.Duration, pollInterval time.Duration) haproxyApplyResourceModel {
	statusText, detail := haproxyApplyStatusText(status)

	return haproxyApplyResourceModel{
		ID:           types.StringValue(haproxyApplyID),
		Triggers:     normalizedHaproxyApplyTriggers(config.Triggers),
		Timeout:      normalizedDurationAttribute(config.Timeout, timeout),
		PollInterval: normalizedDurationAttribute(config.PollInterval, pollInterval),
		Applied:      types.BoolValue(status.Applied),
		Pending:      types.BoolValue(!status.Applied),
		Status:       types.StringValue(statusText),
		StatusDetail: types.StringValue(detail),
	}
}

func haproxyApplyStatusText(status haproxyApplyStatus) (string, string) {
	if status.Applied {
		return "done", "pfSense reports all HAProxy changes are applied (applied=true)."
	}

	return "pending", "pfSense reports HAProxy changes are pending (applied=false). Trigger pfsense_haproxy_apply or inspect HAProxy validation output if the status remains pending."
}

func nullHaproxyApplyResourceModel() haproxyApplyResourceModel {
	return haproxyApplyResourceModel{
		ID:           types.StringNull(),
		Triggers:     types.MapNull(types.StringType),
		Timeout:      types.StringNull(),
		PollInterval: types.StringNull(),
		Applied:      types.BoolNull(),
		Pending:      types.BoolNull(),
		Status:       types.StringNull(),
		StatusDetail: types.StringNull(),
	}
}

func haproxyApplyPollingConfig(model haproxyApplyResourceModel) (time.Duration, time.Duration, diag.Diagnostics) {
	var diags diag.Diagnostics

	timeout, d := durationAttribute(model.Timeout, "timeout", defaultHaproxyApplyTimeout)
	diags.Append(d...)
	pollInterval, d := durationAttribute(model.PollInterval, "poll_interval", defaultHaproxyApplyPollInterval)
	diags.Append(d...)
	if timeout > 0 && pollInterval > timeout {
		diags.AddError("Invalid HAProxy apply polling configuration", "poll_interval must be less than or equal to timeout.")
	}

	return timeout, pollInterval, diags
}

func durationAttribute(value types.String, attrName string, fallback time.Duration) (time.Duration, diag.Diagnostics) {
	var diags diag.Diagnostics
	if value.IsNull() || value.IsUnknown() {
		return fallback, diags
	}

	raw := value.ValueString()
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		diags.AddError("Invalid HAProxy apply duration", fmt.Sprintf("%s=%q is not a valid duration.", attrName, raw))
		return fallback, diags
	}
	if parsed <= 0 {
		diags.AddError("Invalid HAProxy apply duration", fmt.Sprintf("%s must be greater than zero.", attrName))
		return fallback, diags
	}

	return parsed, diags
}

func normalizedDurationAttribute(value types.String, parsed time.Duration) types.String {
	if value.IsNull() || value.IsUnknown() {
		return types.StringValue(parsed.String())
	}
	return value
}

func validHaproxyApplyID(id types.String, diags *diag.Diagnostics) bool {
	if id.IsNull() || id.IsUnknown() {
		return true
	}
	if id.ValueString() == haproxyApplyID {
		return true
	}

	diags.AddError("Invalid HAProxy apply ID", fmt.Sprintf("Expected ID %q, got %q.", haproxyApplyID, id.ValueString()))
	return false
}

func haproxyApplyTriggersChanged(plan types.Map, prior types.Map) bool {
	return !normalizedHaproxyApplyTriggers(plan).Equal(normalizedHaproxyApplyTriggers(prior))
}

func normalizedHaproxyApplyTriggers(value types.Map) types.Map {
	if value.IsNull() || value.IsUnknown() {
		return types.MapNull(types.StringType)
	}

	elements := make(map[string]attr.Value, len(value.Elements()))
	for key, element := range value.Elements() {
		elements[key] = element
	}

	return types.MapValueMust(types.StringType, elements)
}

func applyStatusErrorDetail(err error) string {
	return fmt.Sprintf("%s. Confirm GET %s is available on the UAT firewall, the HAProxy package is installed, and the REST API credential has HAProxy apply privileges.", err.Error(), haproxyApplyPath)
}

func applyOperationErrorDetail(err error) string {
	var pendingErr *haproxyApplyPendingError
	if errors.As(err, &pendingErr) {
		return fmt.Sprintf("%s. POST %s was sent, but GET %s did not report applied=true before timeout. Inspect HAProxy configuration validation output and check pfSense REST API or HAProxy logs before retrying.", err.Error(), haproxyApplyPath, haproxyApplyPath)
	}

	return fmt.Sprintf("%s. Confirm POST %s is available on the UAT firewall, inspect HAProxy configuration validation output, and check pfSense REST API or HAProxy logs before retrying.", err.Error(), haproxyApplyPath)
}
