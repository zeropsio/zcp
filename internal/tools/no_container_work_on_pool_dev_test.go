package tools

import (
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
)

// TestNoContainerWorkOnPoolDevHalf pins the pool-keyed pair's reader rule
// (docs/spec-workflows.md §8 E8 (d), §13 PM-10): a pair's dev half may be a
// dev pool, whose hostname reaches a random one of its containers — possibly
// someone's lease (§8 PS-3). So no function in this package hands a
// ServiceMeta's Hostname to container work without asking DevIsPool.
//
// The scan follows a meta's Hostname — read off the meta, through the
// function's local variables, out of a package function that returns it,
// or through an untagged field of this package's structs that a composite
// literal sets from it (resolvedLaunchRuntime.PushHostname; a tagged field
// is a wire shape, mostly holding what a caller sent) — into container
// work:
//
//   - ssh: an SSHDeployer's ExecSSH / ExecSSHBackground host;
//   - mount: a Mounter's Mount / Unmount / ForceUnmount / HasUnit /
//     CleanupUnit host;
//   - a local read under a mount root: filepath.Join beside an argument
//     named for a mount root (mountRoot, recipePairMountRoot,
//     CodeMountRoot …) or "/var/www/" + host;
//   - a rendered ssh command: an fmt.Sprintf operand between "ssh " and the
//     quoted remote command;
//   - a package or ops function that passes that argument on to any of
//     these (followed to a fixed point, through func-valued vars such as
//     launchLiveRemoteReader).
//
// The function that obtains the meta — reads it from disk, or builds the
// struct carrying its Hostname — asks DevIsPool: a .DevIsPool selector in
// its body (the meta's method, a snapshot's or resolvedLaunchRuntime's
// field), or a value handed to a package function that asks it of that
// parameter (containerGitPushSetupRefusal(input, meta)). A helper handed the
// meta as a parameter passes the question to its callers. The check is per
// function, not per path: every new site has visibly considered the pool
// half; the branch it took is its tests' to prove. Tests are exempt.
func TestNoContainerWorkOnPoolDevHalf(t *testing.T) {
	t.Parallel()
	tools := parsePoolGuardPackage(t, ".")
	ops := parsePoolGuardPackage(t, filepath.Join("..", "ops"))
	violations := poolDevHalfViolations(tools, ops)
	if len(violations) > 0 {
		t.Errorf("container work keyed on a ServiceMeta's Hostname with no DevIsPool asked — a pair's dev half may be a dev pool, whose hostname reaches a random container (docs/spec-workflows.md §8 PS-3, §13 PM-10). Ask meta.DevIsPool() (snapshots: .DevIsPool) and refuse or skip before the work:\n  %s",
			strings.Join(violations, "\n  "))
	}
}

