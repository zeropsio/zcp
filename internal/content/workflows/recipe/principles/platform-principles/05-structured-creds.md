# platform-principles / structured-creds

Managed-service credentials and endpoints come from the platform as separate env vars. Treat them as separate fields at the client-construction site — do not concatenate them into a URL with embedded credentials.

## NATS: user and pass are separate ConnectionOptions fields

NATS client libraries accept the servers list and the credentials as distinct fields:

```typescript
connect({
  servers: `${process.env.queue_hostname}:${process.env.queue_port}`,
  user:    process.env.queue_user,
  pass:    process.env.queue_password,
})
```

The `servers` value is the `hostname:port` pair only — no scheme, no credentials embedded. The `user` and `pass` fields come from the platform-provided env vars as-is.

The URL-embedded shape `nats://user:pass@host:port` fails on reconnect, leaks credentials into connection logs, and re-parses differently across clients. Keep them as separate fields.

## S3: endpoint is the apiUrl, not the apiHost

Object-storage services expose two env var forms:

- `{service}_apiUrl` — the full https:// URL the client connects to.
- `{service}_apiHost` — a host-only value that resolves to an http:// listener returning a 301 redirect to the apiUrl.

Use `apiUrl`. The apiHost 301-redirect path breaks S3 clients that do not follow redirects on signed requests (the signature is computed against one URL; the redirect target rejects the same signature).

```typescript
new S3Client({
  endpoint:       process.env.storage_apiUrl,       // https://
  forcePathStyle: true,
  credentials: {
    accessKeyId:     process.env.storage_accessKeyId,
    secretAccessKey: process.env.storage_secretAccessKey,
  },
})
```

## forcePathStyle: true

`forcePathStyle: true` is required. S3-compatible services (including the Zerops object-storage service) expose buckets as path-prefixed (`https://endpoint/bucket/key`) rather than virtual-hosted (`https://bucket.endpoint/key`). The default for many AWS-flavored S3 clients is virtual-hosted; leaving the default produces DNS-resolution failures for every request.

## Pre-attest before returning

```
ssh {host} "grep -rn 'storage_apiHost' /var/www/src 2>/dev/null; test $? -eq 1"
ssh {host} "grep -rn 'forcePathStyle' /var/www/src 2>/dev/null | grep -q true"
ssh {host} "grep -rnE 'nats://[^ \t]*:[^ \t]*@' /var/www 2>/dev/null; test $? -eq 1"
```

The first succeeds when no source file references `storage_apiHost`. The second succeeds when a `forcePathStyle: true` line is present. The third succeeds when no file embeds credentials in a nats:// URL.
