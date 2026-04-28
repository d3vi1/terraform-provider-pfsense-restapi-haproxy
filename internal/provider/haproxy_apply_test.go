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
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	datasourceschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestHaproxyApplyDataSourceReadPendingUsesGET(t *testing.T) {
	t.Parallel()

	var requests []string
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		mu.Unlock()

		if r.Method != http.MethodGet {
			t.Fatalf("method = %s", r.Method)
		}
		if r.URL.RequestURI() != "/api/v2/services/haproxy/apply" {
			t.Fatalf("request uri = %s", r.URL.RequestURI())
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":{"applied":false}}`))
	}))
	t.Cleanup(server.Close)

	applyDataSource := &haproxyApplyDataSource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	resp := datasource.ReadResponse{
		State: tfsdk.State{Schema: haproxyApplyDataSourceSchema(t)},
	}

	applyDataSource.Read(context.Background(), datasource.ReadRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %#v", resp.Diagnostics)
	}

	mu.Lock()
	gotRequests := append([]string(nil), requests...)
	mu.Unlock()
	if want := []string{"GET /api/v2/services/haproxy/apply"}; !reflect.DeepEqual(gotRequests, want) {
		t.Fatalf("requests = %#v, want %#v", gotRequests, want)
	}

	var state haproxyApplyStatusModel
	diags := resp.State.Get(context.Background(), &state)
	if diags.HasError() {
		t.Fatalf("state get diagnostics: %#v", diags)
	}
	if state.ID.ValueString() != haproxyApplyID {
		t.Fatalf("id = %q", state.ID.ValueString())
	}
	if state.Applied.ValueBool() {
		t.Fatalf("applied = true")
	}
	if !state.Pending.ValueBool() {
		t.Fatalf("pending = false")
	}
	if state.Status.ValueString() != "pending" {
		t.Fatalf("status = %q", state.Status.ValueString())
	}
	if !strings.Contains(state.StatusDetail.ValueString(), "applied=false") {
		t.Fatalf("status detail = %q", state.StatusDetail.ValueString())
	}
}

func TestHaproxyApplyResourceCreatePostsAndPollsUntilDone(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var requests []string
	gets := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		mu.Unlock()

		if r.URL.RequestURI() != "/api/v2/services/haproxy/apply" {
			t.Fatalf("request uri = %s", r.URL.RequestURI())
		}

		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodPost:
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode POST payload: %v", err)
			}
			if payload["async"] != true || len(payload) != 1 {
				t.Fatalf("POST payload = %#v", payload)
			}
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":{"applied":false}}`))
		case http.MethodGet:
			gets++
			if gets == 1 {
				_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":{"applied":false}}`))
				return
			}
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":{"applied":true}}`))
		default:
			t.Fatalf("unexpected method %s", r.Method)
		}
	}))
	t.Cleanup(server.Close)

	applyResource := &haproxyApplyResource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyApplyResourceSchema(t)
	plan := nullHaproxyApplyResourceModel()
	plan.Triggers = testStringMap(t, map[string]string{"settings": "v1"})
	plan.Timeout = types.StringValue("1s")
	plan.PollInterval = types.StringValue("1ns")

	resp := resource.CreateResponse{
		State: testResourceState(t, schema, plan),
	}
	applyResource.Create(context.Background(), resource.CreateRequest{
		Plan: testResourcePlan(t, schema, plan),
	}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %#v", resp.Diagnostics)
	}

	mu.Lock()
	gotRequests := append([]string(nil), requests...)
	mu.Unlock()
	wantRequests := []string{
		"POST /api/v2/services/haproxy/apply",
		"GET /api/v2/services/haproxy/apply",
		"GET /api/v2/services/haproxy/apply",
	}
	if !reflect.DeepEqual(gotRequests, wantRequests) {
		t.Fatalf("requests = %#v, want %#v", gotRequests, wantRequests)
	}

	var state haproxyApplyResourceModel
	diags := resp.State.Get(context.Background(), &state)
	if diags.HasError() {
		t.Fatalf("state get diagnostics: %#v", diags)
	}
	if !state.Applied.ValueBool() || state.Pending.ValueBool() || state.Status.ValueString() != "done" {
		t.Fatalf("state not marked done: %#v", state)
	}
	if state.Timeout.ValueString() != "1s" || state.PollInterval.ValueString() != "1ns" {
		t.Fatalf("polling config not preserved: timeout=%q poll_interval=%q", state.Timeout.ValueString(), state.PollInterval.ValueString())
	}
	if state.Triggers.Elements()["settings"].(types.String).ValueString() != "v1" {
		t.Fatalf("triggers not preserved: %#v", state.Triggers.Elements())
	}
}

