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

func TestHaproxyFrontendActionResourceCreateMatchesPayloadAndDoesNotSendKey(t *testing.T) {
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
		case http.MethodGet + " /api/v2/services/haproxy/frontends":
			if got := r.URL.Query().Get("name"); got != "app_frontend" {
				t.Fatalf("frontend lookup name = %q", got)
			}
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":42,"name":"app_frontend","type":"http"}]}`))
		case http.MethodGet + " /api/v2/services/haproxy/frontend/actions":
			if got := r.URL.Query().Get("parent_id"); got != "42" {
				t.Fatalf("action lookup parent_id = %q", got)
			}
			actionLookups++
			if actionLookups == 1 {
				_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[]}`))
				return
			}
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":7,"action":"use_backend","acl":"host_acl","backend":"app01"},{"id":8,"action":"http-request_allow","acl":"host_acl"}]}`))
		case http.MethodPost + " /api/v2/services/haproxy/frontend/action":
			if err := json.NewDecoder(r.Body).Decode(&postPayload); err != nil {
				t.Fatalf("decode POST payload: %v", err)
			}
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":{"id":7}}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	t.Cleanup(server.Close)

	actionResource := &haproxyFrontendActionResource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyFrontendActionResourceSchema(t)
	plan := nullHaproxyFrontendActionModel()
	plan.FrontendName = types.StringValue("app_frontend")
	plan.Key = types.StringValue("route_app01")
	plan.Action = types.StringValue("use_backend")
	plan.ACL = types.StringValue("host_acl")
	plan.Backend = types.StringValue("app01")
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
		"GET /api/v2/services/haproxy/frontends?name=app_frontend",
		"GET /api/v2/services/haproxy/frontend/actions?parent_id=42",
		"POST /api/v2/services/haproxy/frontend/action",
		"GET /api/v2/services/haproxy/frontend/actions?parent_id=42",
	}
	if !reflect.DeepEqual(gotRequests, wantRequests) {
		t.Fatalf("requests = %#v, want %#v", gotRequests, wantRequests)
	}

	if postPayload["parent_id"] != "42" || postPayload["action"] != "use_backend" || postPayload["acl"] != "host_acl" || postPayload["backend"] != "app01" {
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

	var state haproxyFrontendActionModel
	diags := resp.State.Get(context.Background(), &state)
	if diags.HasError() {
		t.Fatalf("state get diagnostics: %#v", diags)
	}
	if state.ID.ValueString() != "app_frontend/route_app01" || state.Key.ValueString() != "route_app01" || state.Position.ValueInt64() != 0 {
		t.Fatalf("identity state not preserved: %#v", state)
	}
	if state.Action.ValueString() != "use_backend" || state.Backend.ValueString() != "app01" {
		t.Fatalf("payload state not refreshed: %#v", state)
	}
}

func TestHaproxyFrontendActionResourceReadMatchesHTTPActionWithInternalNames(t *testing.T) {
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
		case http.MethodGet + " /api/v2/services/haproxy/frontend/actions":
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":4,"action":"use_backend","acl":"other_acl","backend":"app02"},{"id":9,"action":"http-request_set-header","acl":"host_acl","http-request_set-headername":"X-Forwarded-Proto","http-request_set-headerfmt":"https"}]}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	t.Cleanup(server.Close)

	actionResource := &haproxyFrontendActionResource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyFrontendActionResourceSchema(t)
	stateModel := nullHaproxyFrontendActionModel()
	stateModel.ID = types.StringValue("app_frontend/set_proto")
	stateModel.FrontendName = types.StringValue("app_frontend")
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
		"GET /api/v2/services/haproxy/frontends?name=app_frontend",
		"GET /api/v2/services/haproxy/frontend/actions?parent_id=42",
	}
	if !reflect.DeepEqual(gotRequests, wantRequests) {
		t.Fatalf("requests = %#v, want %#v", gotRequests, wantRequests)
	}

	var state haproxyFrontendActionModel
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

func TestHaproxyFrontendActionResourceReadSkipsUnsupportedSiblingActions(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case http.MethodGet + " /api/v2/services/haproxy/frontends":
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":42,"name":"app_frontend","type":"http"}]}`))
		case http.MethodGet + " /api/v2/services/haproxy/frontend/actions":
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":4,"action":"future-action","acl":"other_acl","future-field":"value"},{"id":7,"action":"use_backend","acl":"host_acl","backend":"app01"}]}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	t.Cleanup(server.Close)

	actionResource := &haproxyFrontendActionResource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyFrontendActionResourceSchema(t)
	stateModel := frontendUseBackendActionModel()

	resp := resource.ReadResponse{
		State: testResourceState(t, schema, stateModel),
	}
	actionResource.Read(context.Background(), resource.ReadRequest{
		State: testResourceState(t, schema, stateModel),
	}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %#v", resp.Diagnostics)
	}

	var state haproxyFrontendActionModel
	diags := resp.State.Get(context.Background(), &state)
	if diags.HasError() {
		t.Fatalf("state get diagnostics: %#v", diags)
	}
	if state.ID.ValueString() != "app_frontend/route_app01" || state.Action.ValueString() != "use_backend" || state.Backend.ValueString() != "app01" {
		t.Fatalf("supported action was not matched after unsupported sibling: %#v", state)
	}
}

