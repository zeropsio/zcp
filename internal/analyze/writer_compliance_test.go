package analyze

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func validReadme() string {
	return `# apidev

<!-- #ZEROPS_EXTRACT_START:intro# -->
The apidev service runs a NestJS API connected to PostgreSQL and NATS.
<!-- #ZEROPS_EXTRACT_END:intro# -->

<!-- #ZEROPS_EXTRACT_START:integration-guide# -->
### Adding zerops.yaml

` + "```yaml" + `
zerops:
  - setup: apidev
` + "```" + `

### Bind to 0.0.0.0

` + "```ts" + `
app.listen(3000, '0.0.0.0');
` + "```" + `

### Trust proxy

` + "```ts" + `
app.set('trust proxy', true);
` + "```" + `
<!-- #ZEROPS_EXTRACT_END:integration-guide# -->

<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->
### Gotchas

- **connection refused on 127.0.0.1** — the L7 balancer reaches containers via VXLAN; bind to 0.0.0.0.
- **Content-Type application/x-www-form-urlencoded rejected** — NestJS default pipes. Enable raw body parsing.
- **zsc execOnce "success" but migration silently skipped** — ts-node not installed in prod image; initCommands ran the plain .js path.
<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->
`
}

func validClaudeMd() string {
	body := strings.Repeat("Some substantive content about the repo and its workflows. ", 30)
	return "# apidev CLAUDE.md\n\n## Dev Loop\n" + body + "\n\n## Migrations\n" + body + "\n\n## Container Traps\n" + body + "\n\n## Testing\n" + body + "\n\n## Resetting dev state\n" + body + "\n\n## Log tailing\n" + body + "\n"
}

func TestReadme_ValidPass(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "README.md")
	if err := os.WriteFile(p, []byte(validReadme()), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := Readme(p)
	if got.Status != StatusPass {
		t.Errorf("status=%s want=pass intro=%+v ig=%+v kb=%+v", got.Status, got.IntroFragment, got.IntegrationGuideFragment, got.KnowledgeBaseFragment)
	}
	if got.IntegrationGuideFragment.H3Count != 3 {
		t.Errorf("ig h3_count=%d want=3", got.IntegrationGuideFragment.H3Count)
	}
	if got.KnowledgeBaseFragment.GotchaBulletCount != 3 {
		t.Errorf("kb bullet_count=%d want=3", got.KnowledgeBaseFragment.GotchaBulletCount)
	}
}

func TestReadme_BrokenMarkersFail(t *testing.T) {
	t.Parallel()
	bad := strings.ReplaceAll(validReadme(), ":intro#", ":intro")
	dir := t.TempDir()
	p := filepath.Join(dir, "README.md")
	if err := os.WriteFile(p, []byte(bad), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := Readme(p)
	if got.IntroFragment.MarkersExactForm {
		t.Errorf("markers_exact_form=true want=false (broken :intro key)")
	}
	if got.Status != StatusFail {
		t.Errorf("status=%s want=fail", got.Status)
	}
}

func TestReadme_MissingFileFail(t *testing.T) {
	t.Parallel()
	got := Readme("/nonexistent/README.md")
	if got.FileExists {
		t.Errorf("file_exists=true want=false")
	}
	if got.Status != StatusFail {
		t.Errorf("status=%s want=fail", got.Status)
	}
}

func TestClaudeMd_ValidPass(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "CLAUDE.md")
	if err := os.WriteFile(p, []byte(validClaudeMd()), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := ClaudeMd(p)
	if got.Status != StatusPass {
		t.Errorf("status=%s want=pass (%+v)", got.Status, got)
	}
	if !got.SizeGE1200 || !got.CustomSectionsGE2 || len(got.BaseSectionsPresent) != 4 {
		t.Errorf("signals %+v", got)
	}
}

func TestClaudeMd_ShortFileFail(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "CLAUDE.md")
	if err := os.WriteFile(p, []byte("## Dev Loop\nshort\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := ClaudeMd(p)
	if got.Status != StatusFail {
		t.Errorf("status=%s want=fail", got.Status)
	}
	if got.SizeGE1200 {
		t.Errorf("size_ge_1200=true want=false (short file)")
	}
}

func TestCollectWriterCompliance_MissingFilesReported(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	readmes, claudeMds := CollectWriterCompliance(dir, []string{"apidev"})
	r := readmes["apidev/README.md"]
	c := claudeMds["apidev/CLAUDE.md"]
	if r.FileExists || c.FileExists {
		t.Errorf("expected both missing; got readme=%v claude=%v", r.FileExists, c.FileExists)
	}
}
