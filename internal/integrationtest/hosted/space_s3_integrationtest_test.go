package hosted

// The S3 space admin surface seen by a process: create a bucket through
// the S3 API, list the identity's buckets, and store one complete target as
// the global S3 default (architecture/login.md).

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/baalimago/slivingdoc/internal/integrationtest/itest"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/baalimago/slivingdoc/internal/settings"
	"github.com/baalimago/slivingdoc/internal/tests3"
)

func TestScenarioSpaceS3(t *testing.T) {
	t.Parallel()
	suite := tests3.Ensure(t)
	env, _ := CliRoots(t)
	env = append(
		env,
		"SLIVINGDOC_CONFIG_DIR="+filepath.Join(t.TempDir(), "cfg"),
		// The helper environment names a protocol prefix by default; a
		// create makes a new store and refuses one.
		"SLIVINGDOC_PREFIX=",
		"AWS_ACCESS_KEY_ID="+tests3.User,
		"AWS_SECRET_ACCESS_KEY="+tests3.Pass,
		"AWS_ENDPOINT_URL_S3="+suite.Endpoint,
		"AWS_REGION="+tests3.Region,
	)
	name := FreshS3Bucket(suite, "space")
	t.Cleanup(func() {
		_, _ = suite.Raw.DeleteBucket(context.Background(), &awss3.DeleteBucketInput{Bucket: aws.String(name)})
	})

	flags := []string{"space", "--storage", "s3", "--endpoint", suite.Endpoint, "--region", tests3.Region}
	create := append(append([]string(nil), flags...), "new", name)
	RunCLIExact(t, "real", env,
		"Created bucket "+name+" (endpoint="+suite.Endpoint+", region="+tests3.Region+", profile=(ambient)).\n"+
			"It is not the default; run 'slivingdoc space --storage s3 --endpoint "+suite.Endpoint+
			" --region "+tests3.Region+" "+name+"' to make it the default.\n",
		create...)

	code, stdout, stderr := RunCLI(t, "real", env, flags...)
	if code != 0 {
		t.Fatalf("space = exit %d; stderr: %s", code, stderr)
	}
	header := "S3  endpoint=" + suite.Endpoint + "  region=" + tests3.Region + "  profile=(ambient)  key=" + tests3.User + "\n"
	if !strings.Contains(stdout, header) {
		t.Fatalf("stdout = %q, want the header %q", stdout, header)
	}
	if !strings.Contains(stdout, "  "+name+"\n") {
		t.Fatalf("stdout = %q, want the created bucket %q", stdout, name)
	}

	// An earlier build stored one default per store key; a new selection
	// replaces every one of them.
	MustDo(t, os.MkdirAll(CredentialsDir(env), 0o700))
	PlantSettings(t, env, S3SettingsFile(settings.Target{
		Kind: settings.KindS3, Endpoint: suite.Endpoint, Region: tests3.Region,
		Name: "old-default", PathStyle: true,
	}))
	RunCLIExact(t, "real", env,
		"The default bucket for s3 endpoint="+suite.Endpoint+", region="+tests3.Region+", profile=(ambient) is now "+name+".\n",
		append(append([]string(nil), flags...), name)...)

	set := LoadSettings(t, env)
	if got := set.Defaults(); len(got) != 1 {
		t.Fatalf("defaults = %+v, want the one selection", got)
	}
	target, ok := set.S3Default()
	if !ok {
		t.Fatalf("no default stored; defaults: %+v", set.Defaults())
	}
	if target.Kind != settings.KindS3 || target.Name != name || target.Endpoint != suite.Endpoint ||
		target.Region != tests3.Region || target.Profile != "" || target.Prefix != "" || !target.PathStyle {
		t.Fatalf("stored default = %+v, want the resolved bucket with endpoint, region, path style and no prefix", target)
	}
	code, stdout, stderr = RunCLI(t, "real", env, flags...)
	if code != 0 || !strings.Contains(stdout, "* "+name+"\n") {
		t.Fatalf("space = exit %d, stdout %q, stderr %q; want the active default marked", code, stdout, stderr)
	}
}
