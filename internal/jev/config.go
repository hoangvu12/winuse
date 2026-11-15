package jev

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Config is where winuse's Jev requests go. A shell's environment does not
// outlive one agent tool call, so the normal home is a file that `winuse
// jev set` writes; the environment variables the TypeSafe SDKs read win
// over it (CI, one-off overrides):
//
//	TYPESAFE_API_KEY    the key
//	TYPESAFE_BASE_URL   the server (default https://api.typesafe.ai)
//	WINUSE_JEV_MODEL    the model (default jev-latest)
type Config struct {
	BaseURL string `json:"base_url,omitempty"`
	Key     string `json:"api_key,omitempty"`
	Model   string `json:"model,omitempty"`

	// Where each value came from: "env", "file" or "default" ("" for a
	// missing key). Not stored.
	BaseURLFrom, KeyFrom, ModelFrom string `json:"-"`
}

// ConfigFile is %LOCALAPPDATA%\winuse\jev.json: only its user can read it.
func ConfigFile(getenv func(string) string) (string, error) {
	dir := getenv("LOCALAPPDATA")
	if dir == "" {
		return "", errors.New("LOCALAPPDATA is not set")
	}
	return filepath.Join(dir, "winuse", "jev.json"), nil
}

// ReadStored reads the stored settings; a missing file is an empty Config.
func ReadStored(file string) (Config, error) {
	var c Config
	raw, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	raw = bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF}) // hand-edited in PowerShell
	if err := json.Unmarshal(raw, &c); err != nil {
		return c, errors.New(file + " is not valid JSON; run winuse jev clear and set it again")
	}
	return c, nil
}

// WriteStored replaces the stored settings.
func WriteStored(file string, c Config) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(file, append(raw, '\n'), 0o600)
}

// Resolve merges the environment over the stored settings over the
// defaults and records where each value came from.
func Resolve(stored Config, getenv func(string) string) Config {
	pick := func(env, file, def string) (string, string) {
		if v := strings.TrimSpace(getenv(env)); v != "" {
			return v, "env"
		}
		if v := strings.TrimSpace(file); v != "" {
			return v, "file"
		}
		if def != "" {
			return def, "default"
		}
		return "", ""
	}
	var c Config
	c.Key, c.KeyFrom = pick("TYPESAFE_API_KEY", stored.Key, "")
	c.BaseURL, c.BaseURLFrom = pick("TYPESAFE_BASE_URL", stored.BaseURL, DefaultBaseURL)
	c.Model, c.ModelFrom = pick("WINUSE_JEV_MODEL", stored.Model, DefaultModel)
	c.BaseURL = strings.TrimRight(c.BaseURL, "/")
	return c
}

// Load reads the stored settings and resolves them against the
// environment.
func Load(getenv func(string) string) (Config, error) {
	file, err := ConfigFile(getenv)
	if err != nil {
		return Resolve(Config{}, getenv), err
	}
	stored, err := ReadStored(file)
	return Resolve(stored, getenv), err
}

// Mask shows a key's start and end only, so it never lands whole in a
// transcript.
func Mask(k string) string {
	if len(k) <= 16 {
		return strings.Repeat("*", len(k))
	}
	return k[:10] + "..." + k[len(k)-4:]
}
