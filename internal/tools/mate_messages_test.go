package tools

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/hq"
)

var messagePeers = []hq.Peer{
	{ProjectID: "p-shop", ProjectName: "shop - Rhea", MateName: "Rhea", Awake: true, Receives: true, SignedIn: true},
	{ProjectID: "p-blog", ProjectName: "blog - Milo", MateName: "Milo", Receives: true},
	{ProjectID: "p-docs", ProjectName: "docs - Milo", MateName: "Milo", Awake: true, Receives: true, SignedIn: true},
}

// witness: internal/tools/mate_messages.go | strings.EqualFold(p.MateName, to) → p.MateName == to
func TestResolvePeer_MateNameInAnyCase_NamesThatMate(t *testing.T) {
	t.Parallel()
	peer, refusal := resolvePeer(messagePeers, " rhea ")
	if refusal != "" || peer.ProjectID != "p-shop" {
		t.Errorf("resolvePeer = %+v, %q; want p-shop and no refusal", peer, refusal)
	}
}

// witness: internal/tools/mate_messages.go | case len(matches) == 1: → case len(matches) >= 1:
func TestResolvePeer_NameSeveralMatesGoBy_SendsToNone(t *testing.T) {
	t.Parallel()
	peer, refusal := resolvePeer(messagePeers, "Milo")
	if refusal == "" || peer.ProjectID != "" {
		t.Errorf("resolvePeer = %+v, %q; want no Mate and a refusal listing the candidates", peer, refusal)
	}
}

// witness: internal/tools/mate_messages.go | if chief != "" && p.ProjectID == chief { → if chief != "" && p.ProjectID != chief {
func TestListMates_PersonsChiefAmongThem_IsNamedTheChief(t *testing.T) {
	t.Parallel()
	for _, line := range strings.Split(listMates(messagePeers, "p-blog"), "\n")[1:] {
		if got, want := strings.Contains(line, "Chief"), strings.HasSuffix(line, "projectId p-blog"); got != want {
			t.Errorf("line %q names the Chief = %v, want %v", line, got, want)
		}
	}
}
