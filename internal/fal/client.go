package fal

import (
	"bytes"
	"os"
)

type DeployStrategy string

const (
	DeployStrategyRecreate DeployStrategy = "recreate"
	DeployStrategyRolling  DeployStrategy = "rolling"
)

type AuthMode string

const (
	AuthModePublic  AuthMode = "public"
	AuthModePrivate AuthMode = "private"
	AuthModeShared  AuthMode = "shared"
)

type Client struct {
	key string
	dir string

	// host is the gRPC endpoint for secrets and environments.
	host string
	// insecure turns off TLS to host, for tests against a local server.
	insecure bool
	ctrl     controller
}

// Option configures a Client.
type Option func(*Client)

// WithHost sets the gRPC endpoint used for secrets and environments
// (DefaultHost when empty). A host without a port gets port 443.
func WithHost(host string) Option {
	return func(c *Client) {
		if host != "" {
			c.host = host
		}
	}
}

// WithInsecure connects to the host without TLS. For tests only.
func WithInsecure() Option {
	return func(c *Client) { c.insecure = true }
}

func NewWithTemp(key string, opts ...Option) (*Client, error) {
	dir, err := os.MkdirTemp("", "fal-*")
	if err != nil {
		return nil, err
	}
	c := &Client{
		key:  key,
		dir:  dir,
		host: DefaultHost,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

func (f *Client) Directory() string {
	return f.dir
}

func (f *Client) sharedEnvironmentVariables() map[string]string {
	return map[string]string{
		"FAL_KEY": f.key,
	}
}

func readAll(c <-chan []byte) string {
	var output bytes.Buffer

wait:
	for {
		select {
		case v, ok := <-c:
			if !ok {
				break wait
			}
			output.WriteString(string(v))
		}
	}

	return output.String()
}
