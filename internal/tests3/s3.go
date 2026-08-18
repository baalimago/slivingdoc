// Package tests3 starts one pinned S3-compatible container per test process
// and hands out per-test prefixes below a shared bucket. The concrete image
// is the current pinned implementation (SeaweedFS); importers depend on the
// S3 contract, not on the vendor.
//
// The package is test-only: only _test.go files import it.
package tests3

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/moby/moby/client"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/baalimago/slivingdoc/internal/storage"
)

// Image is the pinned S3-compatible backend container. SeaweedFS is the
// current implementation; the contract it must satisfy is the S3 protocol
// the probe and the suites exercise, not a vendor feature set.
const (
	Image  = "chrislusf/seaweedfs:4.42"
	User   = "slivingdoc"
	Pass   = "slivingdoc-secret"
	Bucket = "slivingdoc"
	Region = "us-east-1"
)

var (
	startOnce sync.Once
	suite     *Suite
	startErr  error
)

// Suite is one shared S3-compatible container: its HTTP endpoint, a raw S3
// client for direct assertions, and a fresh-prefix factory.
type Suite struct {
	Endpoint string
	Raw      *s3.Client
	ctr      testcontainers.Container
}

// StoreConfig is the plain connection description of the suite's S3
// endpoint. It exposes no AWS SDK type, so store adapters build their own
// configuration from these values.
type StoreConfig struct {
	Endpoint  string
	Region    string
	AccessKey string
	SecretKey string
}

// Start prepares the shared suite once per test process. A package TestMain
// can call it before m.Run so container startup is treated as the mandatory
// test environment prerequisite that it is, rather than consuming a
// scenario's strict per-package test budget.
func Start() error {
	startOnce.Do(func() {
		if err := dockerAvailable(); err != nil {
			startErr = fmt.Errorf("docker unavailable: %w", err)
			return
		}
		suite, startErr = start()
	})
	return startErr
}

// Ensure returns the shared suite, starting the pinned container once per
// test process when a package has not already prepared it in TestMain.
func Ensure(t *testing.T) *Suite {
	t.Helper()
	return require(t, suite, Start())
}

// fataler is the failure half of testing.TB. testing.TB cannot be
// implemented outside the testing package, so the availability policy takes
// this narrower interface and stays directly testable.
type fataler interface {
	Helper()
	Fatalf(format string, args ...any)
}

// require applies the availability policy: Docker is a hard prerequisite of
// the suite, so an unreachable daemon fails the test instead of skipping it.
// A skip here lets a run report success while it silently omits the
// entire storage protocol.
func require(t fataler, s *Suite, err error) *Suite {
	t.Helper()
	if err != nil {
		t.Fatalf("s3 integration unavailable: %v\n"+
			"Docker is required to run this suite; start the daemon and re-run.", err)
		return nil
	}
	return s
}

// Terminate requests that the shared container stop. Call it from TestMain
// after the suite ran; it is a no-op when the container never started.
//
// Termination runs detached so the test-binary critical path never blocks on
// the Docker HTTP client: go test budgets one timeout for the whole binary
// lifetime including TestMain, and the stop request can contend for a moby
// connection on a busy runner. The testcontainers reaper (Ryuk) guarantees
// eventual cleanup, so the binary may exit before the stop completes.
func Terminate() {
	if suite == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = suite.ctr.Terminate(ctx)
	}()
}

// StoreConfig returns the plain connection values of the local S3 endpoint:
// the container credentials ride as plain strings, so no AWS SDK type
// crosses this boundary.
func (s *Suite) StoreConfig() StoreConfig {
	return StoreConfig{Endpoint: s.Endpoint, Region: Region, AccessKey: User, SecretKey: Pass}
}

// FreshPrefix returns a new per-test protocol prefix below namespace. Each
// call yields a unique prefix, so parallel tests never share objects.
func (s *Suite) FreshPrefix(namespace string) string {
	id, err := storage.NewUUIDv7()
	if err != nil {
		panic("tests3: generate uuidv7: " + err.Error())
	}
	return namespace + "/" + id.String()
}

// dockerAvailable pings the Docker daemon; the S3 suite depends on it.
func dockerAvailable() error {
	cli, err := testcontainers.NewDockerClientWithOpts(context.Background())
	if err != nil {
		return fmt.Errorf("docker client: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := cli.Ping(ctx, client.PingOptions{}); err != nil {
		return fmt.Errorf("docker daemon: %w", err)
	}
	return nil
}

// start starts the pinned S3-compatible container with the static identity
// below a shared bucket, waits for the S3 gateway log line, and creates the
// test bucket.
func start() (*Suite, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	req := testcontainers.ContainerRequest{
		Image:        Image,
		ExposedPorts: []string{"8333/tcp"},
		Entrypoint:   []string{"/bin/sh"},
		Cmd: []string{"-c", `echo '{
      "identities": [
        {
          "name": "slivingdoc",
          "credentials": [
            { "accessKey": "slivingdoc", "secretKey": "slivingdoc-secret" }
          ],
          "actions": ["Admin", "Read", "Write", "List", "Tagging"]
        }
      ]
    }' > /etc/seaweedfs/s3.json && weed server -s3 -s3.config /etc/seaweedfs/s3.json -dir /data`},
		WaitingFor: wait.ForLog("Start Seaweed S3 API Server").
			WithStartupTimeout(30 * time.Second),
	}
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, fmt.Errorf("start s3 container: %w", err)
	}
	host, err := c.Host(ctx)
	if err != nil {
		_ = c.Terminate(ctx)
		return nil, fmt.Errorf("s3 host: %w", err)
	}
	port, err := c.MappedPort(ctx, "8333/tcp")
	if err != nil {
		_ = c.Terminate(ctx)
		return nil, fmt.Errorf("s3 port: %w", err)
	}
	endpoint := fmt.Sprintf("http://%s:%s", host, port.Port())
	cfg := aws.Config{
		Region:       Region,
		BaseEndpoint: aws.String(endpoint),
		Credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
			return aws.Credentials{AccessKeyID: User, SecretAccessKey: Pass}, nil
		}),
	}
	raw := s3.NewFromConfig(cfg, func(o *s3.Options) { o.UsePathStyle = true })
	if _, err := raw.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(Bucket)}); err != nil {
		_ = c.Terminate(ctx)
		return nil, fmt.Errorf("create bucket: %w", err)
	}
	return &Suite{Endpoint: endpoint, Raw: raw, ctr: c}, nil
}
