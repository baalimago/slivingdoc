package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/baalimago/slivingdoc/internal/credentials"
	"github.com/baalimago/slivingdoc/internal/mcp"
	"github.com/baalimago/slivingdoc/internal/s3store"
	"github.com/baalimago/slivingdoc/internal/settings"
	"github.com/baalimago/slivingdoc/internal/sitelogin"
	"github.com/baalimago/slivingdoc/internal/storage"
	"github.com/baalimago/slivingdoc/internal/tui"
)

// SpaceFlags are the space command's flags.
type SpaceFlags struct {
	storage   stringFlag
	endpoint  stringFlag
	region    stringFlag
	profile   stringFlag
	prefix    stringFlag
	pathStyle boolFlag
}

// NewSpaceFlags returns an unbound space flag holder.
func NewSpaceFlags() *SpaceFlags { return &SpaceFlags{} }

// Bind registers the space flags on fs.
func (f *SpaceFlags) Bind(fs *flag.FlagSet) {
	fs.Var(&f.storage, "storage", "storage backend: auto, hosted, or s3")
	fs.Var(&f.endpoint, "endpoint", "hosted storage API whose login to use, or the S3 endpoint")
	fs.Var(&f.region, "region", "S3 region")
	fs.Var(&f.profile, "profile", "AWS shared-config profile (S3 only)")
	fs.Var(&f.pathStyle, "path-style", "force S3 path-style addressing")
	fs.Var(&f.prefix, "prefix", "notebook prefix inside an S3 bucket")
}

func (f *SpaceFlags) regionSet() bool    { return f.region.set }
func (f *SpaceFlags) pathStyleSet() bool { return f.pathStyle.set }
func (f *SpaceFlags) profileSet() bool   { return f.profile.set }

// SpaceHeader is the identity one backend's group header names. Endpoint
// is the value --endpoint accepts; Account is the hosted login's account.
type SpaceHeader struct {
	Kind     settings.Kind
	Endpoint string
	Region   string
	Profile  string
	KeyID    string
	Account  string
}

// SpaceRow is one listed target. A recorded or default target the listing
// did not return stays visible with Reachable false.
type SpaceRow struct {
	Name        string
	Owner       string
	Access      credentials.Access
	Directories []string
	Reachable   bool
}

// spaceBackend is the space command's view of one store identity.
type spaceBackend interface {
	Kind() settings.Kind
	Header() SpaceHeader
	List(ctx context.Context) ([]SpaceRow, error)
	Create(ctx context.Context, name string) (SpaceRow, error)
	// Default is the backend's active default, which only the global S3
	// default can carry when it exists.
	Default() (settings.Target, bool)
	// HasDefault reports a stored default even when another backend's
	// default is active, so the listing does not claim there is none.
	HasDefault() bool
	SetDefault(ctx context.Context, name string) error
}

// s3Admin is the bucket administration of one S3 identity.
type s3Admin interface {
	ListBuckets(ctx context.Context) ([]s3store.Bucket, error)
	CreateBucket(ctx context.Context, name string) error
	Identity(ctx context.Context) (s3store.Identity, error)
}

// s3AdminFactory builds one identity's administration client and the
// effective values the SDK resolved.
type s3AdminFactory func(ctx context.Context, cfg s3store.Config, opts s3store.Options) (s3Admin, s3store.Resolved, error)

// realS3Admin resolves the identity and builds its administration client.
// The space tests inject a fake so no AWS configuration is read.
func realS3Admin(ctx context.Context, cfg s3store.Config, opts s3store.Options) (s3Admin, s3store.Resolved, error) {
	resolved, err := s3store.Resolve(ctx, cfg, opts)
	if err != nil {
		return nil, s3store.Resolved{}, err
	}
	admin, err := s3store.NewAdmin(ctx, cfg, opts)
	if err != nil {
		return nil, s3store.Resolved{}, err
	}
	return admin, resolved, nil
}

// spaceMode is what the positional arguments ask for.
type spaceMode int

const (
	spaceListMode spaceMode = iota
	spaceSetMode
	spaceCreateMode
)

// parseSpaceArgs reads the positional words. Flags precede them, because
// the Go flag parser stops at the first positional argument.
func parseSpaceArgs(args []string) (spaceMode, string, error) {
	switch len(args) {
	case 0:
		return spaceListMode, "", nil
	case 1:
		if args[0] == "new" {
			return 0, "", errors.New("space new: name the space to create, as in 'slivingdoc space new research'")
		}
		return spaceSetMode, args[0], nil
	case 2:
		if args[0] == "new" {
			return spaceCreateMode, args[1], nil
		}
		return 0, "", fmt.Errorf("space: unexpected argument %q; a create starts with 'new'", args[0])
	default:
		return 0, "", fmt.Errorf("space: unexpected argument %q", args[2])
	}
}

// Space is a prepared space command: the backend or backends are chosen
// from the flags and the environment, and nothing has been sent yet.
type Space struct {
	opts     ProcessOptions
	file     credentials.File
	stored   credentials.Set
	logins   []credentials.Login
	set      settings.Set
	explicit string
	mode     spaceMode
	name     string
	// storageSet, endpointSet, and the s3 settings record which flags
	// selected the backend, so a create's follow-up hint repeats them.
	storageSet   bool
	endpointSet  bool
	regionSet    bool
	profileSet   bool
	pathStyleSet bool
	prefixSet    bool
	s3           spaceS3Settings
	// useS3, s3Current, and s3Recorded are the S3 identities the command
	// resolves in Run, where a context exists.
	useS3       bool
	useS3Create bool
	s3Current   s3Identity
	s3Recorded  []settings.StoreKey
	prepared    bool
	// hostedIncluded reports whether the command consults hosted storage,
	// so an orphan record is shown only when that backend is in scope.
	// listingFailed reports that at least one backend could not list.
	hostedIncluded bool
	listingFailed  bool
	backends       []spaceBackend
}

// spaceChoice is one picker row: a listed target, or a backend's create
// row. A create row carries the backend that would create the target.
type spaceChoice struct {
	backend     spaceBackend
	header      SpaceHeader
	row         SpaceRow
	defaultName string
	create      bool
}

// spaceS3Settings are the S3 settings the space flags and environment name.
type spaceS3Settings struct {
	region    string
	profile   string
	pathStyle bool
	prefix    string
	// awsEndpoint is what AWS_ENDPOINT_URL_S3 or AWS_ENDPOINT_URL names.
	// The SDK resolves it itself; it is read only to recognize an S3
	// signal and a create that the environment selected.
	awsEndpoint string
}

