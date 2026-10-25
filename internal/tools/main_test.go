package tools

import (
	"context"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestMain sets, once for the whole package and before any test, what the
// lab's tests (hq_lab_test.go) would otherwise each set in the process and so
// could not run in parallel: never changed after, it is no global state they
// share. Each lab's identity toward its fake HQ rides on its runtime.Info
// (runtime.HQLink) instead of the environment.
//   - HOME: a home of the package's own, so no git the tests run reads the
//     developer's configuration; a test that needs its own still sets one.
//   - GIT_SSL_CAINFO: zcp's own git against a fake HQ trusts its
//     certificate, one fixed certificate for every httptest TLS server.
//   - the step-4c session-auth loop tries twice without a pause, and a
//     describe's tries record their waits on the lab that asked
//     (withDescribeWaits) rather than wait them out.
func TestMain(m *testing.M) {
	code, err := runPackageTests(m)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(code)
}

func runPackageTests(m *testing.M) (int, error) {
	dir, err := os.MkdirTemp("", "zcp-tools-test-")
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(dir)
	home := filepath.Join(dir, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		return 0, err
	}
	srv := httptest.NewTLSServer(http.NotFoundHandler())
	cert := srv.Certificate().Raw
	srv.Close()
	caFile := filepath.Join(dir, "hq.pem")
	if err := os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert}), 0o600); err != nil {
		return 0, err
	}
	if err := os.Setenv("HOME", home); err != nil {
		return 0, err
	}
	if err := os.Setenv("GIT_SSL_CAINFO", caFile); err != nil {
		return 0, err
	}
	gitPushSessionAuthAttempts, gitPushSessionAuthDelay = 2, 0
	describeRetry.pause = func(ctx context.Context, d time.Duration) error {
		if lab, ok := ctx.Value(describeWaitsKey{}).(*hqLab); ok {
			lab.describeWaits = append(lab.describeWaits, d)
		}
		return nil
	}
	return m.Run(), nil
}

// describeWaitsKey carries the lab whose describe is running, so its tries'
// waits are recorded on it (hqLab.describeWaits).
type describeWaitsKey struct{}

// withDescribeWaits is ctx recording a describe's waits on lab.
func withDescribeWaits(ctx context.Context, lab *hqLab) context.Context {
	return context.WithValue(ctx, describeWaitsKey{}, lab)
}
