package s3store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

// ErrBucketExists reports a bucket that already exists under another account
// or principal. ErrBucketOwned reports one this principal already owns, which
// the caller can offer to use instead. The space command owns the refusal text
// (architecture/s3store.md).
var (
	ErrBucketExists = errors.New("s3store: bucket already exists")
	ErrBucketOwned  = errors.New("s3store: bucket already owned by this principal")
	// ErrProfileNotFound reports a profile the shared AWS configuration does
	// not define. The space command owns the refusal text
	// (architecture/login.md).
	ErrProfileNotFound = errors.New("s3store: AWS profile not found")
)

// Bucket is one bucket a listing returned.
type Bucket struct {
	Name    string
	Created time.Time
}

// Identity is who an S3 configuration acts as. It carries the access key id,
// which is safe to display, and never the secret.
type Identity struct {
	// Profile is the shared-config selector the configuration named. It is
	// empty when the ambient chain applies; internal/app fills it from --profile
	// or AWS_PROFILE.
	Profile     string
	AccessKeyID string
	Endpoint    string
	Region      string
}

// Admin performs bucket administration over one resolved S3 identity. It owns
// no notebook: NewAdmin ignores Config.Bucket and Config.Prefix, and no call
// here creates a bucket except CreateBucket itself.
type Admin struct {
	client      *s3.Client
	credentials aws.CredentialsProvider
	resolved    Resolved
}

// NewAdmin builds the administration client of cfg. It ignores Config.Bucket
// and Config.Prefix, so a caller that lists or creates buckets needs no
// notebook configuration, and it sends no request.
func NewAdmin(ctx context.Context, cfg Config, opts ...Options) (*Admin, error) {
	awsCfg, err := loadAWSConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	awsCfg = withDefaultRegion(awsCfg)
	client, resolved := newClient(awsCfg, cfg, firstOptions(opts))
	return &Admin{client: client, credentials: awsCfg.Credentials, resolved: resolved}, nil
}

// ListBuckets returns the buckets this identity reaches, in one request.
func (a *Admin) ListBuckets(ctx context.Context) ([]Bucket, error) {
	out, err := a.client.ListBuckets(ctx, &s3.ListBucketsInput{})
	if err != nil {
		return nil, mapError("list buckets", err)
	}
	buckets := make([]Bucket, 0, len(out.Buckets))
	for _, b := range out.Buckets {
		buckets = append(buckets, Bucket{Name: aws.ToString(b.Name), Created: aws.ToTime(b.CreationDate)})
	}
	return buckets, nil
}

// CreateBucket creates one bucket. It sends CreateBucketConfiguration only on
// the default AWS endpoint outside us-east-1: the default region rejects that
// configuration, and a custom endpoint names its own location.
func (a *Admin) CreateBucket(ctx context.Context, name string) error {
	if name == "" {
		return errors.New("s3store: create bucket: bucket name is required")
	}
	in := &s3.CreateBucketInput{Bucket: aws.String(name)}
	if a.resolved.Endpoint == "" && a.resolved.Region != DefaultRegion {
		in.CreateBucketConfiguration = &types.CreateBucketConfiguration{
			LocationConstraint: types.BucketLocationConstraint(a.resolved.Region),
		}
	}
	if _, err := a.client.CreateBucket(ctx, in); err != nil {
		return mapCreateError(err)
	}
	return nil
}

// Identity retrieves the credential once and reports the access key id with the
// selected profile, endpoint, and region. It never returns the secret.
func (a *Admin) Identity(ctx context.Context) (Identity, error) {
	creds, err := a.credentials.Retrieve(ctx)
	if err != nil {
		return Identity{}, accessDenied("identity", "credentials", errDetail(err))
	}
	return Identity{
		Profile:     a.resolved.Profile,
		AccessKeyID: creds.AccessKeyID,
		Endpoint:    a.resolved.Endpoint,
		Region:      a.resolved.Region,
	}, nil
}

// mapCreateError classifies a bucket create refusal. A bucket that this
// principal already owns is reported as owned even when the service names the
// generic exists code too, because the fix is to use it.
func mapCreateError(err error) error {
	var api smithy.APIError
	if errors.As(err, &api) {
		switch api.ErrorCode() {
		case "BucketAlreadyOwnedByYou":
			return fmt.Errorf("s3store: create bucket: %w", ErrBucketOwned)
		case "BucketAlreadyExists":
			return fmt.Errorf("s3store: create bucket: %w", ErrBucketExists)
		}
	}
	return mapError("create bucket", err)
}
