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

func TestHaproxyBackendActionResourceCreateMatchesPayloadAndDoesNotSendKey(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var requests []string
	var postPayload map[string]any
	actionLookups := 0

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
		case http.MethodGet + " /api/v2/services/haproxy/backend/actions":
			if got := r.URL.Query().Get("parent_id"); got != "42" {
				t.Fatalf("action lookup parent_id = %q", got)
			}
			actionLookups++
			if actionLookups == 1 {
				_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[]}`))
				return
			}
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":7,"action":"use_server","acl":"host_acl","server":"app01"},{"id":8,"action":"http-request_allow","acl":"host_acl"}]}`))
		case http.MethodPost + " /api/v2/services/haproxy/backend/action":
			if err := json.NewDecoder(r.Body).Decode(&postPayload); err != nil {
				t.Fatalf("decode POST payload: %v", err)
			}
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":{"id":7}}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	t.Cleanup(server.Close)

	actionResource := &haproxyBackendActionResource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyBackendActionResourceSchema(t)
	plan := nullHaproxyBackendActionModel()
	plan.BackendName = types.StringValue("app_backend")
	plan.Key = types.StringValue("route_app01")
	plan.Action = types.StringValue("use_server")
	plan.ACL = types.StringValue("host_acl")
	plan.Server = types.StringValue("app01")
	plan.Position = types.Int64Value(0)

	resp := resource.CreateResponse{
		State: testResourceState(t, schema, plan),
	}
	actionResource.Create(context.Background(), resource.CreateRequest{
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
		"GET /api/v2/services/haproxy/backend/actions?parent_id=42",
		"POST /api/v2/services/haproxy/backend/action",
		"GET /api/v2/services/haproxy/backend/actions?parent_id=42",
	}
	if !reflect.DeepEqual(gotRequests, wantRequests) {
		t.Fatalf("requests = %#v, want %#v", gotRequests, wantRequests)
	}

	if postPayload["parent_id"] != "42" || postPayload["action"] != "use_server" || postPayload["acl"] != "host_acl" || postPayload["server"] != "app01" {
		t.Fatalf("POST action payload = %#v", postPayload)
	}
	if postPayload["placement"] != float64(0) {
		t.Fatalf("POST placement = %#v", postPayload["placement"])
	}
	for _, forbidden := range []string{"id", "key", "apply", "async"} {
		if _, ok := postPayload[forbidden]; ok {
			t.Fatalf("POST unexpectedly included %q: %#v", forbidden, postPayload)
		}
	}

	var state haproxyBackendActionModel
	diags := resp.State.Get(context.Background(), &state)
	if diags.HasError() {
		t.Fatalf("state get diagnostics: %#v", diags)
	}
	if state.ID.ValueString() != "app_backend/route_app01" || state.Key.ValueString() != "route_app01" || state.Position.ValueInt64() != 0 {
		t.Fatalf("identity state not preserved: %#v", state)
	}
	if state.Action.ValueString() != "use_server" || state.Server.ValueString() != "app01" {
		t.Fatalf("payload state not refreshed: %#v", state)
	}
}

