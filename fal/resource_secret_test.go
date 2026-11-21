package fal

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/fal-ai/terraform-provider-fal/internal/fakefal"
	"github.com/fal-ai/terraform-provider-fal/internal/fal"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// testProvider serves the provider against a fake fal on addr.
func testProvider(addr string) map[string]func() (tfprotov6.ProviderServer, error) {
	return map[string]func() (tfprotov6.ProviderServer, error){
		"fal": providerserver.NewProtocol6WithError(&falProvider{version: "test", clientOpts: []fal.Option{fal.WithInsecure()}}),
	}
}

func providerConfig(addr string) string {
	return fmt.Sprintf(`
provider "fal" {
  fal_key = %q
  host    = %q
}
`, fakefal.Key, addr)
}

func checkStored(server *fakefal.Server, env, name, value string, exposed *bool) resource.TestCheckFunc {
	return func(*terraform.State) error {
		s, ok := server.Secret(env, name)
		if !ok {
			return fmt.Errorf("secret %s/%s not stored", env, name)
		}
		if s.Value != value {
			return fmt.Errorf("secret %s/%s = %q, want %q", env, name, s.Value, value)
		}
		if (exposed == nil) != (s.DefaultExposed == nil) || (exposed != nil && *exposed != *s.DefaultExposed) {
			return fmt.Errorf("secret %s/%s default_exposed = %v, want %v", env, name, s.DefaultExposed, exposed)
		}
		return nil
	}
}

func checkGone(server *fakefal.Server, env, name string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		if _, ok := server.Secret(env, name); ok {
			return fmt.Errorf("secret %s/%s still stored", env, name)
		}
		return nil
	}
}

func boolp(b bool) *bool { return &b }

func TestSecretResource(t *testing.T) {
	server, addr := fakefal.Start(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProvider(addr),
		CheckDestroy:             checkGone(server, "main", "HF_TOKEN"),
		Steps: []resource.TestStep{
			{
				Config: providerConfig(addr) + `
resource "fal_secret" "hf" {
  name  = "HF_TOKEN"
  value = "one"
}
`,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("fal_secret.hf", "id", "main/HF_TOKEN"),
					resource.TestCheckResourceAttr("fal_secret.hf", "environment", "main"),
					resource.TestCheckNoResourceAttr("fal_secret.hf", "default_exposed"),
					checkStored(server, "main", "HF_TOKEN", "one", nil),
				),
			},
			{
				// A new value and default_exposed update in place.
				Config: providerConfig(addr) + `
resource "fal_secret" "hf" {
  name            = "HF_TOKEN"
  value           = "two"
  default_exposed = false
}
`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("fal_secret.hf", plancheck.ResourceActionUpdate)},
				},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("fal_secret.hf", "default_exposed", "false"),
					checkStored(server, "main", "HF_TOKEN", "two", boolp(false)),
				),
			},
			{
				ResourceName:            "fal_secret.hf",
				ImportState:             true,
				ImportStateId:           "HF_TOKEN",
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"value"},
			},
		},
	})
}

func TestSecretResourceWriteOnly(t *testing.T) {
	server, addr := fakefal.Start(t)
	config := func(value string, version int) string {
		return providerConfig(addr) + fmt.Sprintf(`
resource "fal_environment" "staging" {
  name = "staging"
}

resource "fal_secret" "api" {
  name             = "API_KEY"
  environment      = fal_environment.staging.name
  value_wo         = %q
  value_wo_version = %d
}
`, value, version)
	}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProvider(addr),
		CheckDestroy: func(s *terraform.State) error {
			if server.HasEnvironment("staging") {
				return fmt.Errorf("environment staging still exists")
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: config("one", 1),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("fal_secret.api", "id", "staging/API_KEY"),
					resource.TestCheckNoResourceAttr("fal_secret.api", "value_wo"),
					checkStored(server, "staging", "API_KEY", "one", nil),
				),
			},
			{
				// A new write-only value without a version bump is not applied.
				Config: config("two", 1),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: checkStored(server, "staging", "API_KEY", "one", nil),
			},
			{
				Config: config("two", 2),
				Check:  checkStored(server, "staging", "API_KEY", "two", nil),
			},
		},
	})
}

func TestSecretResourceRecreatedWhenDeletedOutsideTerraform(t *testing.T) {
	server, addr := fakefal.Start(t)
	config := providerConfig(addr) + `
resource "fal_secret" "hf" {
  name  = "HF_TOKEN"
  value = "one"
}
`
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProvider(addr),
		Steps: []resource.TestStep{
			{Config: config},
			{
				PreConfig: func() { server.DeleteSecretOutOfBand("main", "HF_TOKEN") },
				Config:    config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("fal_secret.hf", plancheck.ResourceActionCreate)},
				},
				Check: checkStored(server, "main", "HF_TOKEN", "one", nil),
			},
		},
	})
}

func TestSecretResourceValidation(t *testing.T) {
	_, addr := fakefal.Start(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProvider(addr),
		Steps: []resource.TestStep{
			{
				Config: providerConfig(addr) + `
resource "fal_secret" "x" {
  name = "X"
}
`,
				ExpectError: regexp.MustCompile(`Exactly one of these attributes must be configured`),
			},
			{
				Config: providerConfig(addr) + `
resource "fal_secret" "x" {
  name     = "X"
  value    = "a"
  value_wo = "b"
  value_wo_version = 1
}
`,
				ExpectError: regexp.MustCompile(`Invalid Attribute Combination`),
			},
			{
				Config: providerConfig(addr) + `
resource "fal_secret" "x" {
  name     = "X"
  value_wo = "b"
}
`,
				ExpectError: regexp.MustCompile(`Invalid Attribute Combination`),
			},
		},
	})
}

func TestEnvironmentResource(t *testing.T) {
	server, addr := fakefal.Start(t)
	config := func(desc string) string {
		return providerConfig(addr) + fmt.Sprintf(`
resource "fal_environment" "staging" {
  name        = "staging"
  description = %q
}
`, desc)
	}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProvider(addr),
		Steps: []resource.TestStep{
			{
				Config: config("first"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("fal_environment.staging", "id", "staging"),
					resource.TestCheckResourceAttr("fal_environment.staging", "is_default", "false"),
					resource.TestCheckResourceAttrSet("fal_environment.staging", "created_at"),
				),
			},
			{
				Config: config("second"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("fal_environment.staging", plancheck.ResourceActionDestroyBeforeCreate)},
				},
				Check: resource.TestCheckResourceAttr("fal_environment.staging", "description", "second"),
			},
			{
				ResourceName:      "fal_environment.staging",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
	if server.HasEnvironment("staging") {
		t.Fatal("environment staging not destroyed")
	}
}
