package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/d3vi1/terraform-provider-pfsense-restapi-haproxy/internal/pfsense"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestHaproxyFrontendAddressResourceCreateUsesParentLookupAndDoesNotApply(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var requests []string
	var postPayload map[string]any
	addressLookupCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case http.MethodGet + " /api/v2/services/haproxy/frontends":
			if got := r.URL.Query().Get("name"); got != "app_frontend" {
				t.Fatalf("frontend lookup name = %q", got)
			}
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":42,"name":"app_frontend","type":"http"}]}`))
		case http.MethodGet + " /api/v2/services/haproxy/frontend/addresses":
			assertFrontendAddressLookupQuery(t, r, "42", "custom", "192.0.2.10", "443")
			addressLookupCount++
			if addressLookupCount == 1 {
				_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[]}`))
				return
			}
			_, _ = w.Write([]byte(`{
				"code": 200,
				"status": "ok",
				"data": [{
					"id": 7,
					"extaddr": "custom",
					"extaddr_custom": "192.0.2.10",
					"extaddr_port": "443",
					"extaddr_ssl": "yes"
				}]
			}`))
		case http.MethodPost + " /api/v2/services/haproxy/frontend/address":
			if err := json.NewDecoder(r.Body).Decode(&postPayload); err != nil {
				t.Fatalf("decode POST payload: %v", err)
			}
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":{"id":7}}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	t.Cleanup(server.Close)

	addressResource := &haproxyFrontendAddressResource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyFrontendAddressResourceSchema(t)
	plan := nullHaproxyFrontendAddressModel()
	plan.FrontendName = types.StringValue("app_frontend")
	plan.Extaddr = types.StringValue("custom")
	plan.ExtaddrCustom = types.StringValue("192.0.2.10")
	plan.ExtaddrPort = types.Int64Value(443)
	plan.ExtaddrSSL = types.BoolValue(true)

	resp := resource.CreateResponse{
		State: testResourceState(t, schema, plan),
	}
	addressResource.Create(context.Background(), resource.CreateRequest{
		Plan: testResourcePlan(t, schema, plan),
	}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %#v", resp.Diagnostics)
	}

	mu.Lock()
	gotRequests := append([]string(nil), requests...)
	mu.Unlock()
	wantRequests := []string{
		"GET /api/v2/services/haproxy/frontends?name=app_frontend",
		"GET /api/v2/services/haproxy/frontend/addresses?extaddr=custom&extaddr_custom=192.0.2.10&extaddr_port=443&parent_id=42",
		"POST /api/v2/services/haproxy/frontend/address",
		"GET /api/v2/services/haproxy/frontend/addresses?extaddr=custom&extaddr_custom=192.0.2.10&extaddr_port=443&parent_id=42",
	}
	if !reflect.DeepEqual(gotRequests, wantRequests) {
		t.Fatalf("requests = %#v, want %#v", gotRequests, wantRequests)
	}

	if postPayload["parent_id"] != "42" {
		t.Fatalf("POST parent_id = %#v", postPayload["parent_id"])
	}
	if postPayload["extaddr"] != "custom" || postPayload["extaddr_custom"] != "192.0.2.10" {
		t.Fatalf("POST natural fields = %#v", postPayload)
	}
	if postPayload["extaddr_port"] != float64(443) || postPayload["extaddr_ssl"] != true {
		t.Fatalf("POST port/ssl fields = %#v", postPayload)
	}
	for _, forbidden := range []string{"id", "apply", "async", "placement", "exaddr_advanced", "advanced", "a_extaddr"} {
		if _, ok := postPayload[forbidden]; ok {
			t.Fatalf("POST unexpectedly included %q: %#v", forbidden, postPayload)
		}
	}

	var state haproxyFrontendAddressModel
	diags := resp.State.Get(context.Background(), &state)
	if diags.HasError() {
		t.Fatalf("state get diagnostics: %#v", diags)
	}
	if state.ID.ValueString() != "app_frontend/custom/192.0.2.10/443" || state.FrontendName.ValueString() != "app_frontend" {
		t.Fatalf("natural key not preserved in state: %#v", state)
	}
	if state.ExtaddrPort.ValueInt64() != 443 || !state.ExtaddrSSL.ValueBool() {
		t.Fatalf("state was not refreshed from API defaults: %#v", state)
	}
}

func TestHaproxyFrontendAddressResourceCreateRejectsDuplicate(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case http.MethodGet + " /api/v2/services/haproxy/frontends":
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":42,"name":"app_frontend","type":"tcp"}]}`))
		case http.MethodGet + " /api/v2/services/haproxy/frontend/addresses":
			assertFrontendAddressLookupQuery(t, r, "42", "any_ipv4", "", "80")
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":7,"extaddr":"any_ipv4","extaddr_custom":"","extaddr_port":80}]}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	t.Cleanup(server.Close)

	addressResource := &haproxyFrontendAddressResource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyFrontendAddressResourceSchema(t)
	plan := nullHaproxyFrontendAddressModel()
	plan.FrontendName = types.StringValue("app_frontend")
	plan.Extaddr = types.StringValue("any_ipv4")
	plan.ExtaddrPort = types.Int64Value(80)

	var resp resource.CreateResponse
	resp.State = testResourceState(t, schema, plan)
	addressResource.Create(context.Background(), resource.CreateRequest{
		Plan: testResourcePlan(t, schema, plan),
	}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatalf("expected duplicate address diagnostic")
	}
	if requests.Load() != 2 {
		t.Fatalf("requests = %d", requests.Load())
	}
	if !strings.Contains(diagnosticsText(resp.Diagnostics), "terraform import") {
		t.Fatalf("diagnostics did not include import guidance: %s", diagnosticsText(resp.Diagnostics))
	}
}

