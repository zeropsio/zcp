# Rust on Zerops

## Keywords
rust, cargo, rustup, musl, static binary, release build, rust binary, cargo build

## TL;DR
Rust on Zerops compiles to a static binary; always use `--release`, cache `target/` directory, and deploy using tilde syntax for the binary.

## Zerops-Specific Behavior
- Versions: 1.80, 1.78, nightly, stable
- Base: Alpine (default) — musl target
- Build tool: Cargo (pre-installed)
- Working directory: `/var/www`
- No default port — must configure
- Deploy: Single binary (minimal runtime deps)

## Configuration
```yaml
zerops:
  - setup: myapp
    build:
      base: rust@stable
      buildCommands:
        - cargo build --release
      deployFiles:
        - ./target/release/~/myapp
      cache:
        - target
        - ~/.cargo/registry
    run:
      start: ./myapp
      ports:
        - port: 8080
          httpSupport: true
```

### Optimized Build (smaller binary)
```yaml
build:
  buildCommands:
    - RUSTFLAGS="-C target-feature=+crt-static" cargo build --release --target x86_64-unknown-linux-musl
  deployFiles:
    - target/x86_64-unknown-linux-musl/release/myapp
```

## Gotchas
1. **musl linking issues**: Some crates with C deps fail on Alpine's musl — use Ubuntu base or add musl-dev
2. **Cache `target/` and `~/.cargo/registry`**: Rust builds are slow — caching is critical
3. **Always use `--release`**: Debug builds are 10-100x slower and much larger
4. **Deploy only the binary**: Use tilde syntax `./target/release/~/myapp` to extract binary from deep path
5. **Use `rust@stable`**: Recipes use `stable` instead of `latest`

## See Also
- zerops://services/_common-runtime
- zerops://services/alpine
- zerops://services/ubuntu
- zerops://examples/zerops-yml-runtimes
