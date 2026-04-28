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

func TestHaproxyFrontendACLResourceCreateUsesParentLookupPositionAndDoesNotApply(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var requests []string
	var postPayload map[string]any
	filteredACLLookups := 0

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
		case http.MethodGet + " /api/v2/services/haproxy/frontend/acls":
			if got := r.URL.Query().Get("parent_id"); got != "42" {
				t.Fatalf("ACL lookup parent_id = %q", got)
			}
			if r.URL.Query().Get("name") == "host_acl" {
				filteredACLLookups++
				if filteredACLLookups == 1 {
					_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[]}`))
					return
				}
				_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":7,"name":"host_acl","expression":"host_matches","value":"app.example.com","casesensitive":"yes","not":false}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":7,"name":"host_acl","expression":"host_matches","value":"app.example.com","casesensitive":"yes","not":false},{"id":8,"name":"path_acl","expression":"path_starts_with","value":"/api"}]}`))
		case http.MethodPost + " /api/v2/services/haproxy/frontend/acl":
			if err := json.NewDecoder(r.Body).Decode(&postPayload); err != nil {
				t.Fatalf("decode POST payload: %v", err)
			}
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":{"id":7}}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	t.Cleanup(server.Close)

	aclResource := &haproxyFrontendACLResource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyFrontendACLResourceSchema(t)
	plan := nullHaproxyFrontendACLModel()
	plan.FrontendName = types.StringValue("app_frontend")
	plan.Name = types.StringValue("host_acl")
	plan.Expression = types.StringValue("host_matches")
	plan.Value = types.StringValue("app.example.com")
	plan.CaseSensitive = types.BoolValue(true)
	plan.Not = types.BoolValue(false)
	plan.Position = types.Int64Value(0)

	resp := resource.CreateResponse{
		State: testResourceState(t, schema, plan),
	}
	aclResource.Create(context.Background(), resource.CreateRequest{
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
		"GET /api/v2/services/haproxy/frontend/acls?name=host_acl&parent_id=42",
		"POST /api/v2/services/haproxy/frontend/acl",
		"GET /api/v2/services/haproxy/frontend/acls?name=host_acl&parent_id=42",
		"GET /api/v2/services/haproxy/frontend/acls?parent_id=42",
	}
	if !reflect.DeepEqual(gotRequests, wantRequests) {
		t.Fatalf("requests = %#v, want %#v", gotRequests, wantRequests)
	}

	if postPayload["parent_id"] != "42" || postPayload["name"] != "host_acl" {
		t.Fatalf("POST identity fields = %#v", postPayload)
	}
	if postPayload["expression"] != "host_matches" || postPayload["value"] != "app.example.com" {
		t.Fatalf("POST ACL fields = %#v", postPayload)
	}
	if postPayload["casesensitive"] != true || postPayload["not"] != false || postPayload["placement"] != float64(0) {
		t.Fatalf("POST bool/placement fields = %#v", postPayload)
	}
	for _, forbidden := range []string{"id", "apply", "async", "key"} {
		if _, ok := postPayload[forbidden]; ok {
			t.Fatalf("POST unexpectedly included %q: %#v", forbidden, postPayload)
		}
	}

	var state haproxyFrontendACLModel
	diags := resp.State.Get(context.Background(), &state)
	if diags.HasError() {
		t.Fatalf("state get diagnostics: %#v", diags)
	}
	if state.ID.ValueString() != "app_frontend/host_acl" || state.Position.ValueInt64() != 0 || !state.CaseSensitive.ValueBool() {
		t.Fatalf("state was not refreshed from API: %#v", state)
	}
}

func TestHaproxyFrontendACLResourceReadUsesParentLookupAndOrderedPosition(t *testing.T) {
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
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":42,"name":"app_frontend","type":"http"}]}`))
		case http.MethodGet + " /api/v2/services/haproxy/frontend/acls":
			if r.URL.Query().Get("name") == "host_acl" {
				_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":7,"name":"host_acl","expression":"host_matches","value":"","casesensitive":false,"not":"yes"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":1,"name":"first_acl","expression":"path_starts_with","value":"/"},{"id":7,"name":"host_acl","expression":"host_matches","value":"","casesensitive":false,"not":"yes"}]}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	t.Cleanup(server.Close)

	aclResource := &haproxyFrontendACLResource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyFrontendACLResourceSchema(t)
	stateModel := nullHaproxyFrontendACLModel()
	stateModel.ID = types.StringValue("app_frontend/host_acl")
	stateModel.FrontendName = types.StringValue("app_frontend")
	stateModel.Name = types.StringValue("host_acl")

	resp := resource.ReadResponse{
		State: testResourceState(t, schema, stateModel),
	}
	aclResource.Read(context.Background(), resource.ReadRequest{
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
		"GET /api/v2/services/haproxy/frontend/acls?name=host_acl&parent_id=42",
		"GET /api/v2/services/haproxy/frontend/acls?parent_id=42",
	}
	if !reflect.DeepEqual(gotRequests, wantRequests) {
		t.Fatalf("requests = %#v, want %#v", gotRequests, wantRequests)
	}

	var state haproxyFrontendACLModel
	diags := resp.State.Get(context.Background(), &state)
	if diags.HasError() {
		t.Fatalf("state get diagnostics: %#v", diags)
	}
	if state.Value.ValueString() != "" || state.Position.ValueInt64() != 1 || !state.Not.ValueBool() {
		t.Fatalf("state not read from ordered API list: %#v", state)
	}
}

