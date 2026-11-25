# Dev-server answers (`dev_server.go`, `dev_server_supervisor.go`)

- DS-STARTING — A start, restart or status of a server the supervisor reports starting with its start under way or its port still awaited answers `starting` with when to read it again, never a failed start, so an agent waits for a dev server that compiles for 90 s instead of restarting it.
- DS-STALLED — A server whose listen wait the supervisor reports run out with its healthPath never answering answers `start_stalled` with the run's log, to fix and restart, never "check again": the supervisor waits for its answer no more, so the wait would never end.
- DS-LISTENED — A status of a server whose healthPath answered since its start and whose port refuses now probes it as a running one and answers what the probe found, never `starting`: a server whose watcher outlived its crashed child is no start to wait for.
- DS-PORT-ELSEWHERE — A server whose port is still awaited while its process listens on other ports only answers `port_elsewhere`, naming those ports, so a wrong declared port is fixed at once instead of waited for until the listen wait runs out.
- DS-NEWLOG — A status carries at most 40 of the lines a server's log gained, at most 30 of them error lines put first, so an error under a burst of hot-reload lines is never cut off and the newest lines — a watcher's recovery — always have room, and it counts the lines it leaves out.
- DS-ERROR-SHAPE — A log line is an error line only in an error's shape, never because a file name holds the word or a test summary counts none failed, so a designer editing FormErrors.tsx is never told of errors.
- DS-NEWLOG-RUN — A status carries only the lines of the server's current run; the lines an earlier run wrote since the previous status are counted as omitted, so an error a restart already fixed never reads as current.
- DS-UNSAVED — A status of a lease carries what releasing the lease would lose, so the person sees that the Preview holds work no change has yet.
