# Review build

- RB-TRIGGER — Only a ready, open code change without a passing or failing Review build at its head starts or resumes its Review build.
- RB-TAKE — A failed take is tried again; a result already accepted by HQ is never overwritten by a take from stale state.
- RB-DEADLINE — A resumed take keeps its original deadline and reserves the HQ write's call budget; a new or expired take gets the current run's budget.
- RB-FLIGHT — A stage build already running for the ready head is joined after a restart.
- RB-ACTIVE — An active stage version identified as the ready head is probed without another build.
- RB-FAILED — An identified failed version of the ready head is reported instead of built again.
- RB-AGAIN — A stage version whose identity cannot be read is built again, never treated as the ready head.
- RB-STAGES — A Review build covers every recorded stage of the change's repository and writes nothing for a repository with none.
- RB-SETUP — A stage whose setup is not recorded takes the setup from its repository's stand-up tier; a missing row is not guessed.
- RB-RETRY — An unavailable Review build is retried after one, five and fifteen minutes and no fourth time.
- RB-MOVED — A spent retry clock starts again when its change moves, not when its own result or an older state frame arrives.
- RB-RESULT — One result includes all stage parts; a failed part prevents a mixed failed and unavailable result from being retried as unavailable.
- RB-OBSERVE — An identified private stage already active at the ready head returns that version without starting another build.
- RB-RESUME — A named in-flight head is waited to its result and returned without starting another build.
- RB-UNAVAILABLE — A stage lock or build wait that ends without a verdict is unavailable, with the step and its last error line.
- RB-HEALTH — A public stage's probe uses its setup's declared health path, including an inherited path, or the default when none is declared.
- RB-DEVWATCH — The dev git observer reads a replacement after its state watch ends and is opened again, or uses its retry wake when reopening fails.
- RB-WATCH — The Review observer reads a replacement after its state watch ends and is opened again, or uses its stand-up wake when reopening fails.
