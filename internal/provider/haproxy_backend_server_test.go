package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/d3vi1/terraform-provider-pfsense-restapi-haproxy/internal/pfsense"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestHaproxyBackendServerResourceCreateUsesParentLookupAndDoesNotApply(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var requests []string
	var postPayload map[string]any
	serverLookupCount := 0

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
		case http.MethodGet + " /api/v2/services/haproxy/backend/servers":
			if got := r.URL.Query().Get("parent_id"); got != "42" {
				t.Fatalf("server lookup parent_id = %q", got)
			}
			if got := r.URL.Query().Get("name"); got != "app01" {
				t.Fatalf("server lookup name = %q", got)
			}
			serverLookupCount++
			if serverLookupCount == 1 {
				_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[]}`))
				return
			}
			_, _ = w.Write([]byte(`{
				"code": 200,
				"status": "ok",
				"data": [{
					"id": 7,
					"name": "app01",
					"address": "10.0.0.10",
					"port": "8080",
					"status": "active",
					"weight": "10",
					"ssl": "yes",
					"sslserververify": false,
					"serverid": 123
				}]
			}`))
		case http.MethodPost + " /api/v2/services/haproxy/backend/server":
			if err := json.NewDecoder(r.Body).Decode(&postPayload); err != nil {
				t.Fatalf("decode POST payload: %v", err)
			}
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":{"id":7}}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	t.Cleanup(server.Close)

	serverResource := &haproxyBackendServerResource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyBackendServerResourceSchema(t)
	plan := nullHaproxyBackendServerModel()
	plan.BackendName = types.StringValue("app_backend")
	plan.Name = types.StringValue("app01")
	plan.Address = types.StringValue("10.0.0.10")
	plan.Port = types.Int64Value(8080)
	plan.Status = types.StringValue("active")
	plan.Weight = types.Int64Value(10)
	plan.SSL = types.BoolValue(true)
	plan.SSLServerVerify = types.BoolValue(false)

	resp := resource.CreateResponse{
		State: testResourceState(t, schema, plan),
	}
	serverResource.Create(context.Background(), resource.CreateRequest{
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
		"GET /api/v2/services/haproxy/backend/servers?name=app01&parent_id=42",
		"POST /api/v2/services/haproxy/backend/server",
		"GET /api/v2/services/haproxy/backend/servers?name=app01&parent_id=42",
	}
	if !reflect.DeepEqual(gotRequests, wantRequests) {
		t.Fatalf("requests = %#v, want %#v", gotRequests, wantRequests)
	}

	if postPayload["parent_id"] != "42" {
		t.Fatalf("POST parent_id = %#v", postPayload["parent_id"])
	}
	if postPayload["name"] != "app01" || postPayload["address"] != "10.0.0.10" {
		t.Fatalf("POST natural fields = %#v", postPayload)
	}
	if postPayload["port"] != float64(8080) {
		t.Fatalf("POST port = %#v", postPayload["port"])
	}
	if postPayload["ssl"] != true {
		t.Fatalf("POST ssl = %#v", postPayload["ssl"])
	}
	for _, forbidden := range []string{"id", "apply", "async", "serverid", "advanced"} {
		if _, ok := postPayload[forbidden]; ok {
			t.Fatalf("POST unexpectedly included %q: %#v", forbidden, postPayload)
		}
	}

	var state haproxyBackendServerModel
	diags := resp.State.Get(context.Background(), &state)
	if diags.HasError() {
		t.Fatalf("state get diagnostics: %#v", diags)
	}
	if state.ID.ValueString() != "app_backend/app01" || state.BackendName.ValueString() != "app_backend" || state.Name.ValueString() != "app01" {
		t.Fatalf("natural key not preserved in state: %#v", state)
	}
	if state.Port.ValueInt64() != 8080 || !state.SSL.ValueBool() || state.ServerID.ValueInt64() != 123 {
		t.Fatalf("state was not refreshed from API defaults: %#v", state)
	}
}

func TestHaproxyBackendServerResourceReadRemovesWhenParentMissing(t *testing.T) {
	t.Parallel()

	var requests []string
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		mu.Unlock()

		if r.Method != http.MethodGet || r.URL.RequestURI() != "/api/v2/services/haproxy/backends?name=missing_backend" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[]}`))
	}))
	t.Cleanup(server.Close)

	serverResource := &haproxyBackendServerResource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyBackendServerResourceSchema(t)
	stateModel := nullHaproxyBackendServerModel()
	stateModel.ID = types.StringValue("missing_backend/app01")
	stateModel.BackendName = types.StringValue("missing_backend")
	stateModel.Name = types.StringValue("app01")

	resp := resource.ReadResponse{
		State: testResourceState(t, schema, stateModel),
	}
	serverResource.Read(context.Background(), resource.ReadRequest{
		State: testResourceState(t, schema, stateModel),
	}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %#v", resp.Diagnostics)
	}
	if !resp.State.Raw.IsNull() {
		t.Fatalf("missing parent backend did not remove child state")
	}

	mu.Lock()
	gotRequests := append([]string(nil), requests...)
	mu.Unlock()
	wantRequests := []string{"GET /api/v2/services/haproxy/backends?name=missing_backend"}
	if !reflect.DeepEqual(gotRequests, wantRequests) {
		t.Fatalf("requests = %#v, want %#v", gotRequests, wantRequests)
	}
}

