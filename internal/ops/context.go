package ops

// GetContext returns a static precompiled Zerops platform context string.
// Used by the zerops_context MCP tool to provide platform knowledge to AI agents.
// Content is ~800-1200 tokens covering platform fundamentals and critical rules.
func GetContext() string {
	return zeropsContext
}

const zeropsContext = `# Platform Context

## Overview

Zerops is a developer-first PaaS built on bare-metal infrastructure. It runs full Linux containers
(Incus/LXD), not serverless functions. Every container has SSH access, a real filesystem, and runs
managed services (PostgreSQL, MariaDB, Valkey, Elasticsearch, Meilisearch, Kafka, S3-compatible
object storage). Infrastructure is VXLAN-networked with automatic service discovery.

## How Zerops Works

Zerops organizes resources in a hierarchy: Project -> Services -> Containers.

- **Project**: isolated environment with private VXLAN network. All services within a project
  can communicate via hostnames (e.g., http://db:5432).
- **Service**: a logical unit (e.g., "api", "db", "cache") backed by one or more containers.
  Each service has a hostname, type, and scaling configuration.
- **Container**: actual Linux instance running the service. Auto-scaled horizontally (1-N containers)
  and vertically (CPU/RAM/disk).

Networking: services communicate over a private VXLAN overlay network using hostnames.
External traffic enters through an L7 load balancer that terminates SSL.

## Critical Rules

- **Internal networking uses http://, NEVER https://** — SSL terminates at the L7 balancer.
  Services must connect to each other via http://hostname:port.
- **Ports must be in range 10-65435** — ports 0-9 and 65436+ are reserved by the platform.
- **HA mode is immutable** — once a service is created as HA or NON_HA, it cannot be changed.
  Recreate the service to switch modes.
- **Database/cache services REQUIRE mode** — import.yml must specify mode: NON_HA or HA for
  databases (postgresql, mariadb, clickhouse) and caches (valkey, keydb). Omitting mode passes
  dry-run validation but fails real import.
- **Environment variable cross-references use underscores** — ${service_hostname}, not
  ${service-hostname}. Dashes in hostnames are replaced with underscores in env var references.
- **No localhost** — services cannot use localhost/127.0.0.1 to reach other services. Always
  use the service hostname.
- **prepareCommands are cached** — they run once and are cached. Use initCommands for logic
  that must run on every container start.

## Configuration

- **zerops.yml** — build + deploy + run configuration per service. Defines build pipeline
  (base, prepareCommands, buildCommands, deployFiles), runtime (base, initCommands, start),
  and ports/routing.
- **import.yml** — infrastructure-as-code for service creation. Contains a services: array
  defining service type, version, mode, hostname, and initial scaling. Must NOT contain a
  project: section (projects are created separately).

## Service Types

| Category | Services |
|----------|----------|
| Runtime | nodejs, php, python, go, rust, java, dotnet, elixir, gleam, bun, deno |
| Container | alpine, ubuntu, docker (VM-based) |
| Database | postgresql (default), mariadb, clickhouse |
| Cache | valkey (default, Redis-compatible), keydb (deprecated) |
| Search | meilisearch (default), elasticsearch, typesense, qdrant (internal-only) |
| Queue | nats (default), kafka |
| Storage | object-storage (S3/MinIO), shared-storage (POSIX) |
| Web | nginx, static (SPA-ready) |

## Defaults

When not specified, Zerops uses these defaults:
- postgresql@16, valkey@7.2, meilisearch@1.10, nats@2.10
- alpine base image for custom containers
- NON_HA mode (single container, no high availability)
- SHARED CPU mode (burstable, cost-effective)

## Pointers

- Use zerops_knowledge tool to search Zerops documentation for specific topics.
- Use zerops_workflow tool for step-by-step guidance on common tasks (bootstrap, deploy, debug, scale, configure, monitor).
- Use zerops_discover tool to inspect current project and service state.`
