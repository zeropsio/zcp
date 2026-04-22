---
surface: gotcha
verdict: pass
reason: platform-invariant-ok
title: "MinIO-backed object storage rejects virtual-hosted style"
---

> ### `forcePathStyle: true` is mandatory for Zerops object storage
>
> **Symptom**: the S3 SDK's default request shape returns
> `403 Forbidden` with a body saying the hostname
> `<bucket>.storage.zerops.io` does not resolve.
>
> **Mechanism**: Zerops object storage is MinIO-backed. MinIO only serves
> path-style addressing (`https://storage/<bucket>/<key>`); the S3 SDK
> defaults to virtual-hosted addressing
> (`https://<bucket>.storage/<key>`) which MinIO rejects.
>
> **Rule** (see `zerops_knowledge topic=object-storage`): pass
> `forcePathStyle: true` to the S3 client constructor. Also set the
> endpoint to `https://${storage_hostname}` — do NOT prefix with the
> bucket name.

**Why this passes the gotcha test.**
- Mechanism is platform-invariant (MinIO-backed object storage with a
  specific addressing constraint).
- Symptom is concrete (`403 Forbidden` + specific hostname-resolution
  body).
- Rule is load-bearing and cites the platform guide.
- Any porter using S3 SDK against Zerops hits this regardless of
  framework.

Spec §7 classification: platform-invariant. Route to gotcha; cite guide.
