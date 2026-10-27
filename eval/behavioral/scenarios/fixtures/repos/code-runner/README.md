# code-runner

`POST /run` with `{"code": "<python source>"}` runs the snippet and answers
`{"output": "<stdout>", "exitCode": <n>}`.

The code never runs here. Each submission gets a clean container from the
`sandbox` Swarm pool (`http://orch.sandbox.zerops`), and the container is
released with a reset afterwards. `SANDBOX_TOKEN` is the pool's `API_TOKEN`,
referenced in `zerops.yml`, never written down.
