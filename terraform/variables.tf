variable "bucket_name" {
  description = "Name of the S3 bucket that holds the notebook state."
  type        = string
}

variable "tags" {
  description = "Tags applied to the bucket and the IAM user."
  type        = map(string)
  default     = {}
}

variable "iam_user_name" {
  description = "Name of the IAM user that owns the access keys. Defaults to the bucket name."
  type        = string
  default     = null
}

variable "force_destroy" {
  description = "Allow deletion of the bucket when it still contains objects."
  type        = bool
  default     = false
}
