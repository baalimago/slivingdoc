package app

import (
	"errors"
	"fmt"
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

// awsSignals are the environment variables that mean the operator
// configured S3 on purpose. With a stored login for the same space they
// make auto mode refuse rather than guess.
var awsSignals = []string{"AWS_ACCESS_KEY_ID", "AWS_PROFILE", "AWS_ENDPOINT_URL_S3"}

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
// configuration directory) and the clock (which judges expiry).
type storageInputs struct {
	goos string
	now  time.Time
}

// resolveStorage decides whether the process uses S3 or the hosted API,
// with which bucket or space and which token (architecture/login.md):
//
//   - s3: the AWS chain only; SLIVINGDOC_TOKEN and stored logins are
//     ignored and never read.
//   - hosted: SLIVINGDOC_TOKEN if set, else the stored login for the space;
//     neither is a refusal.
//   - auto: SLIVINGDOC_TOKEN → hosted; a stored login plus an AWS variable
//     → refusal; a stored login → hosted; otherwise S3.
//
// Outside s3 mode an omitted bucket is the default login's space. A stored
// token is only used for the endpoint it was issued for: an explicit
// endpoint that differs means the login does not apply. No refusal echoes
// a token.
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

	var logins credentials.Set
	if sel.bucket == "" || token == "" {
		if logins, err = loadLogins(env, in.goos); err != nil {
			return storageSelection{}, err
		}
	}
	if sel.bucket == "" {
		if def, err := logins.Default(); err == nil {
			sel.bucket = def.Space
		}
	}

	if token != "" {
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
		return sel, nil
	case errors.Is(err, credentials.ErrNoLogin):
		return storageSelection{}, noLoginRefusal(logins, explicit, sel.bucket)
	case err != nil:
		return storageSelection{}, err
	}
	if mode == storageAuto {
		if set := awsConfigured(env); len(set) > 0 {
			return storageSelection{}, fmt.Errorf(
				"a stored login for space %q and S3 settings (%s) are both configured; pass --storage hosted or --storage s3",
				sel.bucket, strings.Join(set, ", "))
		}
	}
	if err := login.Usable(in.now); err != nil {
		return storageSelection{}, fmt.Errorf("%w; run 'slivingdoc login --bucket %s'", err, sel.bucket)
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

// awsConfigured returns the AWS signal variables set in env.
func awsConfigured(env map[string]string) []string {
	var set []string
	for _, name := range awsSignals {
		if env[name] != "" {
			set = append(set, name)
		}
	}
	return set
}