func TestHaproxyFrontendAddressResourceCreateErrorsWhenParentMissing(t *testing.T) {
	t.Parallel()

	var requests []string
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		mu.Unlock()

		if r.Method != http.MethodGet || r.URL.RequestURI() != "/api/v2/services/haproxy/frontends?name=missing_frontend" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[]}`))
	}))
	t.Cleanup(server.Close)

	addressResource := &haproxyFrontendAddressResource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyFrontendAddressResourceSchema(t)
	plan := nullHaproxyFrontendAddressModel()
	plan.FrontendName = types.StringValue("missing_frontend")
	plan.Extaddr = types.StringValue("any_ipv4")
	plan.ExtaddrPort = types.Int64Value(80)

	var resp resource.CreateResponse
	resp.State = testResourceState(t, schema, plan)
	addressResource.Create(context.Background(), resource.CreateRequest{
		Plan: testResourcePlan(t, schema, plan),
	}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatalf("expected missing parent diagnostic")
	}
	if !strings.Contains(diagnosticsText(resp.Diagnostics), "Parent frontend") {
		t.Fatalf("diagnostics did not describe missing parent: %s", diagnosticsText(resp.Diagnostics))
	}

	mu.Lock()
	gotRequests := append([]string(nil), requests...)
	mu.Unlock()
	wantRequests := []string{"GET /api/v2/services/haproxy/frontends?name=missing_frontend"}
	if !reflect.DeepEqual(gotRequests, wantRequests) {
		t.Fatalf("requests = %#v, want %#v", gotRequests, wantRequests)
	}
}

func TestHaproxyFrontendAddressResourceUpdateRequeriesParentAndPatchesSSLOnly(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var requests []string
	var patchPayload map[string]any
	addressLookupCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case http.MethodGet + " /api/v2/services/haproxy/frontends":
			if got := r.URL.Query().Get("name"); got != "app_frontend" {
				t.Fatalf("frontend lookup name = %q", got)
			}
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":99,"name":"app_frontend","type":"http"}]}`))
		case http.MethodGet + " /api/v2/services/haproxy/frontend/addresses":
			assertFrontendAddressLookupQuery(t, r, "99", "any_ipv4", "", "443")
			addressLookupCount++
			ssl := ""
			if addressLookupCount > 1 {
				ssl = "yes"
			}
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":"7","extaddr":"any_ipv4","extaddr_custom":"","extaddr_port":"443","extaddr_ssl":"` + ssl + `"}]}`))
		case http.MethodPatch + " /api/v2/services/haproxy/frontend/address":
			if err := json.NewDecoder(r.Body).Decode(&patchPayload); err != nil {
				t.Fatalf("decode PATCH payload: %v", err)
			}
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":null}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	t.Cleanup(server.Close)

	addressResource := &haproxyFrontendAddressResource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyFrontendAddressResourceSchema(t)

	prior := nullHaproxyFrontendAddressModel()
	prior.ID = types.StringValue("app_frontend/any_ipv4/-/443")
	prior.FrontendName = types.StringValue("app_frontend")
	prior.Extaddr = types.StringValue("any_ipv4")
	prior.ExtaddrCustom = types.StringValue("")
	prior.ExtaddrPort = types.Int64Value(443)
	prior.ExtaddrSSL = types.BoolValue(false)

	plan := prior
	plan.ExtaddrSSL = types.BoolValue(true)

	resp := resource.UpdateResponse{
		State: testResourceState(t, schema, prior),
	}
	addressResource.Update(context.Background(), resource.UpdateRequest{
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
		"GET /api/v2/services/haproxy/frontends?name=app_frontend",
		"GET /api/v2/services/haproxy/frontend/addresses?extaddr=any_ipv4&extaddr_custom=&extaddr_port=443&parent_id=99",
		"PATCH /api/v2/services/haproxy/frontend/address",
		"GET /api/v2/services/haproxy/frontend/addresses?extaddr=any_ipv4&extaddr_custom=&extaddr_port=443&parent_id=99",
	}
	if !reflect.DeepEqual(gotRequests, wantRequests) {
		t.Fatalf("requests = %#v, want %#v", gotRequests, wantRequests)
	}

	if patchPayload["parent_id"] != "99" || patchPayload["id"] != "7" {
		t.Fatalf("patch IDs = %#v", patchPayload)
	}
	if patchPayload["extaddr_ssl"] != true {
		t.Fatalf("patch ssl = %#v", patchPayload)
	}
	for _, forbidden := range []string{"frontend_name", "extaddr", "extaddr_custom", "extaddr_port", "apply", "async", "placement", "exaddr_advanced"} {
		if _, ok := patchPayload[forbidden]; ok {
			t.Fatalf("patch unexpectedly included %q: %#v", forbidden, patchPayload)
		}
	}

	var state haproxyFrontendAddressModel
	diags := resp.State.Get(context.Background(), &state)
	if diags.HasError() {
		t.Fatalf("state get diagnostics: %#v", diags)
	}
	if state.ID.ValueString() != "app_frontend/any_ipv4/-/443" || !state.ExtaddrSSL.ValueBool() {
		t.Fatalf("state not refreshed from API: %#v", state)
	}
}

