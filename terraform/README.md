# slivingdoc S3 bucket module

This module provisions the private S3 bucket a slivingdoc server needs,
together with the least-privilege IAM user whose access keys the server
uses. It is the reusable form of the original AWS example.

The bucket is reachable from the public internet, so the module makes
anonymous access impossible and limits bucket access to exactly one IAM
user. The AWS account root stays exempt so the bucket can always be
managed and destroyed.

## Usage

```hcl
module "notebook" {
  # GitHub source: //terraform selects the subdirectory. No ?ref means the
  # default branch (latest); pin a release with ?ref=vX.Y.Z.
  source = "github.com/baalimago/slivingdoc//terraform"

  bucket_name   = "my-notes"
  iam_user_name = "my-notes-mcp"

  tags = {
    purpose = "slivingdoc-notes"
  }
}

output "access_key_id" {
  value     = module.notebook.access_key_id
  sensitive = true
}

output "secret_access_key" {
  value     = module.notebook.secret_access_key
  sensitive = true
}
```

Configure the `aws` provider in the root module; the module does not set a
region. The access keys come from the sensitive outputs:

```text
terraform output -raw access_key_id
terraform output -raw secret_access_key
```

## Inputs

| Name            | Type          | Default      | Effect                                    |
| --------------- | ------------- | ------------ | ----------------------------------------- |
| `bucket_name`   | `string`      | _(required)_ | Name of the S3 bucket.                    |
| `tags`          | `map(string)` | `{}`         | Tags applied to the bucket and the user.  |
| `iam_user_name` | `string`      | bucket name  | Name of the created IAM user.             |
| `force_destroy` | `bool`        | `false`      | Allow bucket deletion with objects in it. |

## Outputs

| Name                | Sensitive | Meaning                            |
| ------------------- | --------- | ---------------------------------- |
| `access_key_id`     | yes       | Access key ID of the IAM user.     |
| `secret_access_key` | yes       | Secret access key of the IAM user. |
| `iam_user_arn`      | no        | ARN of the created IAM user.       |
| `bucket_arn`        | no        | ARN of the created bucket.         |

## What the module creates

The bucket has versioning enabled, `BucketOwnerEnforced` ownership (no
ACLs), SSE-S3 default encryption with the bucket key enabled, and SSE-C
uploads blocked. The public access block sets all four blocks to true.

A bucket policy denies every S3 action to every principal except the
created IAM user and the account root. This is defense in depth on top of
the public access block: no anonymous access, no other IAM principal, and
no AWS service can touch the bucket. The root exemption is deliberate — it
is the recovery path and the only way `terraform destroy` can complete.

The IAM user carries one inline policy scoped to this bucket only: object
get/put/delete, multipart upload and abort, and the bucket listing the
cleanup pass needs. The user cannot manage the bucket, IAM, or any other
AWS resource.

## Notes

- The account root is not denied, so it can always delete the bucket
  policy and the bucket. A stricter "sealed bucket" variant is possible
  but is deliberately not used: a lost access key pair would then lock the
  bucket until AWS Support intervenes.
- `force_destroy` is required to delete a bucket that still contains
  notebook state; without it the destroy fails while objects exist.
- The bucket policy is replaced before the IAM user on destroy, so teardown
  never hits the deny.
- The automated slivingdoc test suites never touch AWS; they use their own
  MinIO containers.
