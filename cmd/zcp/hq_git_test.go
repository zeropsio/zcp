package main

import "testing"

func TestIsHQGit_RecognizesOnlyTheGitPassthrough(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		args []string
		want bool
	}{
		{[]string{"hq", "git"}, true},
		{[]string{"hq", "git", "-C", "/var/www/web", "fetch"}, true},
		{[]string{"hq", "git-credential", "get"}, false},
		{[]string{"hq", "status"}, false},
		{[]string{"git"}, false},
		{nil, false},
	} {
		if got := isHQGit(tt.args); got != tt.want {
			t.Errorf("isHQGit(%v) = %v, want %v", tt.args, got, tt.want)
		}
	}
}