func TestHaproxyFrontendAddressResourceDeleteResolvesCurrentParentAndAddressIDs(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case http.MethodGet + " /api/v2/services/haproxy/frontends":
			if got := r.URL.Query().Get("name"); got != "app_frontend" {
				t.Fatalf("frontend lookup name = %q", got)
			}
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":99,"name":"app_frontend","type":"tcp"}]}`))
		case http.MethodGet + " /api/v2/services/haproxy/frontend/addresses":
			assertFrontendAddressLookupQuery(t, r, "99", "localhost_ipv4", "", "8080")
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":7,"extaddr":"localhost_ipv4","extaddr_custom":"","extaddr_port":8080}]}`))
		case http.MethodDelete + " /api/v2/services/haproxy/frontend/address":
			if got := r.URL.Query().Get("parent_id"); got != "99" {
				t.Fatalf("delete parent_id = %q", got)
			}
			if got := r.URL.Query().Get("id"); got != "7" {
				t.Fatalf("delete id = %q", got)
			}
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":null}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	t.Cleanup(server.Close)

	addressResource := &haproxyFrontendAddressResource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyFrontendAddressResourceSchema(t)
	stateModel := nullHaproxyFrontendAddressModel()
	stateModel.ID = types.StringValue("app_frontend/localhost_ipv4/-/8080")
	stateModel.FrontendName = types.StringValue("app_frontend")
	stateModel.Extaddr = types.StringValue("localhost_ipv4")
	stateModel.ExtaddrCustom = types.StringValue("")
	stateModel.ExtaddrPort = types.Int64Value(8080)

	resp := resource.DeleteResponse{
		State: testResourceState(t, schema, stateModel),
	}
	addressResource.Delete(context.Background(), resource.DeleteRequest{
		State: testResourceState(t, schema, stateModel),
	}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %#v", resp.Diagnostics)
	}

	mu.Lock()
	gotRequests := append([]string(nil), requests...)
	mu.Unlock()
	wantRequests := []string{
		"GET /api/v2/services/haproxy/frontends?name=app_frontend",
		"GET /api/v2/services/haproxy/frontend/addresses?extaddr=localhost_ipv4&extaddr_custom=&extaddr_port=8080&parent_id=99",
		"DELETE /api/v2/services/haproxy/frontend/address?id=7&parent_id=99",
	}
	if !reflect.DeepEqual(gotRequests, wantRequests) {
		t.Fatalf("requests = %#v, want %#v", gotRequests, wantRequests)
	}
	if !resp.State.Raw.IsNull() {
		t.Fatalf("state was not removed")
	}
}

