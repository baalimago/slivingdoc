package itest

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/baalimago/slivingdoc/internal/credentials"
	"github.com/baalimago/slivingdoc/internal/settings"
	"github.com/baalimago/slivingdoc/internal/tests3"
)

// bindingEnv is the environment of S3 scenarios that work with no S3
// identity variable at all: a configuration directory of their own, a
// private root, the suite's static credentials, and empty bucket and prefix
// variables so the record or the default supplies both. The helper base
// environment names a bucket and a prefix, and an empty value is unset.
func BindingEnv(t *testing.T, configDir, privateRoot, workspaceRoot string) []string {
	t.Helper()
	return []string{
		credentials.DirEnv + "=" + configDir,
		"SLIVINGDOC_WORKSPACE_ROOT=" + workspaceRoot,
		"SLIVINGDOC_PRIVATE_ROOT=" + privateRoot,
		"SLIVINGDOC_BUCKET=",
		"SLIVINGDOC_PREFIX=",
		"AWS_ACCESS_KEY_ID=" + tests3.User,
		"AWS_SECRET_ACCESS_KEY=" + tests3.Pass,
	}
}

// bindingDir creates the configuration directory a scenario's processes
// share.
func BindingDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "cfg")
	MustDo(t, os.MkdirAll(dir, 0o700))
	return dir
}

// plantTarget stores one record or replaces the one global default of a
// settings file, and answers the bytes written.
func PlantTarget(t *testing.T, env []string, entry *settings.Entry, def *settings.Target) string {
	t.Helper()
	file := SettingsFile(t, env)
	set, err := file.Load()
	if err != nil {
		t.Fatalf("load the settings file: %v", err)
	}
	switch {
	case entry != nil:
		set = set.Put(*entry)
	case def != nil:
		set = set.PutDefault(*def)
	}
	if err := file.Save(set); err != nil {
		t.Fatalf("save the settings file: %v", err)
	}
	return ReadSettings(t, env)
}

// s3Target is one complete S3 target of the suite.
func S3Target(suite *tests3.Suite, bucket, prefix string) settings.Target {
	return settings.Target{
		Kind: settings.KindS3, Endpoint: suite.Endpoint, Region: tests3.Region,
		Name: bucket, Prefix: prefix, PathStyle: true,
	}
}

// s3Keys lists the object keys below prefix in bucket, so a scenario proves
// which store and prefix the process addressed.
func S3Keys(t *testing.T, suite *tests3.Suite, bucket, prefix string) []string {
	t.Helper()
	out, err := suite.Raw.ListObjectsV2(context.Background(), &awss3.ListObjectsV2Input{
		Bucket: aws.String(bucket), Prefix: aws.String(prefix + "/"),
	})
	if err != nil {
		t.Fatalf("list %q below %q: %v", bucket, prefix, err)
	}
	keys := make([]string, 0, len(out.Contents))
	for _, object := range out.Contents {
		keys = append(keys, aws.ToString(object.Key))
	}
	return keys
}

// identityDirs lists the private state directories of one private root, one
// per notebook identity.
func IdentityDirs(t *testing.T, privateRoot string) []string {
	t.Helper()
	entries, err := os.ReadDir(privateRoot)
	if err != nil {
		t.Fatalf("read the private root: %v", err)
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			out = append(out, entry.Name())
		}
	}
	return out
}

// namesProfile reports whether any file under root names value, so a
// scenario proves which facts the private state keeps.
func NamesProfile(t *testing.T, root, value string) bool {
	t.Helper()
	found := false
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), value) || strings.Contains(path, value) {
			found = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk the private root: %v", err)
	}
	return found
}