func TestHaproxyApplyResourceCreateTimesOutWhenPending(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RequestURI() != "/api/v2/services/haproxy/apply" {
			t.Fatalf("request uri = %s", r.URL.RequestURI())
		}

		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodPost:
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":{"applied":false}}`))
		case http.MethodGet:
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":{"applied":false}}`))
		default:
			t.Fatalf("unexpected method %s", r.Method)
		}
	}))
	t.Cleanup(server.Close)

	applyResource := &haproxyApplyResource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyApplyResourceSchema(t)
	plan := nullHaproxyApplyResourceModel()
	plan.Timeout = types.StringValue("1ns")
	plan.PollInterval = types.StringValue("1ns")

	var resp resource.CreateResponse
	resp.State = testResourceState(t, schema, plan)
	applyResource.Create(context.Background(), resource.CreateRequest{
		Plan: testResourcePlan(t, schema, plan),
	}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatalf("expected pending timeout diagnostic")
	}

	got := diagnosticsText(resp.Diagnostics)
	for _, want := range []string{"pending", "/var/run/haproxy.conf.dirty", "POST /services/haproxy/apply", "HAProxy logs"} {
		if !strings.Contains(got, want) {
			t.Fatalf("diagnostics %q do not contain %q", got, want)
		}
	}
}

func TestHaproxyApplyResourceCreateReportsPostError(t *testing.T) {
	t.Parallel()

	var requests []string
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		mu.Unlock()

		if r.URL.RequestURI() != "/api/v2/services/haproxy/apply" {
			t.Fatalf("request uri = %s", r.URL.RequestURI())
		}
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method %s", r.Method)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{
			"code": 422,
			"status": "error",
			"response_id": "resp-haproxy-apply",
			"message": "invalid HAProxy configuration"
		}`))
	}))
	t.Cleanup(server.Close)

	applyResource := &haproxyApplyResource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyApplyResourceSchema(t)
	plan := nullHaproxyApplyResourceModel()
	plan.Timeout = types.StringValue("1s")
	plan.PollInterval = types.StringValue("1ms")

	var resp resource.CreateResponse
	resp.State = testResourceState(t, schema, plan)
	applyResource.Create(context.Background(), resource.CreateRequest{
		Plan: testResourcePlan(t, schema, plan),
	}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatalf("expected POST error diagnostic")
	}

	mu.Lock()
	gotRequests := append([]string(nil), requests...)
	mu.Unlock()
	if want := []string{"POST /api/v2/services/haproxy/apply"}; !reflect.DeepEqual(gotRequests, want) {
		t.Fatalf("requests = %#v, want %#v", gotRequests, want)
	}

	got := diagnosticsText(resp.Diagnostics)
	for _, want := range []string{"422 Unprocessable Entity", "invalid HAProxy configuration", "response_id: resp-haproxy-apply", "POST /services/haproxy/apply"} {
		if !strings.Contains(got, want) {
			t.Fatalf("diagnostics %q do not contain %q", got, want)
		}
	}
}

func TestHaproxyApplyResourceUpdatePostsOnlyWhenTriggersChange(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		mu.Unlock()

		if r.URL.RequestURI() != "/api/v2/services/haproxy/apply" {
			t.Fatalf("request uri = %s", r.URL.RequestURI())
		}

		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodPost:
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":{"applied":true}}`))
		case http.MethodGet:
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":{"applied":true}}`))
		default:
			t.Fatalf("unexpected method %s", r.Method)
		}
	}))
	t.Cleanup(server.Close)

	applyResource := &haproxyApplyResource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyApplyResourceSchema(t)

	prior := nullHaproxyApplyResourceModel()
	prior.ID = types.StringValue(haproxyApplyID)
	prior.Triggers = testStringMap(t, map[string]string{"settings": "v1"})
	prior.Timeout = types.StringValue("1s")
	prior.PollInterval = types.StringValue("1ms")
	prior.Applied = types.BoolValue(true)
	prior.Pending = types.BoolValue(false)
	prior.Status = types.StringValue("done")
	prior.StatusDetail = types.StringValue("pfSense reports all HAProxy changes are applied (applied=true).")

	sameTriggerPlan := prior
	sameTriggerPlan.Timeout = types.StringValue("2s")

	noPostResp := resource.UpdateResponse{
		State: testResourceState(t, schema, prior),
	}
	applyResource.Update(context.Background(), resource.UpdateRequest{
		Plan:  testResourcePlan(t, schema, sameTriggerPlan),
		State: testResourceState(t, schema, prior),
	}, &noPostResp)
	if noPostResp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics for same trigger update: %#v", noPostResp.Diagnostics)
	}

	changedTriggerPlan := sameTriggerPlan
	changedTriggerPlan.Triggers = testStringMap(t, map[string]string{"settings": "v2"})
	changedResp := resource.UpdateResponse{
		State: testResourceState(t, schema, sameTriggerPlan),
	}
	applyResource.Update(context.Background(), resource.UpdateRequest{
		Plan:  testResourcePlan(t, schema, changedTriggerPlan),
		State: testResourceState(t, schema, sameTriggerPlan),
	}, &changedResp)
	if changedResp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics for changed trigger update: %#v", changedResp.Diagnostics)
	}

	mu.Lock()
	gotRequests := append([]string(nil), requests...)
	mu.Unlock()
	wantRequests := []string{
		"GET /api/v2/services/haproxy/apply",
		"POST /api/v2/services/haproxy/apply",
		"GET /api/v2/services/haproxy/apply",
	}
	if !reflect.DeepEqual(gotRequests, wantRequests) {
		t.Fatalf("requests = %#v, want %#v", gotRequests, wantRequests)
	}
}

