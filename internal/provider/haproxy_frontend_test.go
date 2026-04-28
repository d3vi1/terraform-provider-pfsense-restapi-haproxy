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

func TestHaproxyFrontendResourceCreateUsesNaturalKeyLookupAndDoesNotApply(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var requests []string
	var postPayload map[string]any
	var frontendLookups atomic.Int32

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
			if frontendLookups.Add(1) == 1 {
				_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[]}`))
				return
			}
			_, _ = w.Write([]byte(`{
				"code": 200,
				"status": "ok",
				"data": [{
					"id": 42,
					"name": "app_frontend",
					"type": "http",
					"descr": "App frontend",
					"status": "active",
					"max_connections": "2000",
					"backend_serverpool": "app_backend",
					"socket_stats": "yes",
					"dontlognull": false,
					"dontlog_normal": true,
					"log_separate_errors": true,
					"log_detailed": false,
					"client_timeout": 30000,
					"forwardfor": "yes",
					"httpclose": "http-server-close"
				}]
			}`))
		case http.MethodPost + " /api/v2/services/haproxy/frontend":
			if err := json.NewDecoder(r.Body).Decode(&postPayload); err != nil {
				t.Fatalf("decode POST payload: %v", err)
			}
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":{"id":42}}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	t.Cleanup(server.Close)

	frontendResource := &haproxyFrontendResource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyFrontendResourceSchema(t)
	plan := nullHaproxyFrontendModel()
	plan.Name = types.StringValue("app_frontend")
	plan.Type = types.StringValue("http")
	plan.Description = types.StringValue("App frontend")
	plan.Status = types.StringValue("active")
	plan.MaxConnections = types.Int64Value(2000)
	plan.BackendServerpool = types.StringValue("app_backend")
	plan.SocketStats = types.BoolValue(true)
	plan.DontLogNull = types.BoolValue(false)
	plan.DontLogNormal = types.BoolValue(true)
	plan.LogSeparateErrors = types.BoolValue(true)
	plan.LogDetailed = types.BoolValue(false)
	plan.ClientTimeout = types.Int64Value(30000)
	plan.ForwardFor = types.BoolValue(true)
	plan.HTTPClose = types.StringValue("http-server-close")

	resp := resource.CreateResponse{
		State: testResourceState(t, schema, plan),
	}
	frontendResource.Create(context.Background(), resource.CreateRequest{
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
		"POST /api/v2/services/haproxy/frontend",
		"GET /api/v2/services/haproxy/frontends?name=app_frontend",
	}
	if !reflect.DeepEqual(gotRequests, wantRequests) {
		t.Fatalf("requests = %#v, want %#v", gotRequests, wantRequests)
	}

	if postPayload["name"] != "app_frontend" || postPayload["type"] != "http" {
		t.Fatalf("POST natural fields = %#v", postPayload)
	}
	if postPayload["max_connections"] != float64(2000) || postPayload["client_timeout"] != float64(30000) {
		t.Fatalf("POST numeric fields = %#v", postPayload)
	}
	if postPayload["forwardfor"] != true || postPayload["dontlognull"] != false {
		t.Fatalf("POST bool fields = %#v", postPayload)
	}
	if postPayload["httpclose"] != "http-server-close" {
		t.Fatalf("POST httpclose = %#v", postPayload["httpclose"])
	}
	for _, forbidden := range []string{"id", "apply", "async", "a_extaddr", "ha_acls", "a_actionitems", "ha_certificates", "a_errorfiles", "advanced", "advanced_bind", "ssloffloadcert"} {
		if _, ok := postPayload[forbidden]; ok {
			t.Fatalf("POST unexpectedly included %q: %#v", forbidden, postPayload)
		}
	}

	var state haproxyFrontendModel
	diags := resp.State.Get(context.Background(), &state)
	if diags.HasError() {
		t.Fatalf("state get diagnostics: %#v", diags)
	}
	if state.ID.ValueString() != "app_frontend" || state.Name.ValueString() != "app_frontend" || state.Type.ValueString() != "http" {
		t.Fatalf("natural key not preserved in state: %#v", state)
	}
	if state.MaxConnections.ValueInt64() != 2000 || !state.ForwardFor.ValueBool() || state.HTTPClose.ValueString() != "http-server-close" {
		t.Fatalf("state was not refreshed from API defaults: %#v", state)
	}
}

func TestHaproxyFrontendResourceCreateRejectsExistingFrontend(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != http.MethodGet || r.URL.RequestURI() != "/api/v2/services/haproxy/frontends?name=app_frontend" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"name":"app_frontend","type":"http"}]}`))
	}))
	t.Cleanup(server.Close)

	frontendResource := &haproxyFrontendResource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyFrontendResourceSchema(t)
	plan := nullHaproxyFrontendModel()
	plan.Name = types.StringValue("app_frontend")
	plan.Type = types.StringValue("http")

	var resp resource.CreateResponse
	resp.State = testResourceState(t, schema, plan)
	frontendResource.Create(context.Background(), resource.CreateRequest{
		Plan: testResourcePlan(t, schema, plan),
	}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatalf("expected existing frontend diagnostic")
	}
	if requests.Load() != 1 {
		t.Fatalf("requests = %d", requests.Load())
	}
	if !strings.Contains(diagnosticsText(resp.Diagnostics), "terraform import") {
		t.Fatalf("diagnostics did not include import guidance: %s", diagnosticsText(resp.Diagnostics))
	}
}

