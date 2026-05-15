package tools

import (
	"github.com/zeropsio/zcp/internal/envclass"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// launchEnvsForClassifyPrompt returns the subset of source project envs
// the user needs to classify — envclass-Decision = PromptUser. Drop-
// decision entries (project SYSTEM envs, e.g. zeropsSubdomain*, CDN
// URLs, envIsolation) are excluded; the target project regenerates
// them on import. Source slice is not mutated.
func launchEnvsForClassifyPrompt(envs []platform.ProjectEnvVar) []platform.ProjectEnvVar {
	out := make([]platform.ProjectEnvVar, 0, len(envs))
	for _, env := range envs {
		if envclass.ClassifyProjectEnv(env).Decision == envclass.PromptUser {
			out = append(out, env)
		}
	}
	return out
}

// launchNeedsClassifyPrompt reports whether the classify-prompt status
// must fire. True when at least one envclass-PromptUser env lacks a
// user-supplied classification. Drop-decision envs (SYSTEM scope) are
// already satisfied — they never reach the composer.
func launchNeedsClassifyPrompt(classifications map[string]string, envs []platform.ProjectEnvVar) bool {
	if len(envs) == 0 {
		return false
	}
	for _, env := range envs {
		if envclass.ClassifyProjectEnv(env).Decision != envclass.PromptUser {
			continue
		}
		if _, ok := classifications[env.Key]; !ok {
			return true
		}
	}
	return false
}

// launchBundleProjectEnvs converts the platform-shaped source envs into
// the lossy bundle composer input (`{Key, Value}` only). Drops envs the
// classifier marks as Drop (project SYSTEM, etc.) — keeps the composer
// + SourceSnapshot digest focused on user-controlled values only.
func launchBundleProjectEnvs(envs []platform.ProjectEnvVar) []ops.ProjectEnvVar {
	out := make([]ops.ProjectEnvVar, 0, len(envs))
	for _, env := range envs {
		if envclass.ClassifyProjectEnv(env).Decision != envclass.PromptUser {
			continue
		}
		out = append(out, ops.ProjectEnvVar{Key: env.Key, Value: env.Content})
	}
	return out
}
