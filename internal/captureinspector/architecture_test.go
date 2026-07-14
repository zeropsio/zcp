package captureinspector_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const (
	modulePath              = "github.com/zeropsio/zcp"
	inspectorImportPath     = modulePath + "/internal/captureinspector"
	captureImportPath       = modulePath + "/internal/capture"
	inspectorCLIAdapterPath = "cmd/zcp/capture_ui.go"
)

func TestCaptureInspectorBoundary_UsesCompilerPrivateLayout(t *testing.T) {
	t.Parallel()
	root := inspectorRoot(t)
	for _, relative := range []string{filepath.Join("internal", "projection"), filepath.Join("internal", "web")} {
		info, err := os.Stat(filepath.Join(root, relative))
		if err != nil || !info.IsDir() {
			t.Errorf("capture inspector private package %s is missing: %v", relative, err)
		}
	}
	for _, legacy := range []string{filepath.Join(root, "..", "captureview"), filepath.Join(root, "..", "captureui")} {
		if _, err := os.Stat(legacy); err == nil || !os.IsNotExist(err) {
			t.Errorf("legacy flat inspector package must not exist: %s", legacy)
		}
	}
}

func TestCaptureInspectorBoundary_FacadeHasSingleCLIImporter(t *testing.T) {
	t.Parallel()
	root := repositoryRoot(t)
	violations, err := scanImports(root, func(relative, imported string) bool {
		if imported != inspectorImportPath && !strings.HasPrefix(imported, inspectorImportPath+"/") {
			return false
		}
		if relative == inspectorCLIAdapterPath || strings.HasPrefix(relative, "internal/captureinspector/") {
			return false
		}
		return true
	})
	if err != nil {
		t.Fatalf("scan inspector importers: %v", err)
	}
	for _, violation := range violations {
		t.Errorf("%s imports %q; only %s and the inspector subtree may import the inspector", violation.file, violation.imported, inspectorCLIAdapterPath)
	}
}

func TestCaptureInspectorBoundary_InternalImportsAreAllowlisted(t *testing.T) {
	t.Parallel()
	violations, err := scanImports(inspectorRoot(t), func(_ string, imported string) bool {
		return inspectorDependencyForbidden(imported)
	})
	if err != nil {
		t.Fatalf("scan inspector dependencies: %v", err)
	}
	for _, violation := range violations {
		t.Errorf("%s imports %q outside the inspector allowlist", violation.file, violation.imported)
	}
}

func TestCaptureInspectorBoundary_ProductionImportIsCold(t *testing.T) {
	t.Parallel()
	violations, err := scanProductionSideEffects(inspectorRoot(t))
	if err != nil {
		t.Fatalf("scan inspector side effects: %v", err)
	}
	for _, violation := range violations {
		t.Errorf("%s: %s", violation.file, violation.problem)
	}
}

func TestCaptureInspectorBoundary_ScannersFireOnFixtures(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	source := `package fixture

import (
    _ "github.com/zeropsio/zcp/internal/ops"
    _ "example.com/heavy-browser-library"
    "fmt"
    "os"
)

var started = os.Getenv("STARTED")
func init() {}
func mutate() { _ = os.Setenv("MUTATED", "1"); fmt.Println("unexpected output") }
`
	path := filepath.Join(dir, "fixture.go")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	imports, err := scanImports(dir, func(_ string, imported string) bool {
		return inspectorDependencyForbidden(imported)
	})
	if err != nil {
		t.Fatalf("scan fixture imports: %v", err)
	}
	if len(imports) != 2 || imports[0].imported != modulePath+"/internal/ops" || imports[1].imported != "example.com/heavy-browser-library" {
		t.Fatalf("import scanner did not flag core and third-party dependencies: %+v", imports)
	}
	sideEffects, err := scanProductionSideEffects(dir)
	if err != nil {
		t.Fatalf("scan fixture side effects: %v", err)
	}
	if len(sideEffects) != 4 {
		t.Fatalf("side-effect scanner got %d violations, want init, package initializer, process mutation, and stdout: %+v", len(sideEffects), sideEffects)
	}
}

type importViolation struct {
	file     string
	imported string
}

type sideEffectViolation struct {
	file    string
	problem string
}

func scanImports(root string, forbidden func(relative, imported string) bool) ([]importViolation, error) {
	violations := []importViolation{}
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != root && shouldSkipDirectory(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		parsed, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		for _, spec := range parsed.Imports {
			imported, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			if forbidden(relative, imported) {
				violations = append(violations, importViolation{file: relative, imported: imported})
			}
		}
		return nil
	})
	return violations, err
}

