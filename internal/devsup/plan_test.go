package devsup

import (
	"maps"
	"testing"
)

func TestPlan_Transitions(t *testing.T) {
	t.Parallel()
	web := Server{Name: "web", Port: 3000, Command: "bun --watch index.ts", Cwd: "."}
	web2 := web
	web2.Command = "bun --hot index.ts"
	webWatched := web
	webWatched.Watch = &Watch{Paths: []string{"apps/web"}}
	api := Server{Name: "api", Command: "go run .", Cwd: "."}
	run := func(s Server) wanted { return wanted{server: s, desired: DesiredRunning} }
	stop := func(s Server) wanted { return wanted{server: s, desired: DesiredStopped} }
	tests := []struct {
		name string
		want map[string]wanted
		have map[string]held
		boot bool
		out  map[string]action
	}{
		{
			name: "after a redeploy every declared server starts",
			want: map[string]wanted{"web": run(web), "api": run(api)},
			boot: true,
			out:  map[string]action{"web": actStart, "api": actStart},
		},
		{
			name: "a server stopped on request stays stopped across a restart",
			want: map[string]wanted{"web": stop(web)},
			have: map[string]held{"web": {server: web}},
			boot: true,
			out:  map[string]action{},
		},
		{
			name: "a crash stays down on reload",
			want: map[string]wanted{"web": run(web)},
			have: map[string]held{"web": {server: web, exited: true}},
			out:  map[string]action{},
		},
		{
			name: "a crashed server starts again when the supervisor starts",
			want: map[string]wanted{"web": run(web)},
			have: map[string]held{"web": {server: web, exited: true}},
			boot: true,
			out:  map[string]action{"web": actStart},
		},
		{
			name: "a kept running server starts when the supervisor starts",
			want: map[string]wanted{"web": run(web)},
			have: map[string]held{"web": {server: web}},
			boot: true,
			out:  map[string]action{"web": actStart},
		},
		{
			name: "a running server is left alone",
			want: map[string]wanted{"web": run(web)},
			have: map[string]held{"web": {server: web, live: true}},
			out:  map[string]action{},
		},
		{
			name: "a changed command restarts a running server",
			want: map[string]wanted{"web": run(web2)},
			have: map[string]held{"web": {server: web, live: true}},
			out:  map[string]action{"web": actRestart},
		},
		{
			name: "a changed watch restarts a running server",
			want: map[string]wanted{"web": run(webWatched)},
			have: map[string]held{"web": {server: web, live: true}},
			out:  map[string]action{"web": actRestart},
		},
		{
			name: "a changed command starts a crashed server",
			want: map[string]wanted{"web": run(web2)},
			have: map[string]held{"web": {server: web, exited: true}},
			out:  map[string]action{"web": actStart},
		},
		{
			name: "a changed command of a stopped server keeps it stopped",
			want: map[string]wanted{"web": stop(web2)},
			have: map[string]held{"web": {server: web}},
			out:  map[string]action{},
		},
		{
			name: "stop stops a running server",
			want: map[string]wanted{"web": stop(web)},
			have: map[string]held{"web": {server: web, live: true}},
			out:  map[string]action{"web": actStop},
		},
		{
			name: "a server dropped from the declaration is removed",
			want: map[string]wanted{},
			have: map[string]held{"web": {server: web, live: true}},
			out:  map[string]action{"web": actRemove},
		},
		{
			name: "an added server starts and the rest is untouched",
			want: map[string]wanted{"web": run(web), "api": run(api)},
			have: map[string]held{"web": {server: web, live: true}},
			out:  map[string]action{"api": actStart},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := plan(tt.want, tt.have, tt.boot)
			if !maps.Equal(got, tt.out) {
				t.Fatalf("plan = %v, want %v", got, tt.out)
			}
		})
	}
}
