package topology_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// actionTagViolation is one struct field named Action whose json tag key
// does not resolve to exactly "action".
type actionTagViolation struct {
	File   string
	Line   int
	Struct string
	Tag    string
}

// scanInputStructActionTags walks Go files under root (skipping *_test.go,
// same house convention as scanForDirectClientCalls) for struct type
// declarations whose name ends in "Input" — internal/tools' naming
// convention for MCP tool argument structs (WorkflowInput, ManageInput,
// DevServerInput, ...) — and returns one violation per field literally
// named Action whose json tag key isn't exactly "action" (an empty/missing
// tag counts as a violation too).
//
// Scope is deliberately narrower than "every struct with an Action field"
// (e.g. internal/tools/requires_setup_input.go's RequiresSetupInputRecovery,
// or internal/tools/launch_state.go's launchAuditEntry, both response/audit
// shapes, not MCP tool arguments) — spec T10 protects the middleware peek
// contract on *tool input* structs specifically (spec-telemetry.md §5.3).
func scanInputStructActionTags(root string) ([]actionTagViolation, error) {
	var violations []actionTagViolation
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil // root may not exist in some checkouts; skip silently
			}
			return walkErr
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, spec := range gd.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok || !strings.HasSuffix(ts.Name.Name, "Input") {
					continue
				}
				st, ok := ts.Type.(*ast.StructType)
				if !ok || st.Fields == nil {
					continue
				}
				for _, field := range st.Fields.List {
					if !fieldNamed(field, "Action") {
						continue
					}
					jsonKey := jsonTagKey(field.Tag)
					if jsonKey != "action" {
						pos := fset.Position(field.Pos())
						violations = append(violations, actionTagViolation{
							File:   path,
							Line:   pos.Line,
							Struct: ts.Name.Name,
							Tag:    jsonKey,
						})
					}
				}
			}
		}
		return nil
	})
	return violations, err
}

// fieldNamed reports whether field declares a name identical to name (a
// struct field can declare multiple comma-separated names sharing a type).
func fieldNamed(field *ast.Field, name string) bool {
	for _, n := range field.Names {
		if n.Name == name {
			return true
		}
	}
	return false
}

// jsonTagKey extracts the json struct-tag key (the part before the first
// comma, e.g. "action" out of `json:"action,omitempty"`). A nil tag, an
// unparsable literal, or a missing json key all return "".
func jsonTagKey(tag *ast.BasicLit) string {
	if tag == nil {
		return ""
	}
	raw, err := strconv.Unquote(tag.Value)
	if err != nil {
		return ""
	}
	jsonTag := reflect.StructTag(raw).Get("json")
	if jsonTag == "" {
		return ""
	}
	return strings.Split(jsonTag, ",")[0]
}

// TestActionJSONTag_ToolInputStructsUseActionKey pins spec T10: every
// internal/tools *Input struct field named Action carries json tag
// "action" (allowing ",omitempty"). internal/server's actionPeek
// (telemetry.go) unmarshals raw.Arguments into a struct whose single field
// is named Action with json tag "action"; a tool input struct whose Action
// field is tagged anything else silently breaks that peek — the emitted
// tool_call event's action field goes empty rather than erroring, so this
// can drift undetected without a structural pin.
func TestActionJSONTag_ToolInputStructsUseActionKey(t *testing.T) {
	t.Parallel()

	violations, err := scanInputStructActionTags("../tools")
	if err != nil {
		t.Fatalf("scanInputStructActionTags: %v", err)
	}
	for _, v := range violations {
		t.Errorf(
			"%s:%d: %s.Action has json tag %q, want \"action\" (or \"action,omitempty\")\n"+
				"\t→ breaks internal/server's actionPeek middleware extraction (spec-telemetry.md §5.3, T10)",
			v.File, v.Line, v.Struct, v.Tag,
		)
	}
}

// TestActionJSONTagScanner_FiresOnFixture is the lint engine's self-test
// (house style, same rationale as TestNoDirectClientCallsScanner_FiresOnFixture):
// proves the scanner actually flags a wrongly-tagged Action field, so a
// broken AST matcher can't silently pass the production scan above.
func TestActionJSONTagScanner_FiresOnFixture(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	src := `package fixture

type FooInput struct {
	Action string ` + "`json:\"act\"`" + `
}
`
	if err := os.WriteFile(filepath.Join(dir, "fixture.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	violations, err := scanInputStructActionTags(dir)
	if err != nil {
		t.Fatalf("scanInputStructActionTags: %v", err)
	}
	if len(violations) != 1 || violations[0].Struct != "FooInput" {
		t.Fatalf("expected 1 FooInput violation, got %+v", violations)
	}
}

// TestActionJSONTagScanner_FiresOnMissingTag proves an Action field with no
// json tag at all is flagged too (empty tag key != "action").
func TestActionJSONTagScanner_FiresOnMissingTag(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	src := `package fixture

type FooInput struct {
	Action string
}
`
	if err := os.WriteFile(filepath.Join(dir, "fixture.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	violations, err := scanInputStructActionTags(dir)
	if err != nil {
		t.Fatalf("scanInputStructActionTags: %v", err)
	}
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation for untagged Action field, got %+v", violations)
	}
}

// TestActionJSONTagScanner_AcceptsOmitempty proves the ",omitempty" suffix
// (used by WorkflowInput.Action and ProcessInput.Action in production) is
// not a violation.
func TestActionJSONTagScanner_AcceptsOmitempty(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	src := `package fixture

type FooInput struct {
	Action string ` + "`json:\"action,omitempty\"`" + `
}
`
	if err := os.WriteFile(filepath.Join(dir, "fixture.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	violations, err := scanInputStructActionTags(dir)
	if err != nil {
		t.Fatalf("scanInputStructActionTags: %v", err)
	}
	if len(violations) != 0 {
		t.Errorf("expected zero violations for \"action,omitempty\" tag, got %+v", violations)
	}
}

// TestActionJSONTagScanner_TestFilesExempt proves *_test.go fixtures/mocks
// are skipped (same exemption rationale as the direct-client-call scanner).
func TestActionJSONTagScanner_TestFilesExempt(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	src := `package fixture

type FooInput struct {
	Action string ` + "`json:\"wrong\"`" + `
}
`
	if err := os.WriteFile(filepath.Join(dir, "fixture_test.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	violations, err := scanInputStructActionTags(dir)
	if err != nil {
		t.Fatalf("scanInputStructActionTags: %v", err)
	}
	if len(violations) != 0 {
		t.Errorf("expected zero violations from *_test.go file, got %+v", violations)
	}
}

// TestActionJSONTagScanner_NonInputStructsExempt proves structs whose name
// doesn't end in "Input" (response/audit shapes, e.g.
// RequiresSetupInputRecovery, launchAuditEntry) are out of scope — the
// scanner protects the MCP tool argument-peek contract specifically, not
// every "Action" field in the package.
func TestActionJSONTagScanner_NonInputStructsExempt(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	src := `package fixture

type FooRecovery struct {
	Action string ` + "`json:\"wrong\"`" + `
}
`
	if err := os.WriteFile(filepath.Join(dir, "fixture.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	violations, err := scanInputStructActionTags(dir)
	if err != nil {
		t.Fatalf("scanInputStructActionTags: %v", err)
	}
	if len(violations) != 0 {
		t.Errorf("expected zero violations for a non-Input struct, got %+v", violations)
	}
}