// PrepareSpace chooses the backends the command lists, creates in, or sets
// the default of, from the flags and the environment alone; a directory
// record never selects it.
func PrepareSpace(f *SpaceFlags, args []string, opts ProcessOptions) (*Space, error) {
	mode, name, err := parseSpaceArgs(args)
	if err != nil {
		return nil, err
	}
	env := environ(opts.Env)
	storageMode, err := parseStorageMode(resolveString(&f.storage, env["SLIVINGDOC_STORAGE"], string(storageAuto)))
	if err != nil {
		return nil, fmt.Errorf("space: %w", err)
	}
	file, err := credentialsFile(env)
	if err != nil {
		return nil, err
	}
	stored, err := file.Load()
	if err != nil {
		return nil, fmt.Errorf("space: %w", err)
	}
	explicit, err := normalizeEndpoint(resolveString(&f.endpoint, env["SLIVINGDOC_ENDPOINT"], ""))
	if err != nil {
		return nil, fmt.Errorf("space: endpoint: %w", err)
	}
	s3Settings, err := resolveSpaceS3Settings(f, env)
	if err != nil {
		return nil, fmt.Errorf("space: %w", err)
	}
	if mode == spaceCreateMode {
		if err := storage.ValidateNewSpaceName(name); err != nil {
			return nil, fmt.Errorf("space new: %w", err)
		}
	}
	s := &Space{
		opts: opts, file: file, stored: stored, logins: stored.Logins(),
		explicit: explicit, mode: mode, name: name, s3: s3Settings,
		storageSet: f.storage.set, endpointSet: f.endpoint.set,
		regionSet: f.region.set, profileSet: f.profile.set,
		pathStyleSet: f.pathStyle.set, prefixSet: f.prefix.set,
	}
	if mode != spaceCreateMode {
		s.set, err = spaceSettings(opts)
		if err != nil {
			return nil, fmt.Errorf("space: %w", unusableSettings(err))
		}
	}
	if err := s.resolveBackends(f, env, storageMode); err != nil {
		return nil, err
	}
	return s, nil
}