func TestHaproxyBackendActionResourceReadMatchesHTTPActionWithInternalNames(t *testing.T) {
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
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":42,"name":"app_backend"}]}`))
		case http.MethodGet + " /api/v2/services/haproxy/backend/actions":
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":4,"action":"use_server","acl":"other_acl","server":"app02"},{"id":9,"action":"http-request_set-header","acl":"host_acl","http-request_set-headername":"X-Forwarded-Proto","http-request_set-headerfmt":"https"}]}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	t.Cleanup(server.Close)

	actionResource := &haproxyBackendActionResource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyBackendActionResourceSchema(t)
	stateModel := nullHaproxyBackendActionModel()
	stateModel.ID = types.StringValue("app_backend/set_proto")
	stateModel.BackendName = types.StringValue("app_backend")
	stateModel.Key = types.StringValue("set_proto")
	stateModel.Action = types.StringValue("http-request_set-header")
	stateModel.ACL = types.StringValue("host_acl")
	stateModel.Name = types.StringValue("X-Forwarded-Proto")
	stateModel.Fmt = types.StringValue("https")

	resp := resource.ReadResponse{
		State: testResourceState(t, schema, stateModel),
	}
	actionResource.Read(context.Background(), resource.ReadRequest{
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
		"GET /api/v2/services/haproxy/backend/actions?parent_id=42",
	}
	if !reflect.DeepEqual(gotRequests, wantRequests) {
		t.Fatalf("requests = %#v, want %#v", gotRequests, wantRequests)
	}

	var state haproxyBackendActionModel
	diags := resp.State.Get(context.Background(), &state)
	if diags.HasError() {
		t.Fatalf("state get diagnostics: %#v", diags)
	}
	if state.Action.ValueString() != "http-request_set-header" || state.Name.ValueString() != "X-Forwarded-Proto" || state.Fmt.ValueString() != "https" {
		t.Fatalf("HTTP action was not read from dynamic internal names: %#v", state)
	}
	if state.Position.ValueInt64() != 1 {
		t.Fatalf("position = %d, want 1", state.Position.ValueInt64())
	}
}

func TestHaproxyBackendActionResourceReadSkipsUnsupportedSiblingActions(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case http.MethodGet + " /api/v2/services/haproxy/backends":
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":42,"name":"app_backend"}]}`))
		case http.MethodGet + " /api/v2/services/haproxy/backend/actions":
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":4,"action":"future-action","acl":"other_acl","future-field":"value"},{"id":7,"action":"use_server","acl":"host_acl","server":"app01"}]}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	t.Cleanup(server.Close)

	actionResource := &haproxyBackendActionResource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyBackendActionResourceSchema(t)
	stateModel := backendUseServerActionModel()

	resp := resource.ReadResponse{
		State: testResourceState(t, schema, stateModel),
	}
	actionResource.Read(context.Background(), resource.ReadRequest{
		State: testResourceState(t, schema, stateModel),
	}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %#v", resp.Diagnostics)
	}

	var state haproxyBackendActionModel
	diags := resp.State.Get(context.Background(), &state)
	if diags.HasError() {
		t.Fatalf("state get diagnostics: %#v", diags)
	}
	if state.ID.ValueString() != "app_backend/route_app01" || state.Action.ValueString() != "use_server" || state.Server.ValueString() != "app01" {
		t.Fatalf("supported action was not matched after unsupported sibling: %#v", state)
	}
}

func TestHaproxyBackendActionResourceUpdateReordersWithPlacement(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var requests []string
	var patchPayload map[string]any
	actionLookups := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case http.MethodGet + " /api/v2/services/haproxy/backends":
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":42,"name":"app_backend"}]}`))
		case http.MethodGet + " /api/v2/services/haproxy/backend/actions":
			actionLookups++
			if actionLookups == 1 {
				_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":7,"action":"use_server","acl":"host_acl","server":"app01"},{"id":8,"action":"http-request_allow","acl":"host_acl"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":8,"action":"http-request_allow","acl":"host_acl"},{"id":9,"action":"use_server","acl":"host_acl","server":"app01"}]}`))
		case http.MethodPatch + " /api/v2/services/haproxy/backend/action":
			if err := json.NewDecoder(r.Body).Decode(&patchPayload); err != nil {
				t.Fatalf("decode PATCH payload: %v", err)
			}
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":null}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	t.Cleanup(server.Close)

	actionResource := &haproxyBackendActionResource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyBackendActionResourceSchema(t)
	prior := backendUseServerActionModel()
	prior.Position = types.Int64Value(0)
	plan := prior
	plan.Position = types.Int64Value(1)

	resp := resource.UpdateResponse{
		State: testResourceState(t, schema, prior),
	}
	actionResource.Update(context.Background(), resource.UpdateRequest{
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
		"GET /api/v2/services/haproxy/backend/actions?parent_id=42",
		"PATCH /api/v2/services/haproxy/backend/action",
		"GET /api/v2/services/haproxy/backend/actions?parent_id=42",
	}
	if !reflect.DeepEqual(gotRequests, wantRequests) {
		t.Fatalf("requests = %#v, want %#v", gotRequests, wantRequests)
	}

	if patchPayload["parent_id"] != "42" || patchPayload["id"] != "7" || patchPayload["placement"] != float64(1) {
		t.Fatalf("PATCH placement payload = %#v", patchPayload)
	}
	for _, forbidden := range []string{"key", "apply", "async"} {
		if _, ok := patchPayload[forbidden]; ok {
			t.Fatalf("PATCH unexpectedly included %q: %#v", forbidden, patchPayload)
		}
	}
	if len(patchPayload) != 3 {
		t.Fatalf("PATCH should only include ids and placement for reorder, got %#v", patchPayload)
	}

	var state haproxyBackendActionModel
	diags := resp.State.Get(context.Background(), &state)
	if diags.HasError() {
		t.Fatalf("state get diagnostics: %#v", diags)
	}
	if state.Position.ValueInt64() != 1 {
		t.Fatalf("position = %d, want 1", state.Position.ValueInt64())
	}
}

func TestHaproxyBackendActionResourceUpdatePatchesChangedPayloadField(t *testing.T) {
	t.Parallel()

	var patchPayload map[string]any
	actionLookups := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case http.MethodGet + " /api/v2/services/haproxy/backends":
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":42,"name":"app_backend"}]}`))
		case http.MethodGet + " /api/v2/services/haproxy/backend/actions":
			actionLookups++
			if actionLookups == 1 {
				_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":7,"action":"use_server","acl":"host_acl","server":"app01"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":7,"action":"use_server","acl":"host_acl","server":"app02"}]}`))
		case http.MethodPatch + " /api/v2/services/haproxy/backend/action":
			if err := json.NewDecoder(r.Body).Decode(&patchPayload); err != nil {
				t.Fatalf("decode PATCH payload: %v", err)
			}
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":null}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	t.Cleanup(server.Close)

	actionResource := &haproxyBackendActionResource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyBackendActionResourceSchema(t)
	prior := backendUseServerActionModel()
	plan := prior
	plan.Server = types.StringValue("app02")

	resp := resource.UpdateResponse{
		State: testResourceState(t, schema, prior),
	}
	actionResource.Update(context.Background(), resource.UpdateRequest{
		Plan:  testResourcePlan(t, schema, plan),
		State: testResourceState(t, schema, prior),
	}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %#v", resp.Diagnostics)
	}

	if patchPayload["parent_id"] != "42" || patchPayload["id"] != "7" || patchPayload["server"] != "app02" {
		t.Fatalf("PATCH changed payload field = %#v", patchPayload)
	}
	for _, forbidden := range []string{"key", "placement", "apply", "async"} {
		if _, ok := patchPayload[forbidden]; ok {
			t.Fatalf("PATCH unexpectedly included %q: %#v", forbidden, patchPayload)
		}
	}
	if len(patchPayload) != 3 {
		t.Fatalf("PATCH should only include ids and changed server field, got %#v", patchPayload)
	}
}

func TestHaproxyBackendActionResourceUpdatePatchesChangedActionType(t *testing.T) {
	t.Parallel()

	var patchPayload map[string]any
	actionLookups := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case http.MethodGet + " /api/v2/services/haproxy/backends":
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":42,"name":"app_backend"}]}`))
		case http.MethodGet + " /api/v2/services/haproxy/backend/actions":
			actionLookups++
			if actionLookups == 1 {
				_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":7,"action":"use_server","acl":"host_acl","server":"app01"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":7,"action":"custom","acl":"host_acl","customcustomaction":"set-var(req.route) app"}]}`))
		case http.MethodPatch + " /api/v2/services/haproxy/backend/action":
			if err := json.NewDecoder(r.Body).Decode(&patchPayload); err != nil {
				t.Fatalf("decode PATCH payload: %v", err)
			}
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":null}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	t.Cleanup(server.Close)

	actionResource := &haproxyBackendActionResource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyBackendActionResourceSchema(t)
	prior := backendUseServerActionModel()
	plan := prior
	plan.Action = types.StringValue("custom")
	plan.Server = types.StringNull()
	plan.CustomAction = types.StringValue("set-var(req.route) app")

	resp := resource.UpdateResponse{
		State: testResourceState(t, schema, prior),
	}
	actionResource.Update(context.Background(), resource.UpdateRequest{
		Plan:  testResourcePlan(t, schema, plan),
		State: testResourceState(t, schema, prior),
	}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %#v", resp.Diagnostics)
	}

	if patchPayload["parent_id"] != "42" || patchPayload["id"] != "7" || patchPayload["action"] != "custom" || patchPayload["customaction"] != "set-var(req.route) app" {
		t.Fatalf("PATCH changed action type = %#v", patchPayload)
	}
	for _, forbidden := range []string{"key", "server", "placement", "apply", "async"} {
		if _, ok := patchPayload[forbidden]; ok {
			t.Fatalf("PATCH unexpectedly included %q: %#v", forbidden, patchPayload)
		}
	}
}

func TestHaproxyBackendActionResourceImportThenUpdateMatchesPlanPayload(t *testing.T) {
	t.Parallel()

	var patchPayload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case http.MethodGet + " /api/v2/services/haproxy/backends":
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":42,"name":"app_backend"}]}`))
		case http.MethodGet + " /api/v2/services/haproxy/backend/actions":
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":7,"action":"use_server","acl":"host_acl","server":"app01"}]}`))
		case http.MethodPatch + " /api/v2/services/haproxy/backend/action":
			if err := json.NewDecoder(r.Body).Decode(&patchPayload); err != nil {
				t.Fatalf("decode PATCH payload: %v", err)
			}
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":null}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	t.Cleanup(server.Close)

	actionResource := &haproxyBackendActionResource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyBackendActionResourceSchema(t)
	prior := nullHaproxyBackendActionModel()
	prior.ID = types.StringValue("app_backend/route_app01")
	prior.BackendName = types.StringValue("app_backend")
	prior.Key = types.StringValue("route_app01")
	plan := backendUseServerActionModel()

	resp := resource.UpdateResponse{
		State: testResourceState(t, schema, prior),
	}
	actionResource.Update(context.Background(), resource.UpdateRequest{
		Plan:  testResourcePlan(t, schema, plan),
		State: testResourceState(t, schema, prior),
	}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %#v", resp.Diagnostics)
	}
	if patchPayload["parent_id"] != "42" || patchPayload["id"] != "7" || patchPayload["action"] != "use_server" || patchPayload["acl"] != "host_acl" || patchPayload["server"] != "app01" {
		t.Fatalf("PATCH should identify imported action by plan payload and send full payload, got %#v", patchPayload)
	}
	if _, ok := patchPayload["key"]; ok {
		t.Fatalf("PATCH unexpectedly included key: %#v", patchPayload)
	}
}

