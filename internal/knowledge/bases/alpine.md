# Alpine on Zerops

## Keywords
alpine, musl, apk, base image, lightweight, zerops.yml

## TL;DR
Default base OS (~5MB). Uses musl libc -- some C libraries won't compile. Package manager: `apk add --no-cache`.

### Usage

Default base (~5MB), `apk add --no-cache`.

### Limitation

musl libc -- some C libraries won't compile. Use Ubuntu for glibc-dependent software.
