package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// The dev servers zcp keeps (docs/spec-workflows.md §8 O4): the start the agent
// last made with zerops_dev_server on each dev container, which zcp brings back
// by itself when that container restarts or is redeployed — the process dies
// with the container, and a dev container's run.start is a no-op keepalive —
// until the agent stops it. One index file, under its own lock.
const (
	devServersFile     = "dev-servers.json"
	devServersLockName = ".dev-servers.lock"
)

// Bring-backs within one container life. A dependency that is not up yet right
// after a project starts gets a few more tries; a server that cannot start does
// not loop.
const (
	// MaxDevServerRestoreAttempts bounds the bring-backs in one container life.
	MaxDevServerRestoreAttempts = 3
	// DevServerRestoring is LastRestore.Reason while a claimed bring-back runs.
	DevServerRestoring = "restoring"
	// devServerRestoreRetryAfter spaces the attempts within a life.
	devServerRestoreRetryAfter = 30 * time.Second
	// devServerRestoreStale lets a claim whose outcome was never recorded (its
	// claimer died mid-start) be taken again.
	devServerRestoreStale = 3 * time.Minute
)

// KeptDevServer is one kept dev server: everything a bring-back needs to start
// it again the way the agent did, and the container life it last ran in.
type KeptDevServer struct {
	Hostname    string `json:"hostname"`
	Command     string `json:"command"`
	Port        int    `json:"port,omitempty"`
	HealthPath  string `json:"healthPath,omitempty"`
	WorkDir     string `json:"workDir,omitempty"`
	LogFile     string `json:"logFile,omitempty"`
	WaitSeconds int    `json:"waitSeconds,omitempty"`
	NoHTTPProbe bool   `json:"noHttpProbe,omitempty"`
	// Container names the container life the server was last started in
	// (ops.ContainerIdentity): a restart (a new init) and a redeploy (a new
	// container) both change it, while a crash of the server itself does not.
	Container string `json:"container"`
	StartedAt string `json:"startedAt"`
	// LastRestore is zcp's last bring-back in the current container life:
	// when, whether it came up, and how many were made. Nil while the server
	// runs as the agent started it.
	LastRestore *DevServerRestore `json:"lastRestore,omitempty"`
}

// DevServerRestore is the outcome of the bring-backs in one container life.
type DevServerRestore struct {
	At      string `json:"at"`
	Running bool   `json:"running"`
	Reason  string `json:"reason,omitempty"`
	// Attempts counts the bring-backs claimed in this container life.
	Attempts int `json:"attempts"`
}

// KeepDevServer records rec as the dev server zcp keeps on rec.Hostname,
// replacing whatever was kept there before. A record names the container life
// it was started in; without one a restart could not be told from a crash.
func KeepDevServer(stateDir string, rec KeptDevServer) error {
	if rec.Hostname == "" {
		return errors.New("keep dev server: empty hostname")
	}
	if rec.Container == "" {
		return fmt.Errorf("keep dev server %s: its container life is unknown", rec.Hostname)
	}
	return withKeptDevServers(stateDir, func(kept map[string]KeptDevServer) (bool, error) {
		kept[rec.Hostname] = rec
		return true, nil
	})
}

// ForgetDevServer stops keeping the dev server on hostname. Forgetting what was
// never kept is a no-op.
func ForgetDevServer(stateDir, hostname string) error {
	return withKeptDevServers(stateDir, func(kept map[string]KeptDevServer) (bool, error) {
		if _, ok := kept[hostname]; !ok {
			return false, nil
		}
		delete(kept, hostname)
		return true, nil
	})
}

// noKeptDevServers reports that nothing was ever kept under stateDir — the
// common case, answered without taking the lock or creating anything.
func noKeptDevServers(stateDir string) bool {
	if stateDir == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(stateDir, devServersFile))
	return errors.Is(err, os.ErrNotExist)
}

// KeptDevServerFor returns the dev server kept on hostname, nil when none is.
func KeptDevServerFor(stateDir, hostname string) (*KeptDevServer, error) {
	if noKeptDevServers(stateDir) {
		return nil, nil //nolint:nilnil // nil,nil = nothing kept, by design
	}
	var out *KeptDevServer
	err := withKeptDevServers(stateDir, func(kept map[string]KeptDevServer) (bool, error) {
		if rec, ok := kept[hostname]; ok {
			out = &rec
		}
		return false, nil
	})
	return out, err
}

// KeptDevServers returns every kept dev server, ordered by hostname.
func KeptDevServers(stateDir string) ([]KeptDevServer, error) {
	if noKeptDevServers(stateDir) {
		return nil, nil
	}
	var out []KeptDevServer
	err := withKeptDevServers(stateDir, func(kept map[string]KeptDevServer) (bool, error) {
		for _, rec := range kept {
			out = append(out, rec)
		}
		return false, nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Hostname < out[j].Hostname })
	return out, err
}

