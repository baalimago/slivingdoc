// Package s3store implements the semantic object-store boundary over the
// AWS SDK for Go v2. AWS SDK types never cross the storage boundary: this
// package maps S3 results and errors to the storage semantic errors, owns
// the configured prefix join, and streams every object upload and download.
package s3store

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"

	"github.com/baalimago/slivingdoc/internal/storage"
)

const (
	defaultMultipartThreshold = 64 << 20 // single PUT below 64 MiB
	defaultMultipartPartSize  = 16 << 20 // 16 MiB parts
	minMultipartPartSize      = 5 << 20  // S3 minimum part size except the last
)

// Options tunes the upload strategy and addressing of a Store. The zero
// value uses the defaults: single PUT below 64 MiB, multipart above it,
// and virtual-host addressing (path style only with a custom endpoint).
type Options struct {
	// MultipartThreshold switches to multipart upload at or above this
	// byte size. Zero means the default.
	MultipartThreshold int64
	// MultipartPartSize is the part size for multipart upload, at least
	// the S3 minimum. Zero means the default.
	MultipartPartSize int64
	// ForcePathStyle forces S3 path-style addressing even without a
	// custom base endpoint (the --path-style configuration flag). Custom
	// endpoints always use path style.
	ForcePathStyle bool
}

func (o Options) withDefaults() (threshold, partSize int64, err error) {
	threshold = defaultMultipartThreshold
	if o.MultipartThreshold > 0 {
		threshold = o.MultipartThreshold
	}
	partSize = defaultMultipartPartSize
	if o.MultipartPartSize > 0 {
		partSize = o.MultipartPartSize
	}
	if partSize < minMultipartPartSize {
		return 0, 0, fmt.Errorf("s3store: multipart part size %d is below the S3 minimum %d", partSize, minMultipartPartSize)
	}
	return threshold, partSize, nil
}

// Store is an ObjectStore bound to one bucket and one configured prefix.
// All methods are safe for concurrent use; the store keeps no mutable
// request state.
type Store struct {
	client  *s3.Client
	bucket  string
	prefix  string
	options Options
}

// Config binds a store to one bucket and prefix with plain values only.
// The adapter owns AWS configuration loading, so no SDK type crosses this
// boundary (the AGENTS invariant).
type Config struct {
	// Bucket is the S3 bucket; required.
	Bucket string
	// Prefix is the object prefix below the bucket; validated.
	Prefix string
	// Region is the S3 region; empty keeps the SDK resolution.
	Region string
	// Endpoint is a custom S3-compatible base endpoint URL. Empty means
	// normal AWS resolution; a custom endpoint always uses path style.
	Endpoint string
	// AccessKey and SecretKey bypass the default credential chain when
	// both are set. The MinIO suites inject the container credentials;
	// production leaves them empty and uses the chain.
	AccessKey string
	SecretKey string

	// Test seams, settable only by same-package tests: a recording HTTP
	// client and a retry bound prove the addressing mode without any
	// network.
	httpClient interface {
		Do(*http.Request) (*http.Response, error)
	}
	retryMaxAttempts int
}

// New validates the bucket and prefix and returns a store for them. The
// adapter never creates or configures the bucket; deployment owns it.
func New(ctx context.Context, cfg Config, opts ...Options) (*Store, error) {
	if cfg.Bucket == "" {
		return nil, errors.New("s3store: bucket is required")
	}
	if err := storage.ValidatePrefix(cfg.Prefix); err != nil {
		return nil, err
	}
	var options Options
	if len(opts) > 0 {
		options = opts[0]
	}
	if _, _, err := options.withDefaults(); err != nil {
		return nil, err
	}
	var loadOpts []func(*awsconfig.LoadOptions) error
	if cfg.Region != "" {
		loadOpts = append(loadOpts, awsconfig.WithRegion(cfg.Region))
	}
	if cfg.Endpoint != "" {
		loadOpts = append(loadOpts, awsconfig.WithBaseEndpoint(cfg.Endpoint))
	}
	if cfg.AccessKey != "" && cfg.SecretKey != "" {
		access, secret := cfg.AccessKey, cfg.SecretKey
		loadOpts = append(loadOpts, awsconfig.WithCredentialsProvider(
			aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
				return aws.Credentials{AccessKeyID: access, SecretAccessKey: secret}, nil
			})))
	}
	if cfg.httpClient != nil {
		loadOpts = append(loadOpts, awsconfig.WithHTTPClient(cfg.httpClient))
	}
	if cfg.retryMaxAttempts > 0 {
		loadOpts = append(loadOpts, awsconfig.WithRetryMaxAttempts(cfg.retryMaxAttempts))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return nil, fmt.Errorf("s3store: load AWS configuration: %w", err)
	}
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.Endpoint != "" || options.ForcePathStyle {
			// S3-compatible endpoints (MinIO and similar) resolve
			// bucket names only in path style; --path-style requests the
			// same addressing for the default AWS endpoint.
			o.UsePathStyle = true
		}
	})
	return &Store{client: client, bucket: cfg.Bucket, prefix: cfg.Prefix, options: options}, nil
}