func scanProductionSideEffects(root string) ([]sideEffectViolation, error) {
	violations := []sideEffectViolation{}
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != root && shouldSkipDirectory(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		parsed, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		osNames := map[string]bool{}
		outputPackages := map[string]string{}
		for _, specification := range parsed.Imports {
			imported, err := strconv.Unquote(specification.Path.Value)
			if err != nil {
				return err
			}
			if imported == "os/exec" || imported == "os/signal" {
				violations = append(violations, sideEffectViolation{file: relative, problem: "process-control import " + imported + " is forbidden"})
			}
			name := imported
			if slash := strings.LastIndex(name, "/"); slash >= 0 {
				name = name[slash+1:]
			}
			if specification.Name != nil {
				name = specification.Name.Name
			}
			if imported == "os" {
				osNames[name] = true
			}
			if imported == "fmt" || imported == "log" || imported == "log/slog" {
				outputPackages[name] = imported
			}
		}
		ast.Inspect(parsed, func(candidate ast.Node) bool {
			if call, ok := candidate.(*ast.CallExpr); ok {
				if identifier, ok := call.Fun.(*ast.Ident); ok && (identifier.Name == "print" || identifier.Name == "println") {
					violations = append(violations, sideEffectViolation{file: relative, problem: "process output via built-in " + identifier.Name + " is forbidden"})
				}
			}
			selector, ok := candidate.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			identifier, ok := selector.X.(*ast.Ident)
			if !ok {
				return true
			}
			if osNames[identifier.Name] {
				switch selector.Sel.Name {
				case "Setenv", "Unsetenv", "Chdir", "Exit", "Stdout", "Stderr":
					violations = append(violations, sideEffectViolation{file: relative, problem: "process-global os." + selector.Sel.Name + " use is forbidden"})
				}
			}
			switch outputPackages[identifier.Name] {
			case "fmt":
				if selector.Sel.Name == "Print" || selector.Sel.Name == "Printf" || selector.Sel.Name == "Println" {
					violations = append(violations, sideEffectViolation{file: relative, problem: "process output via fmt." + selector.Sel.Name + " is forbidden"})
				}
			case "log":
				switch selector.Sel.Name {
				case "Print", "Printf", "Println", "Fatal", "Fatalf", "Fatalln", "Panic", "Panicf", "Panicln", "Output":
					violations = append(violations, sideEffectViolation{file: relative, problem: "default logger output via log." + selector.Sel.Name + " is forbidden"})
				}
			case "log/slog":
				switch selector.Sel.Name {
				case "Debug", "DebugContext", "Info", "InfoContext", "Warn", "WarnContext", "Error", "ErrorContext", "Log", "LogAttrs":
					violations = append(violations, sideEffectViolation{file: relative, problem: "default logger output via log/slog." + selector.Sel.Name + " is forbidden"})
				}
			}
			return true
		})
		for _, declaration := range parsed.Decls {
			switch node := declaration.(type) {
			case *ast.FuncDecl:
				if node.Recv == nil && node.Name.Name == "init" {
					violations = append(violations, sideEffectViolation{file: relative, problem: "production init function is forbidden"})
				}
			case *ast.GenDecl:
				if node.Tok != token.VAR {
					continue
				}
				for _, specification := range node.Specs {
					variable, ok := specification.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for _, value := range variable.Values {
						containsCall := false
						ast.Inspect(value, func(candidate ast.Node) bool {
							if _, ok := candidate.(*ast.CallExpr); ok {
								containsCall = true
								return false
							}
							return true
						})
						if containsCall {
							violations = append(violations, sideEffectViolation{file: relative, problem: "package-level call initializer is forbidden"})
						}
					}
				}
			}
		}
		return nil
	})
	return violations, err
}

func inspectorDependencyForbidden(imported string) bool {
	if isStandardLibraryImport(imported) {
		return false
	}
	return !importMatches(imported, inspectorImportPath) && !importMatches(imported, captureImportPath)
}

func isStandardLibraryImport(imported string) bool {
	first, _, _ := strings.Cut(imported, "/")
	return !strings.Contains(first, ".")
}

func importMatches(imported, allowed string) bool {
	return imported == allowed || strings.HasPrefix(imported, allowed+"/")
}

func shouldSkipDirectory(name string) bool {
	switch name {
	case ".git", ".cache", "node_modules", "tmp", "vendor":
		return true
	default:
		return false
	}
}

func inspectorRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("resolve inspector root: %v", err)
	}
	return root
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	return root
}
