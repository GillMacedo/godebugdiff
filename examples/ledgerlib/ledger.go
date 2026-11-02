// Package ledgerlib is a made-up library with no main package, so
// godebugdiff checks its test binary.
package ledgerlib

import "net/url"

// AccountFromURL returns the account query parameter.
func AccountFromURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	return u.Query().Get("account"), nil
}
