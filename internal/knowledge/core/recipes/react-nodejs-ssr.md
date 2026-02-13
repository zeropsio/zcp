# React SSR with Express on Zerops

React with Vite + Express SSR. Requires custom server.js implementation.

## zerops.yml
```yaml
zerops:
  - setup: app
    build:
      base: nodejs@20
      buildCommands:
        - pnpm i
        - pnpm build
      deployFiles:
        - public/
        - node_modules/
        - dist/
        - package.json
        - server.js  # Custom Express server
    run:
      ports:
        - port: 3000
          httpSupport: true
      start: pnpm start
```

## Gotchas
- **Custom server.js** MUST be implemented for Express server
- Not a standard Vite setup - requires manual SSR server implementation
- Deploy includes node_modules (runtime dependencies needed)
- See recipe-react-static for SSG version (no server needed)
