// Package testminio starts one pinned MinIO container per test process and
// hands out per-test prefixes below a shared bucket. Both the s3store
// contract suite and the notebook integration suite run against real HTTP
// conditional writes; sharing the bootstrap keeps the container lifecycle
// in one place.
//
// The package is test-only: only _test.go files import it.
package testminio

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

// The pinned MinIO image and the shared test bucket. Credentials are
// static and local; no test touches a live AWS account.
const (
	Image  = "minio/minio:RELEASE.2025-09-07T16-13-09Z"
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

// Suite is one shared MinIO container: its HTTP endpoint, a raw S3 client
// for direct assertions, and a fresh-prefix factory.
type Suite struct {
	Endpoint string
	Raw      *s3.Client
	ctr      testcontainers.Container
}

// StoreConfig is the plain connection description of the suite's MinIO
// endpoint. It exposes no AWS SDK type, so store adapters build their own
// configuration from these values.
type StoreConfig struct {
	Endpoint  string
	Region    string
	AccessKey string
	SecretKey string
}

// Ensure returns the shared suite, starting the pinned container once per
// test process.
func Ensure(t *testing.T) *Suite {
	t.Helper()
	startOnce.Do(func() {
		if err := dockerAvailable(); err != nil {
			startErr = fmt.Errorf("docker unavailable: %w", err)
			return
		}
		suite, startErr = start()
	})
	return require(t, suite, startErr)
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
		t.Fatalf("minio integration unavailable: %v\n"+
			"Docker is required to run this suite; start the daemon and re-run.", err)
		return nil
	}
	return s
}

// Terminate stops the shared container. Call it from TestMain after the
// suite ran; it is a no-op when the container never started.
func Terminate() {
	if suite != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		_ = suite.ctr.Terminate(ctx)
		cancel()
	}
}

// StoreConfig returns the plain connection values of the local MinIO
// endpoint: the container credentials ride as plain strings, so no AWS SDK
// type crosses this boundary.
func (s *Suite) StoreConfig() StoreConfig {
	return StoreConfig{Endpoint: s.Endpoint, Region: Region, AccessKey: User, SecretKey: Pass}
}

// FreshPrefix returns a new per-test protocol prefix below namespace. Each
// call yields a unique prefix, so parallel tests never share objects.
func (s *Suite) FreshPrefix(namespace string) string {
	id, err := storage.NewUUIDv7()
	if err != nil {
		panic("testminio: generate uuidv7: " + err.Error())
	}
	return namespace + "/" + id.String()
}

// dockerAvailable pings the Docker daemon; the MinIO suite depends on it.
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

// start starts the pinned MinIO container, waits for its health endpoint,
// and creates the test bucket.
func start() (*Suite, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	req := testcontainers.ContainerRequest{
		Image:        Image,
		ExposedPorts: []string{"9000/tcp"},
		Cmd:          []string{"server", "/data"},
		Env: map[string]string{
			"MINIO_ROOT_USER":     User,
			"MINIO_ROOT_PASSWORD": Pass,
		},
		WaitingFor: wait.ForHTTP("/minio/health/live").
			WithPort("9000/tcp").
			WithStartupTimeout(30 * time.Second),
	}
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, fmt.Errorf("start minio container: %w", err)
	}
	host, err := c.Host(ctx)
	if err != nil {
		_ = c.Terminate(ctx)
		return nil, fmt.Errorf("minio host: %w", err)
	}
	port, err := c.MappedPort(ctx, "9000/tcp")
	if err != nil {
		_ = c.Terminate(ctx)
		return nil, fmt.Errorf("minio port: %w", err)
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
