package pfsense

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
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

func TestConcurrentMutatingRequestsSerialize(t *testing.T) {
	methods := []string{
		http.MethodPost,
		http.MethodPatch,
		http.MethodPut,
		http.MethodDelete,
	}

	client := NewClient(Config{Endpoint: "https://pfsense.example.com", APIKey: "test-key"})

	var active atomic.Int32
	var seen atomic.Int32
	client.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if !isConfigMutationMethod(req.Method) {
			return nil, fmt.Errorf("unexpected non-mutating method %s", req.Method)
		}
		if !configWriteGuardHeld() {
			return nil, fmt.Errorf("%s reached transport without holding the shared write guard", req.Method)
		}

		current := active.Add(1)
		defer active.Add(-1)
		if current != 1 {
			return nil, fmt.Errorf("%s overlapped with another mutating request", req.Method)
		}

		seen.Add(1)
		return testJSONResponse(req, `{"ok":true}`), nil
	})

	start := make(chan struct{})
	errs := make(chan error, len(methods))
	var wg sync.WaitGroup
	for _, method := range methods {
		method := method
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start

			var response struct {
				OK bool `json:"ok"`
			}
			var request any
			if method != http.MethodDelete {
				request = map[string]string{"method": method}
			}
			if err := client.Do(context.Background(), method, "/haproxy/test", request, &response); err != nil {
				errs <- err
				return
			}
			if !response.OK {
				errs <- fmt.Errorf("%s response not decoded", method)
				return
			}
			errs <- nil
		}()
	}

	close(start)
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := seen.Load(); got != 4 {
		t.Fatalf("mutating requests sent = %d, want %d", got, len(methods))
	}
}