func TestHaproxyFrontendAddressResourceImportUsesNaturalKey(t *testing.T) {
	t.Parallel()

	addressResource := &haproxyFrontendAddressResource{}
	schema := haproxyFrontendAddressResourceSchema(t)

	validResp := resource.ImportStateResponse{
		State: tfsdk.State{Schema: schema},
	}
	addressResource.ImportState(context.Background(), resource.ImportStateRequest{ID: "app_frontend/custom/192.0.2.10/443"}, &validResp)
	if validResp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %#v", validResp.Diagnostics)
	}
	var state haproxyFrontendAddressModel
	diags := validResp.State.Get(context.Background(), &state)
	if diags.HasError() {
		t.Fatalf("state get diagnostics: %#v", diags)
	}
	if state.ID.ValueString() != "app_frontend/custom/192.0.2.10/443" || state.ExtaddrCustom.ValueString() != "192.0.2.10" || state.ExtaddrPort.ValueInt64() != 443 {
		t.Fatalf("imported custom state = %#v", state)
	}

	validBuiltinResp := resource.ImportStateResponse{
		State: tfsdk.State{Schema: schema},
	}
	addressResource.ImportState(context.Background(), resource.ImportStateRequest{ID: "app_frontend/any_ipv4/-/80"}, &validBuiltinResp)
	if validBuiltinResp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %#v", validBuiltinResp.Diagnostics)
	}
	var builtinState haproxyFrontendAddressModel
	diags = validBuiltinResp.State.Get(context.Background(), &builtinState)
	if diags.HasError() {
		t.Fatalf("state get diagnostics: %#v", diags)
	}
	if builtinState.ID.ValueString() != "app_frontend/any_ipv4/-/80" || builtinState.ExtaddrCustom.ValueString() != "" {
		t.Fatalf("imported built-in state = %#v", builtinState)
	}

	validCanonicalResp := resource.ImportStateResponse{
		State: tfsdk.State{Schema: schema},
	}
	addressResource.ImportState(context.Background(), resource.ImportStateRequest{ID: "app_frontend/custom/2001:0DB8:0000:0000:0000:0000:0000:0010/443"}, &validCanonicalResp)
	if validCanonicalResp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %#v", validCanonicalResp.Diagnostics)
	}
	var canonicalState haproxyFrontendAddressModel
	diags = validCanonicalResp.State.Get(context.Background(), &canonicalState)
	if diags.HasError() {
		t.Fatalf("state get diagnostics: %#v", diags)
	}
	if canonicalState.ID.ValueString() != "app_frontend/custom/2001:db8::10/443" || canonicalState.ExtaddrCustom.ValueString() != "2001:db8::10" {
		t.Fatalf("imported canonical state = %#v", canonicalState)
	}

	for _, id := range []string{
		"",
		"app_frontend",
		"app_frontend/any_ipv4/80",
		"app_frontend/any_ipv4//80",
		"app_frontend/any_ipv4/-/0",
		"app_frontend/any_ipv4/192.0.2.10/80",
		"app_frontend/custom/-/443",
		"app_frontend/custom/not-an-ip/443",
		"app/frontend/any_ipv4/-/80",
	} {
		invalidResp := resource.ImportStateResponse{
			State: tfsdk.State{Schema: schema},
		}
		addressResource.ImportState(context.Background(), resource.ImportStateRequest{ID: id}, &invalidResp)
		if !invalidResp.Diagnostics.HasError() {
			t.Fatalf("expected diagnostic for import id %q", id)
		}
	}
}

