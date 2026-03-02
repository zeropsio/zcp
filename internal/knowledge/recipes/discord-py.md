# Discord Bot with Python on Zerops

Discord.py bot running on Python -- long-running gateway process with slash commands. Uses discord.py with commands extension.

## Keywords
discord, discordpy, python, bot, gateway, slash-commands, cogs

## TL;DR
Discord.py bot on Python -- requires `DISCORD_TOKEN` as envSecret, pip install via prepareCommands. Long-running gateway process with no HTTP traffic.

## zerops.yml

```yaml
zerops:
  - setup: bot
    build:
      base: python@3.12
      deployFiles: /~
      addToRunPrepare:
        - requirements.txt
    run:
      base: python@3.12
      prepareCommands:
        - python3 -m pip install --ignore-installed -r requirements.txt
      ports:
        - port: 8080
          httpSupport: true
      start: python3 bot.py
```

## import.yml

```yaml
services:
  - hostname: bot
    type: python@3.12
    envSecrets:
      DISCORD_CLIENT_ID: fill_your_client_id
      DISCORD_TOKEN: fill_your_bot_token
```

## Configuration

Bot entry point using discord.py commands extension with cog loading:

```python
# bot.py
import discord
from discord.ext import commands
import os

intents = discord.Intents.all()
bot = commands.Bot(command_prefix=["!"], intents=intents)

@bot.event
async def on_ready():
    await bot.tree.sync()
    print("Discord bot is ready!")

bot.run(os.environ["DISCORD_TOKEN"])
```

## Common Failures

- **Bot does not start** -- `DISCORD_TOKEN` envSecret not set. Set it via Zerops GUI or import.yml before deploying.
- **ModuleNotFoundError: discord** -- `prepareCommands` failed to install dependencies. Check that `requirements.txt` includes `discord.py` and is listed in `addToRunPrepare`.
- **Process exits immediately** -- the bot process must stay alive via the Discord gateway connection. Ensure `bot.run()` is called and no unhandled exceptions crash the process.

## Gotchas

- **No HTTP server** -- Discord bots connect to the Discord gateway via WebSocket. The `ports` declaration is required by Zerops for service routing but the bot does not serve HTTP traffic.
- **prepareCommands for pip** -- Python dependencies are installed via `prepareCommands` at runtime, not during build. The `addToRunPrepare` field ensures `requirements.txt` is available in the prepare layer.
- **envSecrets for credentials** -- `DISCORD_TOKEN` must be set as envSecret in import.yml or via the Zerops GUI. It is sensitive and should never be in zerops.yml envVariables.
- **deployFiles: /~** -- the trailing tilde deploys the contents of the root build directory to the runtime container root.
- **Single container recommended** -- running multiple bot containers causes duplicate event handling. Keep `maxContainers: 1` unless the bot is designed for sharding.
- **No build commands** -- Python does not need a compile step. The build phase only collects files for deployment.
