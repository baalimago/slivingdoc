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

// tokenOrigin says where a hosted process's token comes from: the
// variable, or tokens minted from a stored login's key. A refusal names
// the fix that applies.
type tokenOrigin string

const (
	originNone  tokenOrigin = ""
	originEnv   tokenOrigin = "SLIVINGDOC_TOKEN"
	originLogin tokenOrigin = "login"
)

// awsSignals returns the environment variables that mean the operator
// configured S3 on purpose. In auto mode they make a stored login beside an
// explicit bucket a refusal rather than a guess; SLIVINGDOC_TOKEN is
// refused only beside the ones that name an S3 host (destinationSignals).
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

// destinationSignals are the environment variables that name an S3 host.
// Beside SLIVINGDOC_TOKEN in auto mode they are a refusal as an intent
// check: the operator pointed S3 at a host on purpose, so hosted mode is
// not assumed. Hosted mode never reads them, so the token never follows
// them. A region or AWS credentials name no host and are no refusal.
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
// space or bucket, and, when hosted, the SLIVINGDOC_TOKEN value or the
// stored login whose key mints the tokens, the origin, and the hosted
// endpoint.
type storageSelection struct {
	bucket     string
	bucketFrom bucketSource
	token      string
	login      *credentials.Login
	origin     tokenOrigin
	endpoint   string
}

// bucketSource is where the bucket or space came from, in the spelling
// the operator used, so a space mismatch names the setting to change and
// the startup log names the source. --space and SLIVINGDOC_SPACE are the
// hosted names of --bucket and SLIVINGDOC_BUCKET: one setting.
type bucketSource int

const (
	bucketNone bucketSource = iota
	// bucketCleared is an explicitly empty --bucket or --space, which
	// does not fall back to the environment.
	bucketCleared
	// bucketFromFlag is --bucket.
	bucketFromFlag
	// bucketFromSpaceFlag is --space.
	bucketFromSpaceFlag
	// bucketFromEnv is SLIVINGDOC_BUCKET.
	bucketFromEnv
	// bucketFromSpaceEnv is SLIVINGDOC_SPACE.
	bucketFromSpaceEnv
	// bucketFromLogin is the stored login's default space.
	bucketFromLogin
	// bucketFromToken is the space the hosted API says the token reaches.
	bucketFromToken
)

func (b bucketSource) String() string {
	switch b {
	case bucketFromFlag:
		return "--bucket"
	case bucketFromSpaceFlag:
		return "--space"
	case bucketFromEnv:
		return bucketEnv
	case bucketFromSpaceEnv:
		return spaceEnv
	case bucketFromLogin:
		return "default space"
	case bucketFromToken:
		return "token"
	default:
		return "none"
	}
}

// settingKind says whether the space came from a flag, the environment,
// or neither.
type settingKind int

const (
	kindOther settingKind = iota
	kindFlag
	kindEnv
)

func (b bucketSource) kind() settingKind {
	switch b {
	case bucketFromFlag, bucketFromSpaceFlag:
		return kindFlag
	case bucketFromEnv, bucketFromSpaceEnv:
		return kindEnv
	default:
		return kindOther
	}
}

const (
	bucketEnv = "SLIVINGDOC_BUCKET"
	spaceEnv  = "SLIVINGDOC_SPACE"
)

// flagSpace resolves --bucket and --space, the two spellings of one flag.
// Both given with different values is a refusal naming both; the same
// value is fine. An explicitly empty flag is bucketCleared.
func flagSpace(bucket, space *stringFlag) (string, bucketSource, error) {
	switch {
	case bucket.set && space.set && bucket.value != space.value:
		return "", bucketNone, fmt.Errorf("--bucket %q and --space %q name different spaces; they are one setting, so pass one of them", bucket.value, space.value)
	case space.set && space.value != "":
		return space.value, bucketFromSpaceFlag, nil
	case bucket.set && bucket.value != "":
		return bucket.value, bucketFromFlag, nil
	case bucket.set || space.set:
		return "", bucketCleared, nil
	default:
		return "", bucketNone, nil
	}
}

// resolveBucket returns the named bucket and which setting named it: a
// flag wins, even when explicitly empty, over the environment, and
// either spelling of one layer given twice must agree.
func resolveBucket(f *Flags, env map[string]string) (string, bucketSource, error) {
	value, from, err := flagSpace(&f.bucket, &f.space)
	switch {
	case err != nil:
		return "", bucketNone, err
	case from == bucketCleared:
		return "", bucketNone, nil
	case from != bucketNone:
		return value, from, nil
	}
	b, sp := env[bucketEnv], env[spaceEnv]
	switch {
	case b != "" && sp != "" && b != sp:
		return "", bucketNone, fmt.Errorf("%s %q and %s %q name different spaces; they are one setting, so set one of them", bucketEnv, b, spaceEnv, sp)
	case sp != "":
		return sp, bucketFromSpaceEnv, nil
	case b != "":
		return b, bucketFromEnv, nil
	default:
		return "", bucketNone, nil
	}
}

