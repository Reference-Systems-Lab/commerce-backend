package config

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

const pw = "rsldev_0123456789abcdef"

func env(m map[string]string) Lookup {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

func files(m map[string]string) ReadFile {
	return func(name string) ([]byte, error) {
		v, ok := m[name]
		if !ok {
			return nil, errors.New("open " + name + ": no such file or directory")
		}
		return []byte(v), nil
	}
}

func TestLoadDatabase(t *testing.T) {
	ok := map[string]string{
		"DATABASE_URL":           "postgres://commerce@postgres:5432/commerce?sslmode=disable",
		"DATABASE_PASSWORD_FILE": "/run/secrets/postgres_password",
	}
	secrets := map[string]string{"/run/secrets/postgres_password": pw + "\n"}

	t.Run("reads the URL and the password file", func(t *testing.T) {
		db, err := LoadDatabase(env(ok), files(secrets))
		if err != nil {
			t.Fatal(err)
		}
		if db.Password.Reveal() != pw {
			t.Fatalf("password = %q, want the file's value without its newline", db.Password.Reveal())
		}
		if db.URL != ok["DATABASE_URL"] {
			t.Fatalf("URL = %q", db.URL)
		}
	})

	cases := []struct {
		name    string
		env     map[string]string
		files   map[string]string
		wantErr string
	}{
		{"missing URL", map[string]string{"DATABASE_PASSWORD_FILE": "/p"}, secrets, "DATABASE_URL is required"},
		{"empty URL", map[string]string{"DATABASE_URL": "", "DATABASE_PASSWORD_FILE": "/p"}, secrets, "DATABASE_URL is required"},
		{"not postgres", map[string]string{"DATABASE_URL": "mysql://x@h/db", "DATABASE_PASSWORD_FILE": "/p"}, secrets, "postgres:// URL"},
		{"password in URL", map[string]string{"DATABASE_URL": "postgres://u:" + pw + "@h/db", "DATABASE_PASSWORD_FILE": "/p"}, secrets, "must not contain a password"},
		{"password in query", map[string]string{"DATABASE_URL": "postgres://u@h/db?password=" + pw, "DATABASE_PASSWORD_FILE": "/p"}, secrets, "must not contain a password"},
		{"missing password file variable", map[string]string{"DATABASE_URL": ok["DATABASE_URL"]}, secrets, "DATABASE_PASSWORD_FILE is required"},
		{"unreadable password file", ok, map[string]string{}, "DATABASE_PASSWORD_FILE: cannot read /run/secrets/postgres_password"},
		{"empty password file", ok, map[string]string{"/run/secrets/postgres_password": "\n"}, "is empty"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := LoadDatabase(env(c.env), files(c.files))
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("err = %v, want it to contain %q", err, c.wantErr)
			}
			if strings.Contains(err.Error(), pw) {
				t.Fatalf("the error leaks the password: %v", err)
			}
		})
	}
}

func TestLoadServer(t *testing.T) {
	s, err := LoadServer(env(nil))
	if err != nil || s.Port != DefaultPort {
		t.Fatalf("default: %+v, %v", s, err)
	}
	s, err = LoadServer(env(map[string]string{"PORT": "9090"}))
	if err != nil || s.Port != 9090 {
		t.Fatalf("PORT=9090: %+v, %v", s, err)
	}
	for _, bad := range []string{"x", "0", "65536", "-1"} {
		if _, err := LoadServer(env(map[string]string{"PORT": bad})); err == nil {
			t.Fatalf("PORT=%q: want an error", bad)
		}
	}
}

func TestSecretNeverPrints(t *testing.T) {
	s := Secret(pw)
	db := Database{URL: "postgres://u@h/db", Password: s}
	for _, out := range []string{
		fmt.Sprint(s), fmt.Sprintf("%v %+v %#v %s %q", s, db, db, s, s),
	} {
		if strings.Contains(out, pw) {
			t.Fatalf("formatting leaks the secret: %s", out)
		}
	}
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("config", "db", db, "password", s)
	if strings.Contains(buf.String(), pw) {
		t.Fatalf("logging leaks the secret: %s", buf.String())
	}
}
