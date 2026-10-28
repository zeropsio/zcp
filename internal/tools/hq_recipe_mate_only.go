package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// What serves only a Mate — the owner's cloud agent, a person's own tooling —
// stays on the AI Agent tier, and Stage and Small Production leave it out.
// Who decides that is the person, never a heuristic (the owner, 2026-10-28,
// after flip.studio's agentrunner named its pair's own stage, so zcp's
// address rule carried it into both): the agent asks, and records the answer
// with zerops_workflow action="group-recipe" mateOnly={"<hostname>": true}
// — or false, to carry a service the rules would keep. zcp keeps the choice
// beside the other recipe records in the HQ state directory, which every zcp
// of the Mate shares, and every pass composes with it (composeGroupRecipe):
// zcp's start, the pass a service change asks for, and the agent's own.
// Where the person said nothing, zcp's rules decide, and the pass says so
// (bundle.BuildGroupRecipe).

// recipeChoicesFile keeps the person's choices for the recipe, beside
// recipeRequiredFile: a name with a dash, which no hostname has.
const recipeChoicesFile = "group-recipe-choices.json"

// recipeChoicesRecord is what recipeChoicesFile holds: by hostname, true for
// a service that serves only this Mate, false for one carried in every tier.
type recipeChoicesRecord struct {
	MateOnly map[string]bool `json:"mateOnly,omitempty"`
}

func recipeChoicesPath(stateDir string) string {
	return filepath.Join(stateDir, hqStateDir, recipeChoicesFile)
}

// readRecipeChoices is the person's choice of what serves only this Mate, by
// hostname — empty when they made none. A record that cannot be read is an
// error: a pass composed without the person's choice would carry the
// opposite of it into the group repository.
func readRecipeChoices(stateDir string) (map[string]bool, error) {
	data, err := os.ReadFile(recipeChoicesPath(stateDir))
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("could not read the person's choice of what serves only this Mate: %w", err)
	}
	var record recipeChoicesRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, fmt.Errorf("could not read the person's choice of what serves only this Mate (%s): %w", recipeChoicesPath(stateDir), err)
	}
	if record.MateOnly == nil {
		record.MateOnly = map[string]bool{}
	}
	return record.MateOnly, nil
}

// writeRecipeChoices keeps mateOnly as the person's choice, whole: written
// beside and renamed over, so a reader never sees half of it.
func writeRecipeChoices(stateDir string, mateOnly map[string]bool) error {
	path := recipeChoicesPath(stateDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("record recipe choices: %w", err)
	}
	data, err := json.MarshalIndent(recipeChoicesRecord{MateOnly: mateOnly}, "", "  ")
	if err != nil {
		return fmt.Errorf("record recipe choices: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("record recipe choices: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("record recipe choices: %w", err)
	}
	return nil
}

// handleGroupRecipeMateOnly records the person's choice of what serves only
// this Mate (`zerops_workflow action="group-recipe" mateOnly={...}`) — each
// hostname a utility or a stage of a pair the project runs — over what was
// chosen before, then runs the recipe pass, which composes with it, and
// answers with both. A hostname it cannot take refuses the whole call and
// records nothing.
func handleGroupRecipeMateOnly(
	ctx context.Context,
	client platform.Client,
	httpClient ops.HTTPDoer,
	rt runtime.Info,
	stateDir string,
	choice map[string]bool,
) (*mcp.CallToolResult, any, error) {
	if !rt.InContainer || !rt.MateEnabled || client == nil {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"What serves only this Mate was not recorded: the group recipe is a Mate's act — there is no Mate environment here",
			"",
		), WithRecoveryStatus()), nil, nil
	}
	refusals, err := mateOnlyRefusals(ctx, client, rt.ProjectID, stateDir, choice)
	if err != nil {
		return mateOnlyNotRecorded(err), nil, nil
	}
	if len(refusals) > 0 {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"What serves only this Mate was not recorded: "+strings.Join(refusals, "; "),
			"Name a utility or a stage of a pair this project runs — the person's answer, never a guess.",
		), WithRecoveryStatus()), nil, nil
	}
	chosen, err := recordMateOnly(ctx, stateDir, choice)
	if err != nil {
		return mateOnlyNotRecorded(err), nil, nil
	}
	said := mateOnlyLine(choice)
	outcome := groupRecipeOutcome(ctx, client, httpClient, rt, stateDir)
	if outcome.Blocked != "" {
		return jsonResult(map[string]any{
			"mateOnly": chosen,
			"message":  said + " The group recipe was not proposed now: " + outcome.Blocked + ".",
		}), nil, nil
	}
	result := groupRecipeAnswer(outcome)
	result["mateOnly"] = chosen
	result["message"] = said + " " + fmt.Sprint(result["message"])
	return jsonResult(result), nil, nil
}

