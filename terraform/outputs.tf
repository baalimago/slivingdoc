output "access_key_id" {
  description = "Access key ID of the created IAM user."
  value       = aws_iam_access_key.this.id
  sensitive   = true
}

output "secret_access_key" {
  description = "Secret access key of the created IAM user."
  value       = aws_iam_access_key.this.secret
  sensitive   = true
}

output "iam_user_arn" {
  description = "ARN of the created IAM user."
  value       = aws_iam_user.this.arn
}

output "bucket_arn" {
  description = "ARN of the created bucket."
  value       = aws_s3_bucket.this.arn
}
