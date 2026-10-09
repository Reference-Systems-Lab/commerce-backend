// Package config reads the backend's configuration from the environment. Secrets come from files
// named by *_FILE variables, never from the environment or a URL, and never reach logs or errors.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
)

// Secret holds a sensitive value. Formatting or logging it prints a placeholder, never the value.
type Secret string

const redacted = "[redacted]"

// Reveal returns the secret's value, for the one place that must hand it to a client library.
func (s Secret) Reveal() string { return string(s) }

func (Secret) String() string               { return redacted }
func (Secret) GoString() string             { return redacted }
func (Secret) LogValue() slog.Value         { return slog.StringValue(redacted) }
func (Secret) MarshalText() ([]byte, error) { return []byte(redacted), nil }

// Database is how the backend reaches PostgreSQL.
type Database struct {
	// URL is a libpq connection URL without a password.
	URL string
	// Password is read from the file that DATABASE_PASSWORD_FILE names.
	Password Secret
}

// Server holds the HTTP listener's settings.
type Server struct {
	Port int
}

// Lookup reads an environment variable; ReadFile reads a file. Both are injected so tests need no
// real environment.
type (
	Lookup   func(key string) (string, bool)
	ReadFile func(name string) ([]byte, error)
)

// DefaultPort is the port the API listens on when PORT is unset.
const DefaultPort = 8080

// LoadServer reads PORT, defaulting to DefaultPort.
func LoadServer(lookup Lookup) (Server, error) {
	raw, ok := lookup("PORT")
	if !ok || raw == "" {
		return Server{Port: DefaultPort}, nil
	}
	port, err := strconv.Atoi(raw)
	if err != nil || port < 1 || port > 65535 {
		return Server{}, fmt.Errorf("PORT must be a number from 1 to 65535, got %q", raw)
	}
	return Server{Port: port}, nil
}

// LoadDatabase reads DATABASE_URL and the password file that DATABASE_PASSWORD_FILE names.
func LoadDatabase(lookup Lookup, readFile ReadFile) (Database, error) {
	rawURL, err := required(lookup, "DATABASE_URL")
	if err != nil {
		return Database{}, err
	}
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Host == "" {
		// The value itself is left out: a malformed URL may still hold a password.
		return Database{}, errors.New("DATABASE_URL must be a postgres:// URL with a host")
	}
	if _, has := u.User.Password(); has || u.Query().Has("password") {
		return Database{}, errors.New("DATABASE_URL must not contain a password; use DATABASE_PASSWORD_FILE")
	}

	path, err := required(lookup, "DATABASE_PASSWORD_FILE")
	if err != nil {
		return Database{}, err
	}
	data, err := readFile(path)
	if err != nil {
		return Database{}, fmt.Errorf("DATABASE_PASSWORD_FILE: cannot read %s", path)
	}
	password := strings.TrimRight(string(data), "\r\n")
	if password == "" {
		return Database{}, fmt.Errorf("DATABASE_PASSWORD_FILE: %s is empty", path)
	}
	return Database{URL: rawURL, Password: Secret(password)}, nil
}

func required(lookup Lookup, key string) (string, error) {
	v, ok := lookup(key)
	if !ok || v == "" {
		return "", fmt.Errorf("%s is required", key)
	}
	return v, nil
}
