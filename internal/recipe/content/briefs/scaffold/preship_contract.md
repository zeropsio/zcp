# Pre-ship contract

Verify behavior, not code. Preship passes when all five are green on
the dev container.

1. `curl ${appdev-subdomain}/health` → 200 + body.
2. `curl -H "X-Forwarded-For: 1.2.3.4" /debug/remote-ip` echoes
   `1.2.3.4`. Remove `/debug/remote-ip` before prod tiers.
3. Restart the service while a 2s+ request is in flight — request
   finishes 200, new requests hit the new container.
4. Migrations attested once post-deploy; second deploy does not re-run.
5. stderr in first 10s post-boot is empty of error-level logs.

Record a fact for any deviation.
