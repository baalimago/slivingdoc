package app

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/baalimago/slivingdoc/internal/credentials"
	"github.com/baalimago/slivingdoc/internal/settings"
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
// explicit bucket a refusal rather than a guess. Any variable starting
// with awsContainerPrefix counts too, and so do the shared AWS files
// (awsFiles) and the S3-only flags --profile, --region and --path-style
// (s3Signals).
func awsSignals() []string {
	return []string{
		"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN", "AWS_PROFILE",
		"AWS_REGION", "AWS_DEFAULT_REGION", "AWS_CONFIG_FILE", "AWS_SHARED_CREDENTIALS_FILE",
		"AWS_ROLE_ARN", "AWS_WEB_IDENTITY_TOKEN_FILE", "AWS_ENDPOINT_URL", "AWS_ENDPOINT_URL_S3",
		"SLIVINGDOC_PATH_STYLE",
	}
}

// awsContainerPrefix starts the container credential variables of the
// AWS chain (AWS_CONTAINER_CREDENTIALS_RELATIVE_URI, _FULL_URI, ...).
const awsContainerPrefix = "AWS_CONTAINER_CREDENTIALS_"

// awsFiles are the shared AWS files, relative to the home directory, whose
// existence is an S3 signal.
func awsFiles() []string {
	return []string{filepath.Join(".aws", "credentials"), filepath.Join(".aws", "config")}
}

// storageSelection is the outcome of resolveStorage: the store kind, every
// field of its target, and, when hosted, the SLIVINGDOC_TOKEN value or the
// stored login whose key mints the tokens. The record of the notebook
// directory supplies any field where no flag or slivingdoc variable named
// one, and the one global default supplies the remaining fields
// (architecture/config.md, The remembered target of a directory).
type storageSelection struct {
	kind       settings.Kind
	bucket     string
	bucketFrom bucketSource
	token      string
	login      *credentials.Login
	origin     tokenOrigin
	endpoint   string
	region     string
	profile    string
	prefix     string
	pathStyle  bool
	// remembered is the target this directory remembers, and recorded says
	// whether it has one at all.
	remembered settings.Target
	recorded   bool
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
	// bucketFromRemembered is the target this directory remembers in the
	// scoped settings file (architecture/config.md, The scoped settings
	// store).
	bucketFromRemembered
	// bucketFromDefault is the one active global default: the stored S3
	// default, because a hosted default is the login's default space.
	bucketFromDefault
)