// TestPoolDevHalfScanner_FiresOnFixture proves the scanner sees each way a
// meta's Hostname reaches container work and accepts each way of asking, so
// a green TestNoContainerWorkOnPoolDevHalf means "no unasked site", not
// "scanner blind".
func TestPoolDevHalfScanner_FiresOnFixture(t *testing.T) {
	t.Parallel()
	const toolsSrc = `package tools

type launchRow struct{ PushHostname string }

type wireRow struct {
	Service string ` + "`json:\"service\"`" + `
}

var remoteReader = readRemote

func direct(ssh SSHDeployer, stateDir string) {
	meta, _ := workflow.FindServiceMeta(stateDir, "x")
	ssh.ExecSSH(ctx, meta.Hostname, "true") // V: direct
}

func throughLocal(stateDir string) {
	m, _ := workflow.FindServiceMeta(stateDir, "x")
	host := m.Hostname
	mounter.Mount(ctx, host, "/var/www/x") // V: local
}

func throughRange(stateDir, mountRoot string) {
	metas, _ := workflow.ListServiceMetas(stateDir)
	for _, m := range metas {
		_ = filepath.Join(mountRoot, m.Hostname) // V: mount root
	}
}

func readRemote(ssh SSHDeployer, host string) {
	ssh.ExecSSH(ctx, host, "git remote get-url origin")
}

func throughHelperAndVar(ssh SSHDeployer, stateDir string) {
	meta, _ := workflow.FindServiceMeta(stateDir, "x")
	readRemote(ssh, meta.Hostname)   // V: helper
	remoteReader(ssh, meta.Hostname) // V: func var
}

func render(flags, pushHost, key string) string {
	return fmt.Sprintf("$(ssh %s %s 'printf %%s \"$%s\"')", flags, pushHost, key)
}

func throughRendered(stateDir string) string {
	meta, _ := workflow.FindServiceMeta(stateDir, "x")
	_ = render("-T", "x", meta.Hostname) // the remote command's operand: no host
	return render("-T", meta.Hostname, "GIT_TOKEN") // V: rendered ssh
}

func canonical(stateDir string) string {
	m, _ := workflow.FindServiceMeta(stateDir, "x")
	return m.Hostname
}

func throughReturn(ssh SSHDeployer, stateDir string) {
	ssh.ExecSSH(ctx, canonical(stateDir), "true") // V: returned
}

func rows(stateDir string) []launchRow {
	meta, _ := workflow.FindServiceMeta(stateDir, "x")
	return []launchRow{{PushHostname: meta.Hostname}}
}

func readRows(ssh SSHDeployer, rs []launchRow) {
	for _, r := range rs {
		ssh.ExecSSH(ctx, r.PushHostname, "true")
	}
}

func throughField(ssh SSHDeployer, stateDir string) {
	readRows(ssh, rows(stateDir)) // V: field
}

func throughOps(ssh SSHDeployer, stateDir string) {
	meta, _ := workflow.FindServiceMeta(stateDir, "x")
	ops.ReadThing(ctx, ssh, meta.Hostname) // V: ops
}

func handedTheMeta(ssh SSHDeployer, meta *workflow.ServiceMeta) {
	ssh.ExecSSH(ctx, meta.Hostname, "true")
}

func throughParam(ssh SSHDeployer, stateDir string) {
	meta, _ := workflow.FindServiceMeta(stateDir, "x")
	handedTheMeta(ssh, meta) // V: param
}

func askedInline(ssh SSHDeployer, stateDir string) {
	meta, _ := workflow.FindServiceMeta(stateDir, "x")
	if meta.DevIsPool() {
		return
	}
	handedTheMeta(ssh, meta)
}

func refusal(meta *workflow.ServiceMeta) bool { return meta.DevIsPool() }

func askedThroughHelper(ssh SSHDeployer, stateDir string) {
	meta, _ := workflow.FindServiceMeta(stateDir, "x")
	if refusal(meta) {
		return
	}
	ssh.ExecSSH(ctx, meta.Hostname, "true")
}

func notContainerWork(ssh SSHDeployer, svc platform.ServiceStack, input WorkflowInput, stateDir string) {
	meta, _ := workflow.FindServiceMeta(stateDir, "x")
	ssh.ExecSSH(ctx, svc.Hostname, "true")
	ssh.ExecSSH(ctx, input.TargetService, "true")
	_ = filepath.Join(stateDir, "services", meta.Hostname+".json")
	w := wireRow{Service: meta.Hostname}
	ssh.ExecSSH(ctx, w.Service, "true")
}
`
	const opsSrc = `package ops

func ReadThing(ctx context.Context, ssh SSHDeployer, hostname string) {
	inner(ctx, ssh, hostname)
}

func inner(ctx context.Context, ssh SSHDeployer, h string) {
	cmd := "x"
	ssh.ExecSSH(ctx, h, cmd)
}
`
	tools := parsePoolGuardSource(t, "fixture_tools.go", toolsSrc)
	ops := parsePoolGuardSource(t, "fixture_ops.go", opsSrc)
	got := poolDevHalfViolations(tools, ops)
	funcs := make([]string, 0, len(got))
	for _, v := range got {
		funcs = append(funcs, v[strings.LastIndex(v, "(in ")+4:len(v)-1])
	}
	want := []string{
		"direct", "throughField", "throughHelperAndVar", "throughHelperAndVar", "throughLocal",
		"throughOps", "throughParam", "throughRange", "throughRendered", "throughReturn",
	}
	sort.Strings(funcs)
	if !slices.Equal(funcs, want) {
		t.Errorf("scanner flagged %v, want %v:\n  %s", funcs, want, strings.Join(got, "\n  "))
	}
}

// poolGuardPackage is one parsed package of the scan: its non-test files
// and the declarations the scan resolves calls and types against.
type poolGuardPackage struct {
	fset  *token.FileSet
	files []*ast.File
	// funcs are the package's plain functions (no receiver) — the callees a
	// bare call or a func-valued var can name.
	funcs map[string]*ast.FuncDecl
	// aliases map a package var initialized with a plain function's name
	// (`var launchLiveRemoteReader = readLaunchLiveRemote`) to that function.
	aliases map[string]string
	// structs are the struct types the package declares.
	structs map[string]*ast.StructType
}

