package hq

import (
	"reflect"
	"strconv"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

const anchor = "mate-hq:hq1:https://hq-30db-8080.prg1.zerops.app"

func token(name, role, status string) platform.OrgMember {
	return platform.OrgMember{
		ID: "member-" + name, UserID: "user-" + name, RoleCode: role, Status: status,
		Email: "token-" + strconv.Itoa(len(name)) + "@zerops.io", FullName: name,
	}
}

func person(name, role string) platform.OrgMember {
	return platform.OrgMember{
		ID: "member-" + name, UserID: "user-" + name, RoleCode: role, Status: "ACTIVE",
		Email: "ada@example.com", FullName: name,
	}
}

// The verdict is the client's own (client-runtime hq/anchor.ts findOfficialHq),
// case for case.
func TestFindOfficial_MemberList_Verdict(t *testing.T) {
	t.Parallel()

	official := Official{Verdict: VerdictOfficial, ProjectID: "hq1", Address: "https://hq-30db-8080.prg1.zerops.app"}
	tests := []struct {
		name    string
		members []platform.OrgMember
		want    Official
	}{
		{"no anchor at all", []platform.OrgMember{person("Ada", "OWNER")}, Official{Verdict: VerdictNone}},
		{"one active Admin token anchor", []platform.OrgMember{person("Ada", "OWNER"), token(anchor, "ADMIN", "ACTIVE")}, official},
		{"an anchor's trailing slash is not part of the address", []platform.OrgMember{token(anchor+"/", "ADMIN", "ACTIVE")}, official},
		{"a mate-hq name below Admin is no anchor", []platform.OrgMember{token(anchor, "READ_ONLY", "ACTIVE")}, Official{Verdict: VerdictNone}},
		{"the working token is no anchor", []platform.OrgMember{token("mate-hq-org:hq1", "ADMIN", "ACTIVE")}, Official{Verdict: VerdictNone}},
		{
			"anchors naming two projects: no HQ is official",
			[]platform.OrgMember{token(anchor, "ADMIN", "ACTIVE"), token("mate-hq:hq2:https://hq-9-8080.prg1.zerops.app", "ADMIN", "ACTIVE")},
			Official{Verdict: VerdictUnclear, ProjectIDs: []string{"hq1", "hq2"}},
		},
		{
			"a person named like an anchor still names a project, and is no anchor itself",
			[]platform.OrgMember{person(anchor, "ADMIN")},
			Official{Verdict: VerdictUnclear, ProjectIDs: []string{"hq1"}},
		},
		{
			"an anchor that is not active yet",
			[]platform.OrgMember{token(anchor, "ADMIN", "PENDING")},
			Official{Verdict: VerdictUnclear, ProjectIDs: []string{"hq1"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := FindOfficial(tt.members); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("FindOfficial = %+v, want %+v", got, tt.want)
			}
		})
	}
}