func resolveSpaceS3Settings(f *SpaceFlags, env map[string]string) (spaceS3Settings, error) {
	pathStyle, err := resolveBool(&f.pathStyle, env["SLIVINGDOC_PATH_STYLE"], false)
	if err != nil {
		return spaceS3Settings{}, fmt.Errorf("path style: %w", err)
	}
	return spaceS3Settings{
		region:      resolveString(&f.region, env["AWS_REGION"], env["AWS_DEFAULT_REGION"]),
		profile:     resolveString(&f.profile, env["AWS_PROFILE"], ""),
		pathStyle:   pathStyle,
		prefix:      resolveString(&f.prefix, env["SLIVINGDOC_PREFIX"], ""),
		awsEndpoint: firstNonEmpty(env["AWS_ENDPOINT_URL_S3"], env["AWS_ENDPOINT_URL"]),
	}, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// spaceSettings reads the scoped settings of every directory. A process
// with an injected reader uses it; otherwise the file is located and read
// once, and an unreadable file is a refusal because the directory column
// cannot be trusted.
func spaceSettings(opts ProcessOptions) (settings.Set, error) {
	if opts.Load != nil {
		return opts.Load()
	}
	return scopedSettings(opts.Env)()
}

// resolveBackends applies the backend selection table. A hosted backend
// refuses the S3-only flags. In auto, an S3 group appears when the flags,
// the environment, or a record or default names an S3 store, so a
// hosted-only machine never loads AWS configuration. A create needs exactly
// one backend.
func (s *Space) resolveBackends(f *SpaceFlags, env map[string]string, mode storageMode) error {
	s.hostedIncluded = mode != storageS3
	inputs := storageInputs{goos: runtime.GOOS, now: time.Now()}
	if mode == storageHosted {
		if name, ok := hostedS3Flag(f); ok {
			return fmt.Errorf("space: %s names an S3 store, and the backend is hosted; remove it or pass --storage s3", name)
		}
	}
	logins, endpointLogin, err := s.chooseLogins(mode)
	if err != nil {
		return err
	}
	if endpointLogin && mode == storageAuto {
		if name, ok := hostedS3Flag(f); ok {
			return fmt.Errorf("space: %s names an S3 store, and --endpoint selects hosted storage; remove it or use an S3 endpoint", name)
		}
	}
	if s.mode == spaceCreateMode {
		return s.resolveCreate(f, env, mode, logins, endpointLogin)
	}
	useS3 := s.s3Selected(f, env, mode, inputs, endpointLogin)
	if len(logins) == 0 && !useS3 {
		return noBackendRefusal("space")
	}
	if useS3 {
		if err := storage.ValidatePrefix(s.s3.prefix); err != nil {
			return fmt.Errorf("space: %w", err)
		}
	}
	if mode == storageHosted && !slices.ContainsFunc(logins, func(l credentials.Login) bool { return l.Usable(inputs.now) == nil }) {
		return errors.New("space: the stored login expired; run 'slivingdoc login' again")
	}
	for _, login := range logins {
		backend, err := s.hosted(login)
		if err != nil {
			return err
		}
		s.backends = append(s.backends, backend)
	}
	if useS3 {
		s.useS3 = true
		identityFlag := s.s3IdentityFlagSelected(f, endpointLogin)
		// The active default supplies the identity only where no flag or
		// variable named one: an endpoint variable is a naming too.
		useDefault := !identityFlag && !s.s3EnvSelected(env) && !s.s3EndpointSelected(endpointLogin)
		s.s3Current = s.s3IdentityOf(endpointLogin, mode == storageS3 || identityFlag, useDefault)
		s.s3Recorded = s.recordedS3Keys()
	}
	return nil
}

const readOnlyCreate = "space new: this login is read-only and cannot create a space; run 'slivingdoc login' and approve write access"

// command is the refusal prefix of the running mode.
func (s *Space) command() string {
	if s.mode == spaceCreateMode {
		return "space new"
	}
	return "space"
}

func noBackendRefusal(command string) error {
	return fmt.Errorf("%s: no backend is reachable; run 'slivingdoc login' for hosted storage, or set an AWS profile or endpoint for S3", command)
}

// chooseLogins selects the stored logins the command uses. --endpoint
// selects exactly the login stored for it; without one, every stored login
// is chosen, and a create insists on exactly one. An endpoint no stored
// login reaches names an S3 store, which only --storage hosted refuses.
func (s *Space) chooseLogins(mode storageMode) (logins []credentials.Login, endpointLogin bool, err error) {
	if mode == storageS3 {
		return nil, false, nil
	}
	if s.explicit != "" {
		login, err := s.stored.ForEndpoint(s.explicit)
		switch {
		case err == nil:
			return []credentials.Login{login}, true, nil
		case !errors.Is(err, credentials.ErrNoLogin):
			return nil, false, fmt.Errorf("space: %w", err)
		case mode == storageHosted:
			return nil, false, s.hostedRefusal()
		}
	}
	if mode == storageHosted && len(s.logins) == 0 {
		return nil, false, s.hostedRefusal()
	}
	return s.logins, false, nil
}

func (s *Space) hostedRefusal() error {
	if s.explicit != "" {
		return fmt.Errorf("%s: --storage hosted needs a stored login for %s; run 'slivingdoc login'", s.command(), s.explicit)
	}
	return fmt.Errorf("%s: --storage hosted needs a stored login; run 'slivingdoc login'", s.command())
}

// s3Selected says whether the listing includes an S3 group. In auto the
// group appears when the endpoint names an S3 store, when the AWS
// environment or an S3 flag configures one, or when a record or default
// names an S3 target.
func (s *Space) s3Selected(f *SpaceFlags, env map[string]string, mode storageMode, in storageInputs, endpointLogin bool) bool {
	switch mode {
	case storageHosted:
		return false
	case storageS3:
		return true
	}
	return s.s3EndpointSelected(endpointLogin) || s.s3EnvSelected(env) || s.s3FlagSelected(f, endpointLogin) ||
		len(s3Signals(f, env, in)) > 0 || s.hasS3Record()
}

// s3CreateSelected says whether a create addresses S3. In auto the flags or
// the named AWS environment select it; a shared AWS configuration file
// alone does not, so a hosted login keeps creating hosted spaces.
func (s *Space) s3CreateSelected(f *SpaceFlags, env map[string]string, mode storageMode, endpointLogin bool) bool {
	switch mode {
	case storageHosted:
		return false
	case storageS3:
		return true
	}
	return s.s3EndpointSelected(endpointLogin) || s.s3EnvSelected(env) || s.s3FlagSelected(f, endpointLogin)
}

func (s *Space) s3EndpointSelected(endpointLogin bool) bool {
	return s.explicit != "" && !endpointLogin
}

// s3EnvSelected reports an AWS variable that names an S3 store. The shared
// AWS files are a listing signal but not a create selection, and
// SLIVINGDOC_PREFIX is not a selector: it names a notebook inside a store
// that another setting chooses.
func (s *Space) s3EnvSelected(env map[string]string) bool {
	for _, name := range []string{"AWS_PROFILE", "AWS_REGION", "AWS_DEFAULT_REGION", "SLIVINGDOC_PATH_STYLE"} {
		if env[name] != "" {
			return true
		}
	}
	return s.s3.awsEndpoint != ""
}

func (s *Space) s3FlagSelected(f *SpaceFlags, endpointLogin bool) bool {
	return f.profile.set || f.region.set || f.pathStyle.set || f.prefix.set || (f.endpoint.set && !endpointLogin)
}

// s3IdentityFlagSelected reports whether a flag names the S3 identity
// itself, so a failure is the operator's typo and refuses the command. An
// environment variable is ambient: auto degrades it and keeps other groups.
// --prefix names a notebook, not the identity, so it does not count.
func (s *Space) s3IdentityFlagSelected(f *SpaceFlags, endpointLogin bool) bool {
	return f.profile.set || f.region.set || f.pathStyle.set || (f.endpoint.set && !endpointLogin)
}

func (s *Space) hasS3Record() bool {
	_, ok := s.set.S3Default()
	return ok || slices.ContainsFunc(s.set.Entries(), func(e settings.Entry) bool { return e.Target.Kind == settings.KindS3 })
}

// resolveCreate chooses the one identity a create addresses. An S3 create
// always addresses the current chain, never a record or a default.
func (s *Space) resolveCreate(f *SpaceFlags, env map[string]string, mode storageMode, logins []credentials.Login, endpointLogin bool) error {
	if s.prefixSet {
		return fmt.Errorf("space new: %s names the notebook inside an existing store, and a create has none; remove it and run the command again", s.prefixSource())
	}
	if s.s3CreateSelected(f, env, mode, endpointLogin) {
		if s.s3.prefix != "" {
			return fmt.Errorf("space new: %s names the notebook inside an existing store, and a create has none; remove it and run the command again", s.prefixSource())
		}
		s.useS3 = true
		s.useS3Create = true
		s.s3Current = s.s3IdentityOf(endpointLogin, true, false)
		return nil
	}
	if len(logins) == 0 {
		return noBackendRefusal("space new")
	}
	if len(logins) > 1 {
		return errors.New("space new: several stored logins match; pass --endpoint to choose one")
	}
	login := logins[0]
	if err := login.Usable(time.Now()); err != nil {
		return fmt.Errorf("space new: %w; run 'slivingdoc login' again", err)
	}
	if login.Access == credentials.AccessRead {
		return errors.New(readOnlyCreate)
	}
	backend, err := s.hosted(login)
	if err != nil {
		return err
	}
	s.backends = []spaceBackend{backend}
	return nil
}

func (s *Space) prefixSource() string {
	if s.prefixSet {
		return "--prefix"
	}
	return "SLIVINGDOC_PREFIX"
}

// s3Identity is the description of one S3 identity: the configuration to
// resolve and whether the flags selected it. A failure of an explicit
// identity refuses the command; a recorded one degrades its group.
type s3Identity struct {
	cfg      s3store.Config
	opts     s3store.Options
	explicit bool
}

func (s *Space) s3IdentityOf(endpointLogin, explicit, useDefault bool) s3Identity {
	cfg := s3store.Config{
		Region:   s.s3.region,
		Profile:  s.s3.profile,
		Endpoint: s.s3Endpoint(endpointLogin),
	}
	opts := s3store.Options{ForcePathStyle: s.s3.pathStyle}
	if useDefault {
		if def, ok := s.set.S3Default(); ok {
			cfg = s3store.Config{Region: regionDefault(def.Region), Profile: def.Profile, Endpoint: def.Endpoint}
			opts = s3store.Options{ForcePathStyle: def.PathStyle}
		}
	}
	return s3Identity{cfg: cfg, opts: opts, explicit: explicit}
}

func (s *Space) s3Endpoint(endpointLogin bool) string {
	if s.explicit != "" && !endpointLogin {
		return s.explicit
	}
	return ""
}

// recordedS3Keys are the distinct S3 store keys of the records and
// defaults, in settings-file order.
func (s *Space) recordedS3Keys() []settings.StoreKey {
	var keys []settings.StoreKey
	add := func(t settings.Target) {
		if t.Kind != settings.KindS3 {
			return
		}
		if key := t.Key(); !slices.Contains(keys, key) {
			keys = append(keys, key)
		}
	}
	for _, entry := range s.set.Entries() {
		add(entry.Target)
	}
	for _, target := range s.set.Defaults() {
		add(target)
	}
	return keys
}

// prepare resolves the S3 identities in Run, where a context exists. A
// hosted-only command never loads AWS configuration. The current chain
// comes first, then the recorded store keys that differ from it.
func (s *Space) prepare(ctx context.Context) error {
	if s.prepared || !s.useS3 {
		return nil
	}
	s.prepared = true
	current, err := s.newS3Backend(ctx, s.s3Current)
	if err != nil {
		return err
	}
	current.current = true
	s.backends = append(s.backends, current)
	if s.useS3Create {
		return nil
	}
	seen := map[settings.StoreKey]bool{current.key: true}
	for _, key := range s.s3Recorded {
		if seen[key] {
			continue
		}
		backend, err := s.newS3Backend(ctx, s3Identity{cfg: s3store.Config{
			Region: regionDefault(key.Region), Profile: key.Profile, Endpoint: key.Endpoint,
		}})
		if err != nil {
			return err
		}
		if seen[backend.key] {
			continue
		}
		s.backends = append(s.backends, backend)
		seen[backend.key] = true
	}
	return nil
}

// newS3Backend resolves one identity. An explicit identity's failure is the
// command's refusal; a recorded identity's failure leaves a degraded group
// so its records stay visible.
func (s *Space) newS3Backend(ctx context.Context, id s3Identity) (*s3Space, error) {
	backend := &s3Space{s: s, id: id}
	backend.header = s3HeaderOf(id.cfg)
	backend.key = backend.header.storeKey()
	factory := s.opts.newS3Admin
	if factory == nil {
		factory = realS3Admin
	}
	admin, resolved, err := factory(ctx, id.cfg, id.opts)
	if err != nil {
		if id.explicit {
			return nil, s.s3IdentityRefusal(id, err)
		}
		backend.buildErr = err
		return backend, nil
	}
	backend.admin = admin
	backend.resolved = normalizeS3Resolved(resolved)
	backend.header = SpaceHeader{
		Kind: settings.KindS3, Endpoint: backend.resolved.Endpoint,
		Region: backend.resolved.Region, Profile: backend.resolved.Profile,
	}
	backend.key = backend.header.storeKey()
	return backend, nil
}

func normalizeS3Resolved(resolved s3store.Resolved) s3store.Resolved {
	if resolved.Region == "" {
		resolved.Region = s3store.DefaultRegion
	}
	return resolved
}

func s3HeaderOf(cfg s3store.Config) SpaceHeader {
	region := cfg.Region
	if region == "" {
		region = s3store.DefaultRegion
	}
	return SpaceHeader{Kind: settings.KindS3, Endpoint: cfg.Endpoint, Region: region, Profile: cfg.Profile}
}

// storeKey is the identity of the S3 group a header names.
func (h SpaceHeader) storeKey() settings.StoreKey {
	return settings.StoreKey{Kind: h.Kind, Endpoint: h.Endpoint, Region: h.Region, Profile: h.Profile}
}

func (s *Space) s3IdentityRefusal(id s3Identity, err error) error {
	if errors.Is(err, s3store.ErrProfileNotFound) {
		if id.cfg.Profile != "" {
			return fmt.Errorf("%s: no AWS profile named %s; check ~/.aws/config", s.command(), id.cfg.Profile)
		}
		return fmt.Errorf("%s: no AWS profile is selected; check ~/.aws/config", s.command())
	}
	return fmt.Errorf("%s: cannot use the S3 identity endpoint=%s, region=%s, profile=%s: %s; check the profile, region, and endpoint",
		s.command(), endpointDisplay(id.cfg.Endpoint), regionDefault(id.cfg.Region), profileDisplay(id.cfg.Profile), mcp.Redact(err.Error()))
}

func endpointDisplay(endpoint string) string {
	if endpoint == "" {
		return "(AWS)"
	}
	return endpoint
}

func profileDisplay(profile string) string {
	if profile == "" {
		return "(ambient)"
	}
	return profile
}

func regionDefault(region string) string {
	if region == "" {
		return s3store.DefaultRegion
	}
	return region
}

// hostedS3Flag names the first setting that selects an S3 store beside a
// hosted backend. Only the flags count: an explicit hosted backend ignores
// the ambient AWS and S3 environment, which names no store of its own.
func hostedS3Flag(f *SpaceFlags) (string, bool) {
	for _, row := range []struct {
		name string
		set  bool
	}{
		{"--profile", f.profile.set},
		{"--region", f.region.set},
		{"--path-style", f.pathStyle.set},
		{"--prefix", f.prefix.set},
	} {
		if row.set {
			return row.name, true
		}
	}
	return "", false
}

// hosted builds the backend of one stored login.
func (s *Space) hosted(login credentials.Login) (spaceBackend, error) {
	client, err := siteClient(login.Site, s.opts)
	if err != nil {
		return nil, fmt.Errorf("space: %w", err)
	}
	prior, err := s.stored.DefaultSpace(login.Endpoint)
	if err != nil && !errors.Is(err, credentials.ErrNoDefault) {
		return nil, fmt.Errorf("space: %w", err)
	}
	return &hostedSpace{s: s, login: login, prior: prior, client: client}, nil
}

// Run lists, creates, or sets the default, according to the positional
// words.
func (s *Space) Run(ctx context.Context) error {
	if err := s.prepare(ctx); err != nil {
		return err
	}
	switch s.mode {
	case spaceCreateMode:
		return s.create(ctx)
	case spaceSetMode:
		return s.setDefault(ctx, s.name)
	default:
		return s.list(ctx)
	}
}

// create makes one target on the chosen backend and prints it. It never
// changes the default.
func (s *Space) create(ctx context.Context) error {
	backend := s.backends[0]
	row, err := backend.Create(ctx, s.name)
	if err != nil {
		return err
	}
	s.printCreated(backend.Header(), row)
	fmt.Fprintf(s.opts.Out(), "It is not the default; run 'slivingdoc space%s %s' to make it the default.\n", s.createHint(), row.Name)
	return nil
}

// printCreated writes the one create line the non-interactive path and the
// picker share.
func (s *Space) printCreated(header SpaceHeader, row SpaceRow) {
	out, so := s.opts.Out(), s.opts.outStyle()
	if header.Kind == settings.KindS3 {
		detail := fmt.Sprintf("endpoint=%s, region=%s, profile=%s", endpointDisplay(header.Endpoint), header.Region, profileDisplay(header.Profile))
		if so.Mode() == tui.Styled {
			io.WriteString(out, line(so, tui.Done, "Created bucket "+so.Bold(row.Name), detail))
			return
		}
		fmt.Fprintf(out, "Created bucket %s (%s).\n", row.Name, detail)
		return
	}
	if so.Mode() == tui.Styled {
		io.WriteString(out, line(so, tui.Done, "Created hosted space "+so.Bold(row.Name),
			"("+spaceAccess(row)+", "+spaceOwner(row)+") at "+header.Endpoint))
		return
	}
	fmt.Fprintf(out, "Created hosted space %s (%s, %s) at %s.\n", row.Name, spaceAccess(row), spaceOwner(row), header.Endpoint)
}

// createHint repeats the flags that selected the identity, so the printed
// command selects the same one again.
func (s *Space) createHint() string {
	parts := s.createHintParts()
	if len(parts) == 0 {
		return ""
	}
	return " " + strings.Join(parts, " ")
}

func (s *Space) createHintParts() []string {
	var parts []string
	if s.useS3Create {
		if s.storageSet {
			parts = append(parts, "--storage", "s3")
		}
		if s.endpointSet {
			parts = append(parts, "--endpoint", s.explicit)
		}
		if s.regionSet {
			parts = append(parts, "--region", s.s3.region)
		}
		if s.profileSet {
			parts = append(parts, "--profile", s.s3.profile)
		}
		if s.pathStyleSet {
			parts = append(parts, "--path-style")
		}
		return parts
	}
	if s.storageSet {
		parts = append(parts, "--storage", "hosted")
	}
	if s.endpointSet {
		parts = append(parts, "--endpoint", s.explicit)
	}
	return parts
}

// list renders every backend's group, the default marked, and hints for a
// degraded or unreachable identity. On a terminal it offers the same rows
// in the picker, with a create row after each backend.
func (s *Space) list(ctx context.Context) error {
	groups, err := s.listing(ctx)
	if err != nil {
		return err
	}
	if s.opts.pickable() == OnTerminal {
		s.hints(groups)
		if err := s.pick(ctx, groups); err != nil {
			if skipped(err) {
				io.WriteString(s.opts.ErrOut(), line(s.opts.errStyle(), tui.Next, "Nothing was changed", ""))
				return nil
			}
			return err
		}
		return nil
	}
	s.render(groups)
	s.hints(groups)
	return nil
}

// choices maps the listing to one row per target and one create row per
// backend, in the plain listing's order. An S3 create row always creates
// through the current identity, never a recorded one, as 'space new' does.
func (s *Space) choices(groups []spaceGroup) []spaceChoice {
	current := s.currentS3()
	var choices []spaceChoice
	for _, group := range groups {
		if group.backend == nil {
			continue
		}
		for _, row := range group.rows {
			choices = append(choices, spaceChoice{
				backend: group.backend, header: group.header, row: row, defaultName: group.defaultName,
			})
		}
		create := group.backend
		if group.header.Kind == settings.KindS3 && current != nil {
			create = current
		}
		choices = append(choices, spaceChoice{backend: create, header: create.Header(), create: true})
	}
	return choices
}

// currentS3 is the identity the flags and the environment selected, whose
// create row every S3 group offers.
func (s *Space) currentS3() *s3Space {
	for _, backend := range s.backends {
		if admin, ok := backend.(*s3Space); ok && admin.current {
			return admin
		}
	}
	return nil
}

// pick asks which row becomes the default. A target row stores that
// backend's default; a create row prompts for a name, creates it, and
// stores the result as the default.
func (s *Space) pick(ctx context.Context, groups []spaceGroup) error {
	choices := s.choices(groups)
	if len(choices) == 0 {
		return nil
	}
	picked, err := s.opts.errStyle().Pick(s.opts.spacePicker(choices))
	if err != nil {
		return fmt.Errorf("space: %w", err)
	}
	choice := choices[picked[0]]
	if !choice.create {
		if !choice.row.Reachable {
			return s.unreachableRefusal(choice.row.Name, groups)
		}
		if err := choice.backend.SetDefault(ctx, choice.row.Name); err != nil {
			return err
		}
		s.printDefault(choice.header, choice.row)
		return nil
	}
	name, answered, err := s.promptName(ctx, choice.header.Kind)
	if err != nil {
		return fmt.Errorf("space: %w", err)
	}
	if !answered {
		return tui.ErrSkipped
	}
	if err := storage.ValidateNewSpaceName(name); err != nil {
		return fmt.Errorf("space new: %w", err)
	}
	row, err := choice.backend.Create(ctx, name)
	if err != nil {
		return err
	}
	s.printCreated(choice.header, row)
	s.refresh(ctx, choice.backend, &row)
	if err := choice.backend.SetDefault(ctx, row.Name); err != nil {
		return err
	}
	s.printDefault(choice.header, row)
	return nil
}

// promptName reads the new target's name. Only this read runs under
// interruptible, so the first termination signal is a skip instead of an
// exit; the picker table keeps the process's default signal behavior.
func (s *Space) promptName(ctx context.Context, kind settings.Kind) (string, bool, error) {
	fmt.Fprint(s.opts.ErrOut(), namePrompt(kind))
	ctx, stop := s.opts.interruptible(ctx)
	defer stop()
	line, answered, err := readLine(ctx, s.opts.stdin())
	if !answered {
		fmt.Fprintln(s.opts.ErrOut())
		return "", false, nil
	}
	if errors.Is(err, io.EOF) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read the name: %w; nothing was created", err)
	}
	name := strings.TrimSpace(line)
	if name == "" {
		return "", false, nil
	}
	return name, true, nil
}

