package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/d3vi1/terraform-provider-pfsense-restapi-haproxy/internal/pfsense"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	datasourceschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestHaproxyBackendDataSourceReadExactMatchDoesNotRequireRESTIDOrWrite(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		mu.Unlock()

		if r.Method != http.MethodGet {
			t.Fatalf("data source made write request %s %s", r.Method, r.URL.RequestURI())
		}
		if r.URL.RequestURI() != "/api/v2/services/haproxy/backends?name=app_backend" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": 200,
			"status": "ok",
			"data": [{
				"name": "other_backend",
				"balance": "leastconn"
			}, {
				"name": "app_backend",
				"balance": "roundrobin",
				"connection_timeout": "15000",
				"server_timeout": 30000,
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
	}))
	t.Cleanup(server.Close)

	backendDataSource := &haproxyBackendDataSource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyBackendDataSourceSchema(t)
	config := nullHaproxyBackendModel()
	config.Name = types.StringValue("app_backend")

	resp := datasource.ReadResponse{
		State: tfsdk.State{Schema: schema},
	}
	backendDataSource.Read(context.Background(), datasource.ReadRequest{
		Config: testDataSourceConfig(t, schema, config),
	}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %#v", resp.Diagnostics)
	}

	mu.Lock()
	gotRequests := append([]string(nil), requests...)
	mu.Unlock()
	wantRequests := []string{"GET /api/v2/services/haproxy/backends?name=app_backend"}
	if !reflect.DeepEqual(gotRequests, wantRequests) {
		t.Fatalf("requests = %#v, want %#v", gotRequests, wantRequests)
	}

	var state haproxyBackendModel
	diags := resp.State.Get(context.Background(), &state)
	if diags.HasError() {
		t.Fatalf("state get diagnostics: %#v", diags)
	}
	if state.ID.ValueString() != "app_backend" || state.Name.ValueString() != "app_backend" {
		t.Fatalf("natural key not preserved in state: %#v", state)
	}
	if state.Balance.ValueString() != "roundrobin" || state.ConnectionTimeout.ValueInt64() != 15000 || state.ServerTimeout.ValueInt64() != 30000 {
		t.Fatalf("backend fields not read: %#v", state)
	}
	if !state.LogHealthChecks.ValueBool() || state.MonitorURI.ValueString() != "/health" {
		t.Fatalf("backend health check fields not read: %#v", state)
	}

	for _, forbidden := range []string{"api_id", "parent_id"} {
		if _, ok := schema.Attributes[forbidden]; ok {
			t.Fatalf("data source schema exposed %q", forbidden)
		}
	}
}

func TestHaproxyBackendDataSourceReadMissingIsDiagnostic(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		mu.Unlock()

		if r.Method != http.MethodGet || r.URL.RequestURI() != "/api/v2/services/haproxy/backends?name=missing_backend" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"name":"other_backend"}]}`))
	}))
	t.Cleanup(server.Close)

	backendDataSource := &haproxyBackendDataSource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyBackendDataSourceSchema(t)
	config := nullHaproxyBackendModel()
	config.Name = types.StringValue("missing_backend")

	var resp datasource.ReadResponse
	resp.State = tfsdk.State{Schema: schema}
	backendDataSource.Read(context.Background(), datasource.ReadRequest{
		Config: testDataSourceConfig(t, schema, config),
	}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatalf("expected missing backend diagnostic")
	}
	if !strings.Contains(diagnosticsText(resp.Diagnostics), "HAProxy backend not found") {
		t.Fatalf("diagnostics did not describe missing backend: %s", diagnosticsText(resp.Diagnostics))
	}

	mu.Lock()
	gotRequests := append([]string(nil), requests...)
	mu.Unlock()
	wantRequests := []string{"GET /api/v2/services/haproxy/backends?name=missing_backend"}
	if !reflect.DeepEqual(gotRequests, wantRequests) {
		t.Fatalf("requests = %#v, want %#v", gotRequests, wantRequests)
	}
}

func TestHaproxyBackendDataSourceReadDuplicateIsDiagnostic(t *testing.T) {
	t.Parallel()

	var requests []string
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		mu.Unlock()

		if r.Method != http.MethodGet || r.URL.RequestURI() != "/api/v2/services/haproxy/backends?name=app_backend" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": 200,
			"status": "ok",
			"data": [
				{"name": "app_backend", "balance": "roundrobin"},
				{"name": "app_backend", "balance": "leastconn"}
			]
		}`))
	}))
	t.Cleanup(server.Close)

	backendDataSource := &haproxyBackendDataSource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyBackendDataSourceSchema(t)
	config := nullHaproxyBackendModel()
	config.Name = types.StringValue("app_backend")

	var resp datasource.ReadResponse
	resp.State = tfsdk.State{Schema: schema}
	backendDataSource.Read(context.Background(), datasource.ReadRequest{
		Config: testDataSourceConfig(t, schema, config),
	}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatalf("expected duplicate backend diagnostic")
	}
	if !strings.Contains(diagnosticsText(resp.Diagnostics), "multiple HAProxy backends named") {
		t.Fatalf("diagnostics did not describe duplicate backend: %s", diagnosticsText(resp.Diagnostics))
	}

	mu.Lock()
	gotRequests := append([]string(nil), requests...)
	mu.Unlock()
	wantRequests := []string{"GET /api/v2/services/haproxy/backends?name=app_backend"}
	if !reflect.DeepEqual(gotRequests, wantRequests) {
		t.Fatalf("requests = %#v, want %#v", gotRequests, wantRequests)
	}
}

func haproxyBackendDataSourceSchema(t *testing.T) datasourceschema.Schema {
	t.Helper()

	var resp datasource.SchemaResponse
	(&haproxyBackendDataSource{}).Schema(context.Background(), datasource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected data source schema diagnostics: %#v", resp.Diagnostics)
	}

	return resp.Schema
}
