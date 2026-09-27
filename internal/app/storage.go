package app

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/baalimago/slivingdoc/internal/credentials"
)

// storageMode is the --storage choice (architecture/login.md, Which
// storage a process uses).
type storageMode string

const (
	storageAuto   storageMode = "auto"
	storageHosted storageMode = "hosted"
	storageS3     storageMode = "s3"
)

func parseStorageMode(s string) (storageMode, error) {
	switch storageMode(s) {
	case storageAuto, storageHosted, storageS3:
		return storageMode(s), nil
	default:
		return "", fmt.Errorf("storage %q is not one of auto, hosted, s3", s)
	}
}

// tokenOrigin says where a hosted process's token came from, so a refusal
// can name the right fix.
type tokenOrigin string

const (
	originNone  tokenOrigin = ""
	originEnv   tokenOrigin = "SLIVINGDOC_TOKEN"
	originLogin tokenOrigin = "login"
)

// awsSignals returns the environment variables that mean the operator
// configured S3 on purpose. In auto mode they make a stored login for an
// explicit bucket a refusal rather than a guess; SLIVINGDOC_TOKEN is
// refused only beside the ones that name a destination (destinationSignals).
// Any variable starting with awsContainerPrefix counts too, and so do the
// shared AWS files (awsFiles) and the S3-only flags --region and
// --path-style (s3Signals).
func awsSignals() []string {
	return []string{
		"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN", "AWS_PROFILE",
		"AWS_REGION", "AWS_DEFAULT_REGION", "AWS_CONFIG_FILE", "AWS_SHARED_CREDENTIALS_FILE",
		"AWS_ROLE_ARN", "AWS_WEB_IDENTITY_TOKEN_FILE", "AWS_ENDPOINT_URL", "AWS_ENDPOINT_URL_S3",
		"SLIVINGDOC_PATH_STYLE",
	}
}

// destinationSignals are the S3 settings that name the host a request
// goes to. Beside SLIVINGDOC_TOKEN in auto mode they are a refusal: the
// token would follow them to that host. A region or AWS credentials name
// no host and the hosted adapter never reads them, so they are not.
func destinationSignals() []string {
	return []string{"AWS_ENDPOINT_URL", "AWS_ENDPOINT_URL_S3"}
}

// awsContainerPrefix starts the container credential variables of the
// AWS chain (AWS_CONTAINER_CREDENTIALS_RELATIVE_URI, _FULL_URI, ...).
const awsContainerPrefix = "AWS_CONTAINER_CREDENTIALS_"

// awsFiles are the shared AWS files, relative to the home directory, whose
// existence is an S3 signal.
func awsFiles() []string {
	return []string{filepath.Join(".aws", "credentials"), filepath.Join(".aws", "config")}
}

// storageSelection is the outcome of resolveStorage: the store kind, the
// space or bucket, and, when hosted, the token, its origin and the hosted
// endpoint.
type storageSelection struct {
	bucket   string
	token    string
	origin   tokenOrigin
	endpoint string
}

func (s storageSelection) hosted() bool { return s.token != "" }

// storageInputs are the injected facts resolveStorage reads besides the
// flags and the environment: the operating system (which picks the user
// configuration directory and the home variable), the clock (which judges
// expiry) and stat (which finds the shared AWS files; nil is os.Stat).
type storageInputs struct {
	goos string
	now  time.Time
	stat func(string) (fs.FileInfo, error)
}

