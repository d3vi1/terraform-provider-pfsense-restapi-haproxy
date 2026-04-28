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

func TestHaproxyFrontendCertificateResourceCreateUsesParentLookupAndDoesNotApply(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var requests []string
	var postPayload map[string]any
	certificateLookupCount := 0

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
		case http.MethodGet + " /api/v2/services/haproxy/frontend/certificates":
			assertFrontendCertificateLookupQuery(t, r, "42", "existing_cert_ref")
			certificateLookupCount++
			if certificateLookupCount == 1 {
				_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":6,"parent_id":42,"ssl_certificate":"other_cert_ref"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{
				"code": 200,
				"status": "ok",
				"data": [
					{"id": 6, "parent_id": 42, "ssl_certificate": "other_cert_ref"},
					{"id": 7, "parent_id": 42, "ssl_certificate": "existing_cert_ref"}
				]
			}`))
		case http.MethodPost + " /api/v2/services/haproxy/frontend/certificate":
			if err := json.NewDecoder(r.Body).Decode(&postPayload); err != nil {
				t.Fatalf("decode POST payload: %v", err)
			}
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":{"id":7}}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	t.Cleanup(server.Close)

	certificateResource := &haproxyFrontendCertificateResource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyFrontendCertificateResourceSchema(t)
	plan := nullHaproxyFrontendCertificateModel()
	plan.FrontendName = types.StringValue("app_frontend")
	plan.SSLCertificate = types.StringValue("existing_cert_ref")

	resp := resource.CreateResponse{
		State: testResourceState(t, schema, plan),
	}
	certificateResource.Create(context.Background(), resource.CreateRequest{
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
		"GET /api/v2/services/haproxy/frontend/certificates?parent_id=42&ssl_certificate=existing_cert_ref",
		"POST /api/v2/services/haproxy/frontend/certificate",
		"GET /api/v2/services/haproxy/frontend/certificates?parent_id=42&ssl_certificate=existing_cert_ref",
	}
	if !reflect.DeepEqual(gotRequests, wantRequests) {
		t.Fatalf("requests = %#v, want %#v", gotRequests, wantRequests)
	}

	if postPayload["parent_id"] != "42" || postPayload["ssl_certificate"] != "existing_cert_ref" {
		t.Fatalf("POST payload = %#v", postPayload)
	}
	for _, forbidden := range []string{"id", "apply", "async", "certificate", "cert", "crt", "pem", "private_key", "key", "content"} {
		if _, ok := postPayload[forbidden]; ok {
			t.Fatalf("POST unexpectedly included %q: %#v", forbidden, postPayload)
		}
	}

	var state haproxyFrontendCertificateModel
	diags := resp.State.Get(context.Background(), &state)
	if diags.HasError() {
		t.Fatalf("state get diagnostics: %#v", diags)
	}
	if state.ID.ValueString() != "app_frontend/existing_cert_ref" || state.FrontendName.ValueString() != "app_frontend" || state.SSLCertificate.ValueString() != "existing_cert_ref" {
		t.Fatalf("natural key not preserved in state: %#v", state)
	}
}

func TestHaproxyFrontendCertificateResourceCreateRejectsDuplicate(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case http.MethodGet + " /api/v2/services/haproxy/frontends":
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":42,"name":"app_frontend","type":"http"}]}`))
		case http.MethodGet + " /api/v2/services/haproxy/frontend/certificates":
			assertFrontendCertificateLookupQuery(t, r, "42", "existing_cert_ref")
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":7,"ssl_certificate":"existing_cert_ref"}]}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	t.Cleanup(server.Close)

	certificateResource := &haproxyFrontendCertificateResource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyFrontendCertificateResourceSchema(t)
	plan := nullHaproxyFrontendCertificateModel()
	plan.FrontendName = types.StringValue("app_frontend")
	plan.SSLCertificate = types.StringValue("existing_cert_ref")

	var resp resource.CreateResponse
	resp.State = testResourceState(t, schema, plan)
	certificateResource.Create(context.Background(), resource.CreateRequest{
		Plan: testResourcePlan(t, schema, plan),
	}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatalf("expected duplicate certificate diagnostic")
	}
	if requests.Load() != 2 {
		t.Fatalf("requests = %d", requests.Load())
	}
	if !strings.Contains(diagnosticsText(resp.Diagnostics), "terraform import") {
		t.Fatalf("diagnostics did not include import guidance: %s", diagnosticsText(resp.Diagnostics))
	}
}

