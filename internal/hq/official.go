// Package hq is zcp's side of a Mate's HQ: which HQ is the org's official
// one, and the Mate credential HQ issues this container once it has proved,
// through its own project's env, that it controls the project — without its
// Zerops key ever leaving the container.
package hq

import (
	"regexp"
	"slices"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
)

// anchorPrefix starts the anchor's name: an integration token with the org
// role Admin, named mate-hq:<projectId>:<address>, its value dropped the
// moment it was minted.
const anchorPrefix = "mate-hq:"

// tokenEmail is the address the member list gives an integration token.
var tokenEmail = regexp.MustCompile(`(?i)^token-[^@]+@zerops\.io$`)

// Verdict is what the org's member list says of its official HQ.
type Verdict string

const (
	// VerdictNone: no Admin member names an HQ.
	VerdictNone Verdict = "none"
	// VerdictOfficial: one project is named, by an active Admin token.
	VerdictOfficial Verdict = "official"
	// VerdictUnclear: Admin names point at more than one project, or none
	// of them is an active token yet. Nothing enrolls with any of them.
	VerdictUnclear Verdict = "unclear"
	// VerdictUnknown: the member list could not be read.
	VerdictUnknown Verdict = "unknown"
)

// Official is the org's official HQ as its member list names it.
type Official struct {
	Verdict    Verdict  `json:"verdict"`
	ProjectID  string   `json:"projectId,omitempty"`
	Address    string   `json:"address,omitempty"`
	ProjectIDs []string `json:"projectIds,omitempty"`
}

// FindOfficial reads the anchor off the org's member list, the one list
// every member reads in full (a Developer's token list does not show it).
// The verdict is the client's own (client-runtime hq/anchor.ts).
func FindOfficial(members []platform.OrgMember) Official {
	type named struct {
		member             platform.OrgMember
		projectID, address string
	}
	var anchors []named
	for _, m := range members {
		if m.RoleCode != "ADMIN" || !strings.HasPrefix(m.FullName, anchorPrefix) {
			continue
		}
		projectID, address, _ := strings.Cut(strings.TrimPrefix(m.FullName, anchorPrefix), ":")
		anchors = append(anchors, named{m, projectID, strings.TrimRight(address, "/")})
	}
	if len(anchors) == 0 {
		return Official{Verdict: VerdictNone}
	}
	var projectIDs []string
	for _, a := range anchors {
		if !slices.Contains(projectIDs, a.projectID) {
			projectIDs = append(projectIDs, a.projectID)
		}
	}
	slices.Sort(projectIDs)
	if len(projectIDs) == 1 {
		for _, a := range anchors {
			if a.member.Status == "ACTIVE" && tokenEmail.MatchString(a.member.Email) && a.address != "" {
				return Official{Verdict: VerdictOfficial, ProjectID: a.projectID, Address: a.address}
			}
		}
	}
	return Official{Verdict: VerdictUnclear, ProjectIDs: projectIDs}
}
