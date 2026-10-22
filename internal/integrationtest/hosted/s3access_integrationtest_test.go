package hosted

import (
	"strings"
	"testing"

	. "github.com/baalimago/slivingdoc/internal/integrationtest/itest"

	"github.com/baalimago/slivingdoc/internal/tests3"
)

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
			h := S3AccessHarness(t, row.accessKey, row.secret, row.bkt)
			path := h.WorkspaceRoot()
			h.WriteFile(path+"/draft.md", "unsent")

			pull := DecodeEnvelope(t, ToolCall{Tool: ToolPull, Path: path}, h.Pull(""))
			AssertS3Refusal(t, "pull", pull)
			if got := h.ReadFile(path + "/draft.md"); got != "unsent" {
				t.Fatalf("draft = %q, want it untouched", got)
			}
			if !strings.Contains(pull.Message, "AWS credentials") {
				t.Fatalf("message %q names no S3 fix", pull.Message)
			}
		})
	}
}