func TestHaproxyFrontendResourceReadRemovesMissingNaturalKey(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.RequestURI() != "/api/v2/services/haproxy/frontends?name=missing_frontend" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[]}`))
	}))
	t.Cleanup(server.Close)

	frontendResource := &haproxyFrontendResource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyFrontendResourceSchema(t)
	stateModel := nullHaproxyFrontendModel()
	stateModel.ID = types.StringValue("missing_frontend")
	stateModel.Name = types.StringValue("missing_frontend")
	stateModel.Type = types.StringValue("tcp")

	resp := resource.ReadResponse{
		State: testResourceState(t, schema, stateModel),
	}
	frontendResource.Read(context.Background(), resource.ReadRequest{
		State: testResourceState(t, schema, stateModel),
	}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %#v", resp.Diagnostics)
	}
	if !resp.State.Raw.IsNull() {
		t.Fatalf("missing frontend did not remove state")
	}
}

func TestHaproxyFrontendResourceUpdateResolvesAPIIDAndPatchesChangedFieldsOnly(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var requests []string
	var patchPayload map[string]any
	var frontendLookups atomic.Int32

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
			maxConnections := "1000"
			status := "active"
			if frontendLookups.Add(1) > 1 {
				maxConnections = "2000"
				status = "disabled"
			}
			_, _ = w.Write([]byte(`{
				"code": 200,
				"status": "ok",
				"data": [{
					"id": "42",
					"name": "app_frontend",
					"type": "tcp",
					"descr": "App frontend",
					"status": "` + status + `",
					"max_connections": "` + maxConnections + `",
					"backend_serverpool": "app_backend",
					"socket_stats": true,
					"dontlognull": "yes",
					"dontlog_normal": false,
					"log_separate_errors": true,
					"log_detailed": "yes",
					"client_timeout": "45000",
					"httpclose": "http-tunnel"
				}]
			}`))
		case http.MethodPatch + " /api/v2/services/haproxy/frontend":
			if err := json.NewDecoder(r.Body).Decode(&patchPayload); err != nil {
				t.Fatalf("decode PATCH payload: %v", err)
			}
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":null}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	t.Cleanup(server.Close)

	frontendResource := &haproxyFrontendResource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyFrontendResourceSchema(t)

	prior := nullHaproxyFrontendModel()
	prior.ID = types.StringValue("app_frontend")
	prior.Name = types.StringValue("app_frontend")
	prior.Type = types.StringValue("http")
	prior.Description = types.StringValue("App frontend")
	prior.Status = types.StringValue("active")
	prior.MaxConnections = types.Int64Value(1000)
	prior.BackendServerpool = types.StringValue("app_backend")
	prior.SocketStats = types.BoolValue(true)
	prior.DontLogNull = types.BoolValue(true)
	prior.DontLogNormal = types.BoolValue(false)
	prior.LogSeparateErrors = types.BoolValue(true)
	prior.LogDetailed = types.BoolValue(false)
	prior.ClientTimeout = types.Int64Value(30000)
	prior.ForwardFor = types.BoolValue(true)
	prior.HTTPClose = types.StringValue("http-server-close")

	plan := prior
	plan.Type = types.StringValue("tcp")
	plan.Status = types.StringValue("disabled")
	plan.MaxConnections = types.Int64Value(2000)
	plan.LogDetailed = types.BoolValue(true)
	plan.ClientTimeout = types.Int64Value(45000)
	plan.ForwardFor = types.BoolNull()
	plan.HTTPClose = types.StringValue("http-tunnel")

	resp := resource.UpdateResponse{
		State: testResourceState(t, schema, prior),
	}
	frontendResource.Update(context.Background(), resource.UpdateRequest{
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
		"PATCH /api/v2/services/haproxy/frontend",
		"GET /api/v2/services/haproxy/frontends?name=app_frontend",
	}
	if !reflect.DeepEqual(gotRequests, wantRequests) {
		t.Fatalf("requests = %#v, want %#v", gotRequests, wantRequests)
	}

	if patchPayload["id"] != "42" {
		t.Fatalf("patch id = %#v", patchPayload["id"])
	}
	if patchPayload["type"] != "tcp" || patchPayload["status"] != "disabled" || patchPayload["httpclose"] != "http-tunnel" {
		t.Fatalf("patch string fields = %#v", patchPayload)
	}
	if patchPayload["max_connections"] != float64(2000) || patchPayload["client_timeout"] != float64(45000) {
		t.Fatalf("patch numeric fields = %#v", patchPayload)
	}
	if patchPayload["log_detailed"] != true || patchPayload["forwardfor"] != nil {
		t.Fatalf("patch bool/null fields = %#v", patchPayload)
	}
	for _, forbidden := range []string{"name", "apply", "async", "descr", "backend_serverpool", "socket_stats", "dontlognull", "dontlog_normal", "log_separate_errors", "a_extaddr", "ha_acls", "a_actionitems", "ha_certificates", "a_errorfiles", "advanced", "advanced_bind"} {
		if _, ok := patchPayload[forbidden]; ok {
			t.Fatalf("patch unexpectedly included %q: %#v", forbidden, patchPayload)
		}
	}

	var state haproxyFrontendModel
	diags := resp.State.Get(context.Background(), &state)
	if diags.HasError() {
		t.Fatalf("state get diagnostics: %#v", diags)
	}
	if state.Type.ValueString() != "tcp" || state.Status.ValueString() != "disabled" || state.MaxConnections.ValueInt64() != 2000 || state.ClientTimeout.ValueInt64() != 45000 {
		t.Fatalf("state not refreshed from API: %#v", state)
	}
}

func TestHaproxyFrontendResourceUpdateSkipsPatchWithoutChanges(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		mu.Unlock()

		if r.Method != http.MethodGet || r.URL.RequestURI() != "/api/v2/services/haproxy/frontends?name=app_frontend" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":42,"name":"app_frontend","type":"http","status":"active"}]}`))
	}))
	t.Cleanup(server.Close)

	frontendResource := &haproxyFrontendResource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyFrontendResourceSchema(t)
	model := nullHaproxyFrontendModel()
	model.ID = types.StringValue("app_frontend")
	model.Name = types.StringValue("app_frontend")
	model.Type = types.StringValue("http")
	model.Status = types.StringValue("active")

	resp := resource.UpdateResponse{
		State: testResourceState(t, schema, model),
	}
	frontendResource.Update(context.Background(), resource.UpdateRequest{
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
		"GET /api/v2/services/haproxy/frontends?name=app_frontend",
		"GET /api/v2/services/haproxy/frontends?name=app_frontend",
	}
	if !reflect.DeepEqual(gotRequests, wantRequests) {
		t.Fatalf("requests = %#v, want %#v", gotRequests, wantRequests)
	}
}

