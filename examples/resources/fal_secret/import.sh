# <environment>/<name>, or <name> for the main environment. fal never returns a
# secret's value, so the next apply sets it from the configuration.
terraform import fal_secret.api_key staging/API_KEY
terraform import fal_secret.hf_token HF_TOKEN
