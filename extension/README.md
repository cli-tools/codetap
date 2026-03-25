# CodeTap Extension

CodeTap discovers running CodeTap VS Code Server sessions.

## Features

- Sidebar session list with alive/dead status via CTAP1 control protocol
- Automatic session discovery via socket polling

## Commands

- `CodeTap: Refresh Sessions` (`codetap.refresh`)

## Settings

- `codetap.socketDir` (default: `/dev/shm/codetap`)
- `codetap.pollInterval` (default: `3000`)

## Development

```sh
npm ci
npm run compile
npm run package   # produces codetap.vsix
```

## Publishing

The extension is published to the [VS Code Marketplace](https://marketplace.visualstudio.com/items?itemName=cli-tools.codetap) under the `cli-tools` publisher.

Publish a new version (requires Microsoft account with access to the `cli-tools` publisher):

```sh
# Bump version in package.json first, then:
npm run publish
```

This uses `vsce` with Azure credential auth (`--azure-credential`) — no PAT or Azure DevOps project required, just a Microsoft account that has access to the `cli-tools` publisher on the Marketplace.
