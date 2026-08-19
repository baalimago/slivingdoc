// Package tests3 hands out test S3 suites and per-test prefixes below a
// shared bucket. make test starts one broker-owned pinned SeaweedFS container
// and injects its loopback endpoint into every test binary; direct go test
// keeps the fallback that starts one pinned container per test process.
// Importers depend on the S3 contract, not on the vendor.
//
// The package and its lease executable are test-only infrastructure.
package tests3

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
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

	// EndpointEnv injects the loopback endpoint of a broker-owned test
	// backend. make test sets it after starting the test-only lease process;
	// direct go test deliberately leaves it unset and keeps the per-process
	// container startup behavior.
	EndpointEnv = "SLIVINGDOC_TESTS3_ENDPOINT"

	// EndpointFileEnv injects the path where the broker atomically publishes
	// EndpointEnv. It lets go test compile and run non-S3 packages while the
	// one shared S3 container starts.
	EndpointFileEnv = "SLIVINGDOC_TESTS3_ENDPOINT_FILE"
)

const endpointFileWait = 2 * time.Minute

var (
	startOnce sync.Once
	suite     *Suite
	startErr  error
)

// Suite is one shared S3-compatible backend: its HTTP endpoint, a raw S3
// client for direct assertions, and a fresh-prefix factory. ctr is non-nil
// only in the process that owns container lifecycle.
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
		if readyFile := os.Getenv(EndpointFileEnv); readyFile != "" {
			suite, startErr = attachFromFile(readyFile)
			return
		}
		if endpoint := os.Getenv(EndpointEnv); endpoint != "" {
			suite, startErr = attach(endpoint)
			return
		}
		if err := dockerAvailable(); err != nil {
			startErr = fmt.Errorf("docker unavailable: %w", err)
			return
		}
		suite, startErr = start()
	})
	return startErr
}

// Endpoint returns the endpoint of the process's shared suite. The lease
// executable uses it only after Start has made the backend ready.
func Endpoint() (string, error) {
	if err := Start(); err != nil {
		return "", err
	}
	if suite == nil {
		return "", fmt.Errorf("tests3: start returned no suite")
	}
	return suite.Endpoint, nil
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
	if suite == nil || suite.ctr == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = suite.ctr.Terminate(ctx)
	}()
}

// attach builds an attach-only suite for the endpoint published by the
// make-test lease process. It intentionally accepts only loopback HTTP URLs,
// so a stray environment variable cannot send a test to a developer's or
// cloud S3 endpoint. The broker owns termination; attached test binaries
// leave ctr nil and Terminate becomes a no-op.
func attach(endpoint string) (*Suite, error) {
	endpoint, err := loopbackEndpoint(endpoint)
	if err != nil {
		return nil, fmt.Errorf("tests3: injected endpoint: %w", err)
	}
	return &Suite{Endpoint: endpoint, Raw: newRawClient(endpoint)}, nil
}

func attachFromFile(path string) (*Suite, error) {
	deadline := time.Now().Add(endpointFileWait)
	for {
		endpoint, ready, err := endpointFromFile(path)
		if err != nil {
			return nil, fmt.Errorf("tests3: injected endpoint file: %w", err)
		}
		if ready {
			return attach(endpoint)
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("tests3: injected endpoint file %q was not ready within %s", path, endpointFileWait)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func endpointFromFile(path string) (endpoint string, ready bool, err error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	value := strings.TrimSpace(string(data))
	if value == "" {
		return "", false, nil
	}
	if text, ok := strings.CutPrefix(value, "error: "); ok {
		return "", false, errors.New(text)
	}
	return value, true, nil
}

func loopbackEndpoint(endpoint string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("parse URL: %w", err)
	}
	if u.Scheme != "http" || u.Host == "" || u.User != nil || u.Port() == "" {
		return "", fmt.Errorf("must be an http loopback URL with an explicit port")
	}
	if u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("must not contain a path, query, or fragment")
	}
	host := u.Hostname()
	if ip := net.ParseIP(host); ip != nil {
		if !ip.IsLoopback() {
			return "", fmt.Errorf("host %q is not loopback", host)
		}
	} else if !strings.EqualFold(host, "localhost") {
		return "", fmt.Errorf("host %q is not loopback", host)
	}
	return u.String(), nil
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
	raw := newRawClient(endpoint)
	if _, err := raw.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(Bucket)}); err != nil {
		_ = c.Terminate(ctx)
		return nil, fmt.Errorf("create bucket: %w", err)
	}
	return &Suite{Endpoint: endpoint, Raw: raw, ctr: c}, nil
}

func newRawClient(endpoint string) *s3.Client {
	cfg := aws.Config{
		Region:       Region,
		BaseEndpoint: aws.String(endpoint),
		Credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
			return aws.Credentials{AccessKeyID: User, SecretAccessKey: Pass}, nil
		}),
	}
	return s3.NewFromConfig(cfg, func(o *s3.Options) { o.UsePathStyle = true })
}