func TestHaproxyFrontendActionResourceUpdateReordersWithPlacement(t *testing.T) {
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
		case http.MethodGet + " /api/v2/services/haproxy/frontends":
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":42,"name":"app_frontend","type":"http"}]}`))
		case http.MethodGet + " /api/v2/services/haproxy/frontend/actions":
			actionLookups++
			if actionLookups == 1 {
				_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":7,"action":"use_backend","acl":"host_acl","backend":"app01"},{"id":8,"action":"http-request_allow","acl":"host_acl"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":8,"action":"http-request_allow","acl":"host_acl"},{"id":9,"action":"use_backend","acl":"host_acl","backend":"app01"}]}`))
		case http.MethodPatch + " /api/v2/services/haproxy/frontend/action":
			if err := json.NewDecoder(r.Body).Decode(&patchPayload); err != nil {
				t.Fatalf("decode PATCH payload: %v", err)
			}
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":null}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	t.Cleanup(server.Close)

	actionResource := &haproxyFrontendActionResource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyFrontendActionResourceSchema(t)
	prior := frontendUseBackendActionModel()
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
		"GET /api/v2/services/haproxy/frontends?name=app_frontend",
		"GET /api/v2/services/haproxy/frontend/actions?parent_id=42",
		"PATCH /api/v2/services/haproxy/frontend/action",
		"GET /api/v2/services/haproxy/frontend/actions?parent_id=42",
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

	var state haproxyFrontendActionModel
	diags := resp.State.Get(context.Background(), &state)
	if diags.HasError() {
		t.Fatalf("state get diagnostics: %#v", diags)
	}
	if state.Position.ValueInt64() != 1 {
		t.Fatalf("position = %d, want 1", state.Position.ValueInt64())
	}
}

func TestHaproxyFrontendActionResourceUpdatePatchesChangedPayloadField(t *testing.T) {
	t.Parallel()

	var patchPayload map[string]any
	actionLookups := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case http.MethodGet + " /api/v2/services/haproxy/frontends":
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":42,"name":"app_frontend","type":"http"}]}`))
		case http.MethodGet + " /api/v2/services/haproxy/frontend/actions":
			actionLookups++
			if actionLookups == 1 {
				_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":7,"action":"use_backend","acl":"host_acl","backend":"app01"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":7,"action":"use_backend","acl":"host_acl","backend":"app02"}]}`))
		case http.MethodPatch + " /api/v2/services/haproxy/frontend/action":
			if err := json.NewDecoder(r.Body).Decode(&patchPayload); err != nil {
				t.Fatalf("decode PATCH payload: %v", err)
			}
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":null}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	t.Cleanup(server.Close)

	actionResource := &haproxyFrontendActionResource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyFrontendActionResourceSchema(t)
	prior := frontendUseBackendActionModel()
	plan := prior
	plan.Backend = types.StringValue("app02")

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

	if patchPayload["parent_id"] != "42" || patchPayload["id"] != "7" || patchPayload["backend"] != "app02" {
		t.Fatalf("PATCH changed payload field = %#v", patchPayload)
	}
	for _, forbidden := range []string{"key", "placement", "apply", "async"} {
		if _, ok := patchPayload[forbidden]; ok {
			t.Fatalf("PATCH unexpectedly included %q: %#v", forbidden, patchPayload)
		}
	}
	if len(patchPayload) != 3 {
		t.Fatalf("PATCH should only include ids and changed backend field, got %#v", patchPayload)
	}
}

func TestHaproxyFrontendActionResourceUpdatePatchesChangedActionType(t *testing.T) {
	t.Parallel()

	var patchPayload map[string]any
	actionLookups := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case http.MethodGet + " /api/v2/services/haproxy/frontends":
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":42,"name":"app_frontend","type":"http"}]}`))
		case http.MethodGet + " /api/v2/services/haproxy/frontend/actions":
			actionLookups++
			if actionLookups == 1 {
				_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":7,"action":"use_backend","acl":"host_acl","backend":"app01"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":7,"action":"custom","acl":"host_acl","customcustomaction":"set-var(req.route) app"}]}`))
		case http.MethodPatch + " /api/v2/services/haproxy/frontend/action":
			if err := json.NewDecoder(r.Body).Decode(&patchPayload); err != nil {
				t.Fatalf("decode PATCH payload: %v", err)
			}
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":null}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	t.Cleanup(server.Close)

	actionResource := &haproxyFrontendActionResource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyFrontendActionResourceSchema(t)
	prior := frontendUseBackendActionModel()
	plan := prior
	plan.Action = types.StringValue("custom")
	plan.Backend = types.StringNull()
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
	for _, forbidden := range []string{"key", "backend", "placement", "apply", "async"} {
		if _, ok := patchPayload[forbidden]; ok {
			t.Fatalf("PATCH unexpectedly included %q: %#v", forbidden, patchPayload)
		}
	}
}

func TestHaproxyFrontendActionResourceImportThenUpdateMatchesPlanPayload(t *testing.T) {
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
		case http.MethodGet + " /api/v2/services/haproxy/frontend/actions":
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":7,"action":"use_backend","acl":"host_acl","backend":"app01"}]}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	t.Cleanup(server.Close)

	actionResource := &haproxyFrontendActionResource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyFrontendActionResourceSchema(t)
	prior := nullHaproxyFrontendActionModel()
	prior.ID = types.StringValue("app_frontend/route_app01")
	prior.FrontendName = types.StringValue("app_frontend")
	prior.Key = types.StringValue("route_app01")
	plan := frontendUseBackendActionModel()

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
		"GET /api/v2/services/haproxy/frontends?name=app_frontend",
		"GET /api/v2/services/haproxy/frontend/actions?parent_id=42",
		"GET /api/v2/services/haproxy/frontend/actions?parent_id=42",
	}
	if !reflect.DeepEqual(gotRequests, wantRequests) {
		t.Fatalf("requests = %#v, want %#v", gotRequests, wantRequests)
	}

	var state haproxyFrontendActionModel
	diags := resp.State.Get(context.Background(), &state)
	if diags.HasError() {
		t.Fatalf("state get diagnostics: %#v", diags)
	}
	if state.ID.ValueString() != "app_frontend/route_app01" || state.Action.ValueString() != "use_backend" || state.Backend.ValueString() != "app01" {
		t.Fatalf("import adoption did not refresh state from matching payload: %#v", state)
	}
}

func TestHaproxyFrontendActionResourceImportThenUpdatePatchesPlannedPosition(t *testing.T) {
	t.Parallel()

	var patchPayload map[string]any
	actionLookups := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case http.MethodGet + " /api/v2/services/haproxy/frontends":
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":42,"name":"app_frontend","type":"http"}]}`))
		case http.MethodGet + " /api/v2/services/haproxy/frontend/actions":
			actionLookups++
			if actionLookups == 1 {
				_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":8,"action":"http-request_allow","acl":"host_acl"},{"id":7,"action":"use_backend","acl":"host_acl","backend":"app01"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":7,"action":"use_backend","acl":"host_acl","backend":"app01"},{"id":8,"action":"http-request_allow","acl":"host_acl"}]}`))
		case http.MethodPatch + " /api/v2/services/haproxy/frontend/action":
			if err := json.NewDecoder(r.Body).Decode(&patchPayload); err != nil {
				t.Fatalf("decode PATCH payload: %v", err)
			}
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":null}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	t.Cleanup(server.Close)

	actionResource := &haproxyFrontendActionResource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyFrontendActionResourceSchema(t)
	prior := nullHaproxyFrontendActionModel()
	prior.ID = types.StringValue("app_frontend/route_app01")
	prior.FrontendName = types.StringValue("app_frontend")
	prior.Key = types.StringValue("route_app01")
	plan := frontendUseBackendActionModel()
	plan.Position = types.Int64Value(0)

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

	if patchPayload["parent_id"] != "42" || patchPayload["id"] != "7" || patchPayload["placement"] != float64(0) {
		t.Fatalf("PATCH imported position update = %#v", patchPayload)
	}
	for _, forbidden := range []string{"key", "action", "acl", "backend", "apply", "async"} {
		if _, ok := patchPayload[forbidden]; ok {
			t.Fatalf("PATCH unexpectedly included %q: %#v", forbidden, patchPayload)
		}
	}
	if len(patchPayload) != 3 {
		t.Fatalf("PATCH should only include ids and placement for imported position update, got %#v", patchPayload)
	}
}

func TestHaproxyFrontendActionResourceDeleteAfterReorderResolvesCurrentChildID(t *testing.T) {
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
		case http.MethodGet + " /api/v2/services/haproxy/frontend/actions":
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":8,"action":"http-request_allow","acl":"host_acl"},{"id":99,"action":"use_backend","acl":"host_acl","backend":"app01"}]}`))
		case http.MethodDelete + " /api/v2/services/haproxy/frontend/action":
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

	actionResource := &haproxyFrontendActionResource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyFrontendActionResourceSchema(t)
	stateModel := frontendUseBackendActionModel()
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
		"GET /api/v2/services/haproxy/frontends?name=app_frontend",
		"GET /api/v2/services/haproxy/frontend/actions?parent_id=42",
		"DELETE /api/v2/services/haproxy/frontend/action?id=99&parent_id=42",
	}
	if !reflect.DeepEqual(gotRequests, wantRequests) {
		t.Fatalf("requests = %#v, want %#v", gotRequests, wantRequests)
	}
	if !resp.State.Raw.IsNull() {
		t.Fatalf("state was not removed")
	}
}

