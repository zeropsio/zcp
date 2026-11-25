# Log reads (`state.go`)

- LG-SINCE — A log read since a cursor answers only the complete lines written after the earlier read, so a status never repeats a line nor cuts one still being written.
- LG-OTHER-FILE — A cursor of another log file (one rotated away, a new container behind the same alias) reads the log from its start, never from that file's offset.
- LG-LONG-HEAD — A log whose first line is longer than the part that names the file is still named by that part, so a status after it reads only the lines written since, never the whole log again.
