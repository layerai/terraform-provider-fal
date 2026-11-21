# A secret in the main environment, exposed to every app.
resource "fal_secret" "hf_token" {
  name  = "HF_TOKEN"
  value = var.hf_token
}

# A secret kept out of Terraform's state (Terraform 1.11 or later), in its own
# environment, injected only into apps that list it in their secrets.
resource "fal_environment" "staging" {
  name        = "staging"
  description = "Apps under test"
}

resource "fal_secret" "api_key" {
  name             = "API_KEY"
  environment      = fal_environment.staging.name
  value_wo         = var.api_key
  value_wo_version = 1 # bump to set a new value_wo
  default_exposed  = false
}
