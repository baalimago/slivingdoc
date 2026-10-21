package itest

import (
	"context"
	"strings"
	"testing"

	"github.com/baalimago/slivingdoc/internal/s3store"
	"github.com/baalimago/slivingdoc/internal/tests3"
)

// s3AccessHarness wires a harness over the real S3 suite with the given
// credentials and bucket, so the refusal comes from a real S3 service.
func S3AccessHarness(t *testing.T, accessKey, secretKey, bucket string) *Harness {
	t.Helper()
	suite := tests3.Ensure(t)
	prefix := suite.FreshPrefix("integrationtest-access")
	mc := suite.StoreConfig()
	st, err := s3store.New(context.Background(), s3store.Config{
		Bucket: bucket, Prefix: prefix, Region: mc.Region,
		Endpoint: mc.Endpoint, AccessKey: accessKey, SecretKey: secretKey,
	})
	if err != nil {
		t.Fatalf("s3store.New() = %v", err)
	}
	return NewHarness(t, HarnessConfig{Store: st, Prefix: prefix, Bucket: bucket, Endpoint: mc.Endpoint})
}

func AssertS3Refusal(t *testing.T, op string, env Envelope) {
	t.Helper()
	if env.Code != "STORAGE_FAILURE" || env.Reason != "ACCESS_DENIED" || env.Action != "OPERATOR" || env.Retryable {
		t.Fatalf("%s = %s/%s/%s retryable %v, want STORAGE_FAILURE/ACCESS_DENIED/OPERATOR not retryable; message: %s",
			op, env.Code, env.Reason, env.Action, env.Retryable, env.Message)
	}
	if !strings.Contains(env.Message, "The storage says:") {
		t.Fatalf("%s message %q carries no word from the storage", op, env.Message)
	}
}