// refresh replaces the created row with the backend's fresh answer when
// it can. The table is decoration after a create: its failure never turns
// a created target into a reported failure.
func (s *Space) refresh(ctx context.Context, backend spaceBackend, row *SpaceRow) {
	rows, err := backend.List(ctx)
	if err != nil {
		return
	}
	if fresh, ok := rowNamed(rows, row.Name); ok {
		*row = fresh
	}
}

// setDefault makes one listed target the default, or refuses when the name
// is absent, listed twice, or the listing was incomplete.
func (s *Space) setDefault(ctx context.Context, name string) error {
	groups, err := s.listing(ctx)
	if err != nil {
		return err
	}
	return s.setDefaultFrom(ctx, groups, name)
}

// setDefaultFrom applies the default selection to an already collected
// listing.
func (s *Space) setDefaultFrom(ctx context.Context, groups []spaceGroup, name string) error {
	var candidates []spaceCandidate
	for _, group := range groups {
		for _, row := range group.rows {
			if row.Name == name && row.Reachable {
				candidates = append(candidates, spaceCandidate{backend: group.backend, header: group.header, row: row})
			}
		}
	}
	switch {
	case len(candidates) == 1:
		c := candidates[0]
		if err := c.backend.SetDefault(ctx, name); err != nil {
			return err
		}
		s.printDefault(c.header, c.row)
		return nil
	case len(candidates) > 1:
		return ambiguousRefusal(name, candidates)
	}
	return s.unreachableRefusal(name, groups)
}

