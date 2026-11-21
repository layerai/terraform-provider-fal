package fal

import (
	"context"
	"os"

	"github.com/fal-ai/terraform-provider-fal/internal/fal"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const (
	userAgentForProvider = "Fal-Terraform-Provider"
)

var currentVersion = "1.0.0"

// Ensure falProvider satisfies various provider interfaces
var _ provider.Provider = &falProvider{}

// falProvider defines the provider implementation.
type falProvider struct {
	version string
	// clientOpts are extra client options, for tests.
	clientOpts []fal.Option
}

// falProviderModel describes the provider data model.
type falProviderModel struct {
	FalKey types.String `tfsdk:"fal_key"`
	Host   types.String `tfsdk:"host"`
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &falProvider{
			version: version,
		}
	}
}

func (p *falProvider) Metadata(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "fal"
	resp.Version = p.version
}

func (p *falProvider) Schema(ctx context.Context, req provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"fal_key": schema.StringAttribute{
				MarkdownDescription: "fal's authentication key. Can also be set via the FAL_KEY environment variable.",
				Optional:            true,
				Sensitive:           true,
			},
			"host": schema.StringAttribute{
				MarkdownDescription: "The fal API endpoint that secrets and environments are managed through, as `host[:port]`. Defaults to `" + fal.DefaultHost + "`. Can also be set via the `FAL_GRPC_HOST` environment variable.",
				Optional:            true,
			},
		},
	}
}

func getFalKey(data falProviderModel) (string, bool) {
	falKey := data.FalKey.ValueString()

	if falKey != "" {
		return falKey, true
	}

	falKey = os.Getenv("FAL_KEY")
	if falKey != "" {
		return falKey, true
	}

	return "", false
}

func (p *falProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var data falProviderModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	falKey, found := getFalKey(data)

	if !found {
		resp.Diagnostics.AddError(
			"Missing fal key",
			"Either 'fal_key' must be set in the provider configuration or FAL_KEY environment variable must be set.",
		)
		return
	}

	host := data.Host.ValueString()
	if host == "" {
		host = os.Getenv("FAL_GRPC_HOST")
	}

	client, err := fal.NewWithTemp(falKey, append([]fal.Option{fal.WithHost(host)}, p.clientOpts...)...)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to create fal's api client",
			err.Error(),
		)
		return
	}
	resp.DataSourceData = client
	resp.ResourceData = client
}

func (p *falProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewAppResource,
		NewEnvironmentResource,
		NewSecretResource,
	}
}

func (p *falProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{}
}
