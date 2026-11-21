package fal_test

import (
	"context"
	"errors"
	"testing"

	"github.com/fal-ai/terraform-provider-fal/internal/fakefal"
	"github.com/fal-ai/terraform-provider-fal/internal/fal"
)

func newClient(t *testing.T, key string) (*fal.Client, *fakefal.Server) {
	t.Helper()
	server, addr := fakefal.Start(t)
	c, err := fal.NewWithTemp(key, fal.WithHost(addr), fal.WithInsecure())
	if err != nil {
		t.Fatal(err)
	}
	return c, server
}

func TestSecretLifecycle(t *testing.T) {
	ctx := context.Background()
	c, server := newClient(t, fakefal.Key)

	exposed := false
	if err := c.SetSecret(ctx, "HF_TOKEN", "v1", fal.SetSecretOpts{DefaultExposed: &exposed}); err != nil {
		t.Fatal(err)
	}
	got, err := c.GetSecret(ctx, "HF_TOKEN", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Environment != "main" || got.DefaultExposed == nil || *got.DefaultExposed {
		t.Fatalf("unexpected secret %+v", got)
	}

	// Without DefaultExposed, an update keeps the current setting.
	if err := c.SetSecret(ctx, "HF_TOKEN", "v2", fal.SetSecretOpts{Environment: "main"}); err != nil {
		t.Fatal(err)
	}
	stored, _ := server.Secret("main", "HF_TOKEN")
	if stored.Value != "v2" || stored.DefaultExposed == nil || *stored.DefaultExposed {
		t.Fatalf("unexpected stored secret %+v", stored)
	}

	if err := c.DeleteSecret(ctx, "HF_TOKEN", "main"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetSecret(ctx, "HF_TOKEN", "main"); !errors.Is(err, fal.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	// Deleting again is not an error.
	if err := c.DeleteSecret(ctx, "HF_TOKEN", "main"); err != nil {
		t.Fatal(err)
	}
}

func TestSecretInMissingEnvironmentIsNotFound(t *testing.T) {
	c, _ := newClient(t, fakefal.Key)
	if _, err := c.GetSecret(context.Background(), "X", "gone"); !errors.Is(err, fal.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestEnvironmentLifecycle(t *testing.T) {
	ctx := context.Background()
	c, _ := newClient(t, fakefal.Key)

	desc := "staging apps"
	env, err := c.CreateEnvironment(ctx, "staging", &desc)
	if err != nil {
		t.Fatal(err)
	}
	if env.Name != "staging" || env.Description == nil || *env.Description != desc || env.CreatedAt == "" {
		t.Fatalf("unexpected environment %+v", env)
	}
	if _, err := c.GetEnvironment(ctx, "staging"); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteEnvironment(ctx, "staging"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetEnvironment(ctx, "staging"); !errors.Is(err, fal.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if err := c.DeleteEnvironment(ctx, "staging"); err != nil {
		t.Fatal(err)
	}
}

func TestWrongKeyIsRejected(t *testing.T) {
	c, _ := newClient(t, "test-key-id:wrong")
	if _, err := c.GetSecret(context.Background(), "X", ""); err == nil {
		t.Fatal("want an authentication error")
	}
}

func TestMalformedKey(t *testing.T) {
	c, _ := newClient(t, "no-colon")
	if _, err := c.GetEnvironment(context.Background(), "x"); err == nil {
		t.Fatal("want an error for a key without a colon")
	}
}
