# CodeTap Extension

CodeTap discovers and connects to running CodeTap VS Code Server sessions.

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
