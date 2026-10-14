package ops

import (
	"context"
	"strings"
	"testing"
)

func TestFillCodeVolume_FillsOnceFromTheCheckoutLeavingTheVolumeOut(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		mountPath  string
		answer     string
		wantFilled bool
		wantErr    string
	}{
		{name: "an empty volume is filled", mountPath: "/var/www/ws", answer: "@@zcp code-volume filled\n", wantFilled: true},
		{name: "a volume holding a repository is left as it is", mountPath: "/var/www/ws", answer: "@@zcp code-volume already\n"},
		{name: "a silent container is no fill", mountPath: "/var/www/ws", answer: "", wantErr: "no answer"},
		{name: "a mount outside /var/www is refused before SSH", mountPath: "/mnt/vol", wantErr: "no folder under /var/www"},
		{name: "/var/www itself is refused before SSH", mountPath: "/var/www", wantErr: "no folder under /var/www"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ssh := &mockSSHDeployer{output: []byte(tt.answer)}
			filled, err := FillCodeVolume(context.Background(), ssh, "webdev", tt.mountPath)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil || filled != tt.wantFilled {
				t.Fatalf("filled = %v, err = %v; want %v", filled, err, tt.wantFilled)
			}
			if len(ssh.calls) != 1 {
				t.Fatalf("ssh calls = %d", len(ssh.calls))
			}
			cmd := ssh.calls[0].command
			for _, want := range []string{`[ -e "$vol/.git" ]`, "cd '/var/www'", "! -name 'ws'", "vol='/var/www/ws'"} {
				if !strings.Contains(cmd, want) {
					t.Errorf("command lacks %q: %s", want, cmd)
				}
			}
		})
	}
}
