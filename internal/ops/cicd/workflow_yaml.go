package cicd

import "fmt"

// stageWorkflowYAML returns the body of .github/workflows/zerops-stage.yml.
// Triggered by push to main. Uses raw `zcli` (not zeropsio/actions@v1.0.2)
// because the marketplace action doesn't accept a `setup` parameter —
// stage cicd needs `--setup <pair-keyed-name>` to pick the right build
// block from zerops.yaml.
//
// ServiceID is inlined into `--service-id` (not stored as a secret —
// the ID is not sensitive). SetupName is inlined into `--setup`.
func stageWorkflowYAML(serviceID, setupName string) string {
	return fmt.Sprintf(`name: zerops-stage-deploy
on:
  push:
    branches: [main]
jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Install zcli
        run: |
          curl -sSL https://zerops.io/zcli/install.sh | sh
          echo "$HOME/.local/bin" >> "$GITHUB_PATH"
      - name: zcli login
        run: zcli login "$ZEROPS_TOKEN_STAGE"
        env:
          ZEROPS_TOKEN_STAGE: ${{ secrets.ZEROPS_TOKEN_STAGE }}
      - name: zcli push
        run: zcli push --service-id %s --setup %s
        env:
          ZEROPS_TOKEN_STAGE: ${{ secrets.ZEROPS_TOKEN_STAGE }}
`, quoteShellLiteral(serviceID), quoteShellLiteral(setupName))
}

// prodWorkflowYAML returns the body of .github/workflows/zerops-prod.yml.
// Triggered by tag push matching v*.*.*. Same raw-zcli pattern as
// stage; secret name + workflow filename differ so both can coexist in
// the same repo. Setup name defaults to "stage" (the production build
// reuses the source repo's stage build recipe — same recipe, different
// destination service-stack).
func prodWorkflowYAML(serviceID, setupName string) string {
	return fmt.Sprintf(`name: zerops-prod-deploy
on:
  push:
    tags: ['v*.*.*']
jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Install zcli
        run: |
          curl -sSL https://zerops.io/zcli/install.sh | sh
          echo "$HOME/.local/bin" >> "$GITHUB_PATH"
      - name: zcli login
        run: zcli login "$ZEROPS_TOKEN_PROD"
        env:
          ZEROPS_TOKEN_PROD: ${{ secrets.ZEROPS_TOKEN_PROD }}
      - name: zcli push
        run: zcli push --service-id %s --setup %s
        env:
          ZEROPS_TOKEN_PROD: ${{ secrets.ZEROPS_TOKEN_PROD }}
`, quoteShellLiteral(serviceID), quoteShellLiteral(setupName))
}
