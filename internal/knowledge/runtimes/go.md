# Go on Zerops

## Keywords
go, golang, binary, cgo, zerops.yml, go.sum, go mod tidy

## TL;DR
Go compiles to a single binary. Deploy only the binary. NEVER set `run.base: alpine`. NEVER write go.sum by hand -- let `go mod tidy` handle it.

### Base Image

Includes Go compiler, `git`, `wget`.

**Build != Run**: compiled binary -- deploy only binary, no `run.base` needed (omit it).

### Build Procedure

1. Set `build.base: go@latest`
2. `buildCommands`: ALWAYS use `go mod tidy` before build:
   `go mod tidy && go build -o app .`
3. `deployFiles: app` (single binary)
4. `run.start: ./app`

### Binding

Default `:port` binds all interfaces (correct, no change needed).

### Critical Rules

**NEVER set `run.base: alpine@*`** -- causes glibc/musl mismatch for CGO-linked binaries -> 502. Omit `run.base` or use `run.base: go@latest`.

**NEVER write go.sum by hand** -- checksums will be wrong, build fails with `checksum mismatch`. Let `go mod tidy` in buildCommands generate it.

**Do NOT include go.sum in source** when creating new apps -- `go mod tidy` in buildCommands handles it.

### CGO

Requires `os: ubuntu` + `CGO_ENABLED=1`. When unsure: `CGO_ENABLED=0 go build` for static binary.

### Key Settings

Logger MUST output to `os.Stdout`.
Cache: `~/go` (auto-cached).

### Deploy Patterns

**Dev deploy**: `deployFiles: [.]`, `start: zsc noop --silent` (idle container -- agent starts `go run .` manually via SSH for iteration)
**Prod deploy**: `buildCommands: [go mod tidy && go build -o app .]`, `deployFiles: app`, `start: ./app`
