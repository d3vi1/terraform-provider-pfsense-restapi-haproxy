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

func TestHaproxyBackendResourceCreateUsesNaturalKeyLookupAndDoesNotApply(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var requests []string
	var postPayload map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case http.MethodGet + " /api/v2/services/haproxy/backends":
			if got := r.URL.Query().Get("name"); got != "app_backend" {
				t.Fatalf("backend lookup name = %q", got)
			}
			mu.Lock()
			callNumber := len(requests)
			mu.Unlock()
			if callNumber == 1 {
				_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[]}`))
				return
			}
			_, _ = w.Write([]byte(`{
				"code": 200,
				"status": "ok",
				"data": [{
					"id": 42,
					"name": "app_backend",
					"balance": "roundrobin",
					"connection_timeout": 15000,
					"server_timeout": "30000",
					"check_type": "HTTP",
					"checkinter": "2000",
					"log_health_checks": "yes",
					"httpcheck_method": "GET",
					"monitor_uri": "/health",
					"monitor_httpversion": "HTTP/1.1",
					"agent_checks": false,
					"persist_cookie_enabled": false
				}]
			}`))
		case http.MethodPost + " /api/v2/services/haproxy/backend":
			if err := json.NewDecoder(r.Body).Decode(&postPayload); err != nil {
				t.Fatalf("decode POST payload: %v", err)
			}
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":{"id":42}}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	t.Cleanup(server.Close)

	backendResource := &haproxyBackendResource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyBackendResourceSchema(t)
	plan := nullHaproxyBackendModel()
	plan.Name = types.StringValue("app_backend")
	plan.Balance = types.StringValue("roundrobin")
	plan.ConnectionTimeout = types.Int64Value(15000)
	plan.ServerTimeout = types.Int64Value(30000)
	plan.CheckType = types.StringValue("HTTP")
	plan.CheckInterval = types.Int64Value(2000)
	plan.LogHealthChecks = types.BoolValue(true)
	plan.HTTPCheckMethod = types.StringValue("GET")
	plan.MonitorURI = types.StringValue("/health")
	plan.MonitorHTTPVersion = types.StringValue("HTTP/1.1")

	resp := resource.CreateResponse{
		State: testResourceState(t, schema, plan),
	}
	backendResource.Create(context.Background(), resource.CreateRequest{
		Plan: testResourcePlan(t, schema, plan),
	}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %#v", resp.Diagnostics)
	}

	mu.Lock()
	gotRequests := append([]string(nil), requests...)
	mu.Unlock()
	wantRequests := []string{
		"GET /api/v2/services/haproxy/backends?name=app_backend",
		"POST /api/v2/services/haproxy/backend",
		"GET /api/v2/services/haproxy/backends?name=app_backend",
	}
	if !reflect.DeepEqual(gotRequests, wantRequests) {
		t.Fatalf("requests = %#v, want %#v", gotRequests, wantRequests)
	}

	if postPayload["name"] != "app_backend" {
		t.Fatalf("POST name = %#v", postPayload["name"])
	}
	if postPayload["balance"] != "roundrobin" {
		t.Fatalf("POST balance = %#v", postPayload["balance"])
	}
	if postPayload["connection_timeout"] != float64(15000) {
		t.Fatalf("POST connection_timeout = %#v", postPayload["connection_timeout"])
	}
	for _, forbidden := range []string{"id", "apply", "async", "servers", "acls", "actions"} {
		if _, ok := postPayload[forbidden]; ok {
			t.Fatalf("POST unexpectedly included %q: %#v", forbidden, postPayload)
		}
	}

	var state haproxyBackendModel
	diags := resp.State.Get(context.Background(), &state)
	if diags.HasError() {
		t.Fatalf("state get diagnostics: %#v", diags)
	}
	if state.ID.ValueString() != "app_backend" || state.Name.ValueString() != "app_backend" {
		t.Fatalf("natural key not preserved in state: %#v", state)
	}
	if state.ServerTimeout.ValueInt64() != 30000 || !state.LogHealthChecks.ValueBool() {
		t.Fatalf("state was not refreshed from API defaults: %#v", state)
	}
}

func TestHaproxyBackendResourceCreateRejectsExistingBackend(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != http.MethodGet || r.URL.RequestURI() != "/api/v2/services/haproxy/backends?name=app_backend" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":7,"name":"app_backend"}]}`))
	}))
	t.Cleanup(server.Close)

	backendResource := &haproxyBackendResource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyBackendResourceSchema(t)
	plan := nullHaproxyBackendModel()
	plan.Name = types.StringValue("app_backend")

	var resp resource.CreateResponse
	resp.State = testResourceState(t, schema, plan)
	backendResource.Create(context.Background(), resource.CreateRequest{
		Plan: testResourcePlan(t, schema, plan),
	}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatalf("expected existing backend diagnostic")
	}
	if requests.Load() != 1 {
		t.Fatalf("requests = %d", requests.Load())
	}
	if !strings.Contains(diagnosticsText(resp.Diagnostics), "terraform import") {
		t.Fatalf("diagnostics did not include import guidance: %s", diagnosticsText(resp.Diagnostics))
	}
}

