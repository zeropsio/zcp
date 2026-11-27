# Working copy state (`copy_state.go`)

- CS-TRANSITIONS — Lease and supervisor events move a copy only along the allowed transitions; an authoritative report may supersede unseen intermediate events, and park or release also wins after a lost link. A released copy never returns.