func TestHaproxyFrontendActionResourceDuplicatePayloadDiagnostic(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case http.MethodGet + " /api/v2/services/haproxy/frontends":
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":42,"name":"app_frontend","type":"http"}]}`))
		case http.MethodGet + " /api/v2/services/haproxy/frontend/actions":
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":7,"action":"use_backend","acl":"host_acl","backend":"app01"},{"id":8,"action":"use_backend","acl":"host_acl","backend":"app01"}]}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	t.Cleanup(server.Close)

	actionResource := &haproxyFrontendActionResource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyFrontendActionResourceSchema(t)
	stateModel := frontendUseBackendActionModel()

	resp := resource.ReadResponse{
		State: testResourceState(t, schema, stateModel),
	}
	actionResource.Read(context.Background(), resource.ReadRequest{
		State: testResourceState(t, schema, stateModel),
	}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatalf("expected duplicate action diagnostic")
	}
	if !strings.Contains(diagnosticsText(resp.Diagnostics), "multiple HAProxy frontend actions matching") {
		t.Fatalf("diagnostics did not describe duplicate action payload: %s", diagnosticsText(resp.Diagnostics))
	}
}

func TestHaproxyFrontendActionResourceImportUsesFrontendAndKey(t *testing.T) {
	t.Parallel()

	actionResource := &haproxyFrontendActionResource{}
	schema := haproxyFrontendActionResourceSchema(t)

	validResp := resource.ImportStateResponse{
		State: tfsdk.State{Schema: schema},
	}
	actionResource.ImportState(context.Background(), resource.ImportStateRequest{ID: "app_frontend/route_app01"}, &validResp)
	if validResp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %#v", validResp.Diagnostics)
	}
	var state haproxyFrontendActionModel
	diags := validResp.State.Get(context.Background(), &state)
	if diags.HasError() {
		t.Fatalf("state get diagnostics: %#v", diags)
	}
	if state.ID.ValueString() != "app_frontend/route_app01" || state.FrontendName.ValueString() != "app_frontend" || state.Key.ValueString() != "route_app01" {
		t.Fatalf("imported state = %#v", state)
	}

	for _, id := range []string{"", "app_frontend", "app_frontend/", "/route_app01", "app_frontend/route/app01", "app_frontend/bad key"} {
		invalidResp := resource.ImportStateResponse{
			State: tfsdk.State{Schema: schema},
		}
		actionResource.ImportState(context.Background(), resource.ImportStateRequest{ID: id}, &invalidResp)
		if !invalidResp.Diagnostics.HasError() {
			t.Fatalf("expected diagnostic for import id %q", id)
		}
	}
}

func TestHaproxyFrontendActionValidationRejectsIrrelevantFields(t *testing.T) {
	t.Parallel()

	model := frontendUseBackendActionModel()
	model.Fmt = types.StringValue("https")

	_, _, err := validateHaproxyFrontendActionPlan(model)
	if err == nil {
		t.Fatalf("expected irrelevant field validation error")
	}
	if !strings.Contains(err.Error(), "fmt is not applicable") {
		t.Fatalf("error = %v", err)
	}
}

func frontendUseBackendActionModel() haproxyFrontendActionModel {
	model := nullHaproxyFrontendActionModel()
	model.ID = types.StringValue("app_frontend/route_app01")
	model.FrontendName = types.StringValue("app_frontend")
	model.Key = types.StringValue("route_app01")
	model.Action = types.StringValue("use_backend")
	model.ACL = types.StringValue("host_acl")
	model.Backend = types.StringValue("app01")
	return model
}

func haproxyFrontendActionResourceSchema(t *testing.T) resourceschema.Schema {
	t.Helper()

	var resp resource.SchemaResponse
	(&haproxyFrontendActionResource{}).Schema(context.Background(), resource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected resource schema diagnostics: %#v", resp.Diagnostics)
	}

	return resp.Schema
}