var _ storage.ObjectStore = (*Store)(nil)

func (s *Store) fullKey(key string) string { return storage.JoinKey(s.prefix, key) }

func (s *Store) ReadObject(ctx context.Context, key string) (io.ReadCloser, storage.ObjectInfo, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.fullKey(key)),
	})
	if err != nil {
		return nil, storage.ObjectInfo{}, mapError("get "+key, err)
	}
	meta, err := decodeMeta(out.Metadata)
	if err != nil {
		out.Body.Close()
		return nil, storage.ObjectInfo{}, fmt.Errorf("s3store: get %s: %w: %w", key, storage.ErrIntegrity, err)
	}
	return out.Body, storage.ObjectInfo{
		Size: aws.ToInt64(out.ContentLength),
		ETag: storage.ETag(aws.ToString(out.ETag)),
		Meta: meta,
	}, nil
}

func (s *Store) PutObject(ctx context.Context, key string, r io.Reader, meta storage.Metadata) error {
	threshold, partSize, err := s.options.withDefaults()
	if err != nil {
		return err
	}
	if int64(meta.Size) < threshold {
		return s.putSingle(ctx, key, r, meta)
	}
	return s.putMultipart(ctx, key, r, meta, partSize)
}

func (s *Store) putSingle(ctx context.Context, key string, r io.Reader, meta storage.Metadata) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(s.fullKey(key)),
		Body:          r,
		ContentLength: aws.Int64(int64(meta.Size)),
		ContentType:   aws.String("application/octet-stream"),
		Metadata:      encodeMeta(meta),
	})
	if err != nil {
		return mapError("put "+key, err)
	}
	return nil
}

// putMultipart uploads a large pack in parts and aborts the multipart
// upload on any failure, so a failed upload leaves no incomplete state.
func (s *Store) putMultipart(ctx context.Context, key string, r io.Reader, meta storage.Metadata, partSize int64) error {
	full := s.fullKey(key)
	up, err := s.client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(full),
		ContentType: aws.String("application/octet-stream"),
		Metadata:    encodeMeta(meta),
	})
	if err != nil {
		return mapError("multipart create "+key, err)
	}
	abort := func() {
		_, _ = s.client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
			Bucket:   aws.String(s.bucket),
			Key:      aws.String(full),
			UploadId: up.UploadId,
		})
	}

	var parts []types.CompletedPart
	var total int64
	buf := make([]byte, partSize)
	for partNumber := 1; ; partNumber++ {
		n, err := io.ReadFull(r, buf)
		total += int64(n)
		if n > 0 {
			part, err := s.client.UploadPart(ctx, &s3.UploadPartInput{
				Bucket:     aws.String(s.bucket),
				Key:        aws.String(full),
				UploadId:   up.UploadId,
				PartNumber: aws.Int32(int32(partNumber)),
				Body:       bytes.NewReader(buf[:n]),
			})
			if err != nil {
				abort()
				return mapError("multipart part "+key, err)
			}
			parts = append(parts, types.CompletedPart{PartNumber: aws.Int32(int32(partNumber)), ETag: part.ETag})
		}
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		}
		if err != nil {
			abort()
			return fmt.Errorf("s3store: multipart %s: source read failed: %w", key, storage.ErrTransport)
		}
	}
	if total != int64(meta.Size) {
		abort()
		return fmt.Errorf("s3store: multipart %s: body length %d does not match declared size %d: %w", key, total, meta.Size, storage.ErrTransport)
	}
	if _, err := s.client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket:          aws.String(s.bucket),
		Key:             aws.String(full),
		UploadId:        up.UploadId,
		MultipartUpload: &types.CompletedMultipartUpload{Parts: parts},
	}); err != nil {
		return mapError("multipart complete "+key, err)
	}
	return nil
}

