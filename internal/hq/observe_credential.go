package hq

import (
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/zeropsio/zcp/internal/runtime"
)

// ObserveCredentialPath is the single profile credential, written atomically
// by Mate through ordinary ZCP authorization and its HQ link. zcp never writes it.
func ObserveCredentialPath() string {
	return filepath.Join(runtime.HomeDir(), ".zcp", "hq", "observe-credential")
}

var (
	ErrNoObserveCredential      = errors.New("observe authorization required: check normal ZCP authorization and the Mate connection")
	ErrInvalidObserveCredential = errors.New("observe authorization required: the profile credential is malformed; check normal ZCP authorization and the Mate connection")
	ErrReadObserveCredential    = errors.New("observe authorization required: the profile credential could not be read")
)

const observeCredentialLimit = 256

// ReadObserveCredential reads one bounded token at each use. Errors never
// include file contents or paths. Revocation and expiry are decided by HQ.
func ReadObserveCredential(path string) (string, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", ErrNoObserveCredential
	}
	if err != nil {
		return "", ErrReadObserveCredential
	}
	if !info.Mode().IsRegular() || info.Size() > observeCredentialLimit {
		return "", ErrInvalidObserveCredential
	}
	file, err := os.Open(path)
	if err != nil {
		return "", ErrReadObserveCredential
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, observeCredentialLimit+1))
	if err != nil {
		return "", ErrReadObserveCredential
	}
	if len(raw) > observeCredentialLimit {
		return "", ErrInvalidObserveCredential
	}
	credential := strings.TrimSuffix(string(raw), "\n")
	parts := strings.Split(credential, ".")
	if len(parts) != 3 || parts[0] != "zobs" || !regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`).MatchString(parts[1]) {
		return "", ErrInvalidObserveCredential
	}
	secret, err := base64.RawURLEncoding.Strict().DecodeString(parts[2])
	if err != nil || len(secret) != 32 || base64.RawURLEncoding.EncodeToString(secret) != parts[2] {
		return "", ErrInvalidObserveCredential
	}
	return credential, nil
}
