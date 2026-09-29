package app

import (
	"errors"
	"fmt"
	"time"

	"github.com/baalimago/slivingdoc/internal/credentials"
)

// LoginStatus is one stored login as the home screen shows it, read from
// the credentials file alone: nothing is sent to a site.
type LoginStatus struct {
	Account  string
	Endpoint string
	Access   credentials.Access
	Expires  credentials.Expiry
	// Expired reports whether the key's expiry has passed.
	Expired bool
	// DefaultSpace is the endpoint's default space; empty for none.
	DefaultSpace string
}

// StoredLogins reads the logins in the credentials file with their
// default spaces, for the home screen (architecture/tui.md). A missing
// file is no login.
func StoredLogins(opts ProcessOptions) ([]LoginStatus, error) {
	file, err := credentialsFile(environ(opts.Env))
	if err != nil {
		return nil, fmt.Errorf("app: %w", err)
	}
	set, err := file.Load()
	if err != nil {
		return nil, fmt.Errorf("app: read the stored logins: %w", err)
	}
	now := time.Now()
	logins := set.Logins()
	out := make([]LoginStatus, 0, len(logins))
	for _, l := range logins {
		space, err := set.DefaultSpace(l.Endpoint)
		if err != nil && !errors.Is(err, credentials.ErrNoDefault) {
			return nil, fmt.Errorf("app: read the default space: %w", err)
		}
		out = append(out, LoginStatus{
			Account: l.Account, Endpoint: l.Endpoint, Access: l.Access, Expires: l.Expires,
			Expired: l.Expires.Passed(now), DefaultSpace: space,
		})
	}
	return out, nil
}