func parsePoolGuardPackage(t *testing.T, dir string) *poolGuardPackage {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %s: %v", dir, err)
	}
	pkg := newPoolGuardPackage()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(dir, name)
		f, err := parser.ParseFile(pkg.fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		pkg.add(f)
	}
	return pkg
}

func parsePoolGuardSource(t *testing.T, name, src string) *poolGuardPackage {
	t.Helper()
	pkg := newPoolGuardPackage()
	f, err := parser.ParseFile(pkg.fset, name, src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	pkg.add(f)
	return pkg
}

func newPoolGuardPackage() *poolGuardPackage {
	return &poolGuardPackage{
		fset:    token.NewFileSet(),
		funcs:   map[string]*ast.FuncDecl{},
		aliases: map[string]string{},
		structs: map[string]*ast.StructType{},
	}
}

func (p *poolGuardPackage) add(f *ast.File) {
	p.files = append(p.files, f)
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil && d.Body != nil {
				p.funcs[d.Name.Name] = d
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.ValueSpec:
					for i, name := range s.Names {
						if i < len(s.Values) {
							if id, ok := s.Values[i].(*ast.Ident); ok {
								p.aliases[name.Name] = id.Name
							}
						}
					}
				case *ast.TypeSpec:
					if st, ok := s.Type.(*ast.StructType); ok {
						p.structs[s.Name.Name] = st
					}
				}
			}
		}
	}
}

// callee is the plain function a call names in this package — directly or
// through a func-valued var — "" for any other call.
func (p *poolGuardPackage) callee(call *ast.CallExpr) string {
	id, ok := call.Fun.(*ast.Ident)
	if !ok {
		return ""
	}
	name := id.Name
	if target, ok := p.aliases[name]; ok {
		name = target
	}
	if _, ok := p.funcs[name]; !ok {
		return ""
	}
	return name
}

// origin says where a meta's Hostname in an expression comes from: a meta
// the function obtained itself (local), or a parameter it was handed.
type origin struct {
	local  bool
	params map[int]bool
}

func (o origin) tainted() bool { return o.local || len(o.params) > 0 }

func (o origin) union(other origin) origin {
	out := origin{local: o.local || other.local, params: maps.Clone(o.params)}
	if out.params == nil && len(other.params) > 0 {
		out.params = map[int]bool{}
	}
	maps.Copy(out.params, other.params)
	return out
}

func (o origin) equal(other origin) bool {
	return o.local == other.local && maps.Equal(o.params, other.params)
}

// poolGuardFacts are what the fixed points learn about the package's
// functions.
type poolGuardFacts struct {
	// hostSink maps a function to the parameter indices whose value reaches
	// container work in it or in a callee.
	hostSink map[string]map[int]bool
	// opsHostSink is hostSink for the ops package (called as ops.F).
	opsHostSink map[string]map[int]bool
	// returnsHost maps a function to its results that are a meta's
	// Hostname, by where it comes from.
	returnsHost map[string]map[int]origin
	// hostFields are the untagged fields ("<struct>.<field>") of this
	// package's structs a composite literal sets from a meta's Hostname.
	hostFields map[string]bool
	// askers maps a function to the parameter indices it asks DevIsPool of.
	askers map[string]map[int]bool
	// handedOn maps an unasking function to the parameters whose meta's
	// Hostname reaches container work in it: its callers ask instead.
	handedOn map[string]map[int]bool
}