func TestHaproxyFrontendCertificateResourceCreateErrorsWhenParentMissing(t *testing.T) {
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

	certificateResource := &haproxyFrontendCertificateResource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyFrontendCertificateResourceSchema(t)
	plan := nullHaproxyFrontendCertificateModel()
	plan.FrontendName = types.StringValue("missing_frontend")
	plan.SSLCertificate = types.StringValue("existing_cert_ref")

	var resp resource.CreateResponse
	resp.State = testResourceState(t, schema, plan)
	certificateResource.Create(context.Background(), resource.CreateRequest{
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

func TestHaproxyFrontendCertificateResourceReadRemovesWhenChildMissing(t *testing.T) {
	t.Parallel()

	var requests []string
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case http.MethodGet + " /api/v2/services/haproxy/frontends":
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":42,"name":"app_frontend","type":"http"}]}`))
		case http.MethodGet + " /api/v2/services/haproxy/frontend/certificates":
			assertFrontendCertificateLookupQuery(t, r, "42", "existing_cert_ref")
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[]}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	t.Cleanup(server.Close)

	certificateResource := &haproxyFrontendCertificateResource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyFrontendCertificateResourceSchema(t)
	stateModel := nullHaproxyFrontendCertificateModel()
	stateModel.ID = types.StringValue("app_frontend/existing_cert_ref")
	stateModel.FrontendName = types.StringValue("app_frontend")
	stateModel.SSLCertificate = types.StringValue("existing_cert_ref")

	resp := resource.ReadResponse{
		State: testResourceState(t, schema, stateModel),
	}
	certificateResource.Read(context.Background(), resource.ReadRequest{
		State: testResourceState(t, schema, stateModel),
	}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %#v", resp.Diagnostics)
	}
	if !resp.State.Raw.IsNull() {
		t.Fatalf("missing certificate did not remove child state")
	}

	mu.Lock()
	gotRequests := append([]string(nil), requests...)
	mu.Unlock()
	wantRequests := []string{
		"GET /api/v2/services/haproxy/frontends?name=app_frontend",
		"GET /api/v2/services/haproxy/frontend/certificates?parent_id=42&ssl_certificate=existing_cert_ref",
	}
	if !reflect.DeepEqual(gotRequests, wantRequests) {
		t.Fatalf("requests = %#v, want %#v", gotRequests, wantRequests)
	}
}

func TestHaproxyFrontendCertificateResourceUpdateDoesNotPatch(t *testing.T) {
	t.Parallel()

	certificateResource := &haproxyFrontendCertificateResource{}
	var resp resource.UpdateResponse
	certificateResource.Update(context.Background(), resource.UpdateRequest{}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatalf("expected update diagnostic")
	}
	if !strings.Contains(diagnosticsText(resp.Diagnostics), "require replacement") {
		t.Fatalf("diagnostics did not describe replacement-only behavior: %s", diagnosticsText(resp.Diagnostics))
	}
}

func TestHaproxyFrontendCertificateResourceDeleteResolvesCurrentParentAndCertificateIDs(t *testing.T) {
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
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":99,"name":"app_frontend","type":"http"}]}`))
		case http.MethodGet + " /api/v2/services/haproxy/frontend/certificates":
			assertFrontendCertificateLookupQuery(t, r, "99", "existing_cert_ref")
			_, _ = w.Write([]byte(`{"code":200,"status":"ok","data":[{"id":7,"ssl_certificate":"existing_cert_ref"}]}`))
		case http.MethodDelete + " /api/v2/services/haproxy/frontend/certificate":
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

	certificateResource := &haproxyFrontendCertificateResource{
		client: pfsense.NewClient(pfsense.Config{Endpoint: server.URL, APIKey: "test-key"}),
	}
	schema := haproxyFrontendCertificateResourceSchema(t)
	stateModel := nullHaproxyFrontendCertificateModel()
	stateModel.ID = types.StringValue("app_frontend/existing_cert_ref")
	stateModel.FrontendName = types.StringValue("app_frontend")
	stateModel.SSLCertificate = types.StringValue("existing_cert_ref")

	resp := resource.DeleteResponse{
		State: testResourceState(t, schema, stateModel),
	}
	certificateResource.Delete(context.Background(), resource.DeleteRequest{
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
		"GET /api/v2/services/haproxy/frontend/certificates?parent_id=99&ssl_certificate=existing_cert_ref",
		"DELETE /api/v2/services/haproxy/frontend/certificate?id=7&parent_id=99",
	}
	if !reflect.DeepEqual(gotRequests, wantRequests) {
		t.Fatalf("requests = %#v, want %#v", gotRequests, wantRequests)
	}
	if !resp.State.Raw.IsNull() {
		t.Fatalf("state was not removed")
	}
}

func TestHaproxyFrontendCertificateResourceImportUsesNaturalKey(t *testing.T) {
	t.Parallel()

	certificateResource := &haproxyFrontendCertificateResource{}
	schema := haproxyFrontendCertificateResourceSchema(t)

	validResp := resource.ImportStateResponse{
		State: tfsdk.State{Schema: schema},
	}
	certificateResource.ImportState(context.Background(), resource.ImportStateRequest{ID: "app_frontend/existing_cert_ref"}, &validResp)
	if validResp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %#v", validResp.Diagnostics)
	}
	var state haproxyFrontendCertificateModel
	diags := validResp.State.Get(context.Background(), &state)
	if diags.HasError() {
		t.Fatalf("state get diagnostics: %#v", diags)
	}
	if state.ID.ValueString() != "app_frontend/existing_cert_ref" || state.FrontendName.ValueString() != "app_frontend" || state.SSLCertificate.ValueString() != "existing_cert_ref" {
		t.Fatalf("imported state = %#v", state)
	}

	for _, id := range []string{
		"",
		"app_frontend",
		"app_frontend/",
		"/existing_cert_ref",
		"app/frontend/existing_cert_ref",
		"app_frontend/existing cert ref",
		"app_frontend/existing\ncert",
		"app_frontend/-----BEGINCERTIFICATE-----",
	} {
		invalidResp := resource.ImportStateResponse{
			State: tfsdk.State{Schema: schema},
		}
		certificateResource.ImportState(context.Background(), resource.ImportStateRequest{ID: id}, &invalidResp)
		if !invalidResp.Diagnostics.HasError() {
			t.Fatalf("expected diagnostic for import id %q", id)
		}
	}
}

func TestHaproxyFrontendCertificateValidation(t *testing.T) {
	t.Parallel()

	valid := nullHaproxyFrontendCertificateModel()
	valid.FrontendName = types.StringValue("app_frontend")
	valid.SSLCertificate = types.StringValue("existing_cert_ref")

	if _, err := validateHaproxyFrontendCertificatePlan(valid); err != nil {
		t.Fatalf("valid certificate attachment rejected: %v", err)
	}

	tests := map[string]haproxyFrontendCertificateModel{
		"frontend slash": func() haproxyFrontendCertificateModel {
			model := valid
			model.FrontendName = types.StringValue("app/frontend")
			return model
		}(),
		"empty certificate": func() haproxyFrontendCertificateModel {
			model := valid
			model.SSLCertificate = types.StringValue("")
			return model
		}(),
		"whitespace certificate": func() haproxyFrontendCertificateModel {
			model := valid
			model.SSLCertificate = types.StringValue("existing cert ref")
			return model
		}(),
		"leading whitespace": func() haproxyFrontendCertificateModel {
			model := valid
			model.SSLCertificate = types.StringValue(" existing_cert_ref")
			return model
		}(),
		"newline certificate": func() haproxyFrontendCertificateModel {
			model := valid
			model.SSLCertificate = types.StringValue("existing\ncert")
			return model
		}(),
		"certificate slash": func() haproxyFrontendCertificateModel {
			model := valid
			model.SSLCertificate = types.StringValue("existing/cert")
			return model
		}(),
		"pem certificate": func() haproxyFrontendCertificateModel {
			model := valid
			model.SSLCertificate = types.StringValue("-----BEGINCERTIFICATE-----")
			return model
		}(),
		"pem trailer": func() haproxyFrontendCertificateModel {
			model := valid
			model.SSLCertificate = types.StringValue("-----ENDCERTIFICATE-----")
			return model
		}(),
		"private key material": func() haproxyFrontendCertificateModel {
			model := valid
			model.SSLCertificate = types.StringValue("-----BEGINPRIVATEKEY-----")
			return model
		}(),
	}

	for name, model := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := validateHaproxyFrontendCertificatePlan(model); err == nil {
				t.Fatalf("expected validation error")
			}
		})
	}
}

