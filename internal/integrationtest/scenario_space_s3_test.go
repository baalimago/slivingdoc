package integrationtest

// The S3 space admin surface seen by a process: create a bucket through
// the S3 API, list the identity's buckets, and store the resolved target as
// the default keyed by its store identity (architecture/login.md).

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/baalimago/slivingdoc/internal/credentials"
	"github.com/baalimago/slivingdoc/internal/settings"
	"github.com/baalimago/slivingdoc/internal/tests3"
)

func TestScenarioSpaceS3(t *testing.T) {
	t.Parallel()
	suite := tests3.Ensure(t)
	env, _ := cliRoots(t)
	env = append(env,
		"SLIVINGDOC_CONFIG_DIR="+filepath.Join(t.TempDir(), "cfg"),
		// The helper environment names a protocol prefix by default; a
		// create makes a new store and refuses one.
		"SLIVINGDOC_PREFIX=",
		"AWS_ACCESS_KEY_ID="+tests3.User,
		"AWS_SECRET_ACCESS_KEY="+tests3.Pass,
		"AWS_ENDPOINT_URL_S3="+suite.Endpoint,
		"AWS_REGION="+tests3.Region,
	)
	name := strings.ReplaceAll(suite.FreshPrefix("space"), "/", "-")
	t.Cleanup(func() {
		_, _ = suite.Raw.DeleteBucket(context.Background(), &awss3.DeleteBucketInput{Bucket: aws.String(name)})
	})

	flags := []string{"space", "--storage", "s3", "--endpoint", suite.Endpoint, "--region", tests3.Region}
	create := append(append([]string(nil), flags...), "new", name)
	runCLIExact(t, "real", env,
		"Created bucket "+name+" (endpoint="+suite.Endpoint+", region="+tests3.Region+", profile=(ambient)).\n"+
			"It is not the default; run 'slivingdoc space --storage s3 --endpoint "+suite.Endpoint+
			" --region "+tests3.Region+" "+name+"' to make it the default.\n",
		create...)

	code, stdout, stderr := runCLI(t, "real", env, flags...)
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

	runCLIExact(t, "real", env,
		"The default bucket for s3 endpoint="+suite.Endpoint+", region="+tests3.Region+", profile=(ambient) is now "+name+".\n",
		append(append([]string(nil), flags...), name)...)

	file, err := settings.Locate(func(key string) string {
		if key == credentials.DirEnv {
			return credentialsDir(env)
		}
		return ""
	}, runtime.GOOS)
	if err != nil {
		t.Fatal(err)
	}
	set, err := file.Load()
	if err != nil {
		t.Fatalf("load settings = %v", err)
	}
	target, ok := set.Default(settings.StoreKey{Kind: settings.KindS3, Endpoint: suite.Endpoint, Region: tests3.Region})
	if !ok {
		t.Fatalf("no default stored for the resolved S3 store key; defaults: %+v", set.Defaults())
	}
	if target.Name != name || target.Profile != "" || target.Prefix != "" || !target.PathStyle {
		t.Fatalf("stored default = %+v, want the resolved bucket with path style and no prefix", target)
	}
}