// unreachableRefusal names why a target that is visible cannot become the
// default: it is a record whose identity did not list it. An incomplete
// listing is reported as such instead of claiming the target is absent.
func (s *Space) unreachableRefusal(name string, groups []spaceGroup) error {
	if s.listingFailed {
		return fmt.Errorf("space: no reachable backend listed %q, and a listing failed, so the listing was incomplete; run the command again", name)
	}
	return fmt.Errorf("space: no backend lists %q (the listing has %s); nothing was changed", name, listedNames(groups))
}

func ambiguousRefusal(name string, candidates []spaceCandidate) error {
	listed := make([]string, 0, len(candidates))
	anyS3 := false
	var firstS3 *SpaceHeader
	var endpointDiffers, regionDiffers, profileDiffers bool
	for _, c := range candidates {
		if c.header.Kind == settings.KindS3 {
			anyS3 = true
			listed = append(listed, fmt.Sprintf("endpoint=%s region=%s profile=%s", endpointDisplay(c.header.Endpoint), regionDefault(c.header.Region), profileDisplay(c.header.Profile)))
			if firstS3 == nil {
				header := c.header
				firstS3 = &header
			} else {
				endpointDiffers = endpointDiffers || c.header.Endpoint != firstS3.Endpoint
				regionDiffers = regionDiffers || regionDefault(c.header.Region) != regionDefault(firstS3.Region)
				profileDiffers = profileDiffers || c.header.Profile != firstS3.Profile
			}
			continue
		}
		listed = append(listed, c.header.Endpoint)
	}
	noun, selector := "logins", "--endpoint"
	if anyS3 {
		noun = "identities"
		selectors := make([]string, 0, 3)
		if profileDiffers {
			selectors = append(selectors, "--profile")
		}
		if regionDiffers {
			selectors = append(selectors, "--region")
		}
		if endpointDiffers {
			selectors = append(selectors, "--endpoint")
		}
		if len(selectors) == 0 {
			selectors = append(selectors, "--profile", "--region", "--endpoint")
		}
		selector = strings.Join(selectors, " or ")
	}
	return fmt.Errorf("space: %s is listed by %d %s (%s); pass %s to choose one", name, len(candidates), noun, strings.Join(listed, ", "), selector)
}

// printDefault writes the backend's own default line, which names a space or
// a bucket.
func (s *Space) printDefault(header SpaceHeader, row SpaceRow) {
	so := s.opts.outStyle()
	if header.Kind == settings.KindS3 {
		detail := fmt.Sprintf("endpoint=%s, region=%s, profile=%s", endpointDisplay(header.Endpoint), header.Region, profileDisplay(header.Profile))
		if so.Mode() == tui.Styled {
			io.WriteString(s.opts.Out(), line(so, tui.Done, "Default bucket "+so.Bold(row.Name), detail))
			return
		}
		fmt.Fprintf(s.opts.Out(), "The default bucket for s3 %s is now %s.\n", detail, row.Name)
		return
	}
	if so.Mode() == tui.Styled {
		io.WriteString(s.opts.Out(), line(so, tui.Done, "Default space "+so.Bold(row.Name), "("+spaceAccess(row)+", "+spaceOwner(row)+")"))
		return
	}
	fmt.Fprintf(s.opts.Out(), "The default space is now %s (%s, %s).\n", row.Name, spaceAccess(row), spaceOwner(row))
}

