package ops

import (
	"fmt"
	"slices"
	"strings"
)

// The levels access.set gives on a project, as Mate's People give them:
// full access, read only, hidden, or the person's own grant removed so their
// organization role decides again.
const (
	AccessLevelFull   = "full"
	AccessLevelRead   = "read"
	AccessLevelNone   = "none"
	AccessLevelRemove = "remove"
)

// accessLevelWords are each level as the card's summary says it.
var accessLevelWords = map[string]string{
	AccessLevelFull:   "full access",
	AccessLevelRead:   "read only",
	AccessLevelNone:   "hidden",
	AccessLevelRemove: "back to their organization role",
}

// OrgAccessPerson is one person's access to propose. Person is how the
// person deciding named them — an address or a name; the client finds them
// among the organization's members on the card.
type OrgAccessPerson struct {
	Person string `json:"person" jsonschema:"The person: their email address, or their name as the organization's members list shows it."`
	Level  string `json:"level"  jsonschema:"full (works on it), read (only looks), none (hidden from them) or remove (their own grant dropped: their organization role decides)."`
}

// OrgAccessPlace is one project the access is written on.
type OrgAccessPlace struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// OrgAccessSetParams are access.set's params: every project the access is
// written on — an application's production, stage and Mates at once — and
// each person's level there.
type OrgAccessSetParams struct {
	Projects []OrgAccessPlace  `json:"projects"`
	People   []OrgAccessPerson `json:"people"`
}

// accessProposal is access.set resolved: the first project (the proposal's
// top-level one), the name of where it acts, its summary and params.
type accessProposal struct {
	first   *orgProject
	name    string
	summary string
	params  OrgAccessSetParams
}

// accessKindOrder is the order an application's projects are listed in.
var accessKindOrder = []OrgProjectKind{OrgKindProduction, OrgKindStage, OrgKindMate}

// proposeAccess validates access.set: the organization's project or
// application, and each named person once at a level Mate gives. Whether the
// approving person may change access is Zerops's to say when they approve: it
// refuses the write in its own words and the card shows them.
func proposeAccess(snap *orgSnapshot, in OrgActionInput) (*accessProposal, error) {
	places, name, err := snap.accessPlaces(in.Project)
	if err != nil {
		return nil, err
	}
	people, err := accessPeople(in.People)
	if err != nil {
		return nil, err
	}
	params := OrgAccessSetParams{People: people}
	for _, p := range places {
		params.Projects = append(params.Projects, OrgAccessPlace{ID: p.ID, Name: p.Name})
	}
	said := make([]string, len(people))
	for i, one := range people {
		said[i] = one.Person + " " + accessLevelWords[one.Level]
	}
	return &accessProposal{
		first:   places[0],
		name:    name,
		summary: fmt.Sprintf("Access to %s: %s", name, strings.Join(said, ", ")),
		params:  params,
	}, nil
}

// accessPlaces are the projects ref reaches: the project it names, or every
// production, stage and Mate of the application it names — the way Mate's
// People change an application's access everywhere at once. Never the Chief's
// own project.
func (snap *orgSnapshot) accessPlaces(ref string) ([]*orgProject, string, error) {
	p, err := snap.resolve(ref)
	if err == nil {
		if p.Class.Kind == OrgKindChief {
			return nil, "", invalid(fmt.Sprintf("%s is the Chief's own project: yours alone", p.Name), "Name an application or one of its projects")
		}
		return []*orgProject{p}, p.Name, nil
	}
	app := strings.TrimSpace(ref)
	var places []*orgProject
	name := ""
	for _, project := range snap.projects {
		op := snap.project(project)
		if op.Class.App == "" || !strings.EqualFold(op.Class.App, app) || !slices.Contains(accessKindOrder, op.Class.Kind) {
			continue
		}
		places = append(places, op)
		name = op.Class.App
	}
	if len(places) == 0 {
		return nil, "", err
	}
	slices.SortStableFunc(places, func(a, b *orgProject) int {
		return slices.Index(accessKindOrder, a.Class.Kind) - slices.Index(accessKindOrder, b.Class.Kind)
	})
	return places, name, nil
}

// accessPeople are the people as named, trimmed, each once, at a level
// Mate gives.
func accessPeople(raw []OrgAccessPerson) ([]OrgAccessPerson, error) {
	if len(raw) == 0 {
		return nil, invalid("access.set needs a person", "Pass each person with their level")
	}
	people := make([]OrgAccessPerson, 0, len(raw))
	for _, one := range raw {
		person, level := strings.TrimSpace(one.Person), strings.ToLower(strings.TrimSpace(one.Level))
		if person == "" {
			return nil, invalid("access.set names a person without a name", "Pass their email address or their name")
		}
		if _, ok := accessLevelWords[level]; !ok {
			return nil, invalid(fmt.Sprintf("%q is not a level Mate gives", one.Level), "level is full, read, none or remove")
		}
		if slices.ContainsFunc(people, func(held OrgAccessPerson) bool { return strings.EqualFold(held.Person, person) }) {
			return nil, invalid(fmt.Sprintf("%s is named twice", person), "Name each person once, at one level")
		}
		people = append(people, OrgAccessPerson{Person: person, Level: level})
	}
	return people, nil
}