func TestHaproxyBackendResourceReadRemovesMissingNaturalKey(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.RequestURI() != "/api/v2/services/haproxy/backends?name=missing_backend" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[]}`))
	}))
	t.Cleanup(server.Close)

	backendResource := &haproxyBackendResource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyBackendResourceSchema(t)
	stateModel := nullHaproxyBackendModel()
	stateModel.ID = types.StringValue("missing_backend")
	stateModel.Name = types.StringValue("missing_backend")

	resp := resource.ReadResponse{
		State: testResourceState(t, schema, stateModel),
	}
	backendResource.Read(context.Background(), resource.ReadRequest{
		State: testResourceState(t, schema, stateModel),
	}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %#v", resp.Diagnostics)
	}
	if !resp.State.Raw.IsNull() {
		t.Fatalf("missing backend did not remove state")
	}
}

func TestHaproxyBackendResourceUpdatePatchesChangedFieldsOnly(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var requests []string
	var patchPayload map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case http.MethodGet + " /api/v2/services/haproxy/backends":
			if got := r.URL.Query().Get("name"); got != "app_backend" {
				t.Fatalf("backend lookup name = %q", got)
			}
			_, _ = w.Write([]byte(`{
				"code": 200,
				"status": "ok",
				"data": [{
					"id": "42",
					"name": "app_backend",
					"balance": "leastconn",
					"connection_timeout": 10000,
					"server_timeout": 45000,
					"check_type": "HTTP",
					"checkinter": 5000,
					"log_health_checks": true,
					"httpcheck_method": "HEAD",
					"monitor_uri": "/ready",
					"monitor_httpversion": "HTTP/1.1",
					"agent_checks": true,
					"agent_port": 2200,
					"agent_inter": 3000,
					"persist_cookie_enabled": true,
					"persist_cookie_name": "SRV",
					"persist_cookie_mode": "insert-only"
				}]
			}`))
		case http.MethodPatch + " /api/v2/services/haproxy/backend":
			if err := json.NewDecoder(r.Body).Decode(&patchPayload); err != nil {
				t.Fatalf("decode PATCH payload: %v", err)
			}
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":null}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	t.Cleanup(server.Close)

	backendResource := &haproxyBackendResource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyBackendResourceSchema(t)

	prior := nullHaproxyBackendModel()
	prior.ID = types.StringValue("app_backend")
	prior.Name = types.StringValue("app_backend")
	prior.Balance = types.StringValue("roundrobin")
	prior.ConnectionTimeout = types.Int64Value(10000)
	prior.ServerTimeout = types.Int64Value(30000)
	prior.CheckType = types.StringValue("HTTP")
	prior.CheckInterval = types.Int64Value(5000)
	prior.LogHealthChecks = types.BoolValue(false)
	prior.HTTPCheckMethod = types.StringValue("HEAD")
	prior.MonitorURI = types.StringValue("/ready")
	prior.MonitorHTTPVersion = types.StringValue("HTTP/1.1")
	prior.AgentChecks = types.BoolValue(false)
	prior.PersistCookieEnabled = types.BoolValue(false)

	plan := prior
	plan.Balance = types.StringValue("leastconn")
	plan.ServerTimeout = types.Int64Value(45000)
	plan.LogHealthChecks = types.BoolValue(true)
	plan.AgentChecks = types.BoolValue(true)
	plan.AgentPort = types.StringValue("2200")
	plan.AgentInterval = types.Int64Value(3000)
	plan.PersistCookieEnabled = types.BoolValue(true)
	plan.PersistCookieName = types.StringValue("SRV")
	plan.PersistCookieMode = types.StringValue("insert-only")

	resp := resource.UpdateResponse{
		State: testResourceState(t, schema, prior),
	}
	backendResource.Update(context.Background(), resource.UpdateRequest{
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
		"GET /api/v2/services/haproxy/backends?name=app_backend",
		"PATCH /api/v2/services/haproxy/backend",
		"GET /api/v2/services/haproxy/backends?name=app_backend",
	}
	if !reflect.DeepEqual(gotRequests, wantRequests) {
		t.Fatalf("requests = %#v, want %#v", gotRequests, wantRequests)
	}

	if patchPayload["id"] != "42" {
		t.Fatalf("patch id = %#v", patchPayload["id"])
	}
	if patchPayload["balance"] != "leastconn" {
		t.Fatalf("patch balance = %#v", patchPayload["balance"])
	}
	if patchPayload["server_timeout"] != float64(45000) {
		t.Fatalf("patch server_timeout = %#v", patchPayload["server_timeout"])
	}
	if patchPayload["log_health_checks"] != true {
		t.Fatalf("patch log_health_checks = %#v", patchPayload["log_health_checks"])
	}
	if patchPayload["agent_port"] != "2200" {
		t.Fatalf("patch agent_port = %#v", patchPayload["agent_port"])
	}
	if patchPayload["persist_cookie_name"] != "SRV" {
		t.Fatalf("patch persist_cookie_name = %#v", patchPayload["persist_cookie_name"])
	}
	for _, forbidden := range []string{"name", "apply", "async", "connection_timeout", "check_type", "servers", "acls", "actions"} {
		if _, ok := patchPayload[forbidden]; ok {
			t.Fatalf("patch unexpectedly included %q: %#v", forbidden, patchPayload)
		}
	}

	var state haproxyBackendModel
	diags := resp.State.Get(context.Background(), &state)
	if diags.HasError() {
		t.Fatalf("state get diagnostics: %#v", diags)
	}
	if state.Balance.ValueString() != "leastconn" || state.ServerTimeout.ValueInt64() != 45000 || state.AgentPort.ValueString() != "2200" {
		t.Fatalf("state not refreshed from API: %#v", state)
	}
}

func TestHaproxyBackendResourceUpdateSkipsPatchWithoutChanges(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		mu.Unlock()

		if r.Method != http.MethodGet || r.URL.RequestURI() != "/api/v2/services/haproxy/backends?name=app_backend" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":42,"name":"app_backend","balance":"roundrobin"}]}`))
	}))
	t.Cleanup(server.Close)

	backendResource := &haproxyBackendResource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyBackendResourceSchema(t)
	model := nullHaproxyBackendModel()
	model.ID = types.StringValue("app_backend")
	model.Name = types.StringValue("app_backend")
	model.Balance = types.StringValue("roundrobin")

	resp := resource.UpdateResponse{
		State: testResourceState(t, schema, model),
	}
	backendResource.Update(context.Background(), resource.UpdateRequest{
		Plan:  testResourcePlan(t, schema, model),
		State: testResourceState(t, schema, model),
	}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %#v", resp.Diagnostics)
	}

	mu.Lock()
	gotRequests := append([]string(nil), requests...)
	mu.Unlock()
	wantRequests := []string{
		"GET /api/v2/services/haproxy/backends?name=app_backend",
		"GET /api/v2/services/haproxy/backends?name=app_backend",
	}
	if !reflect.DeepEqual(gotRequests, wantRequests) {
		t.Fatalf("requests = %#v, want %#v", gotRequests, wantRequests)
	}
}