// listing collects every backend's rows with their hints. A backend that
// cannot list keeps its default and remembered rows, marked unreachable,
// and its hint, without stopping another backend. An error that is not a
// spaceListError is the command's refusal, such as an explicit identity
// that cannot be resolved.
func (s *Space) listing(ctx context.Context) ([]spaceGroup, error) {
	groups := make([]spaceGroup, 0, len(s.backends))
	for _, backend := range s.backends {
		rows, err := backend.List(ctx)
		def, _ := backend.Default()
		group := spaceGroup{
			backend: backend, header: backend.Header(), rows: rows,
			defaultName: def.Name, hasDefault: backend.HasDefault(),
		}
		if err != nil {
			var hint *spaceListError
			if !errors.As(err, &hint) {
				return nil, err
			}
			group.hints = hint.hints
			s.listingFailed = s.listingFailed || hint.failed
		}
		if len(rows) > 0 && !group.hasDefault {
			group.noDefault = true
		}
		groups = append(groups, group)
	}
	if orphan := s.orphanGroup(); orphan != nil {
		hosted := 0
		for _, group := range groups {
			if group.header.Kind == settings.KindHosted {
				hosted++
			}
		}
		groups = slices.Insert(groups, hosted, *orphan)
	}
	return groups, nil
}

// orphanGroup shows hosted records that no stored login reaches, in one
// synthetic group, so a binding never disappears. It is nil when every
// record is reached.
func (s *Space) orphanGroup() *spaceGroup {
	if !s.hostedIncluded {
		return nil
	}
	orphans := make(map[string][]string)
	var hints []string
	for _, entry := range s.set.Entries() {
		target := entry.Target
		if target.Kind != settings.KindHosted || s.reached(target) {
			continue
		}
		if _, seen := orphans[target.Name]; !seen {
			hints = append(hints, fmt.Sprintf("this directory remembers hosted space %q, which no stored login reaches; run 'slivingdoc login'", target.Name))
		}
		orphans[target.Name] = append(orphans[target.Name], entry.Path)
	}
	if len(orphans) == 0 {
		return nil
	}
	rows := make([]SpaceRow, 0, len(orphans))
	for name, dirs := range orphans {
		rows = append(rows, SpaceRow{Name: name, Directories: dirs})
	}
	slices.SortFunc(rows, func(a, b SpaceRow) int { return strings.Compare(a.Name, b.Name) })
	slices.Sort(hints)
	return &spaceGroup{
		header: SpaceHeader{Kind: settings.KindHosted, Endpoint: "(no stored login)", Account: "—"},
		rows:   rows, hints: hints,
	}
}

func (s *Space) reached(target settings.Target) bool {
	return slices.ContainsFunc(s.logins, func(l credentials.Login) bool {
		return target.Endpoint == "" || target.Endpoint == l.Endpoint
	})
}

// spaceGroup is one backend's listing and the hints that explain its
// degraded or unreachable rows.
type spaceGroup struct {
	backend     spaceBackend
	header      SpaceHeader
	rows        []SpaceRow
	defaultName string
	hasDefault  bool
	hints       []string
	noDefault   bool
}

type spaceCandidate struct {
	backend spaceBackend
	header  SpaceHeader
	row     SpaceRow
}

// spaceListError carries the sanitized hints of a listing. A failed
// listing sets failed, so a selection knows the listing was incomplete; a
// successful listing may still carry hints about targets it did not return.
type spaceListError struct {
	failed bool
	hints  []string
}

func (e *spaceListError) Error() string { return strings.Join(e.hints, "; ") }

// s3Space is the S3 backend of one resolved identity. It lists buckets
// with ListBuckets only, and it never probes a bucket or reads its
// objects: the space command administers stores, it does not open them.
type s3Space struct {
	s        *Space
	id       s3Identity
	admin    s3Admin
	resolved s3store.Resolved
	header   SpaceHeader
	key      settings.StoreKey
	// current marks the identity the flags and the environment selected;
	// its create row is the only S3 create the picker offers, because
	// 'space new' never creates in a recorded identity.
	current bool
	// buildErr is a recorded identity's resolution failure. Its group
	// stays visible as degraded rows instead of refusing the command.
	buildErr error
}

func (h *s3Space) Kind() settings.Kind { return settings.KindS3 }

func (h *s3Space) Header() SpaceHeader { return h.header }

func (h *s3Space) Default() (settings.Target, bool) {
	def, ok := h.s.set.S3Default()
	if !ok || def.Key() != h.key {
		return settings.Target{}, false
	}
	return def, true
}

func (h *s3Space) HasDefault() bool { return len(h.s.set.Defaults()) > 0 }

// List reads the buckets of one identity and adds the configured default
// and the recorded targets the answer did not return, marked unreachable
// so a binding stays visible.
func (h *s3Space) List(ctx context.Context) ([]SpaceRow, error) {
	if h.admin == nil {
		return h.degraded(h.buildHint())
	}
	identity, err := h.admin.Identity(ctx)
	if err != nil {
		if h.id.explicit {
			return nil, h.s.s3IdentityRefusal(h.id, err)
		}
		return h.degraded(h.identityHint(err))
	}
	h.header.KeyID = identity.AccessKeyID
	buckets, err := h.admin.ListBuckets(ctx)
	if err != nil {
		return h.degraded(h.listHint(err))
	}
	rows := make([]SpaceRow, 0, len(buckets))
	listed := make(map[string]bool, len(buckets))
	for _, bucket := range buckets {
		listed[bucket.Name] = true
		rows = append(rows, h.row(bucket.Name, true))
	}
	var hints []string
	for _, name := range h.unlisted(listed) {
		rows = append(rows, h.row(name, false))
		if name == h.defaultName() {
			hints = append(hints, fmt.Sprintf("the configured default bucket %q is not among the buckets this identity lists; run 'slivingdoc space <name>' to choose another", name))
			continue
		}
		hints = append(hints, fmt.Sprintf("this directory remembers bucket %q, which %s did not list", name, h.identityName()))
	}
	sortRows(rows)
	if len(hints) > 0 {
		return rows, &spaceListError{hints: hints}
	}
	return rows, nil
}

// Create makes one bucket on the identity the flags selected. It returns
// the one line an operator can act on for each refusal.
func (h *s3Space) Create(ctx context.Context, name string) (SpaceRow, error) {
	if h.admin == nil {
		return SpaceRow{}, fmt.Errorf("space new: cannot use the S3 identity %s: %s; check the profile, region, and endpoint",
			h.identityName(), mcp.Redact(h.buildErr.Error()))
	}
	err := h.admin.CreateBucket(ctx, name)
	switch {
	case err == nil:
		return SpaceRow{Name: name, Reachable: true}, nil
	case errors.Is(err, s3store.ErrBucketOwned):
		return SpaceRow{}, fmt.Errorf("space new: bucket %s already exists and this account owns it; run 'slivingdoc space%s %s' to use it", name, h.s.createHint(), name)
	case errors.Is(err, s3store.ErrBucketExists):
		return SpaceRow{}, fmt.Errorf("space new: bucket %s already exists in another account; choose another name", name)
	case errors.Is(err, storage.ErrAccessDenied):
		return SpaceRow{}, errors.New("space new: this login may not create a bucket (s3:CreateBucket is denied); check the profile or endpoint")
	case errors.Is(err, storage.ErrTransport):
		return SpaceRow{}, fmt.Errorf("space new: could not create bucket %s; try again later", name)
	default:
		return SpaceRow{}, fmt.Errorf("space new: %s", mcp.Redact(err.Error()))
	}
}

