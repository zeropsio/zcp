package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	// (ops.ContainerIdentity): the container's hostname and its init's start
	// time, so a restart (a new init) and a redeploy (a new container) both
	// change it while a crash of the server itself does not.
	Container string `json:"container"`
	StartedAt string `json:"startedAt"`
	// LastRestore is zcp's last bring-back of this server: when, and whether
	// it came up. Nil until the container first cycles.
	LastRestore *DevServerRestore `json:"lastRestore,omitempty"`
}

// DevServerRestore is the outcome of one bring-back.
type DevServerRestore struct {
	At      string `json:"at"`
	Running bool   `json:"running"`
	Reason  string `json:"reason,omitempty"`
}

// KeepDevServer records rec as the dev server zcp keeps on rec.Hostname,
// replacing whatever was kept there before.
func KeepDevServer(stateDir string, rec KeptDevServer) error {
	if rec.Hostname == "" {
		return errors.New("keep dev server: empty hostname")
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

// KeptDevServerFor returns the dev server kept on hostname, nil when none is.
func KeptDevServerFor(stateDir, hostname string) (*KeptDevServer, error) {
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

// ClaimKeptDevServer claims the bring-back of the dev server kept on hostname
// for the container life container names: when the kept server last ran in a
// different one (the container restarted or was redeployed since), the record
// moves to this life and the caller gets it to start — claimed=true. Every
// later caller in the same life finds it claimed, so the keeper's next tick and
// a deploy's own restore never start it twice. An empty container (the
// identity could not be read) claims nothing, and a record whose own life was
// never read adopts this one as its baseline without a bring-back — nothing
// says the server is gone.
func ClaimKeptDevServer(stateDir, hostname, container string) (*KeptDevServer, bool, error) {
	if container == "" {
		return nil, false, nil
	}
	var out *KeptDevServer
	err := withKeptDevServers(stateDir, func(kept map[string]KeptDevServer) (bool, error) {
		rec, ok := kept[hostname]
		if !ok || rec.Container == container {
			return false, nil
		}
		baseline := rec.Container == ""
		rec.Container = container
		kept[hostname] = rec
		if !baseline {
			out = &rec
		}
		return true, nil
	})
	if err != nil {
		return nil, false, err
	}
	return out, out != nil, nil
}

// RecordDevServerRestore stores the outcome of the bring-back claimed for
// container. A record that moved on meanwhile (a newer start by the agent, or
// another cycle) keeps its own state.
func RecordDevServerRestore(stateDir, hostname, container string, restore DevServerRestore) error {
	return withKeptDevServers(stateDir, func(kept map[string]KeptDevServer) (bool, error) {
		rec, ok := kept[hostname]
		if !ok || rec.Container != container {
			return false, nil
		}
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
