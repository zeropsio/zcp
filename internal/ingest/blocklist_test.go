package ingest

import "testing"

func TestBlocklist_MatchesExactValues(t *testing.T) {
	t.Parallel()

	bl := newBlocklist([]string{"203.0.113.1", "203.0.113.2"}, []string{"11111111-1111-4111-8111-111111111111"})

	if !bl.blockedIP("203.0.113.1") {
		t.Error("blockedIP(203.0.113.1) = false, want true")
	}
	if bl.blockedIP("203.0.113.9") {
		t.Error("blockedIP(203.0.113.9) = true, want false")
	}
	if !bl.blockedInstall("11111111-1111-4111-8111-111111111111") {
		t.Error("blockedInstall = false, want true")
	}
	if bl.blockedInstall("22222222-2222-4222-8222-222222222222") {
		t.Error("blockedInstall = true for an unlisted install, want false")
	}
}

func TestBlocklist_EmptyBlocksNothing(t *testing.T) {
	t.Parallel()

	bl := newBlocklist(nil, nil)

	if bl.blockedIP("203.0.113.1") {
		t.Error("an empty blocklist blocked an IP")
	}
	if bl.blockedInstall("11111111-1111-4111-8111-111111111111") {
		t.Error("an empty blocklist blocked an install")
	}
}

func TestBlocklist_CanonicalizesConfiguredIP(t *testing.T) {
	t.Parallel()

	// A block entry configured in a non-canonical IPv6 spelling must still
	// match the canonical key clientIP() produces (both go through net/netip).
	bl := newBlocklist([]string{"2001:0db8:0000:0000:0000:0000:0000:0001"}, nil)

	if !bl.blockedIP("2001:db8::1") {
		t.Error("blockedIP(canonical) = false — a non-canonical configured entry did not match the canonical request key")
	}
}

func TestBlocklist_BlankEntriesIgnored(t *testing.T) {
	t.Parallel()

	bl := newBlocklist([]string{"", "  ", "203.0.113.1"}, nil)

	if bl.blockedIP("") {
		t.Error("blank entries must never match")
	}
	if !bl.blockedIP("203.0.113.1") {
		t.Error("blockedIP(203.0.113.1) = false, want true")
	}
}
