package fal

import (
	"fmt"

	"github.com/fal-ai/terraform-provider-fal/internal/fal"
	"github.com/hashicorp/terraform-plugin-framework/diag"
)

// clientFromProviderData returns the client the provider configured, or nil
// before the provider is configured.
func clientFromProviderData(data any, diags *diag.Diagnostics) *fal.Client {
	if data == nil {
		return nil
	}
	client, ok := data.(*fal.Client)
	if !ok {
		diags.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *fal.Client, got: %T. Please report this issue to the provider developers.", data),
		)
		return nil
	}
	return client
}
