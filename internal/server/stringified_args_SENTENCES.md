# Stringified tool arguments (`stringified_args.go`)

- SA-1 — An object or array sent as JSON text runs once decoded, a string property keeps its text, and a bare word where an object belongs is refused without running the handler. Breaks: a Mate whose client stringifies a workflow's arguments cannot run it.
- SA-2 — A number or boolean sent as JSON text where the SDK refuses the property's type is retried as the original JSON token; string properties and other refusals stay unchanged. Breaks: a Mate on such a client can never read its failing check's log, scale a service or bound a log read.