func TestHaproxyFrontendACLResourceUpdatePatchesPlacementAndChangedFields(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var requests []string
	var patchPayload map[string]any
	filteredACLLookups := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case http.MethodGet + " /api/v2/services/haproxy/frontends":
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":99,"name":"app_frontend","type":"http"}]}`))
		case http.MethodGet + " /api/v2/services/haproxy/frontend/acls":
			filtered := r.URL.Query().Get("name") == "host_acl"
			if filtered {
				filteredACLLookups++
				expression := "host_matches"
				value := "app.example.com"
				notValue := false
				if filteredACLLookups > 1 {
					expression = "path_starts_with"
					value = "/api"
					notValue = true
				}
				_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":"7","name":"host_acl","expression":"` + expression + `","value":"` + value + `","casesensitive":false,"not":` + boolJSON(notValue) + `}]}`))
				return
			}
			if filteredACLLookups > 1 {
				_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":1,"name":"first_acl","expression":"path_starts_with","value":"/"},{"id":2,"name":"second_acl","expression":"path_starts_with","value":"/v2"},{"id":7,"name":"host_acl","expression":"path_starts_with","value":"/api","casesensitive":false,"not":true}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":7,"name":"host_acl","expression":"host_matches","value":"app.example.com","casesensitive":false,"not":false}]}`))
		case http.MethodPatch + " /api/v2/services/haproxy/frontend/acl":
			if err := json.NewDecoder(r.Body).Decode(&patchPayload); err != nil {
				t.Fatalf("decode PATCH payload: %v", err)
			}
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":null}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	t.Cleanup(server.Close)

	aclResource := &haproxyFrontendACLResource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyFrontendACLResourceSchema(t)
	prior := nullHaproxyFrontendACLModel()
	prior.ID = types.StringValue("app_frontend/host_acl")
	prior.FrontendName = types.StringValue("app_frontend")
	prior.Name = types.StringValue("host_acl")
	prior.Expression = types.StringValue("host_matches")
	prior.Value = types.StringValue("app.example.com")
	prior.CaseSensitive = types.BoolValue(false)
	prior.Not = types.BoolValue(false)
	prior.Position = types.Int64Value(0)
	plan := prior
	plan.Expression = types.StringValue("path_starts_with")
	plan.Value = types.StringValue("/api")
	plan.Not = types.BoolValue(true)
	plan.Position = types.Int64Value(2)

	resp := resource.UpdateResponse{
		State: testResourceState(t, schema, prior),
	}
	aclResource.Update(context.Background(), resource.UpdateRequest{
		Plan:  testResourcePlan(t, schema, plan),
		State: testResourceState(t, schema, prior),
	}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %#v", resp.Diagnostics)
	}

	if patchPayload["parent_id"] != "99" || patchPayload["id"] != "7" {
		t.Fatalf("PATCH IDs = %#v", patchPayload)
	}
	if patchPayload["expression"] != "path_starts_with" || patchPayload["value"] != "/api" || patchPayload["not"] != true || patchPayload["placement"] != float64(2) {
		t.Fatalf("PATCH changed fields = %#v", patchPayload)
	}
	for _, forbidden := range []string{"name", "frontend_name", "apply", "async", "key"} {
		if _, ok := patchPayload[forbidden]; ok {
			t.Fatalf("PATCH unexpectedly included %q: %#v", forbidden, patchPayload)
		}
	}

	var state haproxyFrontendACLModel
	diags := resp.State.Get(context.Background(), &state)
	if diags.HasError() {
		t.Fatalf("state get diagnostics: %#v", diags)
	}
	if state.Position.ValueInt64() != 2 || state.Expression.ValueString() != "path_starts_with" || !state.Not.ValueBool() {
		t.Fatalf("state not refreshed from API: %#v", state)
	}
}

func TestHaproxyFrontendACLResourceDeleteResolvesCurrentParentAndChildIDs(t *testing.T) {
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
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":99,"name":"app_frontend","type":"http"}]}`))
		case http.MethodGet + " /api/v2/services/haproxy/frontend/acls":
			if r.URL.Query().Get("name") == "host_acl" {
				_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":"77","name":"host_acl","expression":"host_matches","value":"app.example.com"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":77,"name":"host_acl","expression":"host_matches","value":"app.example.com"}]}`))
		case http.MethodDelete + " /api/v2/services/haproxy/frontend/acl":
			if got := r.URL.Query().Get("parent_id"); got != "99" {
				t.Fatalf("delete parent_id = %q", got)
			}
			if got := r.URL.Query().Get("id"); got != "77" {
				t.Fatalf("delete id = %q", got)
			}
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":null}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	t.Cleanup(server.Close)

	aclResource := &haproxyFrontendACLResource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyFrontendACLResourceSchema(t)
	stateModel := nullHaproxyFrontendACLModel()
	stateModel.ID = types.StringValue("app_frontend/host_acl")
	stateModel.FrontendName = types.StringValue("app_frontend")
	stateModel.Name = types.StringValue("host_acl")

	resp := resource.DeleteResponse{
		State: testResourceState(t, schema, stateModel),
	}
	aclResource.Delete(context.Background(), resource.DeleteRequest{
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
		"GET /api/v2/services/haproxy/frontend/acls?name=host_acl&parent_id=99",
		"GET /api/v2/services/haproxy/frontend/acls?parent_id=99",
		"DELETE /api/v2/services/haproxy/frontend/acl?id=77&parent_id=99",
	}
	if !reflect.DeepEqual(gotRequests, wantRequests) {
		t.Fatalf("requests = %#v, want %#v", gotRequests, wantRequests)
	}
	if !resp.State.Raw.IsNull() {
		t.Fatalf("state was not removed")
	}
}

func TestHaproxyFrontendACLResourceDuplicateNamesDiagnostic(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case http.MethodGet + " /api/v2/services/haproxy/frontends":
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":42,"name":"app_frontend","type":"http"}]}`))
		case http.MethodGet + " /api/v2/services/haproxy/frontend/acls":
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":7,"name":"host_acl","expression":"host_matches","value":"a.example.com"},{"id":8,"name":"host_acl","expression":"host_matches","value":"b.example.com"}]}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	t.Cleanup(server.Close)

	aclResource := &haproxyFrontendACLResource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyFrontendACLResourceSchema(t)
	stateModel := nullHaproxyFrontendACLModel()
	stateModel.ID = types.StringValue("app_frontend/host_acl")
	stateModel.FrontendName = types.StringValue("app_frontend")
	stateModel.Name = types.StringValue("host_acl")

	resp := resource.ReadResponse{
		State: testResourceState(t, schema, stateModel),
	}
	aclResource.Read(context.Background(), resource.ReadRequest{
		State: testResourceState(t, schema, stateModel),
	}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatalf("expected duplicate ACL diagnostic")
	}
	if !strings.Contains(diagnosticsText(resp.Diagnostics), "multiple HAProxy frontend ACLs named") {
		t.Fatalf("diagnostics did not describe duplicate ACLs: %s", diagnosticsText(resp.Diagnostics))
	}
}