func (s *Store) CreateObject(ctx context.Context, key string, data []byte) (storage.ETag, error) {
	out, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(s.fullKey(key)),
		Body:          bytes.NewReader(data),
		ContentLength: aws.Int64(int64(len(data))),
		ContentType:   aws.String("application/json"),
		IfNoneMatch:   aws.String("*"),
	})
	if err != nil {
		return "", mapError("create "+key, err)
	}
	if out.ETag == nil || *out.ETag == "" {
		return "", fmt.Errorf("s3store: create %s: %w", key, storage.ErrTransport)
	}
	return storage.ETag(*out.ETag), nil
}

func (s *Store) ReplaceObject(ctx context.Context, key string, etag storage.ETag, data []byte) (storage.ETag, error) {
	out, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(s.fullKey(key)),
		Body:          bytes.NewReader(data),
		ContentLength: aws.Int64(int64(len(data))),
		ContentType:   aws.String("application/json"),
		IfMatch:       aws.String(string(etag)),
	})
	if err != nil {
		// MinIO answers a conditional PUT against a missing key with
		// NoSuchKey instead of 412. The protocol sees the same state: the
		// observed precondition does not hold, the object was not mutated.
		// Normalize so every compliant store reports the semantic
		// precondition failure for a lost CAS.
		semantic := mapError("replace "+key, err)
		if errors.Is(semantic, storage.ErrNotFound) {
			semantic = fmt.Errorf("s3store: replace %s: object absent: %w", key, storage.ErrPreconditionFailed)
		}
		return "", semantic
	}
	if out.ETag == nil || *out.ETag == "" {
		return "", fmt.Errorf("s3store: replace %s: %w", key, storage.ErrTransport)
	}
	return storage.ETag(*out.ETag), nil
}

func (s *Store) ListObjects(ctx context.Context, prefix string, fn func(key string) error) error {
	full := s.fullKey(prefix)
	var token *string
	for {
		out, err := s.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(s.bucket),
			Prefix:            aws.String(full),
			ContinuationToken: token,
		})
		if err != nil {
			return mapError("list "+prefix, err)
		}
		for _, obj := range out.Contents {
			proto := strings.TrimPrefix(aws.ToString(obj.Key), s.fullKey(""))
			if err := fn(proto); err != nil {
				return err
			}
		}
		if !aws.ToBool(out.IsTruncated) {
			return nil
		}
		token = out.NextContinuationToken
	}
}

func (s *Store) DeleteObjects(ctx context.Context, keys []string) error {
	const batch = 1000
	for start := 0; start < len(keys); start += batch {
		end := min(start+batch, len(keys))
		ids := make([]types.ObjectIdentifier, 0, end-start)
		for _, key := range keys[start:end] {
			ids = append(ids, types.ObjectIdentifier{Key: aws.String(s.fullKey(key))})
		}
		out, err := s.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: aws.String(s.bucket),
			Delete: &types.Delete{Objects: ids, Quiet: aws.Bool(true)},
		})
		if err != nil {
			return mapError("delete", err)
		}
		if len(out.Errors) > 0 {
			return fmt.Errorf("s3store: delete: %d of %d keys failed: %w", len(out.Errors), len(ids), storage.ErrTransport)
		}
	}
	return nil
}

// mapError converts an AWS SDK error into a semantic storage error:
// NoSuchKey maps to ErrNotFound, PreconditionFailed maps to
// ErrPreconditionFailed, and every other failure — including connection
// errors, timeouts after request bytes were sent, access denials, and
// server errors — maps to ErrTransport. A non-semantic failure keeps its
// real reason in the text so the startup probe can surface it (for example
// an S3 error code and message, or a credential-resolution refusal that
// never reached the server) while the sentinel still classifies it.
func mapError(op string, err error) error {
	if err == nil {
		return nil
	}
	var api smithy.APIError
	if errors.As(err, &api) {
		switch api.ErrorCode() {
		case "NoSuchKey", "NotFound":
			return fmt.Errorf("s3store: %s: %w", op, storage.ErrNotFound)
		case "PreconditionFailed":
			return fmt.Errorf("s3store: %s: %w", op, storage.ErrPreconditionFailed)
		}
		return fmt.Errorf("s3store: %s: %s: %w", op, apiDetail(api), storage.ErrTransport)
	}
	if detail, ok := httpErrorDetail(err); ok {
		return fmt.Errorf("s3store: %s: %s: %w", op, detail, storage.ErrTransport)
	}
	return fmt.Errorf("s3store: %s: %s: %w", op, errDetail(err), storage.ErrTransport)
}

