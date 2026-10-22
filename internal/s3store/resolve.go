package s3store

import "context"

// Resolved is the store identity a configuration addresses: the shared-config
// selector the configuration named, the effective endpoint and region the SDK
// resolved, and the addressing mode. Profile is empty when the ambient chain
// applies; internal/app fills it from --profile or AWS_PROFILE. An empty
// Endpoint is the provider's default AWS endpoint; an empty Region is the SDK's
// answer when nothing named one, and the caller normalizes it for display and
// for a stored target (architecture/s3store.md).
type Resolved struct {
	// Profile is the shared-config selector the configuration named. It is
	// empty when the ambient chain applies; internal/app fills it from --profile
	// or AWS_PROFILE.
	Profile   string
	Endpoint  string
	Region    string
	PathStyle bool
}

// Resolve reports the effective values of cfg without a bucket and without
// retrieving a credential. It loads the AWS configuration exactly as New does
// and reads the client options the SDK resolved, which is the only way to
// follow the SDK's own order for AWS_ENDPOINT_URL_S3, AWS_ENDPOINT_URL, and a
// profile's services s3 endpoint_url. It sends no request.
func Resolve(ctx context.Context, cfg Config, opts ...Options) (Resolved, error) {
	awsCfg, err := loadAWSConfig(ctx, cfg)
	if err != nil {
		return Resolved{}, err
	}
	_, resolved := newClient(awsCfg, cfg, firstOptions(opts))
	return resolved, nil
}
