package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/d3vi1/terraform-provider-pfsense-restapi-haproxy/internal/pfsense"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	datasourceschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func TestProviderRegistersHaproxySettings(t *testing.T) {
	provider := &haproxyProvider{}

	resources := provider.Resources(context.Background())
	if !resourceTypeRegistered(resources, "pfsense_haproxy_apply") {
		t.Fatalf("pfsense_haproxy_apply resource was not registered")
	}
	if !resourceTypeRegistered(resources, "pfsense_haproxy_backend") {
		t.Fatalf("pfsense_haproxy_backend resource was not registered")
	}
	if !resourceTypeRegistered(resources, "pfsense_haproxy_backend_server") {
		t.Fatalf("pfsense_haproxy_backend_server resource was not registered")
	}
	if !resourceTypeRegistered(resources, "pfsense_haproxy_settings") {
		t.Fatalf("pfsense_haproxy_settings resource was not registered")
	}

	dataSources := provider.DataSources(context.Background())
	if !dataSourceTypeRegistered(dataSources, "pfsense_haproxy_apply") {
		t.Fatalf("pfsense_haproxy_apply data source was not registered")
	}
	if !dataSourceTypeRegistered(dataSources, "pfsense_haproxy_backend") {
		t.Fatalf("pfsense_haproxy_backend data source was not registered")
	}
	if !dataSourceTypeRegistered(dataSources, "pfsense_haproxy_backend_server") {
		t.Fatalf("pfsense_haproxy_backend_server data source was not registered")
	}
	if !dataSourceTypeRegistered(dataSources, "pfsense_haproxy_settings") {
		t.Fatalf("pfsense_haproxy_settings data source was not registered")
	}
}

