package skillpacks

import (
	"errors"
	"fmt"
)

// Stable error codes surfaced through Result.Code — the CLI's --json mode
// emits these verbatim, and the (currently separate, JS-side) welcome panel
// maps each to a fixed row message. A code is never invented ad hoc at a
// call site; every failure path constructs a *CodedError with one of these.
const (
	CodeUnknownPack    = "unknown-pack"
	CodeBusy           = "busy"
	CodeGitMissing     = "git-missing"
	CodeDownloadFailed = "download-failed"
	CodeInvalidSource  = "invalid-source"
	CodeNoSkills       = "no-skills"
	CodeCollision      = "collision"
	CodeLocalChanges   = "local-changes"
	CodeIncomplete     = "incomplete"
	CodeLegacyState    = "legacy-state"
	CodeCorruptState   = "corrupt-state"
	CodePermission     = "permission"
	CodeFilesystem     = "filesystem"
	CodeInternal       = "internal"
)

// CodedError pairs a stable Code with a safe, human-readable Message and
// (optionally) the underlying cause — every deliberate failure path in this
// package returns one, so Add/Remove/Status can always populate Result.Code
// without guessing from an error string.
type CodedError struct {
	Code    string
	Message string
	Err     error
}

func (e *CodedError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Err)
	}
	return e.Message
}

func (e *CodedError) Unwrap() error { return e.Err }

// codedErrorf builds a *CodedError whose Message is fmt.Sprintf(format,
// args...) and whose Err is nil — used for a failure that has no further
// wrapped cause to preserve.
func codedErrorf(code, format string, args ...any) *CodedError {
	return &CodedError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// wrapCoded builds a *CodedError that wraps an existing error under a fixed
// code and message.
func wrapCoded(code string, err error, format string, args ...any) *CodedError {
	return &CodedError{Code: code, Message: fmt.Sprintf(format, args...), Err: err}
}

// codeAndMessage extracts the (code, message) pair the CLI's JSON output
// reports for err: a *CodedError's own fields, or CodeInternal with the
// error's own text for anything unexpected.
func codeAndMessage(err error) (code, message string) {
	var ce *CodedError
	if errors.As(err, &ce) {
		return ce.Code, ce.Message
	}
	return CodeInternal, err.Error()
}
