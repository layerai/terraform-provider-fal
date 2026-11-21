// Package fakefal is an in-memory IsolateController for tests: it keeps
// secrets and environments, and checks that every call carries a fal key.
package fakefal

import (
	"context"
	"net"
	"sort"
	"sync"
	"testing"

	"github.com/fal-ai/terraform-provider-fal/internal/isolate"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	KeyID     = "test-key-id"
	KeySecret = "test-key-secret"
	// Key is the fal key the server accepts.
	Key = KeyID + ":" + KeySecret
)

// Secret is a stored secret, value included.
type Secret struct {
	Value          string
	DefaultExposed *bool
}

type Server struct {
	isolate.UnimplementedIsolateControllerServer

	mu           sync.Mutex
	secrets      map[string]map[string]Secret // environment -> name -> secret
	environments map[string]*isolate.EnvironmentInfo
}

// Start serves a fake controller on localhost and returns it with its address.
func Start(t *testing.T) (*Server, string) {
	t.Helper()
	s := &Server{
		secrets: map[string]map[string]Secret{},
		environments: map[string]*isolate.EnvironmentInfo{
			"main": {Name: "main", IsDefault: true, CreatedAt: timestamppb.Now()},
		},
	}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	g := grpc.NewServer(grpc.UnaryInterceptor(checkKey))
	isolate.RegisterIsolateControllerServer(g, s)
	go func() { _ = g.Serve(lis) }()
	t.Cleanup(g.Stop)
	return s, lis.Addr().String()
}

func checkKey(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	if first(md.Get("auth-key-id")) != KeyID || first(md.Get("auth-key")) != KeySecret {
		return nil, status.Error(codes.Unauthenticated, "invalid fal key")
	}
	return handler(ctx, req)
}

func first(v []string) string {
	if len(v) == 0 {
		return ""
	}
	return v[0]
}

func envName(name *string) string {
	if name == nil || *name == "" {
		return "main"
	}
	return *name
}

// Secret returns a stored secret.
func (s *Server) Secret(environment, name string) (Secret, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	secret, ok := s.secrets[environment][name]
	return secret, ok
}

// DeleteSecretOutOfBand deletes a secret as if someone ran `fal secrets unset`.
func (s *Server) DeleteSecretOutOfBand(environment, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.secrets[environment], name)
}

// HasEnvironment reports whether an environment exists.
func (s *Server) HasEnvironment(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.environments[name]
	return ok
}

func (s *Server) SetSecret(_ context.Context, req *isolate.SetSecretRequest) (*isolate.SetSecretResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	env := envName(req.EnvironmentName)
	if _, ok := s.environments[env]; !ok {
		return nil, status.Errorf(codes.NotFound, "environment %q not found", env)
	}
	if req.Value == nil {
		delete(s.secrets[env], req.GetName())
		return &isolate.SetSecretResponse{}, nil
	}
	if s.secrets[env] == nil {
		s.secrets[env] = map[string]Secret{}
	}
	secret := Secret{Value: req.GetValue(), DefaultExposed: req.DefaultExposed}
	if prev, ok := s.secrets[env][req.GetName()]; ok && req.DefaultExposed == nil {
		secret.DefaultExposed = prev.DefaultExposed
	}
	s.secrets[env][req.GetName()] = secret
	return &isolate.SetSecretResponse{}, nil
}

func (s *Server) ListSecrets(_ context.Context, req *isolate.ListSecretsRequest) (*isolate.ListSecretsResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	env := envName(req.EnvironmentName)
	if _, ok := s.environments[env]; !ok {
		return nil, status.Errorf(codes.NotFound, "environment %q not found", env)
	}
	resp := &isolate.ListSecretsResponse{}
	for name, secret := range s.secrets[env] {
		e := env
		resp.Secrets = append(resp.Secrets, &isolate.Secret{
			Name: name, EnvironmentName: &e, DefaultExposed: secret.DefaultExposed, CreatedTime: timestamppb.Now(),
		})
	}
	sort.Slice(resp.Secrets, func(i, j int) bool { return resp.Secrets[i].Name < resp.Secrets[j].Name })
	return resp, nil
}

func (s *Server) ListEnvironments(context.Context, *isolate.ListEnvironmentsRequest) (*isolate.ListEnvironmentsResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	resp := &isolate.ListEnvironmentsResponse{}
	for _, e := range s.environments {
		resp.Environments = append(resp.Environments, e)
	}
	return resp, nil
}

func (s *Server) CreateEnvironment(_ context.Context, req *isolate.CreateEnvironmentRequest) (*isolate.CreateEnvironmentResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.environments[req.GetName()]; ok {
		return nil, status.Errorf(codes.AlreadyExists, "environment %q already exists", req.GetName())
	}
	e := &isolate.EnvironmentInfo{Name: req.GetName(), Description: req.Description, CreatedAt: timestamppb.Now()}
	s.environments[e.Name] = e
	return &isolate.CreateEnvironmentResponse{Environment: e}, nil
}

func (s *Server) DeleteEnvironment(_ context.Context, req *isolate.DeleteEnvironmentRequest) (*isolate.DeleteEnvironmentResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.environments[req.GetName()]; !ok {
		return nil, status.Errorf(codes.NotFound, "environment %q not found", req.GetName())
	}
	delete(s.environments, req.GetName())
	delete(s.secrets, req.GetName())
	return &isolate.DeleteEnvironmentResponse{}, nil
}