func TestHaproxyBackendServerResourceUpdateRequeriesParentAndPatchesChangedFieldsOnly(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var requests []string
	var patchPayload map[string]any
	serverLookupCount := 0

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
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":99,"name":"app_backend"}]}`))
		case http.MethodGet + " /api/v2/services/haproxy/backend/servers":
			if got := r.URL.Query().Get("parent_id"); got != "99" {
				t.Fatalf("server lookup parent_id = %q", got)
			}
			if got := r.URL.Query().Get("name"); got != "app01" {
				t.Fatalf("server lookup name = %q", got)
			}
			serverLookupCount++
			address := "10.0.0.10"
			weight := "10"
			if serverLookupCount > 1 {
				address = "10.0.0.11"
				weight = "20"
			}
			_, _ = w.Write([]byte(`{
				"code": 200,
				"status": "ok",
				"data": [{
					"id": "7",
					"name": "app01",
					"address": "` + address + `",
					"port": 8080,
					"status": "backup",
					"weight": "` + weight + `",
					"ssl": true,
					"sslserververify": "yes",
					"serverid": "123"
				}]
			}`))
		case http.MethodPatch + " /api/v2/services/haproxy/backend/server":
			if err := json.NewDecoder(r.Body).Decode(&patchPayload); err != nil {
				t.Fatalf("decode PATCH payload: %v", err)
			}
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":null}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	t.Cleanup(server.Close)

	serverResource := &haproxyBackendServerResource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyBackendServerResourceSchema(t)

	prior := nullHaproxyBackendServerModel()
	prior.ID = types.StringValue("app_backend/app01")
	prior.BackendName = types.StringValue("app_backend")
	prior.Name = types.StringValue("app01")
	prior.Address = types.StringValue("10.0.0.10")
	prior.Port = types.Int64Value(8080)
	prior.Status = types.StringValue("active")
	prior.Weight = types.Int64Value(10)
	prior.SSL = types.BoolValue(false)
	prior.SSLServerVerify = types.BoolValue(false)
	prior.ServerID = types.Int64Value(123)

	plan := prior
	plan.Address = types.StringValue("10.0.0.11")
	plan.Status = types.StringValue("backup")
	plan.Weight = types.Int64Value(20)
	plan.SSL = types.BoolValue(true)
	plan.SSLServerVerify = types.BoolValue(true)

	resp := resource.UpdateResponse{
		State: testResourceState(t, schema, prior),
	}
	serverResource.Update(context.Background(), resource.UpdateRequest{
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
		"GET /api/v2/services/haproxy/backend/servers?name=app01&parent_id=99",
		"PATCH /api/v2/services/haproxy/backend/server",
		"GET /api/v2/services/haproxy/backend/servers?name=app01&parent_id=99",
	}
	if !reflect.DeepEqual(gotRequests, wantRequests) {
		t.Fatalf("requests = %#v, want %#v", gotRequests, wantRequests)
	}

	if patchPayload["parent_id"] != "99" || patchPayload["id"] != "7" {
		t.Fatalf("patch IDs = %#v", patchPayload)
	}
	if patchPayload["address"] != "10.0.0.11" || patchPayload["status"] != "backup" {
		t.Fatalf("patch scalar fields = %#v", patchPayload)
	}
	if patchPayload["weight"] != float64(20) || patchPayload["ssl"] != true || patchPayload["sslserververify"] != true {
		t.Fatalf("patch bool/int fields = %#v", patchPayload)
	}
	for _, forbidden := range []string{"name", "backend_name", "apply", "async", "serverid", "advanced", "port"} {
		if _, ok := patchPayload[forbidden]; ok {
			t.Fatalf("patch unexpectedly included %q: %#v", forbidden, patchPayload)
		}
	}

	var state haproxyBackendServerModel
	diags := resp.State.Get(context.Background(), &state)
	if diags.HasError() {
		t.Fatalf("state get diagnostics: %#v", diags)
	}
	if state.Address.ValueString() != "10.0.0.11" || state.Status.ValueString() != "backup" || state.Weight.ValueInt64() != 20 {
		t.Fatalf("state not refreshed from API: %#v", state)
	}
}

func TestHaproxyBackendServerResourceUpdateErrorsWhenParentMissing(t *testing.T) {
	t.Parallel()

	var requests []string
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		mu.Unlock()

		if r.Method != http.MethodGet || r.URL.RequestURI() != "/api/v2/services/haproxy/backends?name=missing_backend" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[]}`))
	}))
	t.Cleanup(server.Close)

	serverResource := &haproxyBackendServerResource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyBackendServerResourceSchema(t)
	prior := nullHaproxyBackendServerModel()
	prior.ID = types.StringValue("missing_backend/app01")
	prior.BackendName = types.StringValue("missing_backend")
	prior.Name = types.StringValue("app01")
	prior.Address = types.StringValue("10.0.0.10")
	prior.Port = types.Int64Value(8080)
	plan := prior
	plan.Address = types.StringValue("10.0.0.11")

	var resp resource.UpdateResponse
	resp.State = testResourceState(t, schema, prior)
	serverResource.Update(context.Background(), resource.UpdateRequest{
		Plan:  testResourcePlan(t, schema, plan),
		State: testResourceState(t, schema, prior),
	}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatalf("expected missing parent diagnostic")
	}
	if !strings.Contains(diagnosticsText(resp.Diagnostics), "Parent backend") {
		t.Fatalf("diagnostics did not describe missing parent: %s", diagnosticsText(resp.Diagnostics))
	}

	mu.Lock()
	gotRequests := append([]string(nil), requests...)
	mu.Unlock()
	wantRequests := []string{"GET /api/v2/services/haproxy/backends?name=missing_backend"}
	if !reflect.DeepEqual(gotRequests, wantRequests) {
		t.Fatalf("requests = %#v, want %#v", gotRequests, wantRequests)
	}
}

func TestHaproxyBackendServerResourceDeleteResolvesCurrentParentAndServerIDs(t *testing.T) {
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
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":99,"name":"app_backend"}]}`))
		case http.MethodGet + " /api/v2/services/haproxy/backend/servers":
			if got := r.URL.Query().Get("parent_id"); got != "99" {
				t.Fatalf("server lookup parent_id = %q", got)
			}
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":7,"name":"app01","address":"10.0.0.10","port":8080}]}`))
		case http.MethodDelete + " /api/v2/services/haproxy/backend/server":
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

	serverResource := &haproxyBackendServerResource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyBackendServerResourceSchema(t)
	stateModel := nullHaproxyBackendServerModel()
	stateModel.ID = types.StringValue("app_backend/app01")
	stateModel.BackendName = types.StringValue("app_backend")
	stateModel.Name = types.StringValue("app01")

	resp := resource.DeleteResponse{
		State: testResourceState(t, schema, stateModel),
	}
	serverResource.Delete(context.Background(), resource.DeleteRequest{
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
		"GET /api/v2/services/haproxy/backend/servers?name=app01&parent_id=99",
		"DELETE /api/v2/services/haproxy/backend/server?id=7&parent_id=99",
	}
	if !reflect.DeepEqual(gotRequests, wantRequests) {
		t.Fatalf("requests = %#v, want %#v", gotRequests, wantRequests)
	}
	if !resp.State.Raw.IsNull() {
		t.Fatalf("state was not removed")
	}
}

