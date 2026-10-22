package itest

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/baalimago/slivingdoc/internal/settings"
	"github.com/baalimago/slivingdoc/internal/tests3"
)

// s3DefaultEnv is the environment of a process that reaches the shared S3
// suite through a stored default: the container's static credentials only,
// never an endpoint, region, or profile variable, so the default supplies
// the identity the process lists.
func S3DefaultEnv(t *testing.T) []string {
	t.Helper()
	env, _ := CliRoots(t)
	return append(
		env,
		"SLIVINGDOC_CONFIG_DIR="+filepath.Join(t.TempDir(), "cfg"),
		"SLIVINGDOC_BUCKET=",
		"SLIVINGDOC_PREFIX=",
		"AWS_ACCESS_KEY_ID="+tests3.User,
		"AWS_SECRET_ACCESS_KEY="+tests3.Pass,
	)
}

// s3Credentials adds the suite's static credentials to a scenario that
// reaches S3 through a planted default.
func S3Credentials(env []string) []string {
	return append(env, "AWS_ACCESS_KEY_ID="+tests3.User, "AWS_SECRET_ACCESS_KEY="+tests3.Pass)
}

// s3DefaultTarget is a stored S3 default naming one bucket of the suite,
// with the path style a custom endpoint records.
func S3DefaultTarget(suite *tests3.Suite, name string) settings.Target {
	return settings.Target{
		Kind: settings.KindS3, Endpoint: suite.Endpoint, Region: tests3.Region,
		Name: name, PathStyle: true,
	}
}

// freshS3Bucket is a bucket name unique to one scenario, derived from the
// suite's prefix factory so parallel scenarios never collide.
func FreshS3Bucket(suite *tests3.Suite, namespace string) string {
	return strings.ReplaceAll(suite.FreshPrefix(namespace), "/", "-")
}

// createS3Bucket creates one bucket in the shared suite for a scenario whose
// process only lists and stores targets, and removes it afterwards.
func CreateS3Bucket(t *testing.T, suite *tests3.Suite, name string) {
	t.Helper()
	if _, err := suite.Raw.CreateBucket(context.Background(), &awss3.CreateBucketInput{Bucket: aws.String(name)}); err != nil {
		t.Fatalf("create bucket %q: %v", name, err)
	}
	t.Cleanup(func() {
		_, _ = suite.Raw.DeleteBucket(context.Background(), &awss3.DeleteBucketInput{Bucket: aws.String(name)})
	})
}

// newS3Bucket creates and returns one bucket of the shared suite, unique to
// the scenario.
func NewS3Bucket(t *testing.T, suite *tests3.Suite, namespace string) string {
	t.Helper()
	name := FreshS3Bucket(suite, namespace)
	CreateS3Bucket(t, suite, name)
	return name
}

// privateRootOf is the private root env gives its processes, empty when none
// is configured.
func PrivateRootOf(env []string) string {
	for _, kv := range env {
		if value, ok := strings.CutPrefix(kv, "SLIVINGDOC_PRIVATE_ROOT="); ok {
			return value
		}
	}
	return ""
}