func TestHaproxyBackendActionResourceDeleteAfterReorderResolvesCurrentChildID(t *testing.T) {
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
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":42,"name":"app_backend"}]}`))
		case http.MethodGet + " /api/v2/services/haproxy/backend/actions":
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":8,"action":"http-request_allow","acl":"host_acl"},{"id":99,"action":"use_server","acl":"host_acl","server":"app01"}]}`))
		case http.MethodDelete + " /api/v2/services/haproxy/backend/action":
			if got := r.URL.Query().Get("parent_id"); got != "42" {
				t.Fatalf("delete parent_id = %q", got)
			}
			if got := r.URL.Query().Get("id"); got != "99" {
				t.Fatalf("delete id = %q", got)
			}
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":null}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	t.Cleanup(server.Close)

	actionResource := &haproxyBackendActionResource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyBackendActionResourceSchema(t)
	stateModel := backendUseServerActionModel()
	stateModel.Position = types.Int64Value(1)

	resp := resource.DeleteResponse{
		State: testResourceState(t, schema, stateModel),
	}
	actionResource.Delete(context.Background(), resource.DeleteRequest{
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
		"GET /api/v2/services/haproxy/backend/actions?parent_id=42",
		"DELETE /api/v2/services/haproxy/backend/action?id=99&parent_id=42",
	}
	if !reflect.DeepEqual(gotRequests, wantRequests) {
		t.Fatalf("requests = %#v, want %#v", gotRequests, wantRequests)
	}
	if !resp.State.Raw.IsNull() {
		t.Fatalf("state was not removed")
	}
}

func TestHaproxyBackendActionResourceDuplicatePayloadDiagnostic(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case http.MethodGet + " /api/v2/services/haproxy/backends":
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":42,"name":"app_backend"}]}`))
		case http.MethodGet + " /api/v2/services/haproxy/backend/actions":
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":7,"action":"use_server","acl":"host_acl","server":"app01"},{"id":8,"action":"use_server","acl":"host_acl","server":"app01"}]}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	t.Cleanup(server.Close)

	actionResource := &haproxyBackendActionResource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyBackendActionResourceSchema(t)
	stateModel := backendUseServerActionModel()

	resp := resource.ReadResponse{
		State: testResourceState(t, schema, stateModel),
	}
	actionResource.Read(context.Background(), resource.ReadRequest{
		State: testResourceState(t, schema, stateModel),
	}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatalf("expected duplicate action diagnostic")
	}
	if !strings.Contains(diagnosticsText(resp.Diagnostics), "multiple HAProxy backend actions matching") {
		t.Fatalf("diagnostics did not describe duplicate action payload: %s", diagnosticsText(resp.Diagnostics))
	}
}

func TestHaproxyBackendActionResourceImportUsesBackendAndKey(t *testing.T) {
	t.Parallel()

	actionResource := &haproxyBackendActionResource{}
	schema := haproxyBackendActionResourceSchema(t)

	validResp := resource.ImportStateResponse{
		State: tfsdk.State{Schema: schema},
	}
	actionResource.ImportState(context.Background(), resource.ImportStateRequest{ID: "app_backend/route_app01"}, &validResp)
	if validResp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %#v", validResp.Diagnostics)
	}
	var state haproxyBackendActionModel
	diags := validResp.State.Get(context.Background(), &state)
	if diags.HasError() {
		t.Fatalf("state get diagnostics: %#v", diags)
	}
	if state.ID.ValueString() != "app_backend/route_app01" || state.BackendName.ValueString() != "app_backend" || state.Key.ValueString() != "route_app01" {
		t.Fatalf("imported state = %#v", state)
	}

	for _, id := range []string{"", "app_backend", "app_backend/", "/route_app01", "app_backend/route/app01", "app_backend/bad key"} {
		invalidResp := resource.ImportStateResponse{
			State: tfsdk.State{Schema: schema},
		}
		actionResource.ImportState(context.Background(), resource.ImportStateRequest{ID: id}, &invalidResp)
		if !invalidResp.Diagnostics.HasError() {
			t.Fatalf("expected diagnostic for import id %q", id)
		}
	}
}

func TestHaproxyBackendActionValidationRejectsIrrelevantFields(t *testing.T) {
	t.Parallel()

	model := backendUseServerActionModel()
	model.Fmt = types.StringValue("https")

	_, _, err := validateHaproxyBackendActionPlan(model)
	if err == nil {
		t.Fatalf("expected irrelevant field validation error")
	}
	if !strings.Contains(err.Error(), "fmt is not applicable") {
		t.Fatalf("error = %v", err)
	}
}

func backendUseServerActionModel() haproxyBackendActionModel {
	model := nullHaproxyBackendActionModel()
	model.ID = types.StringValue("app_backend/route_app01")
	model.BackendName = types.StringValue("app_backend")
	model.Key = types.StringValue("route_app01")
	model.Action = types.StringValue("use_server")
	model.ACL = types.StringValue("host_acl")
	model.Server = types.StringValue("app01")
	return model
}

func haproxyBackendActionResourceSchema(t *testing.T) resourceschema.Schema {
	t.Helper()

	var resp resource.SchemaResponse
	(&haproxyBackendActionResource{}).Schema(context.Background(), resource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected resource schema diagnostics: %#v", resp.Diagnostics)
	}

	return resp.Schema
}
