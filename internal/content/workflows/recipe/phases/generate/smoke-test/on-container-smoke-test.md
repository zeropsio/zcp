# On-container smoke test

Every command below runs via SSH against the relevant dev service — `{hostname}dev` is `appdev`, `apidev`, or `workerdev` depending on which codebase is being validated. The package manager install command, compile or type-check command, start command, and HTTP port all come from `plan.Research` data captured at research.

## Three validation steps

### 1. Install dependencies on the container

Run the plan's package manager install command via SSH on the dev container. This catches hallucinated packages, version conflicts, and peer-dependency mismatches in seconds rather than after a full build cycle.

```
ssh {hostname}dev "cd /var/www && {packageManagerInstallCommand}"
```

### 2. Compile or type-check (if the framework has a compilation step)

Run the framework's compile or type-check command from research data. This catches type errors, syntax errors, and missing imports.

```
ssh {hostname}dev "cd /var/www && {compileOrTypeCheckCommand}"
```

### 3. Start the dev process and verify it binds to the expected port

Connection errors to managed services at this step are expected — env vars are not active yet. The goal is "process starts and binds to the port," not "app serves requests." Crashing immediately catches native-binding mismatches, missing modules, and config errors.

- **Implicit-webserver runtimes** (`php-nginx`, `php-apache`, `nginx`): the webserver auto-starts while the container is in the RUNNING state. Curl the port directly:

  ```
  ssh {hostname}dev "curl -s -o /dev/null -w '%{http_code}' http://localhost:{httpPort}/"
  ```

- **All other runtimes**: start the dev process explicitly and verify the port is bound:

  ```
  ssh {hostname}dev "cd /var/www && {startCommand} &"
  sleep 3
  ssh {hostname}dev "curl -s -o /dev/null -w '%{http_code}' http://localhost:{httpPort}/ || echo 'port not bound'"
  ```

## Handling failures

When the smoke test catches an error, fix it on the mount and re-run the failing step. Only proceed to `zerops_deploy` (in the deploy phase) when all three steps pass. Committing and deploying in the hope that the build container produces a different result from the one just observed on the dev container does not change the outcome.

## Multi-codebase plans

Run the three validation steps on each dev container independently. Each mount gets its own pass; every codebase's install + compile + start predicate must hold before the substep is attested.