func TestGetDoesNotWaitForActiveWrite(t *testing.T) {
	writeEntered := make(chan struct{})
	releaseWrite := make(chan struct{})

	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() {
			close(releaseWrite)
		})
	}
	defer release()

	client := NewClient(Config{Endpoint: "https://pfsense.example.com", APIKey: "test-key"})
	client.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.Method {
		case http.MethodPost:
			close(writeEntered)
			<-releaseWrite
		case http.MethodGet:
			if !configWriteGuardHeld() {
				return nil, errors.New("GET reached transport after the active write guard was released")
			}
		default:
			return nil, fmt.Errorf("unexpected method %s", req.Method)
		}

		return testJSONResponse(req, `{"ok":true}`), nil
	})

	writeDone := make(chan error, 1)
	go func() {
		writeDone <- client.Post(context.Background(), "/haproxy/test", map[string]string{"name": "backend-a"}, nil)
	}()
	waitForSignal(t, writeEntered, "write request to enter transport")

	getDone := make(chan error, 1)
	var response struct {
		OK bool `json:"ok"`
	}
	go func() {
		getDone <- client.Get(context.Background(), "/haproxy/test", &response)
	}()

	select {
	case err := <-getDone:
		if err != nil {
			t.Fatalf("Get returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("GET was serialized behind an active write")
	}
	if !response.OK {
		t.Fatalf("response not decoded")
	}

	release()
	if err := <-writeDone; err != nil {
		t.Fatalf("Post returned error: %v", err)
	}
}

func TestWaitingMutatingRequestHonorsContextCancellation(t *testing.T) {
	writeEntered := make(chan struct{})
	releaseWrite := make(chan struct{})
	secondWriteReachedTransport := make(chan struct{}, 1)

	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() {
			close(releaseWrite)
		})
	}
	defer release()

	client := NewClient(Config{Endpoint: "https://pfsense.example.com", APIKey: "test-key"})
	client.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost {
			return nil, fmt.Errorf("unexpected method %s", req.Method)
		}

		select {
		case writeEntered <- struct{}{}:
			<-releaseWrite
		default:
			secondWriteReachedTransport <- struct{}{}
			return testJSONResponse(req, `{"ok":true}`), nil
		}

		return testJSONResponse(req, `{"ok":true}`), nil
	})

	firstWriteDone := make(chan error, 1)
	go func() {
		firstWriteDone <- client.Post(context.Background(), "/haproxy/test", map[string]string{"name": "backend-a"}, nil)
	}()
	waitForSignal(t, writeEntered, "first write request to enter transport")

	waitingCtx, cancel := context.WithCancel(context.Background())
	secondWriteDone := make(chan error, 1)
	go func() {
		secondWriteDone <- client.Post(waitingCtx, "/haproxy/test", map[string]string{"name": "backend-b"}, nil)
	}()
	cancel()

	select {
	case err := <-secondWriteDone:
		if err == nil {
			t.Fatal("second write returned nil error after context cancellation")
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("second write error = %v, want context.Canceled", err)
		}
	case <-secondWriteReachedTransport:
		t.Fatal("canceled waiting write reached transport")
	case <-time.After(2 * time.Second):
		t.Fatal("canceled waiting write did not return promptly")
	}

	release()
	if err := <-firstWriteDone; err != nil {
		t.Fatalf("first Post returned error: %v", err)
	}

	if err := client.Post(context.Background(), "/haproxy/test", map[string]string{"name": "backend-c"}, nil); err != nil {
		t.Fatalf("later Post returned error after cancellation: %v", err)
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

func TestGetRetriesTransientHTTPFailureOnce(t *testing.T) {
	t.Parallel()

	var seen atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if seen.Add(1) == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"message":"pfSense reload in progress"}`))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{Endpoint: server.URL, APIKey: "test-key"})
	var response struct {
		OK bool `json:"ok"`
	}
	if err := client.Get(context.Background(), "/haproxy/test", &response); err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if !response.OK {
		t.Fatalf("response not decoded")
	}
	if got := seen.Load(); got != 2 {
		t.Fatalf("requests = %d, want 2", got)
	}
}

func TestGetRetriesTransientTransportFailureOnce(t *testing.T) {
	t.Parallel()

	client := NewClient(Config{Endpoint: "https://pfsense.example.com", APIKey: "test-key"})
	var seen atomic.Int32
	client.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if seen.Add(1) == 1 {
			return nil, temporaryTimeoutError{}
		}
		return testJSONResponse(req, `{"ok":true}`), nil
	})

	var response struct {
		OK bool `json:"ok"`
	}
	if err := client.Get(context.Background(), "/haproxy/test", &response); err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if !response.OK {
		t.Fatalf("response not decoded")
	}
	if got := seen.Load(); got != 2 {
		t.Fatalf("requests = %d, want 2", got)
	}
}

func TestGetRetriesClientTimeoutOnce(t *testing.T) {
	t.Parallel()

	var seen atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if seen.Add(1) == 1 {
			time.Sleep(200 * time.Millisecond)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{
		Endpoint: server.URL,
		APIKey:   "test-key",
		Timeout:  50 * time.Millisecond,
	})
	var response struct {
		OK bool `json:"ok"`
	}
	if err := client.Get(context.Background(), "/haproxy/test", &response); err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if !response.OK {
		t.Fatalf("response not decoded")
	}
	if got := seen.Load(); got != 2 {
		t.Fatalf("requests = %d, want 2", got)
	}
}

func TestPostTransientHTTPFailureIsNotRetried(t *testing.T) {
	t.Parallel()

	var seen atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		seen.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"message":"temporary API outage"}`))
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{Endpoint: server.URL, APIKey: "test-key"})
	err := client.Post(context.Background(), "/haproxy/test", map[string]string{"name": "backend-a"}, nil)
	if err == nil {
		t.Fatalf("expected error")
	}
	if got := seen.Load(); got != 1 {
		t.Fatalf("requests = %d, want 1", got)
	}
	for _, want := range []string{"Classified as transient", "unsafe config write was not retried automatically", "Refresh or inspect live pfSense HAProxy state"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not contain %q", err.Error(), want)
		}
	}
}

func TestPostTransientTransportFailureIsNotRetried(t *testing.T) {
	t.Parallel()

	client := NewClient(Config{Endpoint: "https://pfsense.example.com", APIKey: "test-key"})
	var seen atomic.Int32
	client.httpClient.Transport = roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		seen.Add(1)
		return nil, temporaryTimeoutError{}
	})

	err := client.Post(context.Background(), "/haproxy/test", map[string]string{"name": "backend-a"}, nil)
	if err == nil {
		t.Fatalf("expected error")
	}
	if got := seen.Load(); got != 1 {
		t.Fatalf("requests = %d, want 1", got)
	}
	for _, want := range []string{"Classified as transient", "unsafe config write was not retried automatically", "Refresh or inspect live pfSense HAProxy state"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not contain %q", err.Error(), want)
		}
	}
}

