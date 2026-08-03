---
description: A newcomer orientation to Zerops & ZCP — the platform model, subdomain URLs, the dev/stage pattern, and the consent boundary. Fetched by the onboarding playbook's "What are Zerops & ZCP?" branch.
---

# Getting oriented: Zerops & ZCP

A project is a private network. Every service you add — your app, a database, a
cache — runs inside it and is reachable only from where you explicitly expose it.
Services find each other by hostname, not by IP address or connection string: your
app looks up its database as `db`, and that's it — no addresses, no credentials
baked into code. Source becomes a running app through build → deploy → run: your
code is built into an image, the image is deployed as a release, and a service runs
it.

A `*.zerops.app` subdomain is Zerops's own instant public URL — every stage service
gets one automatically, live within seconds of a successful deploy, no DNS
configuration required. It gets you to a working app fast; a custom domain is the
path once you're ready for production traffic.

A project built from a recipe carries a dev/stage pair. Dev is the workbench —
where you and the agent make changes and watch the effect immediately. Stage is
the built app that actually serves traffic; its subdomain URL is the live one. The
dev service sits idle when nothing is running on it — that's by design, not a
fault.

ZCP is the control layer the agent drives on your behalf: it sets up a project from
a recipe or your own source, develops it with you, deploys it, and manages it
afterward — scaling, logs, environment variables, domains — all from
plain-language requests.

Two kinds of acts always wait for your explicit yes first: anything destructive or
hard to undo — deleting a service, or overwriting a running deployment through a
forced re-import — and anything that touches a credential you hold, like a Git
token. Everyday deploys are routine and don't wait on you each time; a corrective
redeploy that fixes a failed build just runs on its own.

When you're ready, it's the same two active options: **Build something** or **Try
a ready-made recipe**. Or just tell me what you want, in plain words — that works
for everything here.
