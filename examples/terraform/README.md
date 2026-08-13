# AWS bucket for slivingdoc debugging

This example provisions the live AWS bucket `slivingdoc` through the
reusable Terraform module in [`terraform/`](../../terraform/). The debug
MCP server reads and writes this bucket. The server entry lives in your
MCP host's configuration and runs the server through the npm launcher
(`npx -y slivingdoc`), which selects, downloads, verifies, and runs the
matching release binary.

The example is for manual debugging only. The automated test suites use
their own MinIO containers through testcontainers and never touch AWS.

## 1. Bucket spec

The bucket lives in account `930819553298`, region `eu-north-1`. The
module provisions it with these settings:

| Setting                  | Value                                         |
| ------------------------ | --------------------------------------------- |
| Versioning               | Enabled                                       |
| Ownership                | BucketOwnerEnforced                           |
| Default encryption       | SSE-S3 (AES256), bucket key enabled           |
| Blocked encryption types | SSE-C                                         |
| Bucket policy            | Deny all principals except `slivingdoc-mcp` and the account root |
| Public access block      | all four blocks on                            |
| Tags                     | `purpose=slivingdoc-notes`, `creation=manual` |

The bucket is reachable from the public internet, so anonymous access is
blocked twice: the public access block forbids public policies and ACLs,
and the bucket policy denies every S3 action to every principal except the
`slivingdoc-mcp` user and the account root. The root exemption is the
recovery path. It keeps `terraform destroy` and key recovery possible.

Applying this configuration to a bucket that predates the module adds
the hardening and creates the IAM user and access keys.

## 2. Access

The module creates the IAM user `slivingdoc-mcp` and its access key pair.
The user has one inline policy that grants read and write on the bucket and
nothing else. Fetch the keys from the Terraform outputs:

```text
terraform output -raw access_key_id
terraform output -raw secret_access_key
```

The access key pair is stored in the environment of the MCP server entry,
never in Terraform state files that leave this machine:

```json
{
  "command": "npx",
  "args": [
    "-y",
    "slivingdoc",
    "serve",
    "--bucket",
    "slivingdoc",
    "--region",
    "eu-north-1",
    "--workspace-root",
    "/path/to/slivingdoc-debug/notes",
    "--private-root",
    "/path/to/slivingdoc-debug/private"
  ],
  "env": {
    "AWS_ACCESS_KEY_ID": "<access key id>",
    "AWS_SECRET_ACCESS_KEY": "<secret access key>"
  },
  "name": "slivingdoc"
}
```

Replace the two placeholders with the output values. The file mode is
`0600`.

## 3. Apply

Run the commands from this directory:

```text
terraform init
terraform plan
terraform apply
```

The module source is `github.com/baalimago/slivingdoc//terraform`.
`terraform init` clones it from GitHub. The `//terraform` suffix selects the
subdirectory. No `?ref` means the default branch (latest). Pin a release
with `?ref=vX.Y.Z`. The module must be committed and pushed before the
reference resolves.

Applying this configuration to the live bucket changes it: it attaches the
deny-everything bucket policy, turns all four public access blocks on, and
creates the `slivingdoc-mcp` user with its keys.

## 4. Notes

To deploy a second notebook, call the module from your own root
configuration with a different `bucket_name` (see the module README). The
module keeps the account root exempt from the deny, so the bucket always
stays manageable. A "sealed bucket" variant that also denies the root is
possible, but it locks out everyone except the created user. Lost keys are
then unrecoverable without AWS Support.

`terraform destroy` fails while the bucket still contains notebook state
unless the module is called with `force_destroy = true`. Delete the
private root and pull again to rebuild the local cache from the bucket.
