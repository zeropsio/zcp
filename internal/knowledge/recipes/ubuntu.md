# Ubuntu on Zerops

## Keywords
ubuntu, glibc, apt-get, base image, cgo, zerops.yml

## TL;DR
Full glibc base (~100MB). Use for CGO Go, Python C extensions, legacy glibc-dependent software. Package manager: `apt-get update && apt-get install -y`.

### Usage

Full glibc (~100MB), `apt-get update && apt-get install -y`.

### When to Use

Use for: CGO Go, Python C extensions, Deno, legacy glibc-dependent software.
