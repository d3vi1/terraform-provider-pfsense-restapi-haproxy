package pfsense

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	pathpkg "path"
	"strings"
	"time"
)

const apiPathPrefix = "/api/v2"

// Config contains connection settings for pfSense-pkg-RESTAPI.
type Config struct {
	Endpoint    string
	APIKey      string
	Username    string
	Password    string
	InsecureTLS bool
	Timeout     time.Duration
}

// Client is the REST client used by resources and data sources.
type Client struct {
	endpoint   string
	apiKey     string
	username   string
	password   string
	httpClient *http.Client
}

// APIError describes a non-2xx response from pfSense-pkg-RESTAPI.
type APIError struct {
	StatusCode int
	Code       int
	Method     string
	Path       string
	ResponseID string
	Message    string
	Body       string
}

func (e *APIError) Error() string {
	status := fmt.Sprintf("%d", e.StatusCode)
	if text := http.StatusText(e.StatusCode); text != "" {
		status = fmt.Sprintf("%s %s", status, text)
	}

	message := fmt.Sprintf("pfSense REST API %s %s returned %s", e.Method, e.Path, status)
	if e.Message != "" {
		message = fmt.Sprintf("%s: %s", message, e.Message)
	}
	if e.ResponseID != "" {
		message = fmt.Sprintf("%s (response_id: %s)", message, e.ResponseID)
	}

	body := strings.TrimSpace(e.Body)
	if body != "" && !json.Valid([]byte(body)) && !strings.Contains(e.Message, body) {
		message = fmt.Sprintf("%s (response: %s)", message, truncate(body, 500))
	}

	return message
}

// NewClient returns a pfSense REST API client. Endpoint validation happens in
// provider configuration so tests can construct lightweight clients.
func NewClient(config Config) *Client {
	timeout := config.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	httpClient := &http.Client{
		Timeout: timeout,
	}
	if config.InsecureTLS {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec
		httpClient.Transport = transport
	}

	return &Client{
		endpoint:   strings.TrimRight(strings.TrimSpace(config.Endpoint), "/"),
		apiKey:     config.APIKey,
		username:   config.Username,
		password:   config.Password,
		httpClient: httpClient,
	}
}

func (c *Client) Endpoint() string {
	return c.endpoint
}

func (c *Client) Get(ctx context.Context, path string, out any) error {
	return c.Do(ctx, http.MethodGet, path, nil, out)
}

func (c *Client) Post(ctx context.Context, path string, in any, out any) error {
	return c.Do(ctx, http.MethodPost, path, in, out)
}

func (c *Client) Patch(ctx context.Context, path string, in any, out any) error {
	return c.Do(ctx, http.MethodPatch, path, in, out)
}

func (c *Client) Put(ctx context.Context, path string, in any, out any) error {
	return c.Do(ctx, http.MethodPut, path, in, out)
}

func (c *Client) Delete(ctx context.Context, path string, out any) error {
	return c.Do(ctx, http.MethodDelete, path, nil, out)
}

// Do sends a JSON request to pfSense-pkg-RESTAPI and decodes a JSON response.
func (c *Client) Do(ctx context.Context, method string, path string, in any, out any) error {
	if ctx == nil {
		return errors.New("pfSense REST API request requires a non-nil context")
	}

	requestURL, displayPath, err := c.requestURL(path)
	if err != nil {
		return err
	}

	var body io.Reader
	if in != nil {
		encoded, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("encode %s %s request: %w", method, displayPath, err)
		}
		body = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, requestURL, body)
	if err != nil {
		return fmt.Errorf("create %s %s request: %w", method, displayPath, err)
	}
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s request failed: %w", method, displayPath, err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < http.StatusOK || resp.StatusCode > 299 {
		return newAPIError(resp, method, displayPath)
	}

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read %s %s response: %w", method, displayPath, err)
	}
	if len(bytes.TrimSpace(responseBody)) == 0 {
		return nil
	}

	if envelope, ok := parseEnvelope(responseBody); ok {
		if envelope.Code >= 400 {
			return envelopeAPIError(resp.StatusCode, method, displayPath, responseBody, envelope)
		}
		if out == nil {
			return nil
		}
		if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
			return nil
		}
		if err := json.Unmarshal(envelope.Data, out); err != nil {
			return fmt.Errorf("decode %s %s response data: %w", method, displayPath, err)
		}
		return nil
	}

	if out == nil {
		return nil
	}
	if err := json.Unmarshal(responseBody, out); err != nil {
		return fmt.Errorf("decode %s %s response: %w", method, displayPath, err)
	}

	return nil
}

func (c *Client) setAuth(req *http.Request) {
	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
		return
	}
	if c.username != "" || c.password != "" {
		req.SetBasicAuth(c.username, c.password)
	}
}

func (c *Client) requestURL(requestPath string) (string, string, error) {
	if c.endpoint == "" {
		return "", "", errors.New("pfSense REST API endpoint is required")
	}

	baseURL, err := url.Parse(c.endpoint)
	if err != nil {
		return "", "", fmt.Errorf("parse pfSense REST API endpoint %q: %w", c.endpoint, err)
	}
	if baseURL.Scheme == "" || baseURL.Host == "" {
		return "", "", fmt.Errorf("pfSense REST API endpoint %q must be an absolute URL", c.endpoint)
	}

	relativeURL, err := url.Parse(requestPath)
	if err != nil {
		return "", "", fmt.Errorf("parse pfSense REST API path %q: %w", requestPath, err)
	}
	if relativeURL.IsAbs() || relativeURL.Host != "" {
		return "", "", fmt.Errorf("pfSense REST API path %q must be relative", requestPath)
	}

	joinedPath := joinAPIPath(baseURL.Path, relativeURL.Path)
	baseURL.Path = joinedPath
	baseURL.RawPath = ""
	baseURL.RawQuery = relativeURL.RawQuery
	baseURL.Fragment = ""

	displayPath := joinedPath
	if relativeURL.RawQuery != "" {
		displayPath = displayPath + "?" + relativeURL.RawQuery
	}

	return baseURL.String(), displayPath, nil
}