func TestHaproxyFrontendCertificateSchemaIsConservative(t *testing.T) {
	schema := haproxyFrontendCertificateResourceSchema(t)
	for _, required := range []string{"frontend_name", "ssl_certificate"} {
		if _, ok := schema.Attributes[required]; !ok {
			t.Fatalf("resource schema missing %q", required)
		}
	}
	for _, forbidden := range []string{"parent_id", "rest_id", "api_id", "id_api", "apply", "async", "certificate", "cert", "crt", "pem", "private_key", "key", "content"} {
		if _, ok := schema.Attributes[forbidden]; ok {
			t.Fatalf("resource schema should not expose %q", forbidden)
		}
	}
}

func assertFrontendCertificateLookupQuery(t *testing.T, r *http.Request, parentID string, sslCertificate string) {
	t.Helper()

	if got := r.URL.Query().Get("parent_id"); got != parentID {
		t.Fatalf("certificate lookup parent_id = %q", got)
	}
	if got := r.URL.Query().Get("ssl_certificate"); got != sslCertificate {
		t.Fatalf("certificate lookup ssl_certificate = %q", got)
	}
}

func haproxyFrontendCertificateResourceSchema(t *testing.T) resourceschema.Schema {
	t.Helper()

	var resp resource.SchemaResponse
	(&haproxyFrontendCertificateResource{}).Schema(context.Background(), resource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected resource schema diagnostics: %#v", resp.Diagnostics)
	}

	return resp.Schema
}