func TestHaproxyBackendServerResourceDeleteRemovesStateWhenParentAlreadyMissing(t *testing.T) {
	t.Parallel()

	var requests []string
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		mu.Unlock()

		if r.Method != http.MethodGet || r.URL.RequestURI() != "/api/v2/services/haproxy/backends?name=missing_backend" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[]}`))
	}))
	t.Cleanup(server.Close)

	serverResource := &haproxyBackendServerResource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyBackendServerResourceSchema(t)
	stateModel := nullHaproxyBackendServerModel()
	stateModel.ID = types.StringValue("missing_backend/app01")
	stateModel.BackendName = types.StringValue("missing_backend")
	stateModel.Name = types.StringValue("app01")

	resp := resource.DeleteResponse{
		State: testResourceState(t, schema, stateModel),
	}
	serverResource.Delete(context.Background(), resource.DeleteRequest{
		State: testResourceState(t, schema, stateModel),
	}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %#v", resp.Diagnostics)
	}
	if !resp.State.Raw.IsNull() {
		t.Fatalf("state was not removed")
	}

	mu.Lock()
	gotRequests := append([]string(nil), requests...)
	mu.Unlock()
	wantRequests := []string{"GET /api/v2/services/haproxy/backends?name=missing_backend"}
	if !reflect.DeepEqual(gotRequests, wantRequests) {
		t.Fatalf("requests = %#v, want %#v", gotRequests, wantRequests)
	}
}

func TestHaproxyBackendServerResourceImportUsesBackendAndServerNaturalNames(t *testing.T) {
	t.Parallel()

	serverResource := &haproxyBackendServerResource{}
	schema := haproxyBackendServerResourceSchema(t)

	validResp := resource.ImportStateResponse{
		State: tfsdk.State{Schema: schema},
	}
	serverResource.ImportState(context.Background(), resource.ImportStateRequest{ID: "app_backend/app01"}, &validResp)
	if validResp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %#v", validResp.Diagnostics)
	}
	var state haproxyBackendServerModel
	diags := validResp.State.Get(context.Background(), &state)
	if diags.HasError() {
		t.Fatalf("state get diagnostics: %#v", diags)
	}
	if state.ID.ValueString() != "app_backend/app01" || state.BackendName.ValueString() != "app_backend" || state.Name.ValueString() != "app01" {
		t.Fatalf("imported state = %#v", state)
	}

	for _, id := range []string{"", "app_backend", "app_backend/", "/app01", "app_backend/app01/extra", "app_backend/a"} {
		invalidResp := resource.ImportStateResponse{
			State: tfsdk.State{Schema: schema},
		}
		serverResource.ImportState(context.Background(), resource.ImportStateRequest{ID: id}, &invalidResp)
		if !invalidResp.Diagnostics.HasError() {
			t.Fatalf("expected diagnostic for import id %q", id)
		}
	}
}

func TestHaproxyBackendServerSchemaIsConservative(t *testing.T) {
	schema := haproxyBackendServerResourceSchema(t)
	for _, required := range []string{"backend_name", "name", "address", "port"} {
		if _, ok := schema.Attributes[required]; !ok {
			t.Fatalf("resource schema missing %q", required)
		}
	}
	for _, forbidden := range []string{"advanced", "apply", "async", "parent_id", "server_id", "stats_password"} {
		if _, ok := schema.Attributes[forbidden]; ok {
			t.Fatalf("resource schema should not expose %q before ownership is validated", forbidden)
		}
	}
}

func haproxyBackendServerResourceSchema(t *testing.T) resourceschema.Schema {
	t.Helper()

	var resp resource.SchemaResponse
	(&haproxyBackendServerResource{}).Schema(context.Background(), resource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected resource schema diagnostics: %#v", resp.Diagnostics)
	}

	return resp.Schema
}
