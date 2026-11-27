package topology

import "time"

// DevServerState is one declared dev server on its dev service's envelope
// snapshot (docs/spec-mate.md §2.8a.3): the supervisor's status verbatim, as
// zcp read it at ReadAt.
type DevServerState struct {
	Name     string          `json:"name"`
	Port     int             `json:"port,omitempty"`
	URL      string          `json:"url,omitempty"`
	Status   string          `json:"status"`
	ReadAt   time.Time       `json:"readAt"`
	Verify   *DevServerProbe `json:"verify,omitempty"`
	LastExit *DevServerExit  `json:"lastExit,omitempty"`
}

// DevServerProbe is zcp's HTTP probe of a dev server's port, set only when
// the call that read the server probed it.
type DevServerProbe struct {
	OK         bool      `json:"ok"`
	HTTPStatus int       `json:"httpStatus,omitempty"`
	At         time.Time `json:"at"`
}

// DevServerExit is the supervisor's record of a server's last exit.
type DevServerExit struct {
	Code   *int      `json:"code,omitempty"`
	Signal string    `json:"signal,omitempty"`
	At     time.Time `json:"at"`
}

// StageWire is one stage of a pair as the envelope carries it: hostname and
// setup only (docs/spec-mate.md §2.8a.3).
type StageWire struct {
	Hostname string `json:"hostname"`
	Setup    string `json:"setup,omitempty"`
	// Part is the part the stage builds: a pool-keyed pair's from its
	// leases' dev servers, else the stage's setup, else the stage hostname
	// without its `stage` suffix (docs/spec-mate.md §2.8a.3).
	Part string `json:"part,omitempty"`
}