func TestHaproxyFrontendResourceDeleteResolvesAPIID(t *testing.T) {
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
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":42,"name":"app_frontend","type":"tcp"}]}`))
		case http.MethodDelete + " /api/v2/services/haproxy/frontend":
			if got := r.URL.Query().Get("id"); got != "42" {
				t.Fatalf("delete id = %q", got)
			}
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":null}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	t.Cleanup(server.Close)

	frontendResource := &haproxyFrontendResource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyFrontendResourceSchema(t)
	stateModel := nullHaproxyFrontendModel()
	stateModel.ID = types.StringValue("app_frontend")
	stateModel.Name = types.StringValue("app_frontend")
	stateModel.Type = types.StringValue("tcp")

	resp := resource.DeleteResponse{
		State: testResourceState(t, schema, stateModel),
	}
	frontendResource.Delete(context.Background(), resource.DeleteRequest{
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
		"DELETE /api/v2/services/haproxy/frontend?id=42",
	}
	if !reflect.DeepEqual(gotRequests, wantRequests) {
		t.Fatalf("requests = %#v, want %#v", gotRequests, wantRequests)
	}
	if !resp.State.Raw.IsNull() {
		t.Fatalf("state was not removed")
	}
}

func TestHaproxyFrontendResourceImportUsesNaturalName(t *testing.T) {
	t.Parallel()

	frontendResource := &haproxyFrontendResource{}
	schema := haproxyFrontendResourceSchema(t)

	validResp := resource.ImportStateResponse{
		State: tfsdk.State{Schema: schema},
	}
	frontendResource.ImportState(context.Background(), resource.ImportStateRequest{ID: "app_frontend"}, &validResp)
	if validResp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %#v", validResp.Diagnostics)
	}
	var state haproxyFrontendModel
	diags := validResp.State.Get(context.Background(), &state)
	if diags.HasError() {
		t.Fatalf("state get diagnostics: %#v", diags)
	}
	if state.ID.ValueString() != "app_frontend" || state.Name.ValueString() != "app_frontend" {
		t.Fatalf("imported state = %#v", state)
	}

	for _, id := range []string{"", " ", "a", "app/frontend", "app frontend", "app:frontend"} {
		invalidResp := resource.ImportStateResponse{
			State: tfsdk.State{Schema: schema},
		}
		frontendResource.ImportState(context.Background(), resource.ImportStateRequest{ID: id}, &invalidResp)
		if !invalidResp.Diagnostics.HasError() {
			t.Fatalf("expected diagnostic for import id %q", id)
		}
	}
}