// mateOnlyNotRecorded is the answer of a choice that a read or a write
// stopped: nothing was recorded, and the call can be made again.
func mateOnlyNotRecorded(err error) *mcp.CallToolResult {
	return convertError(platform.NewPlatformError(
		platform.ErrAPIError,
		fmt.Sprintf("What serves only this Mate was not recorded: %v", err),
		"Call it again; nothing was recorded.",
	), WithRecoveryStatus())
}

// recordMateOnly keeps choice over what the person chose before, under the
// Mate's one recipe pass so a pass never composes half of it, and returns
// all they have chosen.
func recordMateOnly(ctx context.Context, stateDir string, choice map[string]bool) (map[string]bool, error) {
	release, err := holdRecipePass(ctx, stateDir)
	if err != nil {
		return nil, err
	}
	defer release()
	chosen, err := readRecipeChoices(stateDir)
	if err != nil {
		return nil, err
	}
	maps.Copy(chosen, choice)
	if err := writeRecipeChoices(stateDir, chosen); err != nil {
		return nil, err
	}
	return chosen, nil
}

// mateOnlyRefusals says, per hostname of choice, why it cannot be chosen: no
// service of the project, a pair's dev half (only the AI Agent tier carries
// it already), a managed service (it goes with the runtimes that use it), a
// Box (a tier carries it where a runtime it carries reads it), or what the
// recipe carries on no tier. None for a utility or a stage of a pair.
func mateOnlyRefusals(ctx context.Context, client platform.Client, projectID, stateDir string, choice map[string]bool) ([]string, error) {
	discovered, err := ops.Discover(ctx, client, projectID, "", false, false, false)
	if err != nil {
		return nil, fmt.Errorf("could not read this Mate's project (%w)", err)
	}
	live := make(map[string]ops.ServiceInfo, len(discovered.Services))
	for _, svc := range discovered.Services {
		live[svc.Hostname] = svc
	}
	metas, err := workflow.ListServiceMetas(stateDir)
	if err != nil {
		return nil, fmt.Errorf("could not read the pair records (%w)", err)
	}
	pairs := workflow.ManagedRuntimeIndex(metas)
	var refusals []string
	for _, host := range slices.Sorted(maps.Keys(choice)) {
		svc, runs := live[host]
		pair := pairs[host]
		switch {
		case !runs:
			refusals = append(refusals, fmt.Sprintf("no service %q runs in this Mate's project", host))
		case pair != nil && pair.Hostname == host:
			refusals = append(refusals, fmt.Sprintf("%q is the dev half of a pair — its dev service or dev pool — which only the AI Agent tier carries already", host))
		case svc.IsInfrastructure:
			refusals = append(refusals, fmt.Sprintf("%q is a managed service: Stage and Small Production carry it for the runtimes they run, and a volume only a runtime serving this Mate uses moves with that runtime", host))
		case topology.IsSandboxType(svc.Type):
			refusals = append(refusals, fmt.Sprintf("%q is a Zerops Box: a tier carries it where a runtime the tier carries reads it", host))
		case topology.IsPoolType(svc.Type) || strings.HasPrefix(svc.Type, "zcp@"):
			refusals = append(refusals, fmt.Sprintf("the recipe carries %q on no tier", host))
		}
	}
	return refusals, nil
}

// mateOnlyLine says what the person chose, per hostname.
func mateOnlyLine(choice map[string]bool) string {
	var said []string
	for _, host := range slices.Sorted(maps.Keys(choice)) {
		if choice[host] {
			said = append(said, fmt.Sprintf("%q serves only this Mate, as the person said: the AI Agent tier carries it, and Stage and Small Production leave it out with a volume only it uses", host))
		} else {
			said = append(said, fmt.Sprintf("%q is carried in every tier, as the person said, whatever zcp's own rules read", host))
		}
	}
	return strings.Join(said, "; ") + ". Recorded: every recipe pass composes with it from now on."
}
