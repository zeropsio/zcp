# Discord Bot with Bun on Zerops

Discord.js bot running on Bun runtime -- direct TypeScript execution, no build step needed. Long-running gateway process.

## Keywords
discord, discordjs, bun, bot, typescript, gateway, slash-commands

## TL;DR
Discord.js bot on Bun -- runs TypeScript directly, requires `DISCORD_TOKEN` and `DISCORD_CLIENT_ID` as envSecrets. Long-running gateway process with no HTTP traffic.

## zerops.yml

```yaml
zerops:
  - setup: bot
    build:
      base: bun@1.2
      buildCommands:
        - bun i
      deployFiles: ./
      cache: node_modules
    run:
      base: bun@1.2
      ports:
        - port: 8080
          httpSupport: true
      start: bun start
```

## import.yml

```yaml
services:
  - hostname: bot
    type: bun@1.2
    envSecrets:
      DISCORD_CLIENT_ID: fill_your_client_id
      DISCORD_TOKEN: fill_your_bot_token
```

## Configuration

Bot entry point connects to the Discord gateway and registers slash commands:

```typescript
// src/index.ts
import { Client, GatewayIntentBits } from "discord.js";

const client = new Client({
  intents: [
    GatewayIntentBits.Guilds,
    GatewayIntentBits.GuildMessages,
    GatewayIntentBits.DirectMessages,
  ],
});

client.once("ready", () => {
  console.log("Discord bot is ready!");
});

client.login(process.env.DISCORD_TOKEN);
```

Config module reads envSecrets:

```typescript
// src/config.ts
export const config = {
  DISCORD_TOKEN: process.env.DISCORD_TOKEN!,
  DISCORD_CLIENT_ID: process.env.DISCORD_CLIENT_ID!,
};
```

## Common Failures

- **Bot does not start** -- `DISCORD_TOKEN` envSecret not set. Set it via Zerops GUI or import.yml before deploying.
- **Commands not registering** -- `DISCORD_CLIENT_ID` envSecret not set. Required for slash command registration via the REST API.
- **Process exits immediately** -- the bot process must stay alive via the Discord gateway connection. Ensure `client.login()` is called and the process is not killed by an unhandled error.

## Gotchas

- **No HTTP server** -- Discord bots connect to the Discord gateway via WebSocket. The `ports` declaration is required by Zerops for service routing but the bot does not serve HTTP traffic.
- **No build step** -- Bun runs TypeScript directly. The build phase only installs dependencies; the entire project is deployed via `deployFiles: ./`.
- **envSecrets for credentials** -- `DISCORD_TOKEN` and `DISCORD_CLIENT_ID` must be set as envSecrets in import.yml or via the Zerops GUI. They are sensitive and should never be in zerops.yml envVariables.
- **Single container recommended** -- running multiple bot containers causes duplicate event handling. Keep `maxContainers: 1` unless the bot is designed for sharding.
- **bun start** -- runs the `start` script from `package.json`, which executes `bun run src/index.ts` directly.