// ClaimKeptDevServer claims a bring-back of the dev server kept on hostname in
// the container life container names, at now. It is claimed when the kept
// server last ran in a different life (the container restarted or was
// redeployed since), and again within the same life only after a bring-back
// that did not come up — up to MaxDevServerRestoreAttempts, spaced apart. The
// claim marks the attempt DevServerRestoring, so every other caller — the
// keeper's next pass, a deploy's own bring-back, another process — finds it
// taken and never starts the server twice. Never claimed: a server the agent
// started in this life (LastRestore nil), one that came up (a later crash is
// the agent's), and an empty container (the identity could not be read).
func ClaimKeptDevServer(stateDir, hostname, container string, now time.Time) (*KeptDevServer, bool, error) {
	if container == "" {
		return nil, false, nil
	}
	var out *KeptDevServer
	err := withKeptDevServers(stateDir, func(kept map[string]KeptDevServer) (bool, error) {
		rec, ok := kept[hostname]
		if !ok {
			return false, nil
		}
		attempts := 0
		if rec.Container == container {
			last := rec.LastRestore
			if last == nil || last.Running || last.Attempts >= MaxDevServerRestoreAttempts {
				return false, nil
			}
			at, _ := time.Parse(time.RFC3339, last.At)
			wait := devServerRestoreRetryAfter
			if last.Reason == DevServerRestoring {
				wait = devServerRestoreStale
			}
			if now.Sub(at) < wait {
				return false, nil
			}
			attempts = last.Attempts
		}
		rec.Container = container
		rec.LastRestore = &DevServerRestore{
			At:       now.UTC().Format(time.RFC3339),
			Reason:   DevServerRestoring,
			Attempts: attempts + 1,
		}
		kept[hostname] = rec
		out = &rec
		return true, nil
	})
	if err != nil {
		return nil, false, err
	}
	return out, out != nil, nil
}

// TakeOverKeptDevServer records that the agent itself is starting the dev
// server kept on hostname in the container life container names: the life is
// the agent's, so no bring-back is claimed in it. Nothing kept, or an empty
// container, leaves the index as it is.
func TakeOverKeptDevServer(stateDir, hostname, container string) error {
	if container == "" {
		return nil
	}
	return withKeptDevServers(stateDir, func(kept map[string]KeptDevServer) (bool, error) {
		rec, ok := kept[hostname]
		if !ok {
			return false, nil
		}
		rec.Container = container
		rec.LastRestore = nil
		kept[hostname] = rec
		return true, nil
	})
}

// RecordDevServerRestore stores the outcome of the bring-back claimed for
// container, keeping the claim's attempt count. A record that moved on
// meanwhile (a newer start by the agent, or another cycle) keeps its own state.
func RecordDevServerRestore(stateDir, hostname, container string, restore DevServerRestore) error {
	return withKeptDevServers(stateDir, func(kept map[string]KeptDevServer) (bool, error) {
		rec, ok := kept[hostname]
		if !ok || rec.Container != container || rec.LastRestore == nil {
			return false, nil
		}
		restore.Attempts = rec.LastRestore.Attempts
		rec.LastRestore = &restore
		kept[hostname] = rec
		return true, nil
	})
}

// withKeptDevServers runs fn on the kept-dev-server index under the index's own
// lock and writes it back when fn reports a change.
func withKeptDevServers(stateDir string, fn func(map[string]KeptDevServer) (bool, error)) error {
	if stateDir == "" {
		return errors.New("kept dev servers: no state directory")
	}
	return withFileLock(filepath.Join(stateDir, devServersLockName), func() error {
		path := filepath.Join(stateDir, devServersFile)
		kept := map[string]KeptDevServer{}
		if data, err := os.ReadFile(path); err == nil {
			if jsonErr := json.Unmarshal(data, &kept); jsonErr != nil {
				return fmt.Errorf("kept dev servers: %w", jsonErr)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("kept dev servers: %w", err)
		}
		changed, err := fn(kept)
		if err != nil || !changed {
			return err
		}
		data, err := json.MarshalIndent(kept, "", "  ")
		if err != nil {
			return fmt.Errorf("kept dev servers: %w", err)
		}
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, data, 0o600); err != nil {
			return fmt.Errorf("kept dev servers: %w", err)
		}
		if err := os.Rename(tmp, path); err != nil {
			return fmt.Errorf("kept dev servers: %w", err)
		}
		return nil
	})
}
