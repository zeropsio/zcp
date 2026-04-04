package ops

import "testing"

func TestParseGitStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		output string
		want   GitStatus
	}{
		{
			name: "S0: no git, no zerops.yaml",
			output: `GIT_DIR=no
REMOTE=
BRANCH=
DIRTY=
ZEROPS_YML=no`,
			want: GitStatus{
				HasGit:       false,
				HasRemote:    false,
				HasZeropsYml: false,
			},
		},
		{
			name: "S1: internal git, no remote, has zerops.yaml",
			output: `GIT_DIR=yes
REMOTE=
BRANCH=main
DIRTY=
ZEROPS_YML=yes`,
			want: GitStatus{
				HasGit:       true,
				HasRemote:    false,
				Branch:       "main",
				HasZeropsYml: true,
			},
		},
		{
			name: "S2: full git with remote",
			output: `GIT_DIR=yes
REMOTE=https://github.com/user/repo.git
BRANCH=main
DIRTY=
ZEROPS_YML=yes`,
			want: GitStatus{
				HasGit:       true,
				HasRemote:    true,
				RemoteURL:    "https://github.com/user/repo.git",
				Branch:       "main",
				HasZeropsYml: true,
			},
		},
		{
			name: "dirty working tree",
			output: `GIT_DIR=yes
REMOTE=https://github.com/user/repo.git
BRANCH=main
DIRTY= M src/index.ts
ZEROPS_YML=yes`,
			want: GitStatus{
				HasGit:       true,
				HasRemote:    true,
				RemoteURL:    "https://github.com/user/repo.git",
				Branch:       "main",
				IsDirty:      true,
				HasZeropsYml: true,
			},
		},
		{
			name: "no branch (detached HEAD)",
			output: `GIT_DIR=yes
REMOTE=
BRANCH=
DIRTY=
ZEROPS_YML=no`,
			want: GitStatus{
				HasGit: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := parseGitStatus(tt.output)

			if got.HasGit != tt.want.HasGit {
				t.Errorf("HasGit = %v, want %v", got.HasGit, tt.want.HasGit)
			}
			if got.HasRemote != tt.want.HasRemote {
				t.Errorf("HasRemote = %v, want %v", got.HasRemote, tt.want.HasRemote)
			}
			if got.RemoteURL != tt.want.RemoteURL {
				t.Errorf("RemoteURL = %q, want %q", got.RemoteURL, tt.want.RemoteURL)
			}
			if got.Branch != tt.want.Branch {
				t.Errorf("Branch = %q, want %q", got.Branch, tt.want.Branch)
			}
			if got.IsDirty != tt.want.IsDirty {
				t.Errorf("IsDirty = %v, want %v", got.IsDirty, tt.want.IsDirty)
			}
			if got.HasZeropsYml != tt.want.HasZeropsYml {
				t.Errorf("HasZeropsYml = %v, want %v", got.HasZeropsYml, tt.want.HasZeropsYml)
			}
		})
	}
}