func poolDevHalfViolations(tools, ops *poolGuardPackage) []string {
	facts := &poolGuardFacts{
		opsHostSink: hostSinkParams(ops, nil),
		returnsHost: map[string]map[int]origin{},
		hostFields:  map[string]bool{},
		askers:      map[string]map[int]bool{},
		handedOn:    map[string]map[int]bool{},
	}
	facts.hostSink = hostSinkParams(tools, facts.opsHostSink)
	for name, fn := range tools.funcs {
		facts.askers[name] = paramsAskingDevIsPool(fn)
	}
	// Returned hostnames, carrier fields and handed-on parameters feed each
	// other's taint: one fixed point over all three.
	for changed := true; changed; {
		changed = false
		for name, fn := range tools.funcs {
			sc := newPoolGuardScope(tools, facts, fn)
			for i, o := range sc.taintedResults(fn) {
				if facts.returnsHost[name] == nil {
					facts.returnsHost[name] = map[int]origin{}
				}
				if !facts.returnsHost[name][i].equal(o) {
					facts.returnsHost[name][i] = o
					changed = true
				}
			}
			for field := range sc.taintedLiteralFields(fn) {
				if !facts.hostFields[field] {
					facts.hostFields[field] = true
					changed = true
				}
			}
			if sc.asksDevIsPool(fn) {
				continue
			}
			for _, w := range sc.containerWork(fn.Body) {
				for i := range w.origin.params {
					if facts.handedOn[name] == nil {
						facts.handedOn[name] = map[int]bool{}
					}
					if !facts.handedOn[name][i] {
						facts.handedOn[name][i] = true
						changed = true
					}
				}
			}
		}
	}

	var out []string
	for _, f := range tools.files {
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			sc := newPoolGuardScope(tools, facts, fn)
			if sc.asksDevIsPool(fn) {
				continue
			}
			for _, w := range sc.containerWork(fn.Body) {
				if w.origin.local || fn.Recv != nil && len(w.origin.params) > 0 {
					// A method's parameters are handed on to no caller the scan
					// can resolve: it asks itself.
					out = append(out, tools.fset.Position(w.pos).String()+" (in "+fn.Name.Name+")")
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

// hostSinkParams is the fixed point of "parameter i of f reaches container
// work": directly (a primitive sink) or by being handed on to a callee's
// sink parameter — in pkg, or in ops for a call ops.F.
func hostSinkParams(pkg *poolGuardPackage, opsSinks map[string]map[int]bool) map[string]map[int]bool {
	out := map[string]map[int]bool{}
	for changed := true; changed; {
		changed = false
		for name, fn := range pkg.funcs {
			derived := paramDerived(fn, true)
			for i := range derived.params {
				if out[name][i] {
					continue
				}
				fromParam := func(e ast.Expr) origin {
					if derived.from(e, i) {
						return origin{local: true}
					}
					return origin{}
				}
				if len(sinkArgs(fn.Body, pkg, out, opsSinks, fromParam)) > 0 {
					if out[name] == nil {
						out[name] = map[int]bool{}
					}
					out[name][i] = true
					changed = true
				}
			}
		}
	}
	return out
}

// paramDerivation tracks, per parameter of a function, the local names
// holding a value derived from it.
type paramDerivation struct {
	params map[int]string
	names  map[string]map[int]bool
	// throughCalls: a call's result derives from its arguments. Off, only
	// assignment, selection, indexing and ranging carry a parameter on — a
	// meta read from disk by a call is the function's own.
	throughCalls bool
}

// from reports whether e mentions a name derived from parameter i.
func (d paramDerivation) from(e ast.Expr, i int) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			if !d.throughCalls {
				return false
			}
		case *ast.FuncLit:
			return false
		case *ast.Ident:
			if d.names[x.Name][i] {
				found = true
			}
		}
		return !found
	})
	return found
}

// paramDerived is fn's parameters and every local name assigned or ranged
// from an expression mentioning one (to a fixed point).
func paramDerived(fn *ast.FuncDecl, throughCalls bool) paramDerivation {
	d := paramDerivation{params: map[int]string{}, names: map[string]map[int]bool{}, throughCalls: throughCalls}
	idx := 0
	for _, field := range fn.Type.Params.List {
		if len(field.Names) == 0 {
			idx++
			continue
		}
		for _, name := range field.Names {
			d.params[idx] = name.Name
			d.names[name.Name] = map[int]bool{idx: true}
			idx++
		}
	}
	mark := func(lhs ast.Expr, rhs ast.Expr) bool {
		id, ok := lhs.(*ast.Ident)
		if !ok || id.Name == "_" {
			return false
		}
		changed := false
		for i := range d.params {
			if d.from(rhs, i) && !d.names[id.Name][i] {
				if d.names[id.Name] == nil {
					d.names[id.Name] = map[int]bool{}
				}
				d.names[id.Name][i] = true
				changed = true
			}
		}
		return changed
	}
	for changed := true; changed; {
		changed = false
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			switch s := n.(type) {
			case *ast.AssignStmt:
				for li, lhs := range s.Lhs {
					rhs := s.Rhs[0]
					if len(s.Rhs) == len(s.Lhs) {
						rhs = s.Rhs[li]
					}
					changed = mark(lhs, rhs) || changed
				}
			case *ast.ValueSpec:
				for li, name := range s.Names {
					if li < len(s.Values) {
						changed = mark(name, s.Values[li]) || changed
					}
				}
			case *ast.RangeStmt:
				if s.Value != nil {
					changed = mark(s.Value, s.X) || changed
				}
			}
			return true
		})
	}
	return d
}

// work is one site where container work takes a meta's Hostname.
type work struct {
	pos    token.Pos
	origin origin
}

