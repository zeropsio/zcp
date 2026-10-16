package devsup

import (
	"bytes"
	"encoding/json"
)

// unitResultOOMKill is systemd's result for a run of a unit it stopped because
// the kernel's OOM killer killed one of the unit's processes (OOMPolicy=stop,
// which the platform's zerops@zerops.service has).
const unitResultOOMKill = "oom-kill"

// reasonOutOfMemoryRestart is the restart reason of a server started after
// systemd restarted the service because it ran out of memory. The journal a
// container can read names neither the killed process nor a usable memory
// figure: the unit's recorded memory peak can be far below what its
// processes used.
const reasonOutOfMemoryRestart = "the service ran out of memory: the kernel killed one of its processes and systemd restarted every dev server"

// Systemd's catalog MESSAGE_IDs of its lines about a unit.
const (
	msgUnitStarted = "39f53479d3a045ac8e11786248231fbf"
	msgUnitSuccess = "7ad2d189f7e94e70a38c781354912448"
	msgUnitResult  = "d9b373ed55a64feb8242e02dbe79a49c"
)

// previousRunResult reads, from systemd's lines about the unit in journal
// order (journalctl UNIT=<unit> -o json), systemd's result for the run before
// the current one: the last end line before the current run's "Started" line,
// with no other start between them. The journal drops its oldest lines first,
// so the previous run's own start may be gone; false when its end is.
func previousRunResult(journal []byte) (string, bool) {
	started := false
	lines := bytes.Split(journal, []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		var e struct {
			ID     string `json:"MESSAGE_ID"`  //nolint:tagliatelle // journal field
			Result string `json:"UNIT_RESULT"` //nolint:tagliatelle // journal field
		}
		if json.Unmarshal(lines[i], &e) != nil {
			continue
		}
		switch {
		case e.ID == msgUnitStarted && started:
			return "", false
		case e.ID == msgUnitStarted:
			started = true
		case started && e.ID == msgUnitResult && e.Result != "":
			return e.Result, true
		case started && e.ID == msgUnitSuccess:
			return "success", true
		}
	}
	return "", false
}
