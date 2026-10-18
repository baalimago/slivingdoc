package keyrecord

import (
	"errors"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/baalimago/slivingdoc/internal/credentials"
	"github.com/baalimago/slivingdoc/internal/settings"
)

// TestWorkspaceSettingsRejectCredentialFields proves the separation this
// package rests on: the remembered notebook record refuses a credential-shaped
// field, and the key record is a different file in the same directory.
func TestWorkspaceSettingsRejectCredentialFields(t *testing.T) {
	for _, data := range []string{
		`{"version":1,"entries":[{"path":"/n","target":{"space":"notes","prefix":"","passphrase":"s3cret"}}]}`,
		`{"version":1,"entries":[{"path":"/n","target":{"space":"notes","prefix":"","passphraseFile":"/keys/n"}}]}`,
		`{"version":1,"entries":[{"path":"/n","target":{"space":"notes","prefix":"","key":"sld_secret"}}]}`,
		`{"version":1,"entries":[{"path":"/n","space":"notes","token":"sld_secret"}]}`,
	} {
		dir := testDir(t)
		writeFile(t, filepath.Join(dir, settings.FileName), data, 0o600)
		file, err := settings.Locate(lookup(map[string]string{credentials.DirEnv: dir}), runtime.GOOS)
		if err != nil {
			t.Fatalf("settings.Locate() = %v", err)
		}
		if _, err := file.Load(); !errors.Is(err, settings.ErrMalformed) ||
			strings.Contains(err.Error(), "s3cret") || strings.Contains(err.Error(), "sld_secret") {
			t.Fatalf("settings.Load(%s) = %v, want ErrMalformed naming the field only", data, err)
		}
	}

	dir := testDir(t)
	env := lookup(map[string]string{credentials.DirEnv: dir})
	keyFile, err := Locate(env, runtime.GOOS)
	if err != nil {
		t.Fatalf("Locate() = %v", err)
	}
	settingsFile, err := settings.Locate(env, runtime.GOOS)
	if err != nil {
		t.Fatalf("settings.Locate() = %v", err)
	}
	if FileName == settings.FileName || keyFile.Path() == settingsFile.Path() ||
		filepath.Dir(keyFile.Path()) != filepath.Dir(settingsFile.Path()) {
		t.Fatalf("key record %s and settings %s must be different files in one directory", keyFile.Path(), settingsFile.Path())
	}
}
