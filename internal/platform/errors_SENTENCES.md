# Error class (unit): zcp's one class of a failure — whether waiting can change it

Rule: zeropsio/zcp failures reach the agent with the source's own words and one class (`unreachable`, `refused`,
`failed`, or none for an arbitrary command's own exit); only `unreachable` is waited on, bounded. Owner:
`internal/platform/errors.go` (`ClassOf`, `WaitedOn`, `SSHExitCode`); its wait consumer `internal/ops/ssh_ready.go`.
Cases E1, E2, E4–E6, E10–E12 live in `internal/platform/errors_class_test.go`; E3 and E7 in `internal/ops/ssh_ready_test.go`.

E1 — ssh's own exit 255 with "Connection refused" (a container stopping or starting) is unreachable, so a wait waits on it.
E2 — ssh's own exit 255 whose connect timed out (a container stopped or restarting) is unreachable, so a wait waits on it (lead decision 2026-11-16 22:35: a wrong wait costs one bound, a wrong refusal tells the agent to give up).
E3 — The readiness probe's non-255 exit (the platform's SSH admission refusal, exit 2) is refused and returned at once, with the platform's words.
E4 — An arbitrary command's own non-255 exit is passed on unclassified: zcp calls it neither unreachable, refused nor failed.
E5 — The platform's 5xx and 429 answers are unreachable, so a wait waits on them.
E6 — The platform's 403 is refused: no wait spends its bound on it.
E7 — A readiness wait whose bound runs out says it waited the bound and carries the last cause verbatim.
E8 — Every tool error the agent receives carries the failure's class and, for an ssh failure, ssh's whole output (case in `internal/tools/convert_test.go`).
E9 — HQ not serving (no answer, 5xx, 429) is unreachable, and HQ's other no (a 4xx) is refused, read through ClassOf (case in `internal/hq/client_class_test.go`).
E10 — A platform call whose connection ended mid-answer (EOF, unexpected EOF) is unreachable, so a wait waits on it.
E11 — A platform call whose TLS handshake timed out is unreachable, so a wait waits on it.
E12 — zcp's own refusal of its input (a PlatformError the API did not answer) is not called refused: only the API's own 4xx is.

E13 — A refusal of the platform that names fields keeps them in its words: each field and its reason follow the message. Breaks: "Invalid parameter provided." alone reaches the agent and the person, who cannot tell what to change.