// sinkArgs are the sites in body where an argument reaching container work
// carries a host (per carries): primitive sinks and the sink parameters of
// package and ops callees.
func sinkArgs(body ast.Node, pkg *poolGuardPackage, sinks, opsSinks map[string]map[int]bool, carries func(ast.Expr) origin) []work {
	var out []work
	check := func(e ast.Expr, value func(ast.Expr) origin) {
		if e == nil {
			return
		}
		if o := value(e); o.tainted() {
			out = append(out, work{pos: e.Pos(), origin: o})
		}
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			for _, arg := range primitiveSinkArgs(x) {
				check(arg, carries)
			}
			if name := pkg.callee(x); name != "" {
				for i := range sinks[name] {
					if i < len(x.Args) {
						check(x.Args[i], carries)
					}
				}
			}
			if sel, ok := x.Fun.(*ast.SelectorExpr); ok && isIdent(sel.X, "ops") {
				for i := range opsSinks[sel.Sel.Name] {
					if i < len(x.Args) {
						check(x.Args[i], carries)
					}
				}
			}
		case *ast.BinaryExpr:
			// "/var/www/" + host: a path under the code mount root.
			if x.Op == token.ADD && isMountRootLiteral(x.X) {
				check(x.Y, carries)
			}
		}
		return true
	})
	return out
}

// containerMethods are the SSHDeployer and Mounter methods whose second
// argument is the service hostname the work lands on.
var containerMethods = map[string]bool{
	"ExecSSH": true, "ExecSSHBackground": true,
	"Mount": true, "Unmount": true, "ForceUnmount": true, "HasUnit": true, "CleanupUnit": true,
}

// primitiveSinkArgs are the arguments of call that name where container
// work lands: an ssh/mount method's host, a path joined under a mount root,
// a rendered ssh command's destination.
func primitiveSinkArgs(call *ast.CallExpr) []ast.Expr {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil
	}
	switch {
	case containerMethods[sel.Sel.Name] && len(call.Args) > 1:
		return call.Args[1:2]
	case isIdent(sel.X, "filepath") && sel.Sel.Name == "Join":
		var rest []ast.Expr
		rooted := false
		for _, arg := range call.Args {
			if namesMountRoot(arg) {
				rooted = true
				continue
			}
			rest = append(rest, arg)
		}
		if rooted {
			return rest
		}
	case isIdent(sel.X, "fmt") && sel.Sel.Name == "Sprintf" && len(call.Args) > 1:
		if lit, ok := call.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
			var out []ast.Expr
			for _, i := range sshDestinationVerbs(lit.Value) {
				if i+1 < len(call.Args) {
					out = append(out, call.Args[i+1])
				}
			}
			return out
		}
	}
	return nil
}

// sshDestinationVerbs are the operand indices of the verbs a format places
// after "ssh " and before the remote command's opening quote: the host (and
// its flags) of a rendered ssh command line.
func sshDestinationVerbs(format string) []int {
	var out []int
	verb, inSSH := 0, false
	for i := 0; i < len(format); i++ {
		switch {
		case strings.HasPrefix(format[i:], "ssh "):
			inSSH = true
		case format[i] == '\'':
			inSSH = false
		case format[i] == '%' && i+1 < len(format):
			if format[i+1] == '%' {
				i++
				continue
			}
			if inSSH {
				out = append(out, verb)
			}
			verb++
		}
	}
	return out
}

// namesMountRoot reports whether e names a local mount root: an identifier
// or selector spelled with "mount" (mountRoot, recipePairMountRoot,
// topology.CodeMountRoot) or the code mount's own path.
func namesMountRoot(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.Ident:
		return strings.Contains(strings.ToLower(x.Name), "mount")
	case *ast.SelectorExpr:
		return strings.Contains(strings.ToLower(x.Sel.Name), "mount")
	case *ast.BasicLit:
		return x.Kind == token.STRING && strings.Trim(x.Value, "\"`") == "/var/www"
	}
	return false
}

func isMountRootLiteral(e ast.Expr) bool {
	lit, ok := e.(*ast.BasicLit)
	return ok && lit.Kind == token.STRING && strings.HasPrefix(strings.Trim(lit.Value, "\"`"), "/var/www/")
}

func isIdent(e ast.Expr, name string) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == name
}

// paramsAskingDevIsPool are fn's parameter indices a .DevIsPool selector
// in fn reads from (the parameter, or a name derived from it).
func paramsAskingDevIsPool(fn *ast.FuncDecl) map[int]bool {
	derived := paramDerived(fn, false)
	out := map[int]bool{}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "DevIsPool" {
			return true
		}
		for i := range derived.params {
			if derived.from(sel.X, i) {
				out[i] = true
			}
		}
		return true
	})
	return out
}