func TestHaproxyBackendResourceDeleteResolvesAPIID(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case http.MethodGet + " /api/v2/services/haproxy/backends":
			if got := r.URL.Query().Get("name"); got != "app_backend" {
				t.Fatalf("backend lookup name = %q", got)
			}
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":42,"name":"app_backend"}]}`))
		case http.MethodDelete + " /api/v2/services/haproxy/backend":
			if got := r.URL.Query().Get("id"); got != "42" {
				t.Fatalf("delete id = %q", got)
			}
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":null}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	t.Cleanup(server.Close)

	backendResource := &haproxyBackendResource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyBackendResourceSchema(t)
	stateModel := nullHaproxyBackendModel()
	stateModel.ID = types.StringValue("app_backend")
	stateModel.Name = types.StringValue("app_backend")

	resp := resource.DeleteResponse{
		State: testResourceState(t, schema, stateModel),
	}
	backendResource.Delete(context.Background(), resource.DeleteRequest{
		State: testResourceState(t, schema, stateModel),
	}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %#v", resp.Diagnostics)
	}

	mu.Lock()
	gotRequests := append([]string(nil), requests...)
	mu.Unlock()
	wantRequests := []string{
		"GET /api/v2/services/haproxy/backends?name=app_backend",
		"DELETE /api/v2/services/haproxy/backend?id=42",
	}
	if !reflect.DeepEqual(gotRequests, wantRequests) {
		t.Fatalf("requests = %#v, want %#v", gotRequests, wantRequests)
	}
	if !resp.State.Raw.IsNull() {
		t.Fatalf("state was not removed")
	}
}