// SetDefault stores the bucket and the resolved identity under the store
// key of the settings file. A dotted legacy bucket on the default AWS
// endpoint records path style, because that address form requires it.
func (h *s3Space) SetDefault(ctx context.Context, name string) error {
	target := settings.Target{
		Kind: settings.KindS3, Endpoint: h.header.Endpoint,
		Region: h.header.Region, Profile: h.header.Profile,
		Name: name, Prefix: h.s.s3.prefix, PathStyle: h.pathStyleFor(name),
	}
	return h.s.storeS3Default(ctx, target)
}

func (h *s3Space) pathStyleFor(name string) bool {
	return h.resolved.PathStyle || h.header.Endpoint != "" || strings.Contains(name, ".")
}

// storeS3Default writes the one S3 default under the settings lock, so
// another process's entry or selection survives the read-modify-write.
func (s *Space) storeS3Default(ctx context.Context, target settings.Target) error {
	return s.editDefaults(ctx, func(set settings.Set) (settings.Set, bool) {
		return set.PutDefault(target), true
	})
}

// clearS3Defaults writes a hosted selection's effect on the settings file:
// no stored default remains, so the login's default space is active.
func (s *Space) clearS3Defaults(ctx context.Context) error {
	return s.editDefaults(ctx, func(set settings.Set) (settings.Set, bool) {
		if _, ok := set.S3Default(); !ok {
			return set, false
		}
		return set.ClearDefaults(), true
	})
}

// editDefaults applies edit to the settings file under its lock. An edit
// that reports no write leaves the file untouched.
func (s *Space) editDefaults(ctx context.Context, edit func(settings.Set) (settings.Set, bool)) (errOut error) {
	file, err := settings.Locate(EnvLookup(s.opts.Env), runtime.GOOS)
	if err != nil {
		return fmt.Errorf("space: %w", err)
	}
	lock, err := file.Lock(ctx)
	if err != nil {
		return fmt.Errorf("space: %w", err)
	}
	defer func() {
		if err := lock.Unlock(); err != nil && errOut == nil {
			errOut = fmt.Errorf("space: %w", err)
		}
	}()
	set, err := file.Load()
	if err != nil {
		return fmt.Errorf("space: %w", err)
	}
	changed, write := edit(set)
	if !write {
		return nil
	}
	if err := file.Save(changed); err != nil {
		return fmt.Errorf("space: %w", err)
	}
	return nil
}

func (h *s3Space) defaultName() string {
	t, ok := h.Default()
	if !ok {
		return ""
	}
	return t.Name
}