func TestHaproxySettingsDataSourceReadUsesGET(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)

		if r.Method != http.MethodGet {
			t.Fatalf("method = %s", r.Method)
		}
		if r.URL.RequestURI() != "/api/v2/services/haproxy/settings" {
			t.Fatalf("request uri = %s", r.URL.RequestURI())
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": 200,
			"status": "ok",
			"data": {
				"enable": true,
				"maxconn": 200,
				"nbthread": "2",
				"terminate_on_reload": "yes",
				"hard_stop_after": "15m",
				"localstatsport": "8404",
				"log-send-hostname": "fw-uat",
				"advanced": "global\n  tune.ssl.default-dh-param 2048\n",
				"dns_resolvers": [{"name": "ignored"}],
				"email_mailers": [{"name": "ignored"}]
			}
		}`))
	}))
	t.Cleanup(server.Close)

	settingsDataSource := &haproxySettingsDataSource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	resp := datasource.ReadResponse{
		State: tfsdk.State{Schema: haproxySettingsDataSourceSchema(t)},
	}

	settingsDataSource.Read(context.Background(), datasource.ReadRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %#v", resp.Diagnostics)
	}
	if requests.Load() != 1 {
		t.Fatalf("requests = %d", requests.Load())
	}

	var state haproxySettingsModel
	diags := resp.State.Get(context.Background(), &state)
	if diags.HasError() {
		t.Fatalf("state get diagnostics: %#v", diags)
	}
	if state.ID.ValueString() != haproxySettingsID {
		t.Fatalf("id = %q", state.ID.ValueString())
	}
	if !state.Enable.ValueBool() {
		t.Fatalf("enable not read")
	}
	if state.Maxconn.ValueInt64() != 200 {
		t.Fatalf("maxconn = %d", state.Maxconn.ValueInt64())
	}
	if state.Nbthread.ValueInt64() != 2 {
		t.Fatalf("nbthread = %d", state.Nbthread.ValueInt64())
	}
	if !state.TerminateOnReload.ValueBool() {
		t.Fatalf("terminate_on_reload not read")
	}
	if state.LocalStatsPort.ValueInt64() != 8404 {
		t.Fatalf("localstatsport = %d", state.LocalStatsPort.ValueInt64())
	}
	if state.LogSendHostname.ValueString() != "fw-uat" {
		t.Fatalf("log_send_hostname = %q", state.LogSendHostname.ValueString())
	}
	if state.Advanced.ValueString() != "global\n  tune.ssl.default-dh-param 2048\n" {
		t.Fatalf("advanced not read")
	}
}

func TestHaproxySettingsResourceImportRequiresFixedID(t *testing.T) {
	t.Parallel()

	settingsResource := &haproxySettingsResource{}
	schema := haproxySettingsResourceSchema(t)

	validResp := resource.ImportStateResponse{
		State: tfsdk.State{Schema: schema},
	}
	settingsResource.ImportState(context.Background(), resource.ImportStateRequest{ID: haproxySettingsID}, &validResp)
	if validResp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %#v", validResp.Diagnostics)
	}
	var state haproxySettingsModel
	diags := validResp.State.Get(context.Background(), &state)
	if diags.HasError() {
		t.Fatalf("state get diagnostics: %#v", diags)
	}
	if state.ID.ValueString() != haproxySettingsID {
		t.Fatalf("imported id = %q", state.ID.ValueString())
	}

	invalidResp := resource.ImportStateResponse{
		State: tfsdk.State{Schema: schema},
	}
	settingsResource.ImportState(context.Background(), resource.ImportStateRequest{ID: "haproxy"}, &invalidResp)
	if !invalidResp.Diagnostics.HasError() {
		t.Fatalf("expected diagnostic for non-fixed import id")
	}
}

func TestHaproxySettingsResourceCreateRequiresImportAndDoesNotWrite(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	settingsResource := &haproxySettingsResource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	var resp resource.CreateResponse

	settingsResource.Create(context.Background(), resource.CreateRequest{}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatalf("expected import-required diagnostic")
	}
	if requests.Load() != 0 {
		t.Fatalf("create made %d REST requests", requests.Load())
	}
}

func TestHaproxySettingsResourceUpdatePatchesChangedFieldsOnly(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var requests []string
	var patchPayload map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		mu.Unlock()

		if r.URL.RequestURI() != "/api/v2/services/haproxy/settings" {
			t.Fatalf("request uri = %s", r.URL.RequestURI())
		}

		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodPatch:
			if err := json.NewDecoder(r.Body).Decode(&patchPayload); err != nil {
				t.Fatalf("decode patch payload: %v", err)
			}
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":null}`))
		case http.MethodGet:
			_, _ = w.Write([]byte(`{
				"code": 200,
				"status": "ok",
				"data": {
					"enable": true,
					"maxconn": 250,
					"log-send-hostname": "fw-uat",
					"advanced": "unchanged advanced text"
				}
			}`))
		default:
			t.Fatalf("unexpected method %s", r.Method)
		}
	}))
	t.Cleanup(server.Close)

	settingsResource := &haproxySettingsResource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxySettingsResourceSchema(t)

	prior := nullHaproxySettingsModel()
	prior.ID = types.StringValue(haproxySettingsID)
	prior.Enable = types.BoolValue(false)
	prior.Maxconn = types.Int64Value(100)
	prior.LogSendHostname = types.StringValue("")
	prior.Advanced = types.StringValue("unchanged advanced text")

	plan := prior
	plan.Enable = types.BoolValue(true)
	plan.Maxconn = types.Int64Value(250)
	plan.LogSendHostname = types.StringValue("fw-uat")

	resp := resource.UpdateResponse{
		State: testResourceState(t, schema, plan),
	}
	settingsResource.Update(context.Background(), resource.UpdateRequest{
		Plan:  testResourcePlan(t, schema, plan),
		State: testResourceState(t, schema, prior),
	}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %#v", resp.Diagnostics)
	}

	mu.Lock()
	gotRequests := append([]string(nil), requests...)
	mu.Unlock()
	wantRequests := []string{
		"PATCH /api/v2/services/haproxy/settings",
		"GET /api/v2/services/haproxy/settings",
	}
	if !reflect.DeepEqual(gotRequests, wantRequests) {
		t.Fatalf("requests = %#v, want %#v", gotRequests, wantRequests)
	}

	if patchPayload["enable"] != true {
		t.Fatalf("enable patch = %#v", patchPayload["enable"])
	}
	if patchPayload["maxconn"] != float64(250) {
		t.Fatalf("maxconn patch = %#v", patchPayload["maxconn"])
	}
	if patchPayload["log_send_hostname"] != "fw-uat" {
		t.Fatalf("log_send_hostname patch = %#v", patchPayload["log_send_hostname"])
	}
	for _, forbidden := range []string{"advanced", "apply", "async", "dns_resolvers", "email_mailers", "log-send-hostname"} {
		if _, ok := patchPayload[forbidden]; ok {
			t.Fatalf("patch unexpectedly included %q: %#v", forbidden, patchPayload)
		}
	}

	var state haproxySettingsModel
	diags := resp.State.Get(context.Background(), &state)
	if diags.HasError() {
		t.Fatalf("state get diagnostics: %#v", diags)
	}
	if !state.Enable.ValueBool() || state.Maxconn.ValueInt64() != 250 || state.LogSendHostname.ValueString() != "fw-uat" {
		t.Fatalf("state not refreshed from GET: %#v", state)
	}
}

func TestHaproxySettingsResourceDeleteRemovesStateOnly(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	settingsResource := &haproxySettingsResource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxySettingsResourceSchema(t)
	stateModel := nullHaproxySettingsModel()
	stateModel.ID = types.StringValue(haproxySettingsID)

	resp := resource.DeleteResponse{
		State: testResourceState(t, schema, stateModel),
	}
	settingsResource.Delete(context.Background(), resource.DeleteRequest{
		State: testResourceState(t, schema, stateModel),
	}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %#v", resp.Diagnostics)
	}
	if requests.Load() != 0 {
		t.Fatalf("delete made %d REST requests", requests.Load())
	}
	if !resp.State.Raw.IsNull() {
		t.Fatalf("state was not removed")
	}
}

