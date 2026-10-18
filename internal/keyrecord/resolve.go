package keyrecord

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/baalimago/slivingdoc/internal/credentials"
	"github.com/baalimago/slivingdoc/internal/crypt"
	"github.com/baalimago/slivingdoc/internal/pathutil"
	"github.com/baalimago/slivingdoc/internal/workspace"
)

// Resolve returns the candidate secrets for the notebook identity id, in the
// order a keyring must try them: the environment value, the value its envfile
// holds, then the value stored in the key record. Each candidate carries its
// source label; the resolver never reports a secret or a path.
//
// A missing key record file, and a machine with no configuration directory at
// all, are no candidate. A malformed, unsafe or unreadable key record, and a
// passphrase file that names no readable secret, are refusals. The application
// prepends the interactive candidate when the command collects one.
func Resolve(id workspace.Identity, getenv func(string) string) ([]crypt.Candidate, error) {
	var candidates []crypt.Candidate
	if secret := getenv(PassphraseEnv); secret != "" {
		candidates = append(candidates, crypt.Candidate{Source: crypt.SourceEnvironment, Secret: []byte(secret)})
	}
	if path := getenv(PassphraseFileEnv); path != "" {
		secret, err := readPassphraseFile(PassphraseFileEnv, path)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, crypt.Candidate{Source: crypt.SourceEnvironmentFile, Secret: []byte(secret)})
	}
	entry, err := lookupEntry(id, getenv)
	if err != nil {
		return nil, err
	}
	switch entry.Kind() {
	case KindPassphrase:
		candidates = append(candidates, crypt.Candidate{Source: crypt.SourceKeyRecord, Secret: []byte(entry.Value())})
	case KindPassphraseFile:
		secret, err := readPassphraseFile(fieldPassphraseFile, entry.Value())
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, crypt.Candidate{Source: crypt.SourceKeyRecord, Secret: []byte(secret)})
	}
	return candidates, nil
}

// lookupEntry returns the key record's entry for id, or the zero entry when
// the notebook stores none.
func lookupEntry(id workspace.Identity, getenv func(string) string) (Entry, error) {
	file, err := Locate(getenv, runtime.GOOS)
	if errors.Is(err, ErrNoConfigDir) {
		// A machine without a configuration directory keeps no key record,
		// and no record is not a failure.
		return Entry{}, nil
	}
	if err != nil {
		return Entry{}, err
	}
	set, err := file.Load()
	if err != nil {
		return Entry{}, err
	}
	entry, err := set.Lookup(workspace.IdentityDigest(id))
	if errors.Is(err, ErrNoMaterial) {
		return Entry{}, nil
	}
	return entry, err
}

// readPassphraseFile returns the passphrase the file at path sets, under the
// hardened private-file checks. label names the setting or record field the
// path came from, so a refusal names the setting and never the path.
func readPassphraseFile(label, path string) (string, error) {
	expanded, err := pathutil.ExpandHome(path)
	if err != nil {
		return "", fmt.Errorf("%w: %s: %w", ErrEnvfile, label, err)
	}
	pf := credentials.PrivateFile{
		Path:     expanded,
		Dir:      filepath.Dir(expanded),
		UID:      os.Geteuid(),
		Private:  credentials.ChecksOwners(runtime.GOOS),
		MaxBytes: credentials.MaxFileSize,
		Wording:  wording,
	}
	data, found, err := pf.Read()
	if err != nil {
		return "", envfileRefusal(label, err)
	}
	if !found {
		return "", fmt.Errorf("%w: %s names no file", ErrEnvfile, label)
	}
	secret, err := envfilePassphrase(data)
	if err != nil {
		return "", fmt.Errorf("%w: %s: %w", ErrEnvfile, label, err)
	}
	return secret, nil
}

// envfileRefusal words a private-file failure without its path: the raw error
// names the file, and no caller-facing text may carry a passphrase-file path.
func envfileRefusal(label string, err error) error {
	switch {
	case errors.Is(err, ErrExposed):
		return fmt.Errorf("%w: %s is not private", ErrEnvfile, label)
	case errors.Is(err, ErrTooLarge):
		return fmt.Errorf("%w: %s is too large", ErrEnvfile, label)
	default:
		return fmt.Errorf("%w: %s cannot be read", ErrEnvfile, label)
	}
}

// envfilePassphrase returns the passphrase an envfile sets for
// SLIVINGDOC_PASSPHRASE. It accepts KEY=VALUE lines, ignores blank lines and
// full-line # comments, strips an optional leading "export ", removes a
// matching quote pair, and expands a leading ~. It refuses every other shape,
// an empty value, and a file that sets no passphrase.
func envfilePassphrase(data []byte) (string, error) {
	for i, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if rest, ok := strings.CutPrefix(line, "export"); ok && (strings.HasPrefix(rest, " ") || strings.HasPrefix(rest, "\t")) {
			line = strings.TrimSpace(rest)
		}
		name, value, ok := strings.Cut(line, "=")
		if !ok {
			return "", fmt.Errorf("line %d is not a KEY=VALUE assignment", i+1)
		}
		if strings.TrimSpace(name) != PassphraseEnv {
			continue
		}
		secret, err := unquote(strings.TrimSpace(value))
		if err != nil {
			return "", err
		}
		if secret == "" {
			return "", errors.New("the passphrase is empty")
		}
		return pathutil.ExpandHome(secret)
	}
	return "", fmt.Errorf("%s sets no passphrase", PassphraseEnv)
}

// unquote removes one matching quote pair from a value, or returns an unquoted
// value unchanged. Quotes are not escapes: a quoted value must close with the
// same quote as its last byte.
func unquote(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if quote := value[0]; quote == '"' || quote == '\'' {
		if len(value) < 2 || value[len(value)-1] != quote {
			return "", fmt.Errorf("the quoted value does not close with %c", quote)
		}
		return value[1 : len(value)-1], nil
	}
	return value, nil
}