// resolveStorage decides whether the process uses S3 or the hosted API,
// with which bucket or space and which token (architecture/login.md, Which
// storage a process uses):
//
//   - s3: the AWS chain only; SLIVINGDOC_TOKEN and stored logins are
//     ignored and never read.
//   - hosted: SLIVINGDOC_TOKEN if set, else the stored login for the space;
//     neither is a refusal.
//   - auto: SLIVINGDOC_TOKEN → hosted, unless the --endpoint flag,
//     AWS_ENDPOINT_URL or AWS_ENDPOINT_URL_S3 is set too (refusal). Else a stored login for the space → hosted when
//     the bucket came from the default login, or when no S3 signal is set;
//     an explicit bucket with an S3 signal is a refusal. Otherwise S3.
//
// Outside s3 mode an omitted bucket is the default login's space, but only
// when the outcome is hosted: a login that does not apply never names an
// S3 bucket. A stored token is only used for the endpoint it was issued
// for: an explicit endpoint that differs means the login does not apply.
// No refusal echoes a token.
func resolveStorage(f *Flags, env map[string]string, in storageInputs) (storageSelection, error) {
	mode, err := parseStorageMode(resolveString(&f.storage, env["SLIVINGDOC_STORAGE"], string(storageAuto)))
	if err != nil {
		return storageSelection{}, err
	}
	sel := storageSelection{bucket: resolveString(&f.bucket, env["SLIVINGDOC_BUCKET"], "")}
	if mode == storageS3 {
		return sel, nil
	}
	explicit, err := normalizeEndpoint(resolveString(&f.endpoint, env["SLIVINGDOC_ENDPOINT"], ""))
	if err != nil {
		return storageSelection{}, err
	}
	token := env["SLIVINGDOC_TOKEN"]
	var signals []string
	if mode == storageAuto {
		signals = s3Signals(f, env, in)
	}

	var logins credentials.Set
	if sel.bucket == "" || token == "" {
		if logins, err = loadLogins(env, in.goos); err != nil {
			return storageSelection{}, err
		}
	}
	defaulted := false
	if sel.bucket == "" {
		if def, err := logins.Default(); err == nil {
			sel.bucket, defaulted = def.Space, true
		}
	}

	if token != "" {
		if mode == storageAuto {
			if dest := tokenDestinations(f, env); len(dest) > 0 {
				return storageSelection{}, fmt.Errorf(
					"SLIVINGDOC_TOKEN and an S3 endpoint (%s) are both configured; pass --storage hosted to send the token there, or --storage s3",
					strings.Join(dest, ", "))
			}
		}
		sel.token, sel.origin, sel.endpoint = token, originEnv, explicit
		if sel.endpoint == "" {
			sel.endpoint = DefaultHostedEndpoint
		}
		return sel, nil
	}
	if sel.bucket == "" {
		if mode == storageHosted {
			return storageSelection{}, errors.New("--storage hosted needs a space: pass --bucket, or run 'slivingdoc login'")
		}
		return sel, nil
	}

	login, err := findLogin(logins, explicit, sel.bucket)
	switch {
	case errors.Is(err, credentials.ErrAmbiguous):
		return storageSelection{}, fmt.Errorf("%w; pass --endpoint to choose one", err)
	case errors.Is(err, credentials.ErrNoLogin) && mode == storageAuto:
		if defaulted {
			// The default login's space names no S3 bucket.
			sel.bucket = ""
		}
		return sel, nil
	case errors.Is(err, credentials.ErrNoLogin):
		return storageSelection{}, noLoginRefusal(logins, explicit, sel.bucket)
	case err != nil:
		return storageSelection{}, err
	}
	if mode == storageAuto && !defaulted && len(signals) > 0 {
		return storageSelection{}, fmt.Errorf(
			"a stored login for space %q and S3 settings (%s) are both configured; pass --storage hosted or --storage s3",
			sel.bucket, strings.Join(signals, ", "))
	}
	if err := login.Usable(in.now); err != nil {
		return storageSelection{}, fmt.Errorf("%w; run 'slivingdoc login --bucket %s', or pass --storage s3 to use S3", err, sel.bucket)
	}
	sel.token, sel.origin, sel.endpoint = login.Token, originLogin, login.Endpoint
	return sel, nil
}

// loadLogins reads the stored logins. A process whose environment names
// no configuration directory has none; a file that exists and cannot be
// read strictly refuses startup, whatever the mode that did not need it.
func loadLogins(env map[string]string, goos string) (credentials.Set, error) {
	file, err := credentials.Locate(func(name string) string { return env[name] }, goos)
	if errors.Is(err, credentials.ErrNoConfigDir) {
		return credentials.Set{}, nil
	}
	if err != nil {
		return credentials.Set{}, err
	}
	set, err := file.Load()
	if err != nil {
		return credentials.Set{}, fmt.Errorf("%w; fix or remove it, or pass --storage s3", err)
	}
	return set, nil
}

// findLogin picks the stored login for space: exactly the explicit
// endpoint's when one is configured, else the one login for the space.
func findLogin(logins credentials.Set, explicit, space string) (credentials.Login, error) {
	if explicit != "" {
		return logins.Lookup(credentials.Key{Endpoint: explicit, Space: space})
	}
	return logins.ForSpace(space)
}

// noLoginRefusal is the --storage hosted refusal when nothing supplies a
// token, naming the endpoint mismatch when that is why a login does not
// apply.
func noLoginRefusal(logins credentials.Set, explicit, space string) error {
	msg := fmt.Sprintf("--storage hosted needs SLIVINGDOC_TOKEN or a stored login for space %q", space)
	if others := logins.Space(space); explicit != "" && len(others) > 0 {
		msg += fmt.Sprintf(" at %s; the stored login was issued for %s", explicit, others[0].Endpoint)
	}
	return fmt.Errorf("%s; run 'slivingdoc login --bucket %s'", msg, space)
}

// tokenDestinations returns the settings that would send SLIVINGDOC_TOKEN
// to an S3 host: the --endpoint flag and the destinationSignals that are
// set. SLIVINGDOC_ENDPOINT alone names the hosted API and is not one.
func tokenDestinations(f *Flags, env map[string]string) []string {
	var set []string
	if f.endpoint.set {
		set = append(set, "--endpoint")
	}
	for _, name := range destinationSignals() {
		if env[name] != "" {
			set = append(set, name)
		}
	}
	return set
}

// s3Signals returns what configures S3 on purpose, in a fixed order: the
// awsSignals variables that are set, the container credential variables,
// the S3-only flags, and the shared AWS files that exist under the home
// directory.
func s3Signals(f *Flags, env map[string]string, in storageInputs) []string {
	var set []string
	for _, name := range awsSignals() {
		if env[name] != "" {
			set = append(set, name)
		}
	}
	var container []string
	for name, value := range env {
		if strings.HasPrefix(name, awsContainerPrefix) && value != "" {
			container = append(container, name)
		}
	}
	slices.Sort(container)
	set = append(set, container...)
	if f.region.set {
		set = append(set, "--region")
	}
	if f.pathStyle.set {
		set = append(set, "--path-style")
	}
	home := env["HOME"]
	if in.goos == "windows" {
		home = lookupFold(env, "USERPROFILE")
	}
	if home == "" || !filepath.IsAbs(home) {
		return set
	}
	stat := in.stat
	if stat == nil {
		stat = os.Stat
	}
	for _, rel := range awsFiles() {
		if _, err := stat(filepath.Join(home, rel)); err == nil {
			set = append(set, "~/"+filepath.ToSlash(rel))
		}
	}
	return set
}