// guardType is a syntactic type the scan tracks: a ServiceMeta ("meta"),
// one of this package's structs ("S:<name>"), or a slice ("[]…") or map
// value ("map:…") of either; "" for every other type.
type guardType string

const guardMeta guardType = "meta"

// typeOf is the guardType a type expression spells in pkg.
func (p *poolGuardPackage) typeOf(e ast.Expr) guardType {
	switch x := e.(type) {
	case *ast.StarExpr:
		return p.typeOf(x.X)
	case *ast.ParenExpr:
		return p.typeOf(x.X)
	case *ast.SelectorExpr:
		if isIdent(x.X, "workflow") && x.Sel.Name == "ServiceMeta" {
			return guardMeta
		}
	case *ast.Ident:
		if _, ok := p.structs[x.Name]; ok {
			return guardType("S:" + x.Name)
		}
	case *ast.ArrayType:
		if elem := p.typeOf(x.Elt); elem != "" {
			return "[]" + elem
		}
	case *ast.MapType:
		if elem := p.typeOf(x.Value); elem != "" {
			return "map:" + elem
		}
	}
	return ""
}

// elem is the element type of a slice or map guardType, "" otherwise.
func (t guardType) elem() guardType {
	if rest, ok := strings.CutPrefix(string(t), "[]"); ok {
		return guardType(rest)
	}
	if rest, ok := strings.CutPrefix(string(t), "map:"); ok {
		return guardType(rest)
	}
	return ""
}

// structName is the package struct t names, "" when t is none.
func (t guardType) structName() string {
	name, ok := strings.CutPrefix(string(t), "S:")
	if !ok {
		return ""
	}
	return name
}

// field is the declaration of field on pkg's struct name, nil for none.
func (p *poolGuardPackage) field(name, field string) *ast.Field {
	st, ok := p.structs[name]
	if !ok {
		return nil
	}
	for _, f := range st.Fields.List {
		for _, n := range f.Names {
			if n.Name == field {
				return f
			}
		}
	}
	return nil
}

// workflowResults are the workflow functions that hand back metas, by the
// type of their first result.
var workflowResults = map[string]guardType{
	"FindServiceMeta":     guardMeta,
	"ReadServiceMeta":     guardMeta,
	"MigratePairToPool":   guardMeta,
	"RevertPoolPair":      guardMeta,
	"NewServiceMeta":      guardMeta,
	"ListServiceMetas":    "[]" + guardMeta,
	"ManagedRuntimeIndex": "map:" + guardMeta,
}

// poolGuardScope is one function's syntactic view: the guardType of each
// name, which names carry a meta's Hostname and from where.
type poolGuardScope struct {
	pkg     *poolGuardPackage
	facts   *poolGuardFacts
	params  paramDerivation
	types   map[string]guardType
	tainted map[string]origin
}

func newPoolGuardScope(pkg *poolGuardPackage, facts *poolGuardFacts, fn *ast.FuncDecl) *poolGuardScope {
	sc := &poolGuardScope{
		pkg: pkg, facts: facts, params: paramDerived(fn, false),
		types: map[string]guardType{}, tainted: map[string]origin{},
	}
	sc.typeParams(fn.Type)
	for changed := true; changed; {
		changed = false
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			switch s := n.(type) {
			case *ast.FuncLit:
				sc.typeParams(s.Type)
			case *ast.DeclStmt:
				changed = sc.declare(s) || changed
			case *ast.AssignStmt:
				changed = sc.assign(s.Lhs, s.Rhs) || changed
			case *ast.RangeStmt:
				if s.Value != nil {
					changed = sc.setType(s.Value, sc.exprType(s.X).elem()) || changed
				}
			}
			return true
		})
	}
	return sc
}

func (sc *poolGuardScope) typeParams(ft *ast.FuncType) {
	if ft.Params == nil {
		return
	}
	for _, field := range ft.Params.List {
		if t := sc.pkg.typeOf(field.Type); t != "" {
			for _, name := range field.Names {
				sc.types[name.Name] = t
			}
		}
	}
}

func (sc *poolGuardScope) setType(e ast.Expr, t guardType) bool {
	id, ok := e.(*ast.Ident)
	if !ok || id.Name == "_" || t == "" || sc.types[id.Name] == t {
		return false
	}
	sc.types[id.Name] = t
	return true
}

