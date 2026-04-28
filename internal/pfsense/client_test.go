package pfsense

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGetUsesAPIKeyAuth(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s", r.Method)
		}
		if r.URL.RequestURI() != "/api/v2/system/status?scope=wan" {
			t.Fatalf("request uri = %s", r.URL.RequestURI())
		}
		if got := r.Header.Get("X-API-Key"); got != "test-key" {
			t.Fatalf("X-API-Key = %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Fatalf("Authorization = %q", got)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Fatalf("Accept = %q", got)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{
		Endpoint: server.URL,
		APIKey:   "test-key",
		Username: "ignored",
		Password: "ignored",
	})

	var response struct {
		OK bool `json:"ok"`
	}
	if err := client.Get(context.Background(), "/system/status?scope=wan", &response); err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if !response.OK {
		t.Fatalf("response not decoded")
	}
}

func TestPostUsesBasicAuthFallback(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		if got := r.Header.Get("X-API-Key"); got != "" {
			t.Fatalf("X-API-Key = %q", got)
		}
		username, password, ok := r.BasicAuth()
		if !ok || username != "api-user" || password != "api-pass" {
			t.Fatalf("basic auth = %q/%q present=%t", username, password, ok)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("Content-Type = %q", got)
		}

		var request struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if request.Name != "backend-a" {
			t.Fatalf("request name = %q", request.Name)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"created"}`))
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{
		Endpoint: server.URL + "/",
		Username: "api-user",
		Password: "api-pass",
	})

	var response struct {
		ID string `json:"id"`
	}
	if err := client.Post(context.Background(), "haproxy/backends", map[string]string{"name": "backend-a"}, &response); err != nil {
		t.Fatalf("Post returned error: %v", err)
	}
	if response.ID != "created" {
		t.Fatalf("response id = %q", response.ID)
	}
}

func TestHTTPMethodHelpers(t *testing.T) {
	t.Parallel()

	type call struct {
		method string
		run    func(context.Context, *Client, any) error
		body   bool
	}

	calls := []call{
		{
			method: http.MethodGet,
			run: func(ctx context.Context, client *Client, out any) error {
				return client.Get(ctx, "/haproxy/test", out)
			},
		},
		{
			method: http.MethodPost,
			run: func(ctx context.Context, client *Client, out any) error {
				return client.Post(ctx, "/haproxy/test", map[string]string{"method": http.MethodPost}, out)
			},
			body: true,
		},
		{
			method: http.MethodPatch,
			run: func(ctx context.Context, client *Client, out any) error {
				return client.Patch(ctx, "/haproxy/test", map[string]string{"method": http.MethodPatch}, out)
			},
			body: true,
		},
		{
			method: http.MethodPut,
			run: func(ctx context.Context, client *Client, out any) error {
				return client.Put(ctx, "/haproxy/test", map[string]string{"method": http.MethodPut}, out)
			},
			body: true,
		},
		{
			method: http.MethodDelete,
			run: func(ctx context.Context, client *Client, out any) error {
				return client.Delete(ctx, "/haproxy/test", out)
			},
		},
	}

	for _, tc := range calls {
		tc := tc
		t.Run(tc.method, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != tc.method {
					t.Fatalf("method = %s", r.Method)
				}
				if r.URL.Path != "/api/v2/haproxy/test" {
					t.Fatalf("path = %s", r.URL.Path)
				}

				if tc.body {
					var request map[string]string
					if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
						t.Fatalf("decode request body: %v", err)
					}
					if request["method"] != tc.method {
						t.Fatalf("request method field = %q", request["method"])
					}
				}

				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"ok":true}`))
			}))
			t.Cleanup(server.Close)

			client := NewClient(Config{Endpoint: server.URL, APIKey: "test-key"})
			var response struct {
				OK bool `json:"ok"`
			}
			if err := tc.run(context.Background(), client, &response); err != nil {
				t.Fatalf("%s helper returned error: %v", tc.method, err)
			}
			if !response.OK {
				t.Fatalf("response not decoded")
			}
		})
	}
}

func TestAPIErrorResponsesAreActionable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status   int
		contains []string
	}{
		{status: http.StatusUnauthorized, contains: []string{"401 Unauthorized", "authentication failed", "check the configured API key"}},
		{status: http.StatusForbidden, contains: []string{"403 Forbidden", "authorization failed", "REST API privileges"}},
		{status: http.StatusNotFound, contains: []string{"404 Not Found", "endpoint or object was not found"}},
		{status: http.StatusConflict, contains: []string{"409 Conflict", "conflicted with existing pfSense configuration"}},
		{status: http.StatusUnprocessableEntity, contains: []string{"422 Unprocessable Entity", "request validation failed"}},
		{status: http.StatusInternalServerError, contains: []string{"500 Internal Server Error", "server error", "pfSense REST API logs"}},
		{status: http.StatusBadGateway, contains: []string{"502 Bad Gateway", "server error", "pfSense REST API logs"}},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"message":"pfSense rejected the request"}`))
			}))
			t.Cleanup(server.Close)

			client := NewClient(Config{Endpoint: server.URL, APIKey: "test-key"})
			err := client.Get(context.Background(), "/haproxy/test", nil)
			if err == nil {
				t.Fatalf("expected error")
			}

			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("expected APIError, got %T", err)
			}
			if apiErr.StatusCode != tc.status {
				t.Fatalf("status code = %d", apiErr.StatusCode)
			}
			if apiErr.Method != http.MethodGet {
				t.Fatalf("method = %s", apiErr.Method)
			}
			if apiErr.Path != "/api/v2/haproxy/test" {
				t.Fatalf("path = %s", apiErr.Path)
			}
			if !strings.Contains(apiErr.Body, "pfSense rejected the request") {
				t.Fatalf("body = %q", apiErr.Body)
			}

			errorMessage := err.Error()
			for _, want := range tc.contains {
				if !strings.Contains(errorMessage, want) {
					t.Fatalf("error %q does not contain %q", errorMessage, want)
				}
			}
			if !strings.Contains(errorMessage, "pfSense rejected the request") {
				t.Fatalf("error %q does not contain response message", errorMessage)
			}
		})
	}
}

func TestEnvelopeResponseDecodesData(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": 200,
			"status": "ok",
			"response_id": "resp-success",
			"message": "ok",
			"data": {"enabled": true, "name": "haproxy"}
		}`))
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{Endpoint: server.URL, APIKey: "test-key"})
	var response struct {
		Enabled bool   `json:"enabled"`
		Name    string `json:"name"`
	}
	if err := client.Get(context.Background(), "/services/haproxy/settings", &response); err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if !response.Enabled || response.Name != "haproxy" {
		t.Fatalf("response = %#v", response)
	}
}