// unlisted returns the default and the recorded names this identity's
// answer did not include, in settings-file order.
func (h *s3Space) unlisted(listed map[string]bool) []string {
	var names []string
	add := func(name string) {
		if name != "" && !listed[name] && !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	add(h.defaultName())
	for _, entry := range h.s.set.Entries() {
		if h.matches(entry.Target) {
			add(entry.Target.Name)
		}
	}
	return names
}

// degraded is a failed identity's answer: its default and recorded targets
// with the one hint that says why they could not be listed.
func (h *s3Space) degraded(hint string) ([]SpaceRow, error) {
	var rows []SpaceRow
	seen := make(map[string]bool)
	add := func(name string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		rows = append(rows, h.row(name, false))
	}
	add(h.defaultName())
	for _, entry := range h.s.set.Entries() {
		if h.matches(entry.Target) {
			add(entry.Target.Name)
		}
	}
	sortRows(rows)
	return rows, &spaceListError{failed: true, hints: []string{hint}}
}

func (h *s3Space) row(name string, reachable bool) SpaceRow {
	return SpaceRow{Name: name, Directories: h.directories(name), Reachable: reachable}
}

// directories are the directories whose record names the bucket and every
// recorded field this identity also has.
func (h *s3Space) directories(name string) []string {
	var dirs []string
	for _, entry := range h.s.set.Entries() {
		if entry.Target.Name == name && h.matches(entry.Target) {
			dirs = append(dirs, entry.Path)
		}
	}
	return dirs
}

// matches reports whether an S3 target belongs to this identity's group.
// Every recorded field is a value: an empty endpoint is the provider
// default and an empty profile is the ambient chain, so none is a wildcard.
func (h *s3Space) matches(t settings.Target) bool {
	if t.Kind != settings.KindS3 || t.Name == "" {
		return false
	}
	return t.Endpoint == h.header.Endpoint && regionDefault(t.Region) == regionDefault(h.header.Region) &&
		t.Profile == h.header.Profile
}

func (h *s3Space) identityName() string {
	return fmt.Sprintf("endpoint=%s, region=%s, profile=%s",
		endpointDisplay(h.header.Endpoint), regionDefault(h.header.Region), profileDisplay(h.header.Profile))
}

func (h *s3Space) buildHint() string {
	if errors.Is(h.buildErr, s3store.ErrProfileNotFound) && h.id.cfg.Profile != "" {
		return fmt.Sprintf("no AWS profile named %s; check ~/.aws/config; the configured default is shown", h.id.cfg.Profile)
	}
	return fmt.Sprintf("could not use the S3 identity %s: %s; the configured default is shown",
		h.identityName(), mcp.Redact(h.buildErr.Error()))
}

func (h *s3Space) identityHint(err error) string {
	if errors.Is(err, storage.ErrAccessDenied) {
		return fmt.Sprintf("this login may not use the S3 identity %s (its credentials are denied); the configured default is shown", h.identityName())
	}
	return fmt.Sprintf("could not read the S3 identity %s: %s; the configured default is shown", h.identityName(), mcp.Redact(err.Error()))
}

func (h *s3Space) listHint(err error) string {
	if errors.Is(err, storage.ErrAccessDenied) {
		return "this login may not list buckets (s3:ListAllMyBuckets is denied); the configured default is shown"
	}
	return fmt.Sprintf("could not list the buckets of %s: %s; the configured default is shown", h.identityName(), mcp.Redact(err.Error()))
}

// hostedSpace is the hosted backend of one stored login.
type hostedSpace struct {
	s      *Space
	login  credentials.Login
	prior  string
	client *sitelogin.Client
}

func (h *hostedSpace) Kind() settings.Kind { return settings.KindHosted }

func (h *hostedSpace) Header() SpaceHeader {
	return SpaceHeader{Kind: settings.KindHosted, Endpoint: h.login.Endpoint, Account: h.login.Account}
}

func (h *hostedSpace) Default() (settings.Target, bool) {
	if h.prior == "" {
		return settings.Target{}, false
	}
	if _, ok := h.s.set.S3Default(); ok {
		return settings.Target{}, false
	}
	return settings.Target{Kind: settings.KindHosted, Name: h.prior}, true
}

func (h *hostedSpace) HasDefault() bool { return h.prior != "" }

// List reads the spaces the login reaches and adds the default and the
// remembered targets the answer did not return, marked unreachable so they
// stay visible.
func (h *hostedSpace) List(ctx context.Context) ([]SpaceRow, error) {
	if err := h.login.Usable(time.Now()); err != nil {
		return h.degraded(fmt.Sprintf("the stored login for %s expired; run 'slivingdoc login' again", h.login.Endpoint))
	}
	spaces, err := h.client.Spaces(ctx, h.login.Key)
	if err != nil {
		return h.degraded(h.failureHint(err))
	}
	rows := make([]SpaceRow, 0, len(spaces))
	listed := make(map[string]bool, len(spaces))
	for _, sp := range spaces {
		listed[sp.Name] = true
		rows = append(rows, h.row(sp.Name, sp.Owner, sp.Access, true))
	}
	var hints []string
	for _, name := range h.unlisted(listed) {
		rows = append(rows, h.row(name, "", "", false))
		if name == h.prior {
			hints = append(hints, fmt.Sprintf("the default space %q is not among the spaces the login for %s reaches; run 'slivingdoc space <name>' to choose another", name, h.login.Endpoint))
			continue
		}
		hints = append(hints, fmt.Sprintf("this directory remembers hosted space %q, which the login for %s did not list", name, h.login.Endpoint))
	}
	sortRows(rows)
	if len(hints) > 0 {
		return rows, &spaceListError{hints: hints}
	}
	return rows, nil
}

// Create asks the site to create one space for the login's account.
func (h *hostedSpace) Create(ctx context.Context, name string) (SpaceRow, error) {
	sp, err := h.client.CreateSpace(ctx, h.login.Key, name)
	if err != nil {
		return SpaceRow{}, createRefusal(err, name)
	}
	return SpaceRow{Name: sp.Name, Owner: sp.Owner, Access: sp.Access, Reachable: true}, nil
}

// SetDefault stores the space as the login's default under the
// credentials lock, provided the login it was listed with is still the one
// stored, then clears the S3 default that would otherwise stay active.
func (h *hostedSpace) SetDefault(ctx context.Context, name string) error {
	if err := storeDefault(ctx, h.s.file, h.login, name); err != nil {
		return err
	}
	return h.s.clearS3Defaults(ctx)
}

// storeDefault stores space as the login's default under the credentials
// lock, provided the login it was listed with is still the one stored. The
// login flow and the space command share it, so both re-check the same
// way.
func storeDefault(ctx context.Context, file credentials.File, login credentials.Login, space string) (errOut error) {
	lock, err := file.Lock(ctx)
	if err != nil {
		return fmt.Errorf("space: %w", err)
	}
	defer func() {
		if err := lock.Unlock(); err != nil && errOut == nil {
			errOut = fmt.Errorf("space: %w", err)
		}
	}()
	set, err := file.Load()
	if err != nil {
		return fmt.Errorf("space: %w", err)
	}
	current, err := set.Lookup(login.ID)
	if err != nil || current.Key != login.Key {
		return errors.New("space: the stored login changed while the spaces were listed; run the command again")
	}
	if err := set.SetDefaultSpace(login.Endpoint, space); err != nil {
		return fmt.Errorf("space: %w", err)
	}
	if err := file.Save(set); err != nil {
		return fmt.Errorf("space: %w", err)
	}
	return nil
}

// degraded is the failed listing's answer: the default and every
// remembered target of this login, marked unreachable, plus one hint.
func (h *hostedSpace) degraded(hint string) ([]SpaceRow, error) {
	var rows []SpaceRow
	seen := make(map[string]bool)
	add := func(name string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		rows = append(rows, h.row(name, "", "", false))
	}
	add(h.prior)
	for _, entry := range h.s.set.Entries() {
		if h.matches(entry.Target) {
			add(entry.Target.Name)
		}
	}
	sortRows(rows)
	return rows, &spaceListError{failed: true, hints: []string{hint}}
}

// unlisted returns the names of the default and the remembered targets
// that a successful listing did not return, in settings-file order.
func (h *hostedSpace) unlisted(listed map[string]bool) []string {
	var names []string
	add := func(name string) {
		if name != "" && !listed[name] && !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	add(h.prior)
	for _, entry := range h.s.set.Entries() {
		if h.matches(entry.Target) {
			add(entry.Target.Name)
		}
	}
	return names
}

// matches reports whether a target belongs to this login's group: hosted,
// with the same name, and an endpoint that is empty or this login's.
func (h *hostedSpace) matches(t settings.Target) bool {
	return t.Kind == settings.KindHosted && t.Name != "" && (t.Endpoint == "" || t.Endpoint == h.login.Endpoint)
}

func (h *hostedSpace) row(name, owner string, access credentials.Access, reachable bool) SpaceRow {
	return SpaceRow{Name: name, Owner: owner, Access: access, Directories: h.directories(name), Reachable: reachable}
}

// directories are the directories of this login that remember the target,
// in settings-file order.
func (h *hostedSpace) directories(name string) []string {
	var dirs []string
	for _, entry := range h.s.set.Entries() {
		if entry.Target.Name == name && h.matches(entry.Target) {
			dirs = append(dirs, entry.Path)
		}
	}
	return dirs
}

// failureHint is the sanitized stderr hint of a listing the site refused
// or could not answer.
func (h *hostedSpace) failureHint(err error) string {
	var refusal *sitelogin.Refusal
	if errors.As(err, &refusal) && refusal.Status == http.StatusUnauthorized {
		return fmt.Sprintf("the site no longer accepts the stored login for %s (revoked or expired); run 'slivingdoc login' again", h.login.Endpoint)
	}
	return fmt.Sprintf("could not list the spaces of %s: %s; the default and remembered spaces are shown", h.login.Endpoint, mcp.Redact(err.Error()))
}

// createRefusal maps a site refusal to the one line an operator can act
// on.
func createRefusal(err error, name string) error {
	var refusal *sitelogin.Refusal
	if !errors.As(err, &refusal) {
		return fmt.Errorf("space new: %s", mcp.Redact(err.Error()))
	}
	switch {
	case refusal.Status == http.StatusUnauthorized:
		return errors.New("space new: the site no longer accepts the stored login (revoked or expired); run 'slivingdoc login' again")
	case refusal.Code == "access_denied":
		return errors.New(readOnlyCreate)
	case refusal.Code == "space_limit":
		return errors.New("space new: this account already owns its allowed number of spaces; remove one on the site or ask for a larger plan")
	case refusal.Code == "name_taken":
		return fmt.Errorf("space new: %s is already used by this account; run 'slivingdoc space %s' to use it", name, name)
	case refusal.Code == "busy":
		return errors.New("space new: the site is busy creating spaces; try again in a minute")
	case refusal.Code == "pool_empty":
		return errors.New("space new: the site has no space capacity right now; try again later")
	default:
		return fmt.Errorf("space new: the site refused: %s", mcp.Redact(refusal.Error()))
	}
}

func rowNamed(rows []SpaceRow, name string) (SpaceRow, bool) {
	for _, row := range rows {
		if row.Name == name {
			return row, true
		}
	}
	return SpaceRow{}, false
}

func listedNames(groups []spaceGroup) string {
	var names []string
	for _, group := range groups {
		for _, row := range group.rows {
			if row.Reachable {
				names = append(names, row.Name)
			}
		}
	}
	if len(names) == 0 {
		return "no space"
	}
	return strings.Join(names, ", ")
}

func sortRows(rows []SpaceRow) {
	slices.SortFunc(rows, func(a, b SpaceRow) int { return strings.Compare(a.Name, b.Name) })
}