func TestHaproxyBackendResourceImportUsesNaturalName(t *testing.T) {
	t.Parallel()

	backendResource := &haproxyBackendResource{}
	schema := haproxyBackendResourceSchema(t)

	validResp := resource.ImportStateResponse{
		State: tfsdk.State{Schema: schema},
	}
	backendResource.ImportState(context.Background(), resource.ImportStateRequest{ID: "app_backend"}, &validResp)
	if validResp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %#v", validResp.Diagnostics)
	}
	var state haproxyBackendModel
	diags := validResp.State.Get(context.Background(), &state)
	if diags.HasError() {
		t.Fatalf("state get diagnostics: %#v", diags)
	}
	if state.ID.ValueString() != "app_backend" || state.Name.ValueString() != "app_backend" {
		t.Fatalf("imported state = %#v", state)
	}

	invalidResp := resource.ImportStateResponse{
		State: tfsdk.State{Schema: schema},
	}
	backendResource.ImportState(context.Background(), resource.ImportStateRequest{ID: "   "}, &invalidResp)
	if !invalidResp.Diagnostics.HasError() {
		t.Fatalf("expected diagnostic for blank import id")
	}
}

func TestHaproxyBackendSchemaIsConservative(t *testing.T) {
	schema := haproxyBackendResourceSchema(t)
	for _, forbidden := range []string{"servers", "acls", "actions", "errorfiles", "advanced", "advanced_backend", "stats_password"} {
		if _, ok := schema.Attributes[forbidden]; ok {
			t.Fatalf("resource schema should not expose %q before ownership is validated", forbidden)
		}
	}
	if _, ok := schema.Attributes["name"]; !ok {
		t.Fatalf("resource schema missing name")
	}
	if _, ok := schema.Attributes["agent_port"]; !ok {
		t.Fatalf("resource schema missing agent_port")
	}
	if _, ok := schema.Attributes["persist_cookie_name"]; !ok {
		t.Fatalf("resource schema missing persist_cookie_name")
	}
}

func haproxyBackendResourceSchema(t *testing.T) resourceschema.Schema {
	t.Helper()

	var resp resource.SchemaResponse
	(&haproxyBackendResource{}).Schema(context.Background(), resource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected resource schema diagnostics: %#v", resp.Diagnostics)
	}

	return resp.Schema
}