func TestHaproxyFrontendAddressResourceModifyPlanCanonicalizesCustomIPv6(t *testing.T) {
	t.Parallel()

	addressResource := &haproxyFrontendAddressResource{}
	schema := haproxyFrontendAddressResourceSchema(t)
	plan := nullHaproxyFrontendAddressModel()
	plan.FrontendName = types.StringValue("app_frontend")
	plan.Extaddr = types.StringValue("custom")
	plan.ExtaddrCustom = types.StringValue("2001:0DB8:0000:0000:0000:0000:0000:0010")
	plan.ExtaddrPort = types.Int64Value(443)

	requestPlan := testResourcePlan(t, schema, plan)
	resp := resource.ModifyPlanResponse{
		Plan: testResourcePlan(t, schema, plan),
	}
	addressResource.ModifyPlan(context.Background(), resource.ModifyPlanRequest{
		Plan: requestPlan,
	}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %#v", resp.Diagnostics)
	}

	var modified haproxyFrontendAddressModel
	diags := resp.Plan.Get(context.Background(), &modified)
	if diags.HasError() {
		t.Fatalf("plan get diagnostics: %#v", diags)
	}
	if modified.ExtaddrCustom.ValueString() != "2001:db8::10" {
		t.Fatalf("modified extaddr_custom = %q, want 2001:db8::10", modified.ExtaddrCustom.ValueString())
	}
}

func TestHaproxyFrontendAddressNormalizesCustomIPv6PayloadForMatchingAndState(t *testing.T) {
	t.Parallel()

	payload := map[string]any{
		"id":             "7",
		"extaddr":        "custom",
		"extaddr_custom": "2001:0DB8:0000:0000:0000:0000:0000:0010",
		"extaddr_port":   "443",
	}
	keys := haproxyFrontendAddressKeys{
		frontendName:  "app_frontend",
		extaddr:       "custom",
		extaddrCustom: "2001:db8::10",
		extaddrPort:   443,
	}

	matches, err := haproxyFrontendAddressPayloadMatches(payload, keys)
	if err != nil {
		t.Fatalf("payload match returned error: %v", err)
	}
	if !matches {
		t.Fatalf("payload did not match canonical custom IPv6 keys")
	}

	model, err := haproxyFrontendAddressModelFromAPI(payload, "app_frontend")
	if err != nil {
		t.Fatalf("model from API returned error: %v", err)
	}
	if model.ID.ValueString() != "app_frontend/custom/2001:db8::10/443" || model.ExtaddrCustom.ValueString() != "2001:db8::10" {
		t.Fatalf("provider model was not normalized from API payload: %#v", model)
	}
}

