terraform {
  required_version = ">= 1.5"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }
  }
}

locals {
  iam_user_name = coalesce(var.iam_user_name, var.bucket_name)
}

data "aws_caller_identity" "current" {}

# --- Bucket ---------------------------------------------------------------

resource "aws_s3_bucket" "this" {
  bucket        = var.bucket_name
  force_destroy = var.force_destroy

  tags = var.tags
}

resource "aws_s3_bucket_versioning" "this" {
  bucket = aws_s3_bucket.this.id

  versioning_configuration {
    status = "Enabled"
  }
}

# BucketOwnerEnforced disables access control lists: IAM is the only access
# path, and no ACL can ever re-open the bucket.
resource "aws_s3_bucket_ownership_controls" "this" {
  bucket = aws_s3_bucket.this.id

  rule {
    object_ownership = "BucketOwnerEnforced"
  }
}

# Notebook packs are encrypted with SSE-S3 at rest. SSE-C uploads are blocked
# so no client can bypass the default encryption with a customer-provided key.
resource "aws_s3_bucket_server_side_encryption_configuration" "this" {
  bucket = aws_s3_bucket.this.id

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }

    bucket_key_enabled       = true
    blocked_encryption_types = ["SSE-C"]
  }
}

# The bucket is reachable from the public internet. These four blocks make
# anonymous access impossible even if a public policy or ACL is added later.
resource "aws_s3_bucket_public_access_block" "this" {
  bucket = aws_s3_bucket.this.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

# Defense in depth on top of the public access block: deny every S3 action to
# every principal except the notebook user and the account root. The root
# exception keeps the bucket manageable and destroyable; every other IAM
# principal and every AWS service is refused.
data "aws_iam_policy_document" "deny_all_except_user" {
  statement {
    sid       = "DenyAllExceptNotebookUser"
    effect    = "Deny"
    actions   = ["s3:*"]
    resources = [aws_s3_bucket.this.arn, "${aws_s3_bucket.this.arn}/*"]

    principals {
      type        = "*"
      identifiers = ["*"]
    }

    condition {
      test     = "StringNotEquals"
      variable = "aws:PrincipalArn"
      values = [
        aws_iam_user.this.arn,
        "arn:aws:iam::${data.aws_caller_identity.current.account_id}:root",
      ]
    }
  }
}

resource "aws_s3_bucket_policy" "deny_all_except_user" {
  bucket = aws_s3_bucket.this.id
  policy = data.aws_iam_policy_document.deny_all_except_user.json
}

# --- IAM user -------------------------------------------------------------

resource "aws_iam_user" "this" {
  name = local.iam_user_name
  tags = var.tags
}

# The user can touch this bucket and nothing else: object read/write/delete,
# multipart upload, and the bucket listing the cleanup pass needs. No IAM,
# no other buckets, no bucket configuration.
data "aws_iam_policy_document" "user_bucket" {
  statement {
    sid    = "AllowBucketObjects"
    effect = "Allow"
    actions = [
      "s3:GetObject",
      "s3:PutObject",
      "s3:DeleteObject",
      "s3:CreateMultipartUpload",
      "s3:UploadPart",
      "s3:CompleteMultipartUpload",
      "s3:AbortMultipartUpload",
      "s3:ListMultipartUploadParts",
    ]
    resources = [aws_s3_bucket.this.arn, "${aws_s3_bucket.this.arn}/*"]
  }

  statement {
    sid    = "AllowListBucket"
    effect = "Allow"
    actions = [
      "s3:ListBucket",
      "s3:ListBucketMultipartUploads",
    ]
    resources = [aws_s3_bucket.this.arn]
  }
}

resource "aws_iam_user_policy" "bucket" {
  name   = "notebook-bucket"
  user   = aws_iam_user.this.name
  policy = data.aws_iam_policy_document.user_bucket.json
}

resource "aws_iam_access_key" "this" {
  user = aws_iam_user.this.name
}