func (sc *poolGuardScope) taint(e ast.Expr, o origin) bool {
	id, ok := e.(*ast.Ident)
	if !ok || id.Name == "_" || !o.tainted() {
		return false
	}
	next := sc.tainted[id.Name].union(o)
	if next.equal(sc.tainted[id.Name]) {
		return false
	}
	sc.tainted[id.Name] = next
	return true
}

func (sc *poolGuardScope) declare(s *ast.DeclStmt) bool {
	gd, ok := s.Decl.(*ast.GenDecl)
	if !ok {
		return false
	}
	changed := false
	for _, spec := range gd.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		lhs := make([]ast.Expr, len(vs.Names))
		for i, name := range vs.Names {
			lhs[i] = name
			if vs.Type != nil {
				changed = sc.setType(name, sc.pkg.typeOf(vs.Type)) || changed
			}
		}
		if len(vs.Values) > 0 {
			changed = sc.assign(lhs, vs.Values) || changed
		}
	}
	return changed
}

func (sc *poolGuardScope) assign(lhs, rhs []ast.Expr) bool {
	changed := false
	if len(rhs) == 1 && len(lhs) > 1 {
		// v, err := f(...): the call's results by position.
		call, ok := rhs[0].(*ast.CallExpr)
		if !ok {
			return false
		}
		for i, l := range lhs {
			changed = sc.setType(l, sc.resultType(call, i)) || changed
			changed = sc.taint(l, sc.returnedHost(call, i)) || changed
		}
		return changed
	}
	for i, l := range lhs {
		if i >= len(rhs) {
			break
		}
		changed = sc.setType(l, sc.exprType(rhs[i])) || changed
		changed = sc.taint(l, sc.carriesHost(rhs[i])) || changed
	}
	return changed
}

// resultType is the guardType of call's i-th result.
func (sc *poolGuardScope) resultType(call *ast.CallExpr, i int) guardType {
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok && isIdent(sel.X, "workflow") {
		if i == 0 {
			return workflowResults[sel.Sel.Name]
		}
		return ""
	}
	name := sc.pkg.callee(call)
	if name == "" {
		return ""
	}
	results := sc.pkg.funcs[name].Type.Results
	if results == nil {
		return ""
	}
	idx := 0
	for _, field := range results.List {
		n := max(len(field.Names), 1)
		if i < idx+n {
			return sc.pkg.typeOf(field.Type)
		}
		idx += n
	}
	return ""
}

// exprType is the guardType of the value e evaluates to.
func (sc *poolGuardScope) exprType(e ast.Expr) guardType {
	switch x := e.(type) {
	case *ast.ParenExpr:
		return sc.exprType(x.X)
	case *ast.StarExpr:
		return sc.exprType(x.X)
	case *ast.Ident:
		return sc.types[x.Name]
	case *ast.SelectorExpr:
		if name := sc.exprType(x.X).structName(); name != "" {
			if f := sc.pkg.field(name, x.Sel.Name); f != nil {
				return sc.pkg.typeOf(f.Type)
			}
		}
	case *ast.IndexExpr:
		return sc.exprType(x.X).elem()
	case *ast.CallExpr:
		return sc.resultType(x, 0)
	case *ast.UnaryExpr:
		return sc.exprType(x.X)
	case *ast.CompositeLit:
		return sc.pkg.typeOf(x.Type)
	}
	return ""
}

// valueOrigin is where a meta, or a struct carrying a meta's Hostname,
// comes from: the parameters it is selected, indexed or ranged out of, else
// the function itself.
func (sc *poolGuardScope) valueOrigin(e ast.Expr) origin {
	var o origin
	for i := range sc.params.params {
		if sc.params.from(e, i) {
			if o.params == nil {
				o.params = map[int]bool{}
			}
			o.params[i] = true
		}
	}
	if len(o.params) == 0 {
		o.local = true
	}
	return o
}

// returnedHost is where call's i-th result, a meta's Hostname, comes from.
func (sc *poolGuardScope) returnedHost(call *ast.CallExpr, i int) origin {
	name := sc.pkg.callee(call)
	if name == "" {
		return origin{}
	}
	ret, ok := sc.facts.returnsHost[name][i]
	if !ok {
		return origin{}
	}
	out := origin{local: ret.local}
	for p := range ret.params {
		if p < len(call.Args) {
			out = out.union(sc.valueOrigin(call.Args[p]))
		}
	}
	return out
}