func TestNonEnvelopeResponseWithStatusFieldDecodesNormally(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"enabled","name":"haproxy"}`))
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{Endpoint: server.URL, APIKey: "test-key"})
	var response struct {
		Status string `json:"status"`
		Name   string `json:"name"`
	}
	if err := client.Get(context.Background(), "/services/haproxy/status", &response); err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if response.Status != "enabled" || response.Name != "haproxy" {
		t.Fatalf("response = %#v", response)
	}
}

func TestEnvelopeErrorWithHTTPSuccessIsActionable(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": 422,
			"status": "error",
			"response_id": "resp-validation",
			"message": "invalid backend name",
			"data": {"field": "name"}
		}`))
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{Endpoint: server.URL, APIKey: "test-key"})
	err := client.Post(context.Background(), "/services/haproxy/backend", map[string]string{"name": ""}, nil)
	if err == nil {
		t.Fatalf("expected error")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %T", err)
	}
	if apiErr.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status code = %d", apiErr.StatusCode)
	}
	if apiErr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("envelope code = %d", apiErr.Code)
	}
	if apiErr.ResponseID != "resp-validation" {
		t.Fatalf("response id = %q", apiErr.ResponseID)
	}
	for _, want := range []string{"422 Unprocessable Entity", "error envelope", "invalid backend name", "response_id: resp-validation"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not contain %q", err.Error(), want)
		}
	}
	if strings.Contains(err.Error(), `"field": "name"`) || strings.Contains(err.Error(), `"data"`) {
		t.Fatalf("error leaks full structured body: %q", err.Error())
	}
}

func TestEnvelopeErrorWithHTTPFailureIncludesResponseID(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{
			"code": 403,
			"status": "error",
			"response_id": "resp-forbidden",
			"message": {"error": "missing HAProxy privilege"}
		}`))
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{Endpoint: server.URL, APIKey: "test-key"})
	err := client.Get(context.Background(), "/services/haproxy/frontends", nil)
	if err == nil {
		t.Fatalf("expected error")
	}
	for _, want := range []string{"403 Forbidden", "missing HAProxy privilege", "response_id: resp-forbidden"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not contain %q", err.Error(), want)
		}
	}
}

func TestAPIErrorIncludesPlainTextBody(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("maintenance window active"))
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{Endpoint: server.URL, APIKey: "test-key"})
	err := client.Get(context.Background(), "/haproxy/test", nil)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "maintenance window active") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestEndpointPathJoining(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		endpointPath string
		requestPath  string
		wantPath     string
	}{
		{
			name:        "root endpoint",
			requestPath: "system/status",
			wantPath:    "/api/v2/system/status",
		},
		{
			name:         "endpoint with prefix",
			endpointPath: "/rest",
			requestPath:  "/system/status",
			wantPath:     "/rest/api/v2/system/status",
		},
		{
			name:         "endpoint already includes api prefix",
			endpointPath: "/api/v2",
			requestPath:  "/system/status",
			wantPath:     "/api/v2/system/status",
		},
		{
			name:        "request already includes api prefix",
			requestPath: "/api/v2/system/status",
			wantPath:    "/api/v2/system/status",
		},
		{
			name:         "endpoint and request include api prefix",
			endpointPath: "/api/v2",
			requestPath:  "/api/v2/system/status",
			wantPath:     "/api/v2/system/status",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tc.wantPath {
					t.Fatalf("path = %s, want %s", r.URL.Path, tc.wantPath)
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			t.Cleanup(server.Close)

			client := NewClient(Config{
				Endpoint: server.URL + tc.endpointPath,
				APIKey:   "test-key",
			})
			if err := client.Get(context.Background(), tc.requestPath, nil); err != nil {
				t.Fatalf("Get returned error: %v", err)
			}
		})
	}
}

func TestTimeoutFromConfig(t *testing.T) {
	t.Parallel()

	client := NewClient(Config{
		Endpoint: "https://pfsense.example.com",
		APIKey:   "test-key",
		Timeout:  5 * time.Second,
	})

	if client.httpClient.Timeout != 5*time.Second {
		t.Fatalf("timeout = %s", client.httpClient.Timeout)
	}
}