func TestHaproxyFrontendAddressValidation(t *testing.T) {
	t.Parallel()

	validBuiltin := nullHaproxyFrontendAddressModel()
	validBuiltin.FrontendName = types.StringValue("app_frontend")
	validBuiltin.Extaddr = types.StringValue("any_ipv4")
	validBuiltin.ExtaddrPort = types.Int64Value(80)

	if _, err := validateHaproxyFrontendAddressPlan(validBuiltin); err != nil {
		t.Fatalf("valid built-in address rejected: %v", err)
	}

	validCustom := validBuiltin
	validCustom.Extaddr = types.StringValue("custom")
	validCustom.ExtaddrCustom = types.StringValue("2001:db8::10")
	validCustom.ExtaddrPort = types.Int64Value(443)
	if _, err := validateHaproxyFrontendAddressPlan(validCustom); err != nil {
		t.Fatalf("valid custom address rejected: %v", err)
	}
	canonicalCustom := validCustom
	canonicalCustom.ExtaddrCustom = types.StringValue("2001:0DB8:0000:0000:0000:0000:0000:0010")
	canonicalKeys, err := validateHaproxyFrontendAddressPlan(canonicalCustom)
	if err != nil {
		t.Fatalf("valid expanded IPv6 custom address rejected: %v", err)
	}
	if canonicalKeys.extaddrCustom != "2001:db8::10" {
		t.Fatalf("canonical custom address = %q, want 2001:db8::10", canonicalKeys.extaddrCustom)
	}

	tests := map[string]haproxyFrontendAddressModel{
		"frontend slash": func() haproxyFrontendAddressModel {
			model := validBuiltin
			model.FrontendName = types.StringValue("app/frontend")
			return model
		}(),
		"bad extaddr": func() haproxyFrontendAddressModel {
			model := validBuiltin
			model.Extaddr = types.StringValue("wan")
			return model
		}(),
		"custom missing ip": func() haproxyFrontendAddressModel {
			model := validBuiltin
			model.Extaddr = types.StringValue("custom")
			return model
		}(),
		"custom hostname": func() haproxyFrontendAddressModel {
			model := validCustom
			model.ExtaddrCustom = types.StringValue("example.com")
			return model
		}(),
		"custom slash": func() haproxyFrontendAddressModel {
			model := validCustom
			model.ExtaddrCustom = types.StringValue("192.0.2.10/32")
			return model
		}(),
		"custom reserved dash": func() haproxyFrontendAddressModel {
			model := validCustom
			model.ExtaddrCustom = types.StringValue("-")
			return model
		}(),
		"custom with built-in selector": func() haproxyFrontendAddressModel {
			model := validBuiltin
			model.ExtaddrCustom = types.StringValue("192.0.2.10")
			return model
		}(),
		"zero port": func() haproxyFrontendAddressModel {
			model := validBuiltin
			model.ExtaddrPort = types.Int64Value(0)
			return model
		}(),
		"high port": func() haproxyFrontendAddressModel {
			model := validBuiltin
			model.ExtaddrPort = types.Int64Value(65536)
			return model
		}(),
	}

	for name, model := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := validateHaproxyFrontendAddressPlan(model); err == nil {
				t.Fatalf("expected validation error")
			}
		})
	}
}

func TestHaproxyFrontendAddressSchemaIsConservative(t *testing.T) {
	schema := haproxyFrontendAddressResourceSchema(t)
	for _, required := range []string{"frontend_name", "extaddr", "extaddr_custom", "extaddr_port", "extaddr_ssl"} {
		if _, ok := schema.Attributes[required]; !ok {
			t.Fatalf("resource schema missing %q", required)
		}
	}
	for _, forbidden := range []string{"parent_id", "rest_id", "api_id", "id_api", "apply", "async", "placement", "a_extaddr", "addresses", "exaddr_advanced", "advanced", "ha_acls", "acls", "a_actionitems", "actions", "ha_certificates", "certificates"} {
		if _, ok := schema.Attributes[forbidden]; ok {
			t.Fatalf("resource schema should not expose %q before ownership is validated", forbidden)
		}
	}
}

func assertFrontendAddressLookupQuery(t *testing.T, r *http.Request, parentID string, extaddr string, extaddrCustom string, extaddrPort string) {
	t.Helper()

	if got := r.URL.Query().Get("parent_id"); got != parentID {
		t.Fatalf("address lookup parent_id = %q", got)
	}
	if got := r.URL.Query().Get("extaddr"); got != extaddr {
		t.Fatalf("address lookup extaddr = %q", got)
	}
	if got := r.URL.Query().Get("extaddr_custom"); got != extaddrCustom {
		t.Fatalf("address lookup extaddr_custom = %q", got)
	}
	if got := r.URL.Query().Get("extaddr_port"); got != extaddrPort {
		t.Fatalf("address lookup extaddr_port = %q", got)
	}
}

func haproxyFrontendAddressResourceSchema(t *testing.T) resourceschema.Schema {
	t.Helper()

	var resp resource.SchemaResponse
	(&haproxyFrontendAddressResource{}).Schema(context.Background(), resource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected resource schema diagnostics: %#v", resp.Diagnostics)
	}

	return resp.Schema
}
