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

- Sidebar session list with alive/dead status via CTAP1 control protocol
- Connect to a selected alive session (CTAP1 CONNECT handshake)
- Automatic version negotiation with the server

## Commands

- `CodeTap: Connect to Session` (`codetap.connect`)
- `CodeTap: Refresh Sessions` (`codetap.refresh`)

## Settings

- `codetap.socketDir` (default: `/dev/shm/codetap`)
- `codetap.pollInterval` (default: `3000`)

## Development

```sh
npm ci
npm run compile
npm run package
```
