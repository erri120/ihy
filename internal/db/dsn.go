package db

import (
	"fmt"
	"net/url"
)

// parseDSN parses a postgres URL DSN.
func parseDSN(dsn string) (*url.URL, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return nil, fmt.Errorf("unsupported DSN scheme %q", u.Scheme)
	}
	return u, nil
}

// swapPath returns the DSN with the database path replaced. Used to
// point at the admin "postgres" database or a freshly created test
// database on the same server.
func swapPath(u *url.URL, dbname string) string {
	c := *u
	c.Path = "/" + dbname
	return c.String()
}
