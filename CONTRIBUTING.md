# Contributing to CodeTap

## Architecture

The Go codebase follows a hexagonal (ports & adapters) architecture with three layers:

```
cmd/codetap/main.go          CLI: flag parsing, dependency wiring, command dispatch
        │
        ▼
internal/app/service.go      Application: orchestrates the Run/List/Clean workflows
        │
   ┌────┴────┐
   ▼         ▼
domain/    adapter/
ports.go   downloader/   HTTPDownloader    → downloads VS Code Server tarballs from Microsoft CDN
model.go   extractor/    TarExtractor      → unpacks tarballs, checks provisioning state
           server/       ProcessRunner     → launches code-server with signal forwarding
           store/        FileStore         → persists .json metadata + .token files
           relay/        Host / Container  → binary frame protocol for stdio multiplexing
           commit/       Resolver          → resolves versions/latest to commit hashes via MS Update API
           token/        RandomGenerator   → 32-byte crypto/rand tokens
           platform/     Platform          → arch detection + path resolution
           logger/       Stderr            → structured stderr logging
```

**Domain layer** (`internal/domain/`) defines data models (`Metadata`, `SocketEntry`) and port interfaces (`Downloader`, `Extractor`, `Provisioner`, `ServerRunner`, `MetadataStore`, `TokenGenerator`, `Logger`). No implementation details leak into this layer.

**Application layer** (`internal/app/`) contains `Service`, which takes all ports via constructor injection and orchestrates the full lifecycle: provision server → generate token → write metadata → start server → cleanup on exit.

**Adapter layer** (`internal/adapter/`) provides concrete implementations. All adapters are stateless or use file-based storage. The relay package implements a binary frame protocol (`[type:1][conn_id:4][length:4][payload]`) that multiplexes multiple VS Code connections over a single stdin/stdout pipe.

## Testing

All domain interfaces are tested via mocks in `internal/app/mock_test.go`. The relay frame codec and platform detection have dedicated unit tests.

```sh
make test
```

## Code Quality

Format, vet, and lint targets keep the codebase consistent:

```sh
make fmt      # gofmt all Go files
make vet      # go vet static analysis
make lint     # vet + golangci-lint (if installed)
make check    # vet + assert formatting (CI-oriented, non-modifying)
```

`make check` is the read-only gate used by CI — it fails if any file is not `gofmt`-clean.

## CI/CD

GitHub Actions pipeline (`.github/workflows/ci.yml`) triggers on pushes to `master`/`main`, pull requests, and version tags (`v*`).

| Job           | Runs on                              | What it does                                                                        |
| ------------- | ------------------------------------ | ----------------------------------------------------------------------------------- |
| **test**      | ubuntu-latest                        | `CGO_ENABLED=0 go test -v ./...`                                                    |
| **vet**       | ubuntu-latest                        | `gofmt` check → `go vet` → `golangci-lint`                                          |
| **build**     | ubuntu-latest (matrix: amd64, arm64) | Cross-compiles static binaries, verifies static linking on amd64, uploads artifacts |
| **extension** | ubuntu-latest                        | `npm ci` → compile → `vsce package` → uploads `.vsix` artifact                      |
| **release**   | ubuntu-latest (on `v*` tags only)    | Downloads all artifacts, creates GitHub Release with 2 binaries + `.vsix`           |

The build job depends on both test and vet passing. The release job depends on both build and extension succeeding. Tagging a `v*` release produces the full artifact set: two static binaries and a `.vsix`.

## Project structure

```
codetap/
├── cmd/codetap/main.go           # CLI entry point
├── internal/
│   ├── domain/
│   │   ├── model.go              # Metadata, SocketEntry
│   │   └── ports.go              # Interface definitions
│   ├── app/
│   │   ├── service.go            # Application service layer
│   │   ├── service_test.go       # Service tests
│   │   └── mock_test.go          # Mock implementations
│   ├── adapter/
│   │   ├── commit/resolve.go     # Commit hash resolution (version/latest → hash)
│   │   ├── downloader/http.go    # HTTP tarball downloader
│   │   ├── extractor/tar.go      # Tar extraction + provisioning
│   │   ├── logger/stderr.go      # Stderr structured logger
│   │   ├── platform/platform.go  # Architecture + path resolution
│   │   ├── relay/                # Stdio mux relay (frame protocol)
│   │   │   ├── frame.go          # Wire format codec
│   │   │   ├── host.go           # Host-side multiplexer
│   │   │   └── container.go      # Container-side multiplexer
│   │   ├── server/process.go     # VS Code Server process manager
│   │   ├── store/file.go         # File-based metadata store
│   │   └── token/random.go       # Crypto token generator
├── extension/                    # VS Code extension (TypeScript)
│   ├── src/
│   │   ├── extension.ts          # Extension entry point
│   │   ├── resolver.ts           # Remote authority resolver
│   │   ├── sessionProvider.ts    # TreeView data provider
│   │   ├── sessionWatcher.ts     # Session polling watcher
│   │   └── types.ts              # TypeScript interfaces
│   └── package.json
├── Makefile
└── .github/workflows/ci.yml     # CI/CD pipeline
```
