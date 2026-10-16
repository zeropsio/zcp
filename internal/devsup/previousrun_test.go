package devsup

import (
	"strings"
	"testing"
)

// Systemd's own lines about the platform unit (journalctl UNIT=… -o json),
// read on a live container after the kernel's OOM killer killed a process of
// the unit and systemd restarted it: the run that ended, then the current one.
const oomRestartJournal = `{"__REALTIME_TIMESTAMP": "1792159601355276", "MESSAGE_ID": "39f53479d3a045ac8e11786248231fbf", "MESSAGE": "Started zerops@zerops.service - Zerops Runtime Service - zerops@zerops.", "UNIT": "zerops@zerops.service", "INVOCATION_ID": "e2d2a8099b614d95b14dffe2ff04d315", "_PID": "1"}
{"__REALTIME_TIMESTAMP": "1792160039595295", "MESSAGE_ID": "fe6faa94e7774663a0da52717891d8ef", "MESSAGE": "zerops@zerops.service: A process of this unit has been killed by the OOM killer.", "UNIT": "zerops@zerops.service", "INVOCATION_ID": "e2d2a8099b614d95b14dffe2ff04d315", "_PID": "1"}
{"__REALTIME_TIMESTAMP": "1792160039663229", "MESSAGE_ID": "98e322203f7a4ed290d09fe03c09fe15", "MESSAGE": "zerops@zerops.service: Main process exited, code=exited, status=15/n/a", "UNIT": "zerops@zerops.service", "INVOCATION_ID": "e2d2a8099b614d95b14dffe2ff04d315", "EXIT_CODE": "exited", "EXIT_STATUS": "15", "_PID": "1"}
{"__REALTIME_TIMESTAMP": "1792160039664160", "MESSAGE_ID": "d9b373ed55a64feb8242e02dbe79a49c", "MESSAGE": "zerops@zerops.service: Failed with result 'oom-kill'.", "UNIT": "zerops@zerops.service", "UNIT_RESULT": "oom-kill", "INVOCATION_ID": "e2d2a8099b614d95b14dffe2ff04d315", "_PID": "1"}
{"__REALTIME_TIMESTAMP": "1792160039673391", "MESSAGE_ID": "ae8f7b866b0347b9af31fe1c80b127c0", "MESSAGE": "zerops@zerops.service: Consumed 40.183s CPU time, 976.7M memory peak, 415.2M memory swap peak.", "UNIT": "zerops@zerops.service", "INVOCATION_ID": "e2d2a8099b614d95b14dffe2ff04d315", "MEMORY_PEAK": "1024188416", "MEMORY_SWAP_PEAK": "435384320", "_PID": "1"}
{"__REALTIME_TIMESTAMP": "1792160042882739", "MESSAGE_ID": "5eb03494b6584870a536b337290809b3", "MESSAGE": "zerops@zerops.service: Scheduled restart job, restart counter is at 1.", "UNIT": "zerops@zerops.service", "INVOCATION_ID": "e2d2a8099b614d95b14dffe2ff04d315", "N_RESTARTS": "1", "_PID": "1"}
{"__REALTIME_TIMESTAMP": "1792160042911748", "MESSAGE_ID": "39f53479d3a045ac8e11786248231fbf", "MESSAGE": "Started zerops@zerops.service - Zerops Runtime Service - zerops@zerops.", "UNIT": "zerops@zerops.service", "INVOCATION_ID": "64f0cdbf3d104534a2c5d4333737905d", "_PID": "1"}
`

// journalLines keeps the lines of oomRestartJournal whose numbers (from 1)
// are given.
func journalLines(nums ...int) string {
	all := strings.Split(strings.TrimSpace(oomRestartJournal), "\n")
	out := make([]string, 0, len(nums))
	for _, n := range nums {
		out = append(out, all[n-1])
	}
	return strings.Join(out, "\n") + "\n"
}

const (
	deactivatedLine  = `{"MESSAGE_ID": "7ad2d189f7e94e70a38c781354912448", "MESSAGE": "zerops@zerops.service: Deactivated successfully.", "UNIT": "zerops@zerops.service", "INVOCATION_ID": "e2d2a8099b614d95b14dffe2ff04d315", "_PID": "1"}`
	olderStartedLine = `{"MESSAGE_ID": "39f53479d3a045ac8e11786248231fbf", "MESSAGE": "Started zerops@zerops.service.", "UNIT": "zerops@zerops.service", "INVOCATION_ID": "0123456789abcdef0123456789abcdef", "_PID": "1"}`
)

// The supervisor learns how the run of its unit before the current one ended
// from systemd's lines about the unit, and nothing when the journal does not
// hold that run's end.
func TestPreviousRunResult_ReadsHowSystemdEndedTheRunBeforeTheCurrentOne(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		journal string
		want    string
		ok      bool
	}{
		{"the OOM killer ended it", oomRestartJournal, "oom-kill", true},
		{"it ended successfully", journalLines(1) + deactivatedLine + "\n" + journalLines(7), "success", true},
		{"the previous run's start is gone", journalLines(4, 5, 7), "oom-kill", true},
		{"the current run is the first", journalLines(7), "", false},
		{"the previous run's end is gone, an older run's is not", journalLines(1, 4, 5) + olderStartedLine + "\n" + journalLines(7), "", false},
		{"no line", "", "", false},
		{"not the journal's JSON", "No journal files were opened due to insufficient permissions.\n", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := previousRunResult([]byte(tt.journal))
			if got != tt.want || ok != tt.ok {
				t.Fatalf("previousRunResult = %q, %v; want %q, %v", got, ok, tt.want, tt.ok)
			}
		})
	}
}
