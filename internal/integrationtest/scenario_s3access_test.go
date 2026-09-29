package integrationtest

import (
	"context"
	"strings"
	"testing"

	"github.com/baalimago/slivingdoc/internal/s3store"
	"github.com/baalimago/slivingdoc/internal/tests3"
)

// s3AccessHarness wires a harness over the real S3 suite with the given
// credentials and bucket, so the refusal comes from a real S3 service.
func s3AccessHarness(t *testing.T, accessKey, secretKey, bucket string) *Harness {
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

// TestScenarioS3AccessRefusedIsNotRetryable proves that an S3 service
// refusing the credentials, or a bucket that does not exist, is reported as
// STORAGE_FAILURE/ACCESS_DENIED with the OPERATOR action and no retry, with
// the service's own words and the fix in the message, for a pull, and leaves the
// notebook directory as it was.
func TestScenarioS3AccessRefusedIsNotRetryable(t *testing.T) {
	t.Parallel()
	suite := tests3.Ensure(t)
	for _, row := range []struct {
		name                   string
		accessKey, secret, bkt string
		wantSays               string
	}{
		{"unknown access key", "slivingdoc-bad", "definitely-not-the-secret", tests3.Bucket, "InvalidAccessKeyId"},
		{"missing bucket", suite.StoreConfig().AccessKey, suite.StoreConfig().SecretKey, "no-such-bucket-here", "NoSuchBucket"},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			h := s3AccessHarness(t, row.accessKey, row.secret, row.bkt)
			path := h.Path("notes")
			h.WriteFile(path+"/draft.md", "unsent")

			pull := decodeEnvelope(t, ToolCall{Tool: toolPull, Path: path}, h.Pull("", path))
			assertS3Refusal(t, "pull", pull)
			if got := h.ReadFile(path + "/draft.md"); got != "unsent" {
				t.Fatalf("draft = %q, want it untouched", got)
			}
			if !strings.Contains(pull.Message, "AWS credentials") {
				t.Fatalf("message %q names no S3 fix", pull.Message)
			}
		})
	}
}

func assertS3Refusal(t *testing.T, op string, env envelope) {
	t.Helper()
	if env.Code != "STORAGE_FAILURE" || env.Reason != "ACCESS_DENIED" || env.Action != "OPERATOR" || env.Retryable {
		t.Fatalf("%s = %s/%s/%s retryable %v, want STORAGE_FAILURE/ACCESS_DENIED/OPERATOR not retryable; message: %s",
			op, env.Code, env.Reason, env.Action, env.Retryable, env.Message)
	}
	if !strings.Contains(env.Message, "The storage says:") {
		t.Fatalf("%s message %q carries no word from the storage", op, env.Message)
	}
}