func TestHaproxyFrontendACLResourceImportUsesFrontendAndACLNames(t *testing.T) {
	t.Parallel()

	aclResource := &haproxyFrontendACLResource{}
	schema := haproxyFrontendACLResourceSchema(t)

	validResp := resource.ImportStateResponse{
		State: tfsdk.State{Schema: schema},
	}
	aclResource.ImportState(context.Background(), resource.ImportStateRequest{ID: "app_frontend/host_acl"}, &validResp)
	if validResp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %#v", validResp.Diagnostics)
	}
	var state haproxyFrontendACLModel
	diags := validResp.State.Get(context.Background(), &state)
	if diags.HasError() {
		t.Fatalf("state get diagnostics: %#v", diags)
	}
	if state.ID.ValueString() != "app_frontend/host_acl" || state.FrontendName.ValueString() != "app_frontend" || state.Name.ValueString() != "host_acl" {
		t.Fatalf("imported state = %#v", state)
	}

	for _, id := range []string{"", "app_frontend", "app_frontend/", "/host_acl", "app_frontend/host_acl/extra", "app_frontend/a"} {
		invalidResp := resource.ImportStateResponse{
			State: tfsdk.State{Schema: schema},
		}
		aclResource.ImportState(context.Background(), resource.ImportStateRequest{ID: id}, &invalidResp)
		if !invalidResp.Diagnostics.HasError() {
			t.Fatalf("expected diagnostic for import id %q", id)
		}
	}
}

func haproxyFrontendACLResourceSchema(t *testing.T) resourceschema.Schema {
	t.Helper()

	var resp resource.SchemaResponse
	(&haproxyFrontendACLResource{}).Schema(context.Background(), resource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected resource schema diagnostics: %#v", resp.Diagnostics)
	}

	return resp.Schema
}