// rememberedSource is how a record reads everywhere: the wording of its
// source, the trailer status prints, and the help text.
const (
	// rememberedSource is a hosted record's source wording.
	rememberedSource = "remembered space"
	// rememberedBucketSource is an S3 record's source wording; SpaceSource
	// selects it because the source itself carries no kind.
	rememberedBucketSource = "remembered bucket"
	// defaultBucketSource is the active global S3 default's wording.
	defaultBucketSource = "S3 default"
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
	case bucketFromRemembered:
		return rememberedSource
	case bucketFromDefault:
		return defaultBucketSource
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

// namedAsBucket says whether the space was named with the S3 spelling of
// the setting, which beside S3 settings selects S3 (architecture/login.md,
// Which storage a process uses).
func (b bucketSource) namedAsBucket() bool {
	return b == bucketFromFlag || b == bucketFromEnv
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

// storageInputs are the injected facts resolveStorage reads besides the
// flags and the environment: the operating system (which picks the user
// configuration directory and the home variable), the clock (which judges
// expiry), stat (which finds the shared AWS files; nil is os.Stat) and the
// scoped settings of the directories (their record and the global default).
type storageInputs struct {
	goos string
	now  time.Time
	stat func(string) (fs.FileInfo, error)
	// remembered is the scoped settings this process consults, or an error
	// when the file cannot be read. Nil is a process that consults no
	// association.
	remembered func() (settingsView, error)
}

// resolveStorage decides whether the process uses S3 or the hosted API and
// resolves every field of its target (architecture/login.md, Which storage a
// process uses; architecture/config.md, The remembered target of a
// directory):
//
//   - s3: the AWS chain only; SLIVINGDOC_TOKEN is ignored and stored logins
//     are never read.
//   - hosted: SLIVINGDOC_TOKEN if set, else the stored login; neither is a
//     refusal.
//   - auto: SLIVINGDOC_TOKEN → hosted. Else the bucket spelling beside an S3
//     signal, then the kind this directory remembers, then the S3 default,
//     then a stored login, else S3.
//
// Each field of an S3 target resolves from its flag or slivingdoc variable,
// then a record of the selected kind, then the S3 default, then the ambient
// AWS configuration. With a stored login the name is the bucket setting,
// else the record, else the login's default space; with none it is a
// refusal naming 'slivingdoc space <name>'. A space named as --space or
// SLIVINGDOC_SPACE beside S3 settings, an S3 record, or the S3 default is a
// refusal rather than a guess. No refusal echoes a key or a token.
func resolveStorage(f *Flags, env map[string]string, in storageInputs) (storageSelection, error) {
	mode, err := parseStorageMode(resolveString(&f.storage, env["SLIVINGDOC_STORAGE"], string(storageAuto)))
	if err != nil {
		return storageSelection{}, err
	}
	var sel storageSelection
	if sel.bucket, sel.bucketFrom, err = resolveBucket(f, env); err != nil {
		return storageSelection{}, err
	}
	// The hosted endpoint is normalized before the backend is known, so a
	// malformed value refuses a process that turns out to be S3 too.
	explicit, err := normalizeEndpoint(resolveString(&f.endpoint, env["SLIVINGDOC_ENDPOINT"], ""))
	if err != nil {
		return storageSelection{}, err
	}
	view, err := scopedView(in)
	if err != nil {
		return storageSelection{}, err
	}
	sel.remembered, sel.recorded = view.record, view.recorded
	var logins credentials.Set
	if mode != storageS3 && env["SLIVINGDOC_TOKEN"] == "" {
		if logins, err = loadLogins(env, in.goos); err != nil {
			if errors.Is(err, credentials.ErrUnsupportedVersion) {
				return storageSelection{}, fmt.Errorf("%w, or pass --storage s3 to use S3", err)
			}
			return storageSelection{}, fmt.Errorf("%w; fix or remove the file, or pass --storage s3 to use S3", err)
		}
	}
	login, err := selectKind(f, env, in, mode, &sel, view, logins, explicit)
	if err != nil {
		return storageSelection{}, err
	}
	if sel.kind == settings.KindS3 {
		return resolveS3Target(f, env, sel, view)
	}
	return resolveHostedTarget(f, env, sel, mode, logins, login, explicit, in)
}

// scopedView is the record and the S3 default of this directory, or an empty
// view for a process that consults no association.
func scopedView(in storageInputs) (settingsView, error) {
	if in.remembered == nil {
		return settingsView{}, nil
	}
	return in.remembered()
}

// selectKind resolves the backend alone: what the flags and variables name,
// then the directory's record, then the S3 default, then a stored login. It
// returns the login only when the stored login is what selected the hosted
// backend; the token case returns none, because its space comes from the
// hosted API. (I4, I5)
func selectKind(f *Flags, env map[string]string, in storageInputs, mode storageMode, sel *storageSelection, view settingsView, logins credentials.Set, explicit string) (*credentials.Login, error) {
	if mode == storageS3 {
		sel.kind = settings.KindS3
		return nil, nil
	}
	// A variable's token selects hosted under auto and hosted alike; no
	// stored login is read, so a broken credentials file never stops it.
	if env["SLIVINGDOC_TOKEN"] != "" {
		sel.kind = settings.KindHosted
		return nil, nil
	}
	if mode == storageHosted {
		sel.kind = settings.KindHosted
		refusal := noLoginRefusal(logins, explicit)
		if sel.recorded && view.record.Kind == settings.KindHosted {
			refusal = noCredentialRefusal(logins, explicit, view.record.Name)
		}
		return loginFor(logins, explicit, needsSignIn{refusal})
	}
	if sel.bucketFrom.namedAsBucket() && len(s3Signals(f, env, in)) > 0 {
		sel.kind = settings.KindS3
		return nil, nil
	}
	if sel.recorded {
		sel.kind = view.record.Kind
		if sel.kind == settings.KindS3 {
			return nil, nil
		}
		return loginFor(logins, explicit, needsSignIn{noCredentialRefusal(logins, explicit, view.record.Name)})
	}
	if view.defaultFound {
		sel.kind = settings.KindS3
		return nil, nil
	}
	login, err := pickLogin(logins, explicit)
	switch {
	case errors.Is(err, credentials.ErrNoLogin):
		sel.kind = settings.KindS3
		return nil, nil
	case errors.Is(err, credentials.ErrAmbiguous):
		return nil, ambiguousLoginRefusal(err)
	case err != nil:
		return nil, err
	}
	sel.kind = settings.KindHosted
	return &login, nil
}

// loginFor picks the stored login a hosted selection needs, refusing with
// the caller's wording when none applies.
func loginFor(logins credentials.Set, explicit string, refuse error) (*credentials.Login, error) {
	login, err := pickLogin(logins, explicit)
	switch {
	case errors.Is(err, credentials.ErrAmbiguous):
		return nil, ambiguousLoginRefusal(err)
	case err != nil:
		return nil, refuse
	}
	return &login, nil
}

// ambiguousLoginRefusal adds the fixes to two stored logins that match.
func ambiguousLoginRefusal(err error) error {
	return fmt.Errorf("%w; pass --endpoint to choose one, or --storage s3", err)
}

// resolveHostedTarget resolves the name, endpoint, and prefix of a hosted
// selection. A token ignores the record entirely: it names its space for
// one run, and an omitted name stays empty for resolveHostedSpace to fill.
// (I4, I6)
func resolveHostedTarget(f *Flags, env map[string]string, sel storageSelection, mode storageMode, logins credentials.Set, login *credentials.Login, explicit string, in storageInputs) (storageSelection, error) {
	sel.kind = settings.KindHosted
	if login == nil {
		sel.token, sel.origin, sel.endpoint = env["SLIVINGDOC_TOKEN"], originEnv, explicit
		if sel.endpoint == "" {
			sel.endpoint = DefaultHostedEndpoint
		}
		return resolveTargetPrefix(f, env, sel, settings.Target{}, false, settings.Target{}, false)
	}
	if sel.bucket == "" && sel.recorded && sel.remembered.Kind == settings.KindHosted {
		sel.bucket, sel.bucketFrom = sel.remembered.Name, bucketFromRemembered
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
	// Beside S3 settings the spelling decides: --space and SLIVINGDOC_SPACE
	// name a hosted space, which is a refusal rather than a guess, while the
	// bucket spelling already selected S3 (selectKind). The login's default
	// space and a record are the target this machine means for this
	// directory, not a choice the operator made beside S3 settings, so
	// neither is judged here.
	if mode == storageAuto && sel.bucketFrom.kind() != kindOther {
		if signals := s3Signals(f, env, in); len(signals) > 0 {
			return storageSelection{}, fmt.Errorf(
				"a stored login and S3 settings (%s) are both configured for %q; pass --storage hosted or --storage s3",
				strings.Join(signals, ", "), sel.bucket)
		}
	}
	if err := login.Usable(in.now); err != nil {
		return storageSelection{}, needsSignIn{fmt.Errorf("%w; run 'slivingdoc login' again, or pass --storage s3 to use S3", err)}
	}
	sel.login, sel.origin, sel.endpoint = login, originLogin, login.Endpoint
	sameRecord := sel.recorded && sel.remembered.Kind == settings.KindHosted && sel.remembered.Name == sel.bucket
	record := settings.Target{}
	if sameRecord {
		record = sel.remembered
	}
	return resolveTargetPrefix(f, env, sel, record, sameRecord, settings.Target{}, false)
}

// resolveS3Target resolves every field of an S3 target from its flag or
// slivingdoc variable, then a record of the selected kind, then the one S3
// default, then the ambient AWS configuration. (I4, I5)
func resolveS3Target(f *Flags, env map[string]string, sel storageSelection, view settingsView) (storageSelection, error) {
	sel.kind = settings.KindS3
	record := settings.Target{}
	recorded := sel.recorded && view.record.Kind == settings.KindS3
	if recorded {
		record = view.record
	}
	def := settings.Target{}
	if view.defaultFound {
		def = view.s3Default
	}
	if (sel.bucketFrom == bucketFromSpaceFlag || sel.bucketFrom == bucketFromSpaceEnv) && (recorded || view.defaultFound) {
		// The hosted spelling beside an S3 record or the S3 default asks for
		// a choice rather than guessing which store the operator means. With
		// neither, the same spelling simply names an S3 bucket, because a
		// space is a bucket in a store that has no hosted alternative.
		cause := "the S3 default"
		if recorded {
			cause = "an S3 record of this directory"
		}
		return storageSelection{}, fmt.Errorf("%s and %s naming %q point at different stores; pass --storage hosted or --storage s3",
			cause, sel.bucketFrom, sel.bucket)
	}
	if sel.bucket == "" && recorded {
		sel.bucket, sel.bucketFrom = record.Name, bucketFromRemembered
	}
	if sel.bucket == "" && view.defaultFound {
		sel.bucket, sel.bucketFrom = def.Name, bucketFromDefault
	}
	switch {
	case f.profile.set:
		sel.profile = f.profile.value
	case recorded:
		sel.profile = record.Profile
	case view.defaultFound:
		sel.profile = def.Profile
	default:
		sel.profile = env["AWS_PROFILE"]
	}
	switch {
	case f.endpoint.set:
		sel.endpoint = f.endpoint.value
	case recorded:
		sel.endpoint = record.Endpoint
	case view.defaultFound:
		sel.endpoint = def.Endpoint
	default:
		sel.endpoint = env["AWS_ENDPOINT_URL_S3"]
	}
	switch {
	case f.region.set:
		sel.region = f.region.value
	case recorded:
		sel.region = record.Region
	case view.defaultFound:
		sel.region = def.Region
	case env["AWS_REGION"] != "":
		sel.region = env["AWS_REGION"]
	default:
		sel.region = env["AWS_DEFAULT_REGION"]
	}
	switch {
	case f.pathStyle.set:
		sel.pathStyle = f.pathStyle.value
	case env["SLIVINGDOC_PATH_STYLE"] != "":
		v, err := strconv.ParseBool(env["SLIVINGDOC_PATH_STYLE"])
		if err != nil {
			return storageSelection{}, fmt.Errorf("invalid boolean value %q", env["SLIVINGDOC_PATH_STYLE"])
		}
		sel.pathStyle = v
	case recorded:
		sel.pathStyle = record.PathStyle
	case view.defaultFound:
		sel.pathStyle = def.PathStyle
	}
	sameRecord := recorded && record.Name == sel.bucket
	sameDefault := view.defaultFound && def.Name == sel.bucket
	return resolveTargetPrefix(f, env, sel, record, sameRecord, def, sameDefault)
}

// resolveTargetPrefix resolves the object prefix: the flag or variable, else
// the record of this notebook, else the S3 default's prefix, else the
// built-in default. A named prefix that contradicts the record is a refusal,
// because the record says which notebook of the store this directory holds.
func resolveTargetPrefix(f *Flags, env map[string]string, sel storageSelection, record settings.Target, recorded bool, def settings.Target, hasDefault bool) (storageSelection, error) {
	if from := resolvePrefixSource(&f.prefix, env["SLIVINGDOC_PREFIX"]); from != kindOther {
		sel.prefix = resolveString(&f.prefix, env["SLIVINGDOC_PREFIX"], "")
		if recorded && record.Prefix != "" && sel.prefix != record.Prefix {
			return storageSelection{}, prefixMismatch(record.Kind, record.Name, sel.prefix, record.Prefix)
		}
		return sel, nil
	}
	sel.prefix = defaultPrefix
	switch {
	case recorded && record.Prefix != "":
		sel.prefix = record.Prefix
	case hasDefault && def.Prefix != "":
		sel.prefix = def.Prefix
	}
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

// s3FlagSignals is the narrow view of a flag holder that s3Signals reads:
// the three S3-only flags, in both the shared and the space flag sets.
type s3FlagSignals interface {
	regionSet() bool
	pathStyleSet() bool
	profileSet() bool
}

// s3Signals returns what configures S3 on purpose, in a fixed order: the
// awsSignals variables that are set, the container credential variables,
// the S3-only flags, and the shared AWS files that exist under the home
// directory.
func s3Signals(f s3FlagSignals, env map[string]string, in storageInputs) []string {
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
	if f.profileSet() {
		set = append(set, "--profile")
	}
	if f.regionSet() {
		set = append(set, "--region")
	}
	if f.pathStyleSet() {
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
