package client

import (
	"net/http"
	"time"
)

// Option configures a Client.
type Option func(*Client)

// WithTimeout sets the HTTP client timeout.
// Default is 5 seconds.
func WithTimeout(timeout time.Duration) Option {
	return func(c *Client) {
		c.httpClient.Timeout = timeout
	}
}

// WithHTTPClient sets a custom HTTP client.
// Useful for testing or advanced configuration (TLS, proxies, etc.).
func WithHTTPClient(client *http.Client) Option {
	return func(c *Client) {
		c.httpClient = client
	}
}
