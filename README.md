# fal Terraform Provider

The [fal Terraform Provider](https://registry.terraform.io/providers/fal-ai/fal/latest/docs) offers convenient access to [fal's APIs](https://docs.fal.ai/serverless) from Terraform.

## Requirements

To manage `fal_app` resources, you will need to [install uv](https://docs.astral.sh/uv/getting-started/installation/) into your Terraform host environment.

If you're running Terraform in GitHub Actions, an example of this might look like:
```yaml
name: Example

jobs:
   terraform-uv-example:
      name: python
      runs-on: ubuntu-latest

      steps:
         - uses: actions/checkout@v4

         - name: Install uv
           uses: astral-sh/setup-uv@v6

         - name: Install terraform
           uses: hashicorp/setup-terraform@v3
      
         - run: terraform init
```

## Usage

Add the following to your `main.tf` file:
```terraform
terraform {
  required_providers {
    fal = {
      source = "fal-ai/fal"
      version = "~> 1"
    }
  }
}

provider "fal" {
  fal_key = "<fal key here>"
}

resource "fal_app" "sana_app" {
  entrypoint = "fal_demos/image/sana.py"
  git = {
    url = "https://github.com/fal-ai-community/fal-demos.git"
  }
}
```

Manage the secrets fal injects into your apps, without uv:
```terraform
resource "fal_secret" "hf_token" {
  name  = "HF_TOKEN"
  value = var.hf_token
}
```

Initialize your project by running `terraform init` in the directory.

You can refer to the full documentation on [the Terraform Registry](https://registry.terraform.io/providers/fal-ai/fal/latest/docs).

## Development

```sh
make test           # unit tests, plus resource tests through Terraform against a fake fal
make generate-proto # after changing internal/isolate/controller.proto (needs protoc)
go generate ./...   # regenerates docs/; restore the hand-written docs/index.md and docs/resources/app.md
```
