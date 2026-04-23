# CODEBASE_KB

## Pass — cites guide, names mechanism + symptom

```
### Cross-service env vars auto-inject — never declare the alias

Zerops injects `db_hostname`, `db_user`, `db_password`, `db_port`
project-wide. Declaring `DB_HOST: ${db_hostname}` creates a self-shadow:
the platform copy and the alias collide, `DB_HOST` reads empty
(symptom: connection timeout with blank host in the error).

Cite `env-var-model`. Fix: do not declare the alias; read
`db_hostname` directly.
```

## Fail — framework quirk disguised as gotcha

```
### @Controller collides with setGlobalPrefix

`@Controller('api/users')` + `app.setGlobalPrefix('api')` produces
`/api/api/users`. Use one OR the other.
```

Zero Zerops involvement — belongs in framework docs, not a gotcha.
