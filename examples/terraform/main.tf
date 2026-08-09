# The AWS debug bucket, provisioned through the slivingdoc module.
#
# This root configuration recreates the debug bucket "slivingdoc" in
# account 930819553298, region eu-north-1, with the hardened module
# defaults. The debug MCP server (registered in
# ~/.config/.clai/mcpServers/slivingdoc.json) reads and writes this bucket
# with the access keys the module creates. See README.md for the recipe.

terraform {
  required_version = ">= 1.5"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }
  }
}

provider "aws" {
  region = "eu-north-1"
}

module "slivingdoc" {
  # GitHub module source: the //terraform suffix selects the subdirectory.
  # No ?ref means the default branch (latest); pin a release with
  # ?ref=vX.Y.Z. See the module README.
  source = "github.com/baalimago/slivingdoc//terraform"

  bucket_name   = "slivingdoc"
  iam_user_name = "slivingdoc-mcp"

  tags = {
    purpose  = "slivingdoc-notes"
    creation = "manual"
  }
}

output "access_key_id" {
  value     = module.slivingdoc.access_key_id
  sensitive = true
}

output "secret_access_key" {
  value     = module.slivingdoc.secret_access_key
  sensitive = true
}
