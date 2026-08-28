package topology

import "strings"

// gitignoreBaseLines is the language-agnostic .gitignore body every
// host-side git init writes, regardless of the service's runtime type:
// OS noise (.DS_Store) and ZCP's own on-host artifacts that must never be
// committed — `.env` (deploy env materialized to disk, spec-env-handling)
// and `.zcp/` (gitignored project meta: dotenv locks, guided-mode marker,
// skill-pack state — internal/ops/env_dotenv_lock.go, internal/content/
// guided.go). `*.log` covers the generic case every runtime accumulates.
var gitignoreBaseLines = []string{".env", ".zcp/", ".DS_Store", "*.log"}

// gitignoreLanguageBlock is one runtime-family's extra ignore lines, keyed
// by the bare (OS-prefix-and-mode-stripped) type prefix it applies to.
type gitignoreLanguageBlock struct {
	prefix string
	lines  []string
}

// gitignoreLanguageBlocks maps a normalized runtime-type prefix to the
// extra lines a fresh service of that family needs beyond the base set.
// Deliberately conservative: each entry lists only directories a normal
// build/install step regenerates AND that are large enough on a dev box
// that staging them by accident (e.g. an external tool's `git add -A`
// checkpoint before the user has written their own .gitignore) turns a
// routine git operation from sub-second to minutes — the measured failure
// mode this mapping exists to prevent (z3 S0.3/S0.4/S0.13: node_modules
// staged over an SSHFS mount, 245s).
//
// bun/deno are JavaScript runtimes on Zerops and share node's entry —
// their local caches/build output land in the same directory shapes.
//
// go is deliberately OMITTED: unlike node/python/rust/dotnet/java/ruby,
// Go has no single conventional build-output directory a fresh service
// always regenerates (`bin/`/`build/`-style output is a project
// convention, not a toolchain default) — guessing wrong risks ignoring a
// directory a project genuinely commits to. go services get the base
// lines only until a real pattern justifies a specific one.
//
// Ordered (not a map) so iteration is deterministic and the first
// matching prefix wins — load-bearing only for the case where one
// family's prefix is itself a prefix of another entry (php vs a
// hypothetical php-something); none of the current entries collide, but
// keeping this ordered means adding one later can't introduce
// map-iteration nondeterminism.
var gitignoreLanguageBlocks = []gitignoreLanguageBlock{
	{prefix: "nodejs", lines: []string{"node_modules/", "dist/", ".next/", ".nuxt/", ".output/"}},
	{prefix: "bun", lines: []string{"node_modules/", "dist/", ".next/", ".nuxt/", ".output/"}},
	{prefix: "deno", lines: []string{"node_modules/", "dist/", ".next/", ".nuxt/", ".output/"}},
	{prefix: "python", lines: []string{"__pycache__/", ".venv/", "venv/"}},
	{prefix: "php", lines: []string{"vendor/"}}, // covers php-apache, php-nginx via prefix match
	{prefix: "rust", lines: []string{"target/"}},
	{prefix: "dotnet", lines: []string{"bin/", "obj/"}},
	{prefix: "java", lines: []string{"target/", "build/"}},
	{prefix: "ruby", lines: []string{"vendor/bundle/"}},
}

// GitignoreFor returns the .gitignore body — one entry per line, in commit
// order — a fresh host-side git init should write for serviceType: the
// small base set every service needs (see gitignoreBaseLines) plus the
// per-language block for the service's runtime family, if recognized.
//
// serviceType is a raw Zerops type identifier in either legacy bare form
// (`nodejs@22`) or post-Sunday-release composite form (`alpine/nodejs@22`,
// `ubuntu/php-nginx@8.4`); CanonicalBareForm normalizes both before the
// prefix match. An empty, managed-service, or unrecognized serviceType
// returns the base lines only — conservative by construction: nothing
// this function emits is ever WRONG to ignore, only sometimes incomplete
// for a language this mapping does not yet special-case.
//
// Single source of truth for every host-side git-init self-heal site
// (ops.GitEnsureRepoHeadCommand, BuildGitOriginSyncCommand,
// BuildGitReconstructCommand) — never duplicate this list at a call site.
func GitignoreFor(serviceType string) []string {
	lines := make([]string, 0, len(gitignoreBaseLines)+5)
	lines = append(lines, gitignoreBaseLines...)

	bare := strings.ToLower(CanonicalBareForm(serviceType))
	for _, block := range gitignoreLanguageBlocks {
		if bare != "" && strings.HasPrefix(bare, block.prefix) {
			lines = append(lines, block.lines...)
			break
		}
	}
	return lines
}