func (s storageSelection) hosted() bool { return s.token != "" || s.login != nil }

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
// with which bucket or space and which credential (architecture/login.md,
// Which storage a process uses):
//
//   - s3: the AWS chain only; SLIVINGDOC_TOKEN and stored logins are
//     ignored and never read.
//   - hosted: SLIVINGDOC_TOKEN if set, else the stored login; neither is a
//     refusal.
//   - auto: SLIVINGDOC_TOKEN → hosted, unless the --endpoint flag,
//     AWS_ENDPOINT_URL or AWS_ENDPOINT_URL_S3 is set too (refusal). Else a
//     stored login → hosted when the space is the login's stored default,
//     or when no S3 signal is set; an explicit bucket with an S3 signal is
//     a refusal. Otherwise S3.
//
// With SLIVINGDOC_TOKEN the stored logins are never read: an omitted
// bucket stays empty and resolveHostedSpace asks the API for the token's
// space. With a stored login the space is the bucket setting, else the
// login's default space; with neither it is a refusal naming
// 'slivingdoc space <name>'. The login is the one for the explicit
// endpoint when one is configured, else the only one stored. No refusal
// echoes a key or a token.
func resolveStorage(f *Flags, env map[string]string, in storageInputs) (storageSelection, error) {
	mode, err := parseStorageMode(resolveString(&f.storage, env["SLIVINGDOC_STORAGE"], string(storageAuto)))
	if err != nil {
		return storageSelection{}, err
	}
	var sel storageSelection
	sel.bucket, sel.bucketFrom, err = resolveBucket(f, env)
	if err != nil {
		return storageSelection{}, err
	}
	if mode == storageS3 {
		return sel, nil
	}
	explicit, err := normalizeEndpoint(resolveString(&f.endpoint, env["SLIVINGDOC_ENDPOINT"], ""))
	if err != nil {
		return storageSelection{}, err
	}
	if token := env["SLIVINGDOC_TOKEN"]; token != "" {
		// The token alone is enough: its space comes from the hosted API
		// (resolveHostedSpace), and the stored logins are not read, so a
		// broken credentials file never stops a token user.
		if mode == storageAuto {
			if dest := tokenDestinations(f, env); len(dest) > 0 {
				return storageSelection{}, fmt.Errorf(
					"SLIVINGDOC_TOKEN and an S3 endpoint (%s) are both configured; pass --storage hosted to use hosted storage (the token goes only to the hosted endpoint), or --storage s3",
					strings.Join(dest, ", "))
			}
		}
		sel.token, sel.origin, sel.endpoint = token, originEnv, explicit
		if sel.endpoint == "" {
			sel.endpoint = DefaultHostedEndpoint
		}
		return sel, nil
	}
	logins, err := loadLogins(env, in.goos)
	if err != nil {
		if errors.Is(err, credentials.ErrUnsupportedVersion) {
			return storageSelection{}, fmt.Errorf("%w, or pass --storage s3 to use S3", err)
		}
		return storageSelection{}, fmt.Errorf("%w; fix or remove the file, or pass --storage s3 to use S3", err)
	}
	login, err := pickLogin(logins, explicit)
	switch {
	case errors.Is(err, credentials.ErrAmbiguous):
		return storageSelection{}, fmt.Errorf("%w; pass --endpoint to choose one, or --storage s3", err)
	case errors.Is(err, credentials.ErrNoLogin) && mode == storageAuto:
		return sel, nil
	case errors.Is(err, credentials.ErrNoLogin):
		return storageSelection{}, noLoginRefusal(logins, explicit)
	case err != nil:
		return storageSelection{}, err
	}
	if sel.bucket == "" {
		if space, err := logins.DefaultSpace(login.Endpoint); err == nil {
			sel.bucket, sel.bucketFrom = space, bucketFromLogin
		}
	}
	if sel.bucket == "" {
		return storageSelection{}, fmt.Errorf(
			"the stored login for %s has no default space; run 'slivingdoc space' to list its spaces and 'slivingdoc space <name>' to choose one, or pass --space (for S3, pass --storage s3 and --bucket)",
			login.Endpoint)
	}
	if mode == storageAuto && sel.bucketFrom != bucketFromLogin {
		if signals := s3Signals(f, env, in); len(signals) > 0 {
			return storageSelection{}, fmt.Errorf(
				"a stored login and S3 settings (%s) are both configured for %q; pass --storage hosted or --storage s3",
				strings.Join(signals, ", "), sel.bucket)
		}
	}
	if err := login.Usable(in.now); err != nil {
		return storageSelection{}, fmt.Errorf("%w; run 'slivingdoc login' again, or pass --storage s3 to use S3", err)
	}
	sel.login, sel.origin, sel.endpoint = &login, originLogin, login.Endpoint
	return sel, nil
}

// loadLogins reads the stored logins. A process whose environment names
// no configuration directory has none; a file that exists and cannot be
// read is an error.
func loadLogins(env map[string]string, goos string) (credentials.Set, error) {
	file, err := credentials.Locate(func(name string) string { return env[name] }, goos)
	if errors.Is(err, credentials.ErrNoConfigDir) {
		return credentials.Set{}, nil
	}
	if err != nil {
		return credentials.Set{}, err
	}
	return file.Load()
}

// pickLogin chooses the stored login: exactly the explicit endpoint's when
// one is configured, else the only one stored.
func pickLogin(logins credentials.Set, explicit string) (credentials.Login, error) {
	if explicit != "" {
		return logins.ForEndpoint(explicit)
	}
	return logins.Only()
}

// noLoginRefusal is the --storage hosted refusal when nothing supplies a
// credential, naming the endpoint mismatch when that is why a login does
// not apply.
func noLoginRefusal(logins credentials.Set, explicit string) error {
	msg := "--storage hosted needs SLIVINGDOC_TOKEN or a stored login"
	if others := logins.Logins(); explicit != "" && len(others) > 0 {
		msg += fmt.Sprintf(" for %s; the stored login is for %s", explicit, others[0].Endpoint)
	}
	return fmt.Errorf("%s; run 'slivingdoc login'", msg)
}

// tokenDestinations returns the settings beside SLIVINGDOC_TOKEN that say
// the operator meant an S3 host: the --endpoint flag (which is the S3
// endpoint outside hosted mode) and the destinationSignals that are set.
// SLIVINGDOC_ENDPOINT alone names the hosted API and is not one.
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
