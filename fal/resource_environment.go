package fal

import (
	"context"
	"errors"

	"github.com/fal-ai/terraform-provider-fal/internal/fal"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &EnvironmentResource{}
	_ resource.ResourceWithConfigure   = &EnvironmentResource{}
	_ resource.ResourceWithImportState = &EnvironmentResource{}
)

func NewEnvironmentResource() resource.Resource {
	return &EnvironmentResource{}
}

// EnvironmentResource manages a fal environment, the equivalent of
// `fal environments create`.
type EnvironmentResource struct {
	client *fal.Client
}

type EnvironmentResourceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	IsDefault   types.Bool   `tfsdk:"is_default"`
	CreatedAt   types.String `tfsdk:"created_at"`
}

func (r *EnvironmentResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_environment"
}

func (r *EnvironmentResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A fal environment, which holds its own apps and secrets, the equivalent of `fal environments create`. " +
			"Every account has a `" + fal.MainEnvironment + "` environment, which needs no resource. " +
			"Deleting an environment deletes everything in it.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The environment's name.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The environment's name.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
					stringvalidator.NoneOf(fal.MainEnvironment),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "A description of the environment. When not set, Terraform leaves the environment's description as it is, so importing an environment without copying its description is safe. " +
					"fal cannot change a description in place, so setting a different one replaces the environment, deleting everything in it.",
				Optional: true,
				Computed: true,
				PlanModifiers: []planmodifier.String{
					// Keeps the current description when the configuration has none,
					// so RequiresReplace below sees no change.
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplace(),
				},
			},
			"is_default": schema.BoolAttribute{
				MarkdownDescription: "Whether this is the account's default environment.",
				Computed:            true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "When the environment was created (RFC 3339).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *EnvironmentResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (m *EnvironmentResourceModel) set(env *fal.Environment) {
	m.ID = types.StringValue(env.Name)
	m.Name = types.StringValue(env.Name)
	m.Description = types.StringPointerValue(env.Description)
	m.IsDefault = types.BoolValue(env.IsDefault)
	m.CreatedAt = types.StringValue(env.CreatedAt)
}

func (r *EnvironmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan EnvironmentResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	env, err := r.client.CreateEnvironment(ctx, plan.Name.ValueString(), plan.Description.ValueStringPointer())
	if err != nil {
		resp.Diagnostics.AddError("Unable to create fal environment", err.Error())
		return
	}
	plan.set(env)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *EnvironmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state EnvironmentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	env, err := r.client.GetEnvironment(ctx, state.Name.ValueString())
	if errors.Is(err, fal.ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read fal environment", err.Error())
		return
	}
	state.set(env)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update is never called with a change: every configurable attribute
// requires replacement.
func (r *EnvironmentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan EnvironmentResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *EnvironmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state EnvironmentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteEnvironment(ctx, state.Name.ValueString()); err != nil {
		resp.Diagnostics.AddError("Unable to delete fal environment", err.Error())
	}
}

func (r *EnvironmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), req.ID)...)
}
