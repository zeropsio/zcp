package tools

import (
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