// carriesHost is where the meta's Hostname e holds comes from; untainted
// when it holds none.
func (sc *poolGuardScope) carriesHost(e ast.Expr) origin {
	switch x := e.(type) {
	case *ast.ParenExpr:
		return sc.carriesHost(x.X)
	case *ast.Ident:
		return sc.tainted[x.Name]
	case *ast.SelectorExpr:
		recv := sc.exprType(x.X)
		if x.Sel.Name == "Hostname" && recv == guardMeta ||
			recv.structName() != "" && sc.facts.hostFields[recv.structName()+"."+x.Sel.Name] {
			return sc.valueOrigin(x.X)
		}
	case *ast.BinaryExpr:
		if x.Op == token.ADD {
			return sc.carriesHost(x.X).union(sc.carriesHost(x.Y))
		}
	case *ast.CallExpr:
		if sel, ok := x.Fun.(*ast.SelectorExpr); ok &&
			(isIdent(sel.X, "strings") && sel.Sel.Name == "TrimSpace" || isIdent(sel.X, "cmp") && sel.Sel.Name == "Or") {
			var o origin
			for _, arg := range x.Args {
				o = o.union(sc.carriesHost(arg))
			}
			return o
		}
		return sc.returnedHost(x, 0)
	}
	return origin{}
}

// taintedResults are fn's results (in return statements outside nested
// closures) that hold a meta's Hostname, by where it comes from.
func (sc *poolGuardScope) taintedResults(fn *ast.FuncDecl) map[int]origin {
	out := map[int]origin{}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			for i, r := range x.Results {
				if o := sc.carriesHost(r); o.tainted() {
					out[i] = out[i].union(o)
				}
			}
		}
		return true
	})
	return out
}

// taintedLiteralFields are the untagged fields ("<struct>.<field>") of
// this package's struct literals in fn set from a meta's Hostname — named,
// or elided inside a slice or map literal of one.
func (sc *poolGuardScope) taintedLiteralFields(fn *ast.FuncDecl) map[string]bool {
	out := map[string]bool{}
	var visit func(lit *ast.CompositeLit, t guardType)
	visit = func(lit *ast.CompositeLit, t guardType) {
		if lit.Type != nil {
			t = sc.pkg.typeOf(lit.Type)
		}
		if name := t.structName(); name != "" {
			for _, elt := range lit.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				key, ok := kv.Key.(*ast.Ident)
				if !ok || !sc.carriesHost(kv.Value).tainted() {
					continue
				}
				if f := sc.pkg.field(name, key.Name); f != nil && f.Tag == nil {
					out[name+"."+key.Name] = true
				}
			}
		}
		for _, elt := range lit.Elts {
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				elt = kv.Value
			}
			if inner, ok := elt.(*ast.CompositeLit); ok && inner.Type == nil {
				visit(inner, t.elem())
			}
		}
	}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if lit, ok := n.(*ast.CompositeLit); ok && lit.Type != nil {
			visit(lit, "")
		}
		return true
	})
	return out
}

// containerWork are the sites in body where container work takes a meta's
// Hostname: a host argument carrying it, or a meta (or carrier) handed to a
// callee that does the work without asking.
func (sc *poolGuardScope) containerWork(body *ast.BlockStmt) []work {
	out := sinkArgs(body, sc.pkg, sc.facts.hostSink, sc.facts.opsHostSink, sc.carriesHost)
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		name := sc.pkg.callee(call)
		for i := range sc.facts.handedOn[name] {
			if i < len(call.Args) && sc.carriesMeta(call.Args[i]) {
				out = append(out, work{pos: call.Args[i].Pos(), origin: sc.valueOrigin(call.Args[i])})
			}
		}
		return true
	})
	return out
}

// carriesMeta reports whether e is a meta, or one of this package's structs
// (or a slice/map of them) with a field carrying a meta's Hostname.
func (sc *poolGuardScope) carriesMeta(e ast.Expr) bool {
	t := sc.exprType(e)
	for t.elem() != "" {
		t = t.elem()
	}
	if t == guardMeta {
		return true
	}
	name := t.structName()
	if name == "" {
		return false
	}
	for field := range sc.facts.hostFields {
		if strings.HasPrefix(field, name+".") {
			return true
		}
	}
	return false
}

// asksDevIsPool reports whether fn reads a .DevIsPool selector, or hands an
// argument to a package function that asks it of that parameter.
func (sc *poolGuardScope) asksDevIsPool(fn *ast.FuncDecl) bool {
	asked := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.SelectorExpr:
			if x.Sel.Name == "DevIsPool" {
				asked = true
			}
		case *ast.CallExpr:
			if name := sc.pkg.callee(x); name != "" {
				for i := range sc.facts.askers[name] {
					if i < len(x.Args) {
						asked = true
					}
				}
			}
		}
		return !asked
	})
	return asked
}
