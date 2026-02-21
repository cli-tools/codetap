# CodeTap Extension

CodeTap discovers and connects to running CodeTap VS Code Server sessions.

> Note: this extension uses the VS Code resolver API proposal.
> One-time setup on stable VS Code:
> 1. Run `Preferences: Configure Runtime Arguments`.
> 2. Add this to `argv.json`:
>    ```json
>    {
>      "enable-proposed-api": ["codetap.codetap"]
>    }
>    ```
> 3. Restart VS Code.
> Flatpak `argv.json` path:
> `~/.var/app/com.visualstudio.code/config/Code/argv.json`

## Features

- Sidebar session list with alive/dead status
- Connect to a selected alive session
- Copy session connection token

## Commands

- `CodeTap: Connect to Session` (`codetap.connect`)
- `CodeTap: Refresh Sessions` (`codetap.refresh`)
- `CodeTap: Copy Connection Token` (`codetap.copyToken`)

## Settings

- `codetap.socketDir` (default: `/dev/shm/codetap`)
- `codetap.pollInterval` (default: `3000`)

## Development

```sh
npm ci
npm run compile
npm run package
```
