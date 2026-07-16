package provider

// ActionID names an operation-level Data Console affordance. The Go-owned set
// is exported to the SPA contract; app.js may reference only these IDs.
type ActionID string

const (
	ActionReadBlob     ActionID = "readBlob"
	ActionWriteBlob    ActionID = "writeBlob"
	ActionDeleteNode   ActionID = "deleteNode"
	ActionRenameObject ActionID = "renameObject"
	ActionUploadObject ActionID = "uploadObject"
	ActionQuerySQL     ActionID = "querySQL"
	ActionReadTable    ActionID = "readTable"
	ActionEditCell     ActionID = "editCell"
	ActionInsertRow    ActionID = "insertRow"
	ActionDeleteRow    ActionID = "deleteRow"
	ActionEditKVEntry  ActionID = "editKVEntry"
	ActionSetTTL       ActionID = "setTTL"
	ActionShowVPNGate  ActionID = "showVPNGate"
)

const (
	reasonReadOnlySession = "session is read-only"
	reasonViewOnly        = "service is view-only"
	reasonNotYet          = "service is not yet supported"
	reasonNotAvailable    = "action is not available for this service"
	vpnGateReason         = "This managed service is on the project's private network; bring up the VPN if it is unreachable."
)

var allActionIDs = []ActionID{
	ActionReadBlob,
	ActionWriteBlob,
	ActionDeleteNode,
	ActionRenameObject,
	ActionUploadObject,
	ActionQuerySQL,
	ActionReadTable,
	ActionEditCell,
	ActionInsertRow,
	ActionDeleteRow,
	ActionEditKVEntry,
	ActionSetTTL,
	ActionShowVPNGate,
}

// AllActionIDs returns the ordered Go-owned action id set.
func AllActionIDs() []ActionID {
	out := make([]ActionID, len(allActionIDs))
	copy(out, allActionIDs)
	return out
}

// ServiceActions is the connection-free single owner of service-level
// affordances. It derives only from classifier family/support and the immutable
// launch posture; live provider Caps() can still refine a specific opened table
// or blob, but the SPA policy for service operations comes from this descriptor
// list and nowhere else.
func ServiceActions(fam Family, sup Support, allowWrites bool) []Action {
	reads := actionSet(familyReadActionIDs(fam))
	mutations := actionSet(familyMutatingActionIDs(fam))
	out := make([]Action, 0, len(reads)+len(mutations)+1)
	for _, id := range allActionIDs {
		switch {
		case reads[id]:
			out = append(out, readAction(id, sup))
		case mutations[id]:
			out = append(out, mutatingAction(id, sup, allowWrites))
		case id == ActionShowVPNGate && vpnGateFamily(fam) && sup != SupportNotYet:
			out = append(out, Action{ID: id, Enabled: true, ReadOnly: true, Reason: vpnGateReason})
		}
	}
	return out
}

func readAction(id ActionID, sup Support) Action {
	enabled := sup != SupportNotYet
	reason := ""
	if !enabled {
		reason = reasonNotYet
	}
	return Action{ID: id, Enabled: enabled, ReadOnly: true, Reason: reason}
}

func mutatingAction(id ActionID, sup Support, allowWrites bool) Action {
	enabled := allowWrites && sup == SupportFull
	reason := ""
	if !enabled {
		reason = disabledMutationReason(sup, allowWrites)
	}
	return Action{ID: id, Enabled: enabled, ReadOnly: false, Reason: reason}
}

func disabledMutationReason(sup Support, allowWrites bool) string {
	switch {
	case sup == SupportNotYet:
		return reasonNotYet
	case sup == SupportViewOnly:
		return reasonViewOnly
	case !allowWrites:
		return reasonReadOnlySession
	default:
		return reasonNotAvailable
	}
}

func familyReadActionIDs(fam Family) []ActionID {
	switch fam {
	case FamilyObject, FamilyDocument, FamilyStream:
		return []ActionID{ActionReadBlob}
	case FamilyTabular:
		return []ActionID{ActionQuerySQL, ActionReadTable}
	case FamilyKV:
		return []ActionID{ActionReadBlob, ActionReadTable}
	case FamilyFile, FamilyUnknown:
		return nil
	default:
		return nil
	}
}

func familyMutatingActionIDs(fam Family) []ActionID {
	switch fam {
	case FamilyObject:
		return []ActionID{ActionWriteBlob, ActionDeleteNode, ActionRenameObject, ActionUploadObject}
	case FamilyTabular:
		return []ActionID{ActionEditCell, ActionInsertRow, ActionDeleteRow}
	case FamilyKV:
		return []ActionID{ActionWriteBlob, ActionDeleteNode, ActionEditKVEntry, ActionSetTTL}
	case FamilyDocument:
		return []ActionID{ActionWriteBlob, ActionDeleteNode}
	case FamilyStream, FamilyFile, FamilyUnknown:
		return nil
	default:
		return nil
	}
}

// MutatingActionIDs returns the ordered set of action IDs that mutate data — the
// single-owner definition of "which actions write". It builds the per-family
// mutating sets above AND (in package server) pins that every route with
// mutating:true carries one of these action IDs, and vice versa
// (TestServer_APIRoutes_ActionMutatingCoherence).
func MutatingActionIDs() []ActionID {
	return []ActionID{
		ActionWriteBlob,
		ActionDeleteNode,
		ActionRenameObject,
		ActionUploadObject,
		ActionEditCell,
		ActionInsertRow,
		ActionDeleteRow,
		ActionEditKVEntry,
		ActionSetTTL,
	}
}

func vpnGateFamily(fam Family) bool {
	switch fam {
	case FamilyTabular, FamilyKV, FamilyDocument, FamilyStream:
		return true
	case FamilyObject, FamilyFile, FamilyUnknown:
		return false
	default:
		return false
	}
}

func actionSet(ids []ActionID) map[ActionID]bool {
	out := make(map[ActionID]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out
}
