---
id: develop-env-cheatsheet-sql
priority: 2
phases: [develop-active]
envelopeDeployStates: [never-deployed]
managedTypes: [postgresql, mariadb, mysql]
title: "Env keys — SQL databases"
---

### SQL database env keys

- **Postgres / MariaDB / MySQL** — `connectionString` is
  `protocol://${user}:${password}@${hostname}:${port}` and **omits the
  db name**; append `/${db_dbName}` for Prisma / Drizzle / sqlx /
  SQLAlchemy / Sequelize (worked example in the env-var-model atom).
- **Prisma `migrate dev` P3014** — shadow DB needs DDL the regular user
  lacks: use `prisma db push` for fresh schemas, or pass the
  `${db_superUser}:${db_superUserPassword}` URL for that one call.
- **Elevated DDL** — `superUser`/`superUserPassword`, only when DDL
  needs them.
