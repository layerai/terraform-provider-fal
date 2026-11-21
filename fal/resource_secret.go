package fal

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/fal-ai/terraform-provider-fal/internal/fal"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/resourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                     = &SecretResource{}
	_ resource.ResourceWithConfigure        = &SecretResource{}
	_ resource.ResourceWithImportState      = &SecretResource{}
	_ resource.ResourceWithConfigValidators = &SecretResource{}
)

func NewSecretResource() resource.Resource {
	return &SecretResource{}
}

// SecretResource manages a fal secret, the equivalent of `fal secrets set`.
type SecretResource struct {
	client *fal.Client
}

type SecretResourceModel struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	Environment    types.String `tfsdk:"environment"`
	Value          types.String `tfsdk:"value"`
	ValueWO        types.String `tfsdk:"value_wo"`
	ValueWOVersion types.Int64  `tfsdk:"value_wo_version"`
	DefaultExposed types.Bool   `tfsdk:"default_exposed"`
}

func (r *SecretResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_secret"
}

func (r *SecretResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A secret that fal injects into apps as an environment variable, the equivalent of `fal secrets set`.\n\n" +
			"fal never returns a secret's value, so Terraform cannot detect a value changed outside Terraform. " +
			"Set the value with either `value`, which Terraform keeps in its state, or `value_wo` (Terraform 1.11 or later), which it does not.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The secret's ID, `<environment>/<name>`.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The secret's name, which is also the name of the environment variable apps see.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"environment": schema.StringAttribute{
				MarkdownDescription: fmt.Sprintf("The fal environment the secret belongs to. Defaults to `%s`.", fal.MainEnvironment),
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString(fal.MainEnvironment),
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"value": schema.StringAttribute{
				MarkdownDescription: "The secret's value. Terraform stores it in its state; to keep it out, use `value_wo` instead.",
				Optional:            true,
				Sensitive:           true,
			},
			"value_wo": schema.StringAttribute{
				MarkdownDescription: "The secret's value, never stored in Terraform's state (write-only, Terraform 1.11 or later). " +
					"Terraform cannot tell when it changes, so change `value_wo_version` to set a new value.",
				Optional:  true,
				Sensitive: true,
				WriteOnly: true,
			},
			"value_wo_version": schema.Int64Attribute{
				MarkdownDescription: "Any number, required with `value_wo`; change it to set `value_wo` again.",
				Optional:            true,
				Validators: []validator.Int64{
					int64validator.AlsoRequires(path.MatchRoot("value_wo")),
				},
			},
			"default_exposed": schema.BoolAttribute{
				MarkdownDescription: "Whether fal injects the secret into every app (`true`), or only into apps that list it in their secrets (`false`). " +
					"When not set, a new secret follows the account's default, and an existing one keeps its setting.",
				Optional: true,
				Computed: true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *SecretResource) ConfigValidators(ctx context.Context) []resource.ConfigValidator {
	return []resource.ConfigValidator{
		resourcevalidator.ExactlyOneOf(
			path.MatchRoot("value"),
			path.MatchRoot("value_wo"),
		),
		resourcevalidator.RequiredTogether(
			path.MatchRoot("value_wo"),
			path.MatchRoot("value_wo_version"),
		),
	}
}

func (r *SecretResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func secretID(environment, name string) string {
	return environment + "/" + name
}

// secretValue returns the value to send to fal: `value` from the plan, or the
// write-only `value_wo`, which only the configuration holds.
func secretValue(ctx context.Context, plan SecretResourceModel, config interface {
	GetAttribute(context.Context, path.Path, interface{}) diag.Diagnostics
}, diags *diag.Diagnostics) string {
	if !plan.Value.IsNull() {
		return plan.Value.ValueString()
	}
	var wo types.String
	diags.Append(config.GetAttribute(ctx, path.Root("value_wo"), &wo)...)
	return wo.ValueString()
}

func boolPointer(b types.Bool) *bool {
	if b.IsNull() || b.IsUnknown() {
		return nil
	}
	v := b.ValueBool()
	return &v
}

// refresh reads the secret back from fal into the model.
func (r *SecretResource) refresh(ctx context.Context, data *SecretResourceModel) error {
	secret, err := r.client.GetSecret(ctx, data.Name.ValueString(), data.Environment.ValueString())
	if err != nil {
		return err
	}
	data.ID = types.StringValue(secretID(secret.Environment, secret.Name))
	data.Environment = types.StringValue(secret.Environment)
	if secret.DefaultExposed != nil {
		data.DefaultExposed = types.BoolValue(*secret.DefaultExposed)
	} else if data.DefaultExposed.IsUnknown() {
		data.DefaultExposed = types.BoolNull()
	}
	return nil
}

func (r *SecretResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan SecretResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	value := secretValue(ctx, plan, req.Config, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.SetSecret(ctx, plan.Name.ValueString(), value, fal.SetSecretOpts{
		Environment:    plan.Environment.ValueString(),
		DefaultExposed: boolPointer(plan.DefaultExposed),
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to create fal secret", err.Error())
		return
	}

	if err := r.refresh(ctx, &plan); err != nil {
		resp.Diagnostics.AddError("Unable to read fal secret after creating it", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *SecretResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state SecretResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.refresh(ctx, &state); err != nil {
		if errors.Is(err, fal.ErrNotFound) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Unable to read fal secret", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *SecretResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan SecretResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	value := secretValue(ctx, plan, req.Config, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.SetSecret(ctx, plan.Name.ValueString(), value, fal.SetSecretOpts{
		Environment:    plan.Environment.ValueString(),
		DefaultExposed: boolPointer(plan.DefaultExposed),
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to update fal secret", err.Error())
		return
	}

	if err := r.refresh(ctx, &plan); err != nil {
		resp.Diagnostics.AddError("Unable to read fal secret after updating it", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *SecretResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state SecretResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteSecret(ctx, state.Name.ValueString(), state.Environment.ValueString()); err != nil {
		resp.Diagnostics.AddError("Unable to delete fal secret", err.Error())
	}
}

// ImportState takes `<environment>/<name>`, or a bare `<name>` in the main
// environment. The value is not imported: fal never returns it, so the next
// apply sets it from the configuration.
func (r *SecretResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	environment, name, found := strings.Cut(req.ID, "/")
	if !found {
		environment, name = fal.MainEnvironment, req.ID
	}
	if environment == "" || name == "" {
		resp.Diagnostics.AddError("Invalid import ID", fmt.Sprintf("Expected `<environment>/<name>` or `<name>`, got %q.", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), secretID(environment, name))...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), name)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("environment"), environment)...)
}
