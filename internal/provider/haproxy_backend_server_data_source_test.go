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

func TestHaproxyBackendServerDataSourceReadExactMatchDoesNotRequireChildRESTIDOrWrite(t *testing.T) {
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

		w.Header().Set("Content-Type", "application/json")
		switch r.URL.RequestURI() {
		case "/api/v2/services/haproxy/backends?name=app+backend%3Ablue":
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":42,"name":"app backend:blue"}]}`))
		case "/api/v2/services/haproxy/backend/servers?name=app01&parent_id=42":
			_, _ = w.Write([]byte(`{
				"code": 200,
				"status": "ok",
				"data": [{
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
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	t.Cleanup(server.Close)

	serverDataSource := &haproxyBackendServerDataSource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyBackendServerDataSourceSchema(t)
	config := nullHaproxyBackendServerModel()
	config.BackendName = types.StringValue("app backend:blue")
	config.Name = types.StringValue("app01")

	resp := datasource.ReadResponse{
		State: tfsdk.State{Schema: schema},
	}
	serverDataSource.Read(context.Background(), datasource.ReadRequest{
		Config: testDataSourceConfig(t, schema, config),
	}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %#v", resp.Diagnostics)
	}

	mu.Lock()
	gotRequests := append([]string(nil), requests...)
	mu.Unlock()
	wantRequests := []string{
		"GET /api/v2/services/haproxy/backends?name=app+backend%3Ablue",
		"GET /api/v2/services/haproxy/backend/servers?name=app01&parent_id=42",
	}
	if !reflect.DeepEqual(gotRequests, wantRequests) {
		t.Fatalf("requests = %#v, want %#v", gotRequests, wantRequests)
	}

	var state haproxyBackendServerModel
	diags := resp.State.Get(context.Background(), &state)
	if diags.HasError() {
		t.Fatalf("state get diagnostics: %#v", diags)
	}
	if state.ID.ValueString() != "app backend:blue/app01" || state.BackendName.ValueString() != "app backend:blue" || state.Name.ValueString() != "app01" {
		t.Fatalf("natural key not preserved in state: %#v", state)
	}
	if state.Address.ValueString() != "10.0.0.10" || state.Port.ValueInt64() != 8080 || !state.SSL.ValueBool() || state.ServerID.ValueInt64() != 123 {
		t.Fatalf("server fields not read: %#v", state)
	}

	for _, forbidden := range []string{"api_id", "parent_id", "server_id"} {
		if _, ok := schema.Attributes[forbidden]; ok {
			t.Fatalf("data source schema exposed %q", forbidden)
		}
	}
}

func TestHaproxyBackendServerDataSourceReadMissingParentIsDiagnostic(t *testing.T) {
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
		_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[]}`))
	}))
	t.Cleanup(server.Close)

	serverDataSource := &haproxyBackendServerDataSource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyBackendServerDataSourceSchema(t)
	config := nullHaproxyBackendServerModel()
	config.BackendName = types.StringValue("missing_backend")
	config.Name = types.StringValue("app01")

	resp := datasource.ReadResponse{State: tfsdk.State{Schema: schema}}
	serverDataSource.Read(context.Background(), datasource.ReadRequest{
		Config: testDataSourceConfig(t, schema, config),
	}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatalf("expected missing parent diagnostic")
	}
	if !strings.Contains(diagnosticsText(resp.Diagnostics), "HAProxy backend not found") {
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

func TestHaproxyBackendServerDataSourceReadMissingChildIsDiagnostic(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch r.URL.RequestURI() {
		case "/api/v2/services/haproxy/backends?name=app_backend":
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":42,"name":"app_backend"}]}`))
		case "/api/v2/services/haproxy/backend/servers?name=missing01&parent_id=42":
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[]}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	t.Cleanup(server.Close)

	serverDataSource := &haproxyBackendServerDataSource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyBackendServerDataSourceSchema(t)
	config := nullHaproxyBackendServerModel()
	config.BackendName = types.StringValue("app_backend")
	config.Name = types.StringValue("missing01")

	resp := datasource.ReadResponse{State: tfsdk.State{Schema: schema}}
	serverDataSource.Read(context.Background(), datasource.ReadRequest{
		Config: testDataSourceConfig(t, schema, config),
	}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatalf("expected missing child diagnostic")
	}
	if !strings.Contains(diagnosticsText(resp.Diagnostics), "HAProxy backend server not found") {
		t.Fatalf("diagnostics did not describe missing child: %s", diagnosticsText(resp.Diagnostics))
	}

	mu.Lock()
	gotRequests := append([]string(nil), requests...)
	mu.Unlock()
	wantRequests := []string{
		"GET /api/v2/services/haproxy/backends?name=app_backend",
		"GET /api/v2/services/haproxy/backend/servers?name=missing01&parent_id=42",
	}
	if !reflect.DeepEqual(gotRequests, wantRequests) {
		t.Fatalf("requests = %#v, want %#v", gotRequests, wantRequests)
	}
}

func TestHaproxyBackendServerDataSourceReadDuplicateChildIsDiagnostic(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch r.URL.RequestURI() {
		case "/api/v2/services/haproxy/backends?name=app_backend":
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":42,"name":"app_backend"}]}`))
		case "/api/v2/services/haproxy/backend/servers?name=app01&parent_id=42":
			_, _ = w.Write([]byte(`{
				"code": 200,
				"status": "ok",
				"data": [
					{"name": "app01", "address": "10.0.0.10", "port": 8080},
					{"name": "app01", "address": "10.0.0.11", "port": 8080}
				]
			}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	t.Cleanup(server.Close)

	serverDataSource := &haproxyBackendServerDataSource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyBackendServerDataSourceSchema(t)
	config := nullHaproxyBackendServerModel()
	config.BackendName = types.StringValue("app_backend")
	config.Name = types.StringValue("app01")

	resp := datasource.ReadResponse{State: tfsdk.State{Schema: schema}}
	serverDataSource.Read(context.Background(), datasource.ReadRequest{
		Config: testDataSourceConfig(t, schema, config),
	}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatalf("expected duplicate child diagnostic")
	}
	if !strings.Contains(diagnosticsText(resp.Diagnostics), "multiple HAProxy backend servers named") {
		t.Fatalf("diagnostics did not describe duplicate child: %s", diagnosticsText(resp.Diagnostics))
	}
}

func haproxyBackendServerDataSourceSchema(t *testing.T) datasourceschema.Schema {
	t.Helper()

	var resp datasource.SchemaResponse
	(&haproxyBackendServerDataSource{}).Schema(context.Background(), datasource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected data source schema diagnostics: %#v", resp.Diagnostics)
	}

	return resp.Schema
}
