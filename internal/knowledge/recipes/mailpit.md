---
description: "Mailpit — a development mail catcher: a local SMTP server that captures all outgoing email so the app never sends real mail, plus a web UI to inspect captured messages. Deploy as a utility service alongside the app and point the app's SMTP config at host `mailpit`, port 1025."
repo: "https://github.com/zerops-recipe-apps/mailpit-app"
---

# Mailpit — dev mail catcher

Mailpit captures outgoing email in development instead of delivering it: it runs
an SMTP server (port 1025) and a web UI (port 8025) to inspect what the app
sent. Use it in dev/stage so mail features can be exercised without sending real
email. It is the opposite of an outbound SMTP setup — for real delivery use a
provider, not mailpit.

## Wiring

Add the mailpit service (via the recipe's import YAML / `zerops_import`), then
point the app's mail transport at it:

- SMTP host: `mailpit`
- SMTP port: `1025`
- No authentication, no TLS

Enable subdomain access to open the web UI (port 8025) in a browser.

## Not for production

Mailpit never delivers mail. Before production, switch the app's mail transport
to a real SMTP provider and remove the mailpit service.
