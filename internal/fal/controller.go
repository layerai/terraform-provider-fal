package fal

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/fal-ai/terraform-provider-fal/internal/isolate"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// DefaultHost is the gRPC endpoint the fal CLI talks to.
const DefaultHost = "api.alpha.fal.ai"

// MainEnvironment is the environment fal uses when none is given.
const MainEnvironment = "main"

// ErrNotFound is returned when a secret or environment does not exist.
var ErrNotFound = errors.New("not found")

// keyAuth sends a fal key the way the fal CLI does: as auth-key-id and auth-key
// metadata on every call.
type keyAuth struct {
	id, secret string
	insecure   bool
}

func (k keyAuth) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	return map[string]string{"auth-key-id": k.id, "auth-key": k.secret}, nil
}

func (k keyAuth) RequireTransportSecurity() bool { return !k.insecure }

// splitKey splits a fal key ("<key id>:<key secret>") into its two halves.
func splitKey(key string) (string, string, error) {
	id, secret, ok := strings.Cut(key, ":")
	if !ok || id == "" || secret == "" {
		return "", "", errors.New(`the fal key must have the form "<key id>:<key secret>"`)
	}
	return id, secret, nil
}

type controller struct {
	once   sync.Once
	client isolate.IsolateControllerClient
	err    error
}

// controller connects to fal's IsolateController on first use, so a
// configuration that manages only apps never opens the connection.
func (f *Client) controller() (isolate.IsolateControllerClient, error) {
	f.ctrl.once.Do(func() {
		id, secret, err := splitKey(f.key)
		if err != nil {
			f.ctrl.err = err
			return
		}

		target := f.host
		if !strings.Contains(target, ":") {
			target += ":443"
		}

		transport := credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12})
		if f.insecure {
			transport = insecure.NewCredentials()
		}

		conn, err := grpc.NewClient(
			target,
			grpc.WithTransportCredentials(transport),
			grpc.WithPerRPCCredentials(keyAuth{id: id, secret: secret, insecure: f.insecure}),
			grpc.WithUserAgent("terraform-provider-fal"),
		)
		if err != nil {
			f.ctrl.err = fmt.Errorf("connecting to %s: %w", target, err)
			return
		}
		f.ctrl.client = isolate.NewIsolateControllerClient(conn)
	})
	return f.ctrl.client, f.ctrl.err
}

func environmentOrMain(environment string) string {
	if environment == "" {
		return MainEnvironment
	}
	return environment
}

// Secret is a secret's metadata. fal never returns a secret's value.
type Secret struct {
	Name        string
	Environment string
	// DefaultExposed is nil when the account-level default applies.
	DefaultExposed *bool
}

// SetSecretOpts are the options for SetSecret.
type SetSecretOpts struct {
	Environment string
	// DefaultExposed, when nil, keeps the current setting of an existing
	// secret, or the account-level default for a new one.
	DefaultExposed *bool
}

// SetSecret creates or replaces a secret.
func (f *Client) SetSecret(ctx context.Context, name, value string, opts SetSecretOpts) error {
	c, err := f.controller()
	if err != nil {
		return err
	}
	_, err = c.SetSecret(ctx, &isolate.SetSecretRequest{
		Name:            name,
		Value:           proto.String(value),
		EnvironmentName: proto.String(environmentOrMain(opts.Environment)),
		DefaultExposed:  opts.DefaultExposed,
	})
	if err != nil {
		return fmt.Errorf("setting secret %q: %w", name, err)
	}
	return nil
}

// DeleteSecret deletes a secret. Deleting a missing secret is not an error.
func (f *Client) DeleteSecret(ctx context.Context, name, environment string) error {
	c, err := f.controller()
	if err != nil {
		return err
	}
	// A SetSecret without a value deletes the secret, as `fal secrets unset` does.
	_, err = c.SetSecret(ctx, &isolate.SetSecretRequest{
		Name:            name,
		EnvironmentName: proto.String(environmentOrMain(environment)),
	})
	if err != nil && status.Code(err) != codes.NotFound {
		return fmt.Errorf("deleting secret %q: %w", name, err)
	}
	return nil
}

// GetSecret returns a secret's metadata, or ErrNotFound.
func (f *Client) GetSecret(ctx context.Context, name, environment string) (*Secret, error) {
	c, err := f.controller()
	if err != nil {
		return nil, err
	}
	env := environmentOrMain(environment)
	resp, err := c.ListSecrets(ctx, &isolate.ListSecretsRequest{EnvironmentName: proto.String(env)})
	if err != nil {
		// Listing the secrets of an environment that no longer exists.
		if status.Code(err) == codes.NotFound {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("listing secrets in environment %q: %w", env, err)
	}
	for _, s := range resp.GetSecrets() {
		if s.GetName() != name {
			continue
		}
		secret := &Secret{Name: s.GetName(), Environment: env, DefaultExposed: s.DefaultExposed}
		if s.EnvironmentName != nil && s.GetEnvironmentName() != "" {
			secret.Environment = s.GetEnvironmentName()
		}
		return secret, nil
	}
	return nil, ErrNotFound
}

// Environment is a fal environment.
type Environment struct {
	Name        string
	Description *string
	IsDefault   bool
	CreatedAt   string
}

func environmentFromProto(e *isolate.EnvironmentInfo) *Environment {
	env := &Environment{Name: e.GetName(), Description: e.Description, IsDefault: e.GetIsDefault()}
	if e.CreatedAt != nil {
		env.CreatedAt = e.GetCreatedAt().AsTime().UTC().Format("2006-01-02T15:04:05Z07:00")
	}
	return env
}

// CreateEnvironment creates an environment.
func (f *Client) CreateEnvironment(ctx context.Context, name string, description *string) (*Environment, error) {
	c, err := f.controller()
	if err != nil {
		return nil, err
	}
	resp, err := c.CreateEnvironment(ctx, &isolate.CreateEnvironmentRequest{Name: name, Description: description})
	if err != nil {
		return nil, fmt.Errorf("creating environment %q: %w", name, err)
	}
	return environmentFromProto(resp.GetEnvironment()), nil
}

// GetEnvironment returns an environment, or ErrNotFound.
func (f *Client) GetEnvironment(ctx context.Context, name string) (*Environment, error) {
	c, err := f.controller()
	if err != nil {
		return nil, err
	}
	resp, err := c.ListEnvironments(ctx, &isolate.ListEnvironmentsRequest{})
	if err != nil {
		return nil, fmt.Errorf("listing environments: %w", err)
	}
	for _, e := range resp.GetEnvironments() {
		if e.GetName() == name {
			return environmentFromProto(e), nil
		}
	}
	return nil, ErrNotFound
}

// DeleteEnvironment deletes an environment. Deleting a missing environment is
// not an error.
func (f *Client) DeleteEnvironment(ctx context.Context, name string) error {
	c, err := f.controller()
	if err != nil {
		return err
	}
	_, err = c.DeleteEnvironment(ctx, &isolate.DeleteEnvironmentRequest{Name: name})
	if err != nil && status.Code(err) != codes.NotFound {
		return fmt.Errorf("deleting environment %q: %w", name, err)
	}
	return nil
}