func TestHaproxyFrontendValidation(t *testing.T) {
	t.Parallel()

	valid := nullHaproxyFrontendModel()
	valid.Name = types.StringValue("app_frontend")
	valid.Type = types.StringValue("http")
	valid.Status = types.StringValue("active")
	valid.MaxConnections = types.Int64Value(0)
	valid.ClientTimeout = types.Int64Value(0)
	valid.ForwardFor = types.BoolValue(true)
	valid.HTTPClose = types.StringValue("http-keep-alive")

	if _, err := validateHaproxyFrontendPlan(valid); err != nil {
		t.Fatalf("valid frontend rejected: %v", err)
	}

	tests := map[string]haproxyFrontendModel{
		"short name": func() haproxyFrontendModel {
			model := valid
			model.Name = types.StringValue("a")
			return model
		}(),
		"name slash": func() haproxyFrontendModel {
			model := valid
			model.Name = types.StringValue("app/frontend")
			return model
		}(),
		"name pattern": func() haproxyFrontendModel {
			model := valid
			model.Name = types.StringValue("app frontend")
			return model
		}(),
		"https type deferred": func() haproxyFrontendModel {
			model := valid
			model.Type = types.StringValue("https")
			return model
		}(),
		"bad status": func() haproxyFrontendModel {
			model := valid
			model.Status = types.StringValue("backup")
			return model
		}(),
		"bad httpclose": func() haproxyFrontendModel {
			model := valid
			model.HTTPClose = types.StringValue("invalid")
			return model
		}(),
		"forwardfor tcp": func() haproxyFrontendModel {
			model := valid
			model.Type = types.StringValue("tcp")
			model.ForwardFor = types.BoolValue(true)
			return model
		}(),
		"negative max connections": func() haproxyFrontendModel {
			model := valid
			model.MaxConnections = types.Int64Value(-1)
			return model
		}(),
		"negative client timeout": func() haproxyFrontendModel {
			model := valid
			model.ClientTimeout = types.Int64Value(-1)
			return model
		}(),
	}

	for name, model := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := validateHaproxyFrontendPlan(model); err == nil {
				t.Fatalf("expected validation error")
			}
		})
	}
}

func TestHaproxyFrontendSchemaIsConservative(t *testing.T) {
	schema := haproxyFrontendResourceSchema(t)
	for _, required := range []string{"name", "type"} {
		if _, ok := schema.Attributes[required]; !ok {
			t.Fatalf("resource schema missing %q", required)
		}
	}
	for _, exposed := range []string{"descr", "status", "max_connections", "backend_serverpool", "socket_stats", "dontlognull", "dontlog_normal", "log_separate_errors", "log_detailed", "client_timeout", "forwardfor", "httpclose"} {
		if _, ok := schema.Attributes[exposed]; !ok {
			t.Fatalf("resource schema missing %q", exposed)
		}
	}
	for _, forbidden := range []string{"a_extaddr", "addresses", "ha_acls", "acls", "a_actionitems", "actions", "ha_certificates", "certificates", "ssl_certificate", "a_errorfiles", "error_files", "advanced", "advanced_bind", "ssloffloadcert", "apply", "async"} {
		if _, ok := schema.Attributes[forbidden]; ok {
			t.Fatalf("resource schema should not expose %q before ownership is validated", forbidden)
		}
	}
}

func haproxyFrontendResourceSchema(t *testing.T) resourceschema.Schema {
	t.Helper()

	var resp resource.SchemaResponse
	(&haproxyFrontendResource{}).Schema(context.Background(), resource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected resource schema diagnostics: %#v", resp.Diagnostics)
	}

	return resp.Schema
}
