package telemetry

import (
	"fmt"
	"io"
	"time"
)

// TODO(owner): confirm the legal controller entity + a real privacy contact
// address before any public release.
const (
	disclosureController = "Zerops s.r.o."
	disclosureContact    = "privacy@zerops.io"
)

// FullDisclosure writes the long-form GDPR Article 13 transparency notice
// for `zcp telemetry disclosure` (spec §3.5). Unlike disclosureNotice in
// config.go (the one-line notice shown once per install before the first
// event, spec §3.3/B5), this is the on-demand full text: every Art-13
// element in its own labelled section, plain text wrapped for a terminal.
func FullDisclosure(w io.Writer) {
	fmt.Fprintf(w, `ZCP TELEMETRY — FULL DISCLOSURE NOTICE

WHAT IS COLLECTED
  Anonymous usage events, each carrying only:
    - tool name, command, action (which ZCP tool/subcommand ran)
    - duration (ms), success (bool), error code + optional error subcode
    - workflow route and workflow step (which flow stage, if any)
    - a small fixed set of enum "dims" (closed value sets — never free text)
    - os, arch, zcp version
    - two random UUIDs: an install id (per machine) and a session id
      (per process) — neither is derived from anything identifying you

WHAT IS NEVER COLLECTED
  - command arguments, free text, or file paths
  - hostnames
  - IP addresses — used only in-memory for rate-limiting at the ingest
    edge, never stored or logged
  - any account, project, or user identifier

WHY / LEGAL BASIS
  Legitimate interest (GDPR Art 6(1)(f)): understanding real usage to
  improve the product. The data is anonymous by construction, so it
  cannot identify you or your organization.

OPT-IN / OPT-OUT
  Telemetry is OFF by default. It sends data ONLY when you explicitly set
  ZCP_TELEMETRY=1. Turn it off again with ZCP_TELEMETRY=0 or
  DO_NOT_TRACK=1 — or run "zcp telemetry disable" to record a persistent
  opt-out that survives even if ZCP_TELEMETRY=1 is set again later.

RETENTION
  Raw events auto-expire after 15 months. Aggregate statistics (which
  carry no per-install identity) are kept indefinitely.

RECIPIENTS / TRANSFERS
  None. Data goes only to the ZCP maintainers' internal telemetry
  pipeline. It is not sold and not shared with third parties.

YOUR RIGHTS
  You have the right to access and erasure. Run "zcp telemetry id" to
  print your install id, then request deletion via the contact below.
  You may also lodge a complaint with your data-protection authority.

CONTROLLER & CONTACT
  Controller: %s
  Contact:    %s

  For the full text again, run "zcp telemetry disclosure".
`, disclosureController, disclosureContact)
}

// Enable clears the install file's disabled flag and stamps disclosure if
// this install has never been disclosed before (spec §3.5 "enable — clears
// disabled, stamps disclosure"). Unlike Resolve's automatic pre-disclosure
// stamp (spec §3.3, which withholds telemetry for the REST of that
// process), Enable is a direct, explicit user action — the caller is
// expected to show the disclosure notice (PrintDisclosureNotice) alongside
// this call, since consent is happening right now, not "starting next run".
func Enable(homeDir string, internalChannel bool, now time.Time) error {
	path := installFilePath(homeDir, internalChannel)
	f, exists, err := loadInstallFile(path)
	if err != nil {
		return fmt.Errorf("read install file: %w", err)
	}
	if !exists {
		id, err := newUUIDv4()
		if err != nil {
			return fmt.Errorf("mint install id: %w", err)
		}
		f = installFile{InstallID: id}
	}
	f.Disabled = false
	if f.DisclosedAt == "" {
		f.DisclosedAt = now.UTC().Format(time.RFC3339)
	}
	if err := writeInstallFileAtomic(path, f); err != nil {
		return fmt.Errorf("write install file: %w", err)
	}
	return nil
}

// Disable sets the install file's disabled flag (spec §3.5 "disable — sets
// disabled: true (keeps installId; rows purgeable on request)"). Existing
// installId/disclosedAt are preserved untouched — installId stays the
// erasure-request key even while opted out. Safe to call with no prior
// install file (pre-emptive opt-out): writes a disabled record with no id.
func Disable(homeDir string, internalChannel bool) error {
	path := installFilePath(homeDir, internalChannel)
	f, _, err := loadInstallFile(path)
	if err != nil {
		return fmt.Errorf("read install file: %w", err)
	}
	f.Disabled = true
	if err := writeInstallFileAtomic(path, f); err != nil {
		return fmt.Errorf("write install file: %w", err)
	}
	return nil
}

// InstallIDOf reads the install UUID for `zcp telemetry id` (spec §3.5 —
// "the erasure-request key").
func InstallIDOf(homeDir string, internalChannel bool) (string, error) {
	path := installFilePath(homeDir, internalChannel)
	f, exists, err := loadInstallFile(path)
	if err != nil {
		return "", fmt.Errorf("read install file: %w", err)
	}
	if !exists || f.InstallID == "" {
		return "", fmt.Errorf("no install id yet — telemetry has not been enabled on this machine")
	}
	return f.InstallID, nil
}
