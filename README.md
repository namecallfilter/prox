# prox

prox is a personal fork of [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) by router-for-me. It is a proxy server that signs in to CLI subscriptions (Claude Code, Codex, Antigravity and others) and serves them through OpenAI, Gemini, Claude and Codex compatible API endpoints, pooling several accounts per provider behind one address.

All of the proxy itself is upstream's work. This fork is unofficial and is not affiliated with router-for-me.

## What this fork changes

- **`soonest-reset` routing.** New sessions go to the account whose weekly quota window resets soonest, so allowance that is about to expire is used first. Reset times come from the Claude and Codex response headers the proxy already records. Accounts with no reset on record are tried first, in rotation, and providers that report none keep round-robin.
- **Name.** The binary is `prox`, it identifies itself as prox, and the default auth directory is `~/.prox`.

Go import paths, config keys, the plugin ABI and the Management API are unchanged, so upstream plugins, panels and docs still apply.

## Build and run

```bash
go build -o prox ./cmd/server
cp config.example.yaml config.yaml
./prox -config config.yaml
```

Sign accounts in on the machine that runs prox:

```bash
./prox -claude-login
./prox -codex-login          # or -codex-device-login on a headless machine
```

## Routing

```yaml
routing:
  strategy: "soonest-reset" # round-robin (default), weighted-round-robin, fill-first, soonest-reset
  session-affinity: true    # keep a session on one account so prompt caches are reused
  session-affinity-ttl: "1h"
```

## Google accounts through the official Antigravity CLI

prox loads the [cliproxy-antigravity](https://github.com/adeebahmad01/cliproxy-antigravity) plugin unchanged. It runs Google's `agy` CLI for each request instead of reusing extracted OAuth tokens. To make that the only path for Google accounts, enable the plugin and switch off the built-in Antigravity channel:

```yaml
oauth:
  excluded-models:
    antigravity: ["*"]
plugins:
  enabled: true
  dir: "plugins"
  configs:
    cliproxy-antigravity:
      enabled: true
      binary_path: "agy"
      accounts_dir: "~/.agy-accounts" # one agy home per Google account
      auth_dir: "~/.prox"             # the plugin assumes ~/.cli-proxy-api unless told
```

Without `auth_dir` the plugin looks for upstream's default auth directory, finds nothing, and silently imports no accounts.

## Documentation

Upstream's guides describe everything not listed above: [help.router-for.me](https://help.router-for.me/). Where they say `cli-proxy-api` or `~/.cli-proxy-api`, use `prox` and `~/.prox`.

- Management API: [help.router-for.me/management/api](https://help.router-for.me/management/api)
- Go SDK: [docs/sdk-usage.md](docs/sdk-usage.md), [docs/sdk-advanced.md](docs/sdk-advanced.md), [docs/sdk-access.md](docs/sdk-access.md), [docs/sdk-watcher.md](docs/sdk-watcher.md)
- Plugin examples: [examples/plugin](examples/plugin/README.md)

## License

MIT, the same as upstream. See [LICENSE](LICENSE).
