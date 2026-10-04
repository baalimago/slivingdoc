package settings

import (
	"github.com/baalimago/slivingdoc/internal/credentials"
)

// Locate resolves the settings file by the credentials file's own rule:
// credentials.DirEnv when set, else the platform's user configuration
// directory plus "slivingdoc". getenv and goos are the process
// environment and operating system, injected so a test never reads the
// developer's own file. A directory that does not resolve is
// ErrNoConfigDir, worded in this package's vocabulary.
func Locate(getenv func(string) string, goos string) (File, error) {
	dir, uid, err := credentials.ConfigDir(getenv, goos)
	if err != nil {
		return File{}, credentials.NoDirectory(ErrNoConfigDir, err)
	}
	return File{dir: dir, private: credentials.ChecksOwners(goos), uid: uid}, nil
}
