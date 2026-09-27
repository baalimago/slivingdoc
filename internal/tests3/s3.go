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
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

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
	ctr      *container
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
		d, err := dockerAvailable()
		if err != nil {
			startErr = fmt.Errorf("docker unavailable: %w", err)
			return
		}
		suite, startErr = start(d)
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

// Terminate stops the shared container. Call it from TestMain after the
// suite ran; it is a no-op when this process does not own a container.
//
// Stopping only closes the container's stdin, a local socket close that
// never blocks the test binary's critical path; the container's command
// ends at end of input and the daemon removes it. A process that dies
// without calling Terminate closes that socket too, so no container
// outlives its owner.
func Terminate() {
	if suite == nil || suite.ctr == nil {
		return
	}
	suite.ctr.stop()
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

// dockerAvailable locates and pings the Docker daemon; the S3 suite
// depends on it.
func dockerAvailable() (*dockerClient, error) {
	d, err := dockerDaemon(os.Getenv)
	if err != nil {
		return nil, fmt.Errorf("docker client: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := d.ping(ctx); err != nil {
		return nil, fmt.Errorf("docker daemon: %w", err)
	}
	return d, nil
}

// seaweedScript writes the static identity, serves S3 in the background,
// and runs until its stdin ends: the stdin attachment is the container's
// lease (see container).
const seaweedScript = `echo '{
      "identities": [
        {
          "name": "slivingdoc",
          "credentials": [
            { "accessKey": "slivingdoc", "secretKey": "slivingdoc-secret" }
          ],
          "actions": ["Admin", "Read", "Write", "List", "Tagging"]
        }
      ]
    }' > /etc/seaweedfs/s3.json && { weed server -s3 -s3.config /etc/seaweedfs/s3.json -dir /data & } && cat > /dev/null`

// readyTimeout bounds how long the S3 gateway may take to accept the
// bucket after the container started.
const readyTimeout = 30 * time.Second

// start starts the pinned S3-compatible container with the static identity
// below a shared bucket and creates the test bucket, retrying until the S3
// gateway accepts it.
func start(d *dockerClient) (*Suite, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := d.ensureImage(ctx, Image); err != nil {
		return nil, fmt.Errorf("s3 image: %w", err)
	}
	c, port, err := d.run(ctx, containerSpec{
		Image:      Image,
		Entrypoint: []string{"/bin/sh"},
		Cmd:        []string{"-c", seaweedScript},
		Port:       "8333/tcp",
		Labels:     map[string]string{"org.slivingdoc.tests3": "seaweedfs"},
	})
	if err != nil {
		return nil, fmt.Errorf("start s3 container: %w", err)
	}
	endpoint := "http://" + net.JoinHostPort(d.host, port)
	raw := newRawClient(endpoint)
	if err := createBucket(ctx, raw, readyTimeout); err != nil {
		c.stop()
		return nil, err
	}
	return &Suite{Endpoint: endpoint, Raw: raw, ctr: c}, nil
}

// createBucket creates the suite bucket once the gateway answers. A bucket
// that already exists is ours: an earlier attempt landed but its answer
// was lost.
func createBucket(ctx context.Context, raw *s3.Client, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		_, err := raw.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(Bucket)})
		var owned *types.BucketAlreadyOwnedByYou
		if err == nil || errors.As(err, &owned) {
			return nil
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return fmt.Errorf("create bucket: the S3 gateway did not accept it within %s: %w", timeout, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
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