func TestGetPermanentFailuresAreNotRetried(t *testing.T) {
	t.Parallel()

	statuses := []int{
		http.StatusBadRequest,
		http.StatusUnauthorized,
		http.StatusForbidden,
		http.StatusNotFound,
		http.StatusConflict,
		http.StatusUnprocessableEntity,
	}

	for _, status := range statuses {
		status := status
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Parallel()

			var seen atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				seen.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"message":"permanent failure"}`))
			}))
			t.Cleanup(server.Close)

			client := NewClient(Config{Endpoint: server.URL, APIKey: "test-key"})
			err := client.Get(context.Background(), "/haproxy/test", nil)
			if err == nil {
				t.Fatalf("expected error")
			}
			if got := seen.Load(); got != 1 {
				t.Fatalf("requests = %d, want 1", got)
			}
			if !strings.Contains(err.Error(), "Classified as permanent") {
				t.Fatalf("error %q does not classify permanent failure", err.Error())
			}
		})
	}
}

func TestGetRetriesTransientEnvelopeFailureOnce(t *testing.T) {
	t.Parallel()

	var seen atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if seen.Add(1) == 1 {
			_, _ = w.Write([]byte(`{
				"code": 503,
				"status": "error",
				"response_id": "resp-transient",
				"message": "reload in progress"
			}`))
			return
		}
		_, _ = w.Write([]byte(`{
			"code": 200,
			"status": "ok",
			"data": {"ok": true}
		}`))
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{Endpoint: server.URL, APIKey: "test-key"})
	var response struct {
		OK bool `json:"ok"`
	}
	if err := client.Get(context.Background(), "/haproxy/test", &response); err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if !response.OK {
		t.Fatalf("response not decoded")
	}
	if got := seen.Load(); got != 2 {
		t.Fatalf("requests = %d, want 2", got)
	}
}

func TestRetryWaitHonorsContextCancellation(t *testing.T) {
	t.Parallel()

	var seen atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		seen.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "2")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"message":"retry later"}`))
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{Endpoint: server.URL, APIKey: "test-key"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	time.AfterFunc(50*time.Millisecond, cancel)

	start := time.Now()
	err := client.Get(ctx, "/haproxy/test", nil)
	if err == nil {
		t.Fatalf("expected error")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("retry wait did not honor context cancellation; elapsed = %s", elapsed)
	}
	if got := seen.Load(); got != 1 {
		t.Fatalf("requests = %d, want 1", got)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error %q does not wrap context.Canceled", err.Error())
	}
	for _, want := range []string{"Classified as transient", "safe GET retry wait canceled"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not contain %q", err.Error(), want)
		}
	}
}

func TestEnvelopeRetryWaitHonorsRetryAfterAndContextCancellation(t *testing.T) {
	t.Parallel()

	var seen atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		seen.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "1")
		_, _ = w.Write([]byte(`{
			"code": 503,
			"status": "error",
			"response_id": "resp-retry-after",
			"message": "retry later"
		}`))
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{Endpoint: server.URL, APIKey: "test-key"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	time.AfterFunc(150*time.Millisecond, cancel)

	start := time.Now()
	err := client.Get(ctx, "/haproxy/test", nil)
	if err == nil {
		t.Fatalf("expected error")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("retry wait did not honor context cancellation; elapsed = %s", elapsed)
	}
	if got := seen.Load(); got != 1 {
		t.Fatalf("requests = %d, want 1", got)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error %q does not wrap context.Canceled", err.Error())
	}
	for _, want := range []string{"Classified as transient", "safe GET retry wait canceled"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not contain %q", err.Error(), want)
		}
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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type temporaryTimeoutError struct{}

func (temporaryTimeoutError) Error() string {
	return "temporary timeout"
}

func (temporaryTimeoutError) Timeout() bool {
	return true
}

func (temporaryTimeoutError) Temporary() bool {
	return true
}

func configWriteGuardHeld() bool {
	return len(configWriteGuard) > 0
}

func testJSONResponse(req *http.Request, body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

func waitForSignal(t *testing.T, ch <-chan struct{}, description string) {
	t.Helper()

	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}