// apiDetail renders one non-semantic S3 API error as a single-line
// code-and-message pair, so a transport failure keeps its server-visible
// reason without leaking request context. An empty message keeps just the
// code.
func apiDetail(api smithy.APIError) string {
	msg := strings.TrimSpace(api.ErrorMessage())
	if msg == "" {
		return api.ErrorCode()
	}
	return api.ErrorCode() + ": " + msg
}

// errDetail renders a non-API error as a single line, collapsing any
// whitespace so a connection or credential-resolution failure keeps its
// real reason without spilling across diagnostic lines.
func errDetail(err error) string {
	return strings.Join(strings.Fields(err.Error()), " ")
}

// httpErrorDetail renders an HTTP failure the SDK could not deserialize
// (a non-JSON response, typically an HTML error page) as one concise line:
// the status code and a short, tag-stripped body fragment. The second
// result reports whether the error is this case, so the caller falls back
// to the generic reason for anything else.
func httpErrorDetail(err error) (string, bool) {
	var de *smithy.DeserializationError
	if !errors.As(err, &de) {
		return "", false
	}
	snippet := bodySnippet(de.Snapshot)
	status := 0
	var respErr *smithyhttp.ResponseError
	if errors.As(err, &respErr) {
		status = respErr.HTTPStatusCode()
	}
	switch {
	case status != 0 && snippet != "":
		return fmt.Sprintf("service returned a non-JSON HTTP %d response: %q", status, snippet), true
	case snippet != "":
		return fmt.Sprintf("service returned a non-JSON response: %q", snippet), true
	case status != 0:
		return fmt.Sprintf("service returned a non-JSON HTTP %d response", status), true
	default:
		return "service returned a non-JSON response", true
	}
}

// bodySnippet reduces an error-page body to a short plain-text fragment:
// tags are removed, entities decoded, whitespace collapsed, and the result
// bounded so the diagnostic stays on one readable line.
func bodySnippet(body []byte) string {
	const maxRunes = 160
	text := html.UnescapeString(string(stripTags(body)))
	text = strings.Join(strings.Fields(text), " ")
	if r := []rune(text); len(r) > maxRunes {
		text = string(r[:maxRunes]) + "..."
	}
	return text
}

// stripTags removes angle-bracket tag spans from markup, leaving the text
// content (titles, headings, and error prose) for a readable fragment.
func stripTags(b []byte) []byte {
	var out []byte
	inTag := false
	for _, c := range b {
		switch {
		case c == '<':
			inTag = true
		case c == '>':
			inTag = false
		case !inTag:
			out = append(out, c)
		}
	}
	return out
}

// Metadata header names (architecture section 9.1). The AWS SDK exposes
// user metadata without the x-amz-meta- prefix, in lowercase.
const (
	metaSHA256     = "slivingdoc-sha256"
	metaSize       = "slivingdoc-size"
	metaKind       = "slivingdoc-kind"
	metaGeneration = "slivingdoc-generation"
)

func encodeMeta(meta storage.Metadata) map[string]string {
	return map[string]string{
		metaSHA256:     meta.SHA256.String(),
		metaSize:       strconv.FormatUint(meta.Size, 10),
		metaKind:       string(meta.Kind),
		metaGeneration: strconv.FormatUint(meta.Generation, 10),
	}
}

// decodeMeta decodes the slivingdoc metadata headers. Absent headers leave
// the zero metadata; present-but-malformed headers are an integrity error
// because the object claims to be a protocol pack it cannot describe.
func decodeMeta(md map[string]string) (storage.Metadata, error) {
	var meta storage.Metadata
	if len(md) == 0 {
		return meta, nil
	}
	if v, ok := md[metaSHA256]; ok {
		h, err := storage.ParseSHA256(v)
		if err != nil {
			return meta, fmt.Errorf("s3store: metadata %s: %w", metaSHA256, err)
		}
		meta.SHA256 = h
	}
	if v, ok := md[metaSize]; ok {
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return meta, fmt.Errorf("s3store: metadata %s: %w", metaSize, err)
		}
		meta.Size = n
	}
	if v, ok := md[metaKind]; ok {
		if !storage.PackKind(v).Valid() {
			return meta, fmt.Errorf("s3store: metadata %s: invalid kind %q", metaKind, v)
		}
		meta.Kind = storage.PackKind(v)
	}
	if v, ok := md[metaGeneration]; ok {
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return meta, fmt.Errorf("s3store: metadata %s: %w", metaGeneration, err)
		}
		meta.Generation = n
	}
	return meta, nil
}
