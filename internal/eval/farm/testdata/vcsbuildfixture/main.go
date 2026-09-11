// Package main is a trivial fixture binary for
// cmd/zcp/eval_farm_test.go's candidate-info push tests. It exists only to
// be `go build`-able from inside this repo's git working tree so the
// resulting binary carries real vcs.revision/vcs.time/vcs.modified
// settings in its embedded debug/buildinfo — a fabricated file body
// cannot exercise the real debug/buildinfo.ReadFile path at all. Under
// "testdata/", it is skipped by every "./..." build/vet/lint sweep and
// built only by the test that names it explicitly by import path.
package main

func main() {}
