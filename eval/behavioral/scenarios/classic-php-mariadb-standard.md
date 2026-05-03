---
id: classic-php-mariadb-standard
description: |
  Greenfield PHP + MariaDB via classic bootstrap, standard pair (dev/stage).
  Tests RuntimeImplicitWeb path (php-apache or php-nginx) — webserver is part
  of the runtime, agent should NOT scaffold its own start command. Also
  tests non-Postgres managed dependency (MariaDB) for cross-service env-var
  pattern coverage.
seed: empty
tags: [bootstrap, classic-route, standard-pair, implicit-webserver, php, mariadb, managed-dep]
area: bootstrap
retrospective:
  promptStyle: briefing-future-agent
notableFriction:
  - id: implicit-webserver-no-start
    description: |
      php-apache / php-nginx are RuntimeImplicitWeb — platform manages the
      webserver, no start command needed in zerops.yaml. Surfaces whether
      agent reflexively writes a start: command anyway (anti-pattern for
      this runtime class).
  - id: php-version-pick
    description: |
      Live catalog has php-apache@{8.1,8.3,8.4,8.5} and php-nginx@same set.
      Agent must pick a concrete version. Surfaces whether agent guesses
      vs reads the live catalog.
  - id: mariadb-env-shape
    description: |
      MariaDB env catalog differs from Postgres (e.g. no connectionTlsString
      with the same shape). Surfaces whether the discover-env-catalog atom
      lists vars at runtime catalog instead of relying on training-data
      assumptions.
---

I want to deploy a PHP web app backed by MariaDB. I need both a development environment and a staging slot for testing builds.
