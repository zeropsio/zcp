# Pool template, repository half (`pool_template.go`)

- PT-SEED-POOL — A seed gives each part a pool setup named after its pool that builds and runs on the part's own runtime type, never the pool's `swarm@1`, installs zcp-devsup and starts `zcp-devsup run`, so a lease's container has the part's toolchain under the supervisor.
- PT-SEED-PORTS — A seed's pool setup declares exactly the ports previewPortBase + 1 … previewPortBase + maxContainers, each with httpSupport, so every lease's preview has its port and no port beside them is opened.
- PT-SEED-STAGE — A seed gives each part a stage setup named after the part that deploys `./`, with the idle start for a runtime that takes a start command and none for a static one, so the stage builds before the part has code.
- PT-CHECK-MISSING — A main holding code without the parts' setups is refused naming each part's missing pool setup, each missing stage setup and the missing dev-server declaration, so the person knows what to add.
- PT-CHECK-SEED — The seed's own two files pass the setup check, so a plan sent again on a seeded main writes nothing and is never refused.
