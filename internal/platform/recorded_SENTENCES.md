# Recorded Zerops reads (unit): zcp's client decodes what Zerops answered

The recordings in `e2e/recordings/` are Zerops's own answers, written by their writer `e2e/record_test.go`
(TestRecord) and never edited; one that no longer decodes is recorded again. Owner: `internal/platform/zerops.go`
(the reads), `internal/platform/zerops_mappers.go` (their mappers). The case lives in `recorded_test.go`.

- RC-DECODE — Every recording in `e2e/recordings/` decodes through ZeropsClient's own reads into the user, the project and every recorded service with its hostname, status and ports as the recording holds them. Breaks: a mapper that drops or misreads a field leaves it empty in every tool, unseen until a live run.
