# Pool stage deploy source

- PD-CHOICE — A stage deploy chooses its pool's sole held lease only when verified; with several reported held, it names none and lists the choices with their verification state, and with none or a sole unverified one it asks for a lease. Breaks: a deploy takes another branch's work or asks for a new lease when the work already has one.
