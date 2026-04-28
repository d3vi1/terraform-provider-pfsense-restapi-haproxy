package pfsense

import (
	"net/http"
	"time"
)

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

// NewClient returns a pfSense REST API client. Endpoint validation happens in
// provider configuration so tests can construct lightweight clients.
func NewClient(config Config) *Client {
	timeout := config.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	return &Client{
		endpoint: config.Endpoint,
		apiKey:   config.APIKey,
		username: config.Username,
		password: config.Password,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *Client) Endpoint() string {
	return c.endpoint
}