func TestHaproxySettingsSchemasAreConservative(t *testing.T) {
	resourceSchema := haproxySettingsResourceSchema(t)
	if _, ok := resourceSchema.Attributes["dns_resolvers"]; ok {
		t.Fatalf("resource schema should not manage nested dns_resolvers")
	}
	if _, ok := resourceSchema.Attributes["email_mailers"]; ok {
		t.Fatalf("resource schema should not manage nested email_mailers")
	}
	resourceAdvanced, ok := resourceSchema.Attributes["advanced"]
	if !ok {
		t.Fatalf("resource schema missing advanced")
	}
	if !resourceAdvanced.IsSensitive() {
		t.Fatalf("resource advanced attribute is not sensitive")
	}

	dataSourceSchema := haproxySettingsDataSourceSchema(t)
	dataSourceAdvanced, ok := dataSourceSchema.Attributes["advanced"]
	if !ok {
		t.Fatalf("data source schema missing advanced")
	}
	if !dataSourceAdvanced.IsSensitive() {
		t.Fatalf("data source advanced attribute is not sensitive")
	}
}

func haproxySettingsResourceSchema(t *testing.T) resourceschema.Schema {
	t.Helper()

	var resp resource.SchemaResponse
	(&haproxySettingsResource{}).Schema(context.Background(), resource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected resource schema diagnostics: %#v", resp.Diagnostics)
	}

	return resp.Schema
}

func haproxySettingsDataSourceSchema(t *testing.T) datasourceschema.Schema {
	t.Helper()

	var resp datasource.SchemaResponse
	(&haproxySettingsDataSource{}).Schema(context.Background(), datasource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected data source schema diagnostics: %#v", resp.Diagnostics)
	}

	return resp.Schema
}

func testResourcePlan(t *testing.T, schema resourceschema.Schema, model any) tfsdk.Plan {
	t.Helper()

	plan := tfsdk.Plan{Schema: schema}
	diags := plan.Set(context.Background(), model)
	if diags.HasError() {
		t.Fatalf("plan set diagnostics: %#v", diags)
	}

	return plan
}

func testResourceState(t *testing.T, schema resourceschema.Schema, model any) tfsdk.State {
	t.Helper()

	state := tfsdk.State{Schema: schema}
	diags := state.Set(context.Background(), model)
	if diags.HasError() {
		t.Fatalf("state set diagnostics: %#v", diags)
	}

	return state
}

func testDataSourceConfig(t *testing.T, schema datasourceschema.Schema, model any) tfsdk.Config {
	t.Helper()

	ctx := context.Background()
	attrTypes := make(map[string]tftypes.Type, len(schema.Attributes))
	rawValues := make(map[string]tftypes.Value, len(schema.Attributes))
	values := testDataSourceConfigValues(t, model)

	for name, attribute := range schema.Attributes {
		terraformType := attribute.GetType().TerraformType(ctx)
		attrTypes[name] = terraformType
		if value, ok := values[name]; ok {
			terraformValue, err := value.ToTerraformValue(ctx)
			if err != nil {
				t.Fatalf("config value %q conversion failed: %v", name, err)
			}
			rawValues[name] = terraformValue
			continue
		}
		rawValues[name] = tftypes.NewValue(terraformType, nil)
	}

	return tfsdk.Config{
		Raw:    tftypes.NewValue(tftypes.Object{AttributeTypes: attrTypes}, rawValues),
		Schema: schema,
	}
}

func testDataSourceConfigValues(t *testing.T, model any) map[string]attr.Value {
	t.Helper()

	switch typed := model.(type) {
	case haproxyBackendModel:
		values := typed.attrValues()
		values["id"] = typed.ID
		values["name"] = typed.Name
		return values
	case haproxyBackendServerModel:
		values := typed.attrValues()
		values["id"] = typed.ID
		values["backend_name"] = typed.BackendName
		values["name"] = typed.Name
		values["serverid"] = typed.ServerID
		return values
	default:
		t.Fatalf("unsupported data source config model %T", model)
		return nil
	}
}

func resourceTypeRegistered(resources []func() resource.Resource, typeName string) bool {
	for _, constructor := range resources {
		var metadata resource.MetadataResponse
		constructor().Metadata(context.Background(), resource.MetadataRequest{}, &metadata)
		if metadata.TypeName == typeName {
			return true
		}
	}

	return false
}

func dataSourceTypeRegistered(dataSources []func() datasource.DataSource, typeName string) bool {
	for _, constructor := range dataSources {
		var metadata datasource.MetadataResponse
		constructor().Metadata(context.Background(), datasource.MetadataRequest{}, &metadata)
		if metadata.TypeName == typeName {
			return true
		}
	}

	return false
}