func joinAPIPath(basePath string, requestPath string) string {
	basePath = strings.TrimRight(basePath, "/")
	requestPath = "/" + strings.TrimLeft(requestPath, "/")
	if requestPath == "/" {
		requestPath = ""
	}

	var joined string
	switch {
	case strings.HasPrefix(requestPath, apiPathPrefix+"/") || requestPath == apiPathPrefix:
		if strings.HasSuffix(basePath, apiPathPrefix) {
			requestPath = strings.TrimPrefix(requestPath, apiPathPrefix)
		}
		joined = pathpkg.Join(basePath, requestPath)
	case strings.HasSuffix(basePath, apiPathPrefix):
		joined = pathpkg.Join(basePath, requestPath)
	default:
		joined = pathpkg.Join(basePath, apiPathPrefix, requestPath)
	}

	if joined == "." || joined == "" {
		return "/"
	}
	if !strings.HasPrefix(joined, "/") {
		return "/" + joined
	}
	return joined
}

func newAPIError(resp *http.Response, method string, path string) error {
	bodyBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	body := strings.TrimSpace(string(bodyBytes))

	message := statusGuidance(resp.StatusCode)
	envelope, hasEnvelope := parseEnvelope(bodyBytes)
	if hasEnvelope && envelope.Code >= 400 {
		return envelopeAPIError(resp.StatusCode, method, path, bodyBytes, envelope)
	}
	if detail := errorDetail(body); detail != "" {
		message = message + ": " + detail
	}
	if readErr != nil {
		message = message + fmt.Sprintf(": failed to read error response body: %v", readErr)
	}

	return &APIError{
		StatusCode: resp.StatusCode,
		Method:     method,
		Path:       path,
		Message:    message,
		Body:       body,
	}
}

type responseEnvelope struct {
	Code       int             `json:"code"`
	Status     string          `json:"status"`
	ResponseID string          `json:"response_id"`
	Message    json.RawMessage `json:"message"`
	Data       json.RawMessage `json:"data"`
	Links      json.RawMessage `json:"_links"`
}

func parseEnvelope(body []byte) (responseEnvelope, bool) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return responseEnvelope{}, false
	}

	if _, ok := raw["code"]; !ok {
		return responseEnvelope{}, false
	}
	if _, ok := raw["data"]; !ok {
		if _, ok := raw["message"]; !ok {
			return responseEnvelope{}, false
		}
	}
	var envelope responseEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return responseEnvelope{}, false
	}
	return envelope, true
}

func envelopeAPIError(statusCode int, method string, path string, body []byte, envelope responseEnvelope) error {
	effectiveStatusCode := statusCode
	if envelope.Code >= 400 {
		effectiveStatusCode = envelope.Code
	}

	message := statusGuidance(effectiveStatusCode)
	if statusCode >= http.StatusOK && statusCode <= 299 {
		message = message + "; pfSense REST API returned an error envelope"
	}
	if detail := detailFromRawMessage(envelope.Message); detail != "" {
		message = message + ": " + detail
	}

	return &APIError{
		StatusCode: effectiveStatusCode,
		Code:       envelope.Code,
		Method:     method,
		Path:       path,
		ResponseID: envelope.ResponseID,
		Message:    message,
		Body:       strings.TrimSpace(string(body)),
	}
}

func statusGuidance(statusCode int) string {
	switch statusCode {
	case http.StatusUnauthorized:
		return "authentication failed; check the configured API key or username/password"
	case http.StatusForbidden:
		return "authorization failed; check REST API privileges for the configured user or key"
	case http.StatusNotFound:
		return "endpoint or object was not found"
	case http.StatusConflict:
		return "request conflicted with existing pfSense configuration"
	case http.StatusUnprocessableEntity:
		return "request validation failed"
	default:
		if statusCode >= 500 {
			return "pfSense REST API server error; retry or inspect pfSense REST API logs"
		}
		return "pfSense REST API request failed"
	}
}

func errorDetail(body string) string {
	if body == "" {
		return ""
	}

	var payload any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return truncate(body, 500)
	}

	if detail := detailFromValue(payload); detail != "" {
		return truncate(detail, 500)
	}

	return truncate(body, 500)
}

func detailFromRawMessage(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}

	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return truncate(string(raw), 500)
	}
	return truncate(detailFromValue(value), 500)
}

func detailFromValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case map[string]any:
		for _, key := range []string{"message", "error", "detail", "details", "description", "errors"} {
			if field, ok := typed[key]; ok {
				if detail := detailFromValue(field); detail != "" {
					return detail
				}
			}
		}
	case []any:
		details := make([]string, 0, len(typed))
		for _, item := range typed {
			if detail := detailFromValue(item); detail != "" {
				details = append(details, detail)
			}
		}
		return strings.Join(details, "; ")
	}

	encoded, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(encoded)
}

func truncate(value string, maxLength int) string {
	if len(value) <= maxLength {
		return value
	}
	return value[:maxLength] + "..."
}