func TestHaproxyApplyResourceImportRequiresFixedID(t *testing.T) {
	t.Parallel()

	applyResource := &haproxyApplyResource{}
	schema := haproxyApplyResourceSchema(t)

	validResp := resource.ImportStateResponse{
		State: tfsdk.State{Schema: schema},
	}
	applyResource.ImportState(context.Background(), resource.ImportStateRequest{ID: haproxyApplyID}, &validResp)
	if validResp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %#v", validResp.Diagnostics)
	}

	var state haproxyApplyResourceModel
	diags := validResp.State.Get(context.Background(), &state)
	if diags.HasError() {
		t.Fatalf("state get diagnostics: %#v", diags)
	}
	if state.ID.ValueString() != haproxyApplyID {
		t.Fatalf("imported id = %q", state.ID.ValueString())
	}

	invalidResp := resource.ImportStateResponse{
		State: tfsdk.State{Schema: schema},
	}
	applyResource.ImportState(context.Background(), resource.ImportStateRequest{ID: "haproxy"}, &invalidResp)
	if !invalidResp.Diagnostics.HasError() {
		t.Fatalf("expected diagnostic for non-fixed import id")
	}
}

func haproxyApplyResourceSchema(t *testing.T) resourceschema.Schema {
	t.Helper()

	var resp resource.SchemaResponse
	(&haproxyApplyResource{}).Schema(context.Background(), resource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected resource schema diagnostics: %#v", resp.Diagnostics)
	}

	return resp.Schema
}

func haproxyApplyDataSourceSchema(t *testing.T) datasourceschema.Schema {
	t.Helper()

	var resp datasource.SchemaResponse
	(&haproxyApplyDataSource{}).Schema(context.Background(), datasource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected data source schema diagnostics: %#v", resp.Diagnostics)
	}

	return resp.Schema
}

func testStringMap(t *testing.T, values map[string]string) types.Map {
	t.Helper()

	elements := make(map[string]attr.Value, len(values))
	for key, value := range values {
		elements[key] = types.StringValue(value)
	}

	return types.MapValueMust(types.StringType, elements)
}

func diagnosticsText(diags diag.Diagnostics) string {
	parts := make([]string, 0, len(diags)*2)
	for _, d := range diags {
		parts = append(parts, d.Summary(), d.Detail())
	}
	return strings.Join(parts, "\n")
}
