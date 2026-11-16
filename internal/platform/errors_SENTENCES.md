# Error class (unit): zcp's one class of a failure — whether waiting can change it

Rule: zeropsio/zcp failures reach the agent with the source's own words and one class (`unreachable`, `refused`,
`failed`, or none for an arbitrary command's own exit); only `unreachable` is waited on, bounded. Owner:
`internal/platform/errors.go` (`ClassOf`, `WaitedOn`, `SSHExitCode`); its wait consumer `internal/ops/ssh_ready.go`.
Cases E1, E2, E4–E6 live in `internal/platform/errors_class_test.go`; E3 and E7 in `internal/ops/ssh_ready_test.go`.

E1 — ssh's own exit 255 with "Connection refused" (a container stopping or starting) is unreachable, so a wait waits on it.
E2 — ssh's own exit 255 whose connect timed out (no container answers the address) is refused: no wait spends its bound on it.
E3 — The readiness probe's non-255 exit (the platform's SSH admission refusal, exit 2) is refused and returned at once, with the platform's words.
E4 — An arbitrary command's own non-255 exit is passed on unclassified: zcp calls it neither unreachable, refused nor failed.
E5 — The platform's 5xx and 429 answers are unreachable, so a wait waits on them.
E6 — The platform's 403 is refused: no wait spends its bound on it.
E7 — A readiness wait whose bound runs out says it waited the bound and carries the last cause verbatim.
E8 — Every tool error the agent receives carries the failure's class and, for an ssh failure, ssh's whole output (case in `internal/tools/convert_test.go`).
E9 — HQ not serving (no answer, 5xx, 429) is unreachable, and HQ's other no (a 4xx) is refused, read through ClassOf (case in `internal/hq/client_class_test.go`).
