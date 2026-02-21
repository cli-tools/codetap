package main

import (
	"crypto/rand"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"codetap/internal/adapter/commit"
	"codetap/internal/adapter/downloader"
	"codetap/internal/adapter/extractor"
	"codetap/internal/adapter/logger"
	"codetap/internal/adapter/platform"
	"codetap/internal/adapter/relay"
	"codetap/internal/adapter/server"
	"codetap/internal/adapter/store"
	"codetap/internal/adapter/token"
	"codetap/internal/app"
	"codetap/internal/domain"
)

const usage = `codetap — VS Code Server for containers

Usage:
  codetap run [flags]                Start a VS Code Server session
  codetap relay [flags] -- CMD...    Relay a remote session over stdio
  codetap list [flags]               List discovered sessions
  codetap clean [flags]              Remove stale sessions

Running with no subcommand prints this help. Flags without a subcommand
default to "codetap run" (e.g. codetap --commit abc123).

Examples:
  # Auto-resolves latest VS Code Server
  codetap run --name myproject

  # Pin to a specific VS Code version
  codetap run --commit 1.109.5 --name myproject

  # Stdio relay via docker (no --ipc=host needed)
  codetap relay --name dev -- docker exec -i ctr codetap run --stdio

  # Stdio relay via SSH
  codetap relay --name remote -- ssh host codetap run --stdio

  # Stdio relay via kubectl
  codetap relay --name pod -- kubectl exec -i pod -- codetap run --stdio

Run "codetap COMMAND --help" for command-specific flags.
`

// printFlags formats flag defaults with -- prefix instead of Go's default single -.
func printFlags(fs *flag.FlagSet) {
	fs.VisitAll(func(f *flag.Flag) {
		isBool := f.DefValue == "false" || f.DefValue == "true"
		if isBool {
			fmt.Fprintf(os.Stderr, "  --%-20s %s\n", f.Name, f.Usage)
		} else {
			label := f.Name + " " + strings.ToUpper(strings.ReplaceAll(f.Name, "-", "_"))
			fmt.Fprintf(os.Stderr, "  --%-20s %s\n", label, f.Usage)
		}
	})
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(0)
	}

	arg := os.Args[1]

	switch arg {
	case "-h", "-help", "--help", "help":
		fmt.Fprint(os.Stderr, usage)
		os.Exit(0)
	case "run":
		runCmd(os.Args[2:])
	case "list":
		listCmd(os.Args[2:])
	case "clean":
		cleanCmd(os.Args[2:])
	case "relay":
		relayCmd(os.Args[2:])
	default:
		if arg[0] == '-' {
			// Flags without subcommand → treat as "run"
			runCmd(os.Args[1:])
		} else {
			fmt.Fprintf(os.Stderr, "codetap: unknown command %q\n\n", arg)
			fmt.Fprint(os.Stderr, usage)
			os.Exit(1)
		}
	}
}

func runCmd(args []string) {
	fs := flag.NewFlagSet("codetap run", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, `Start a VS Code Server session on a Unix socket in /dev/shm/codetap/.

Usage:
  codetap run [flags]

The --commit value can be a 40-char hex hash, a version like "1.109.5", or
"latest". If omitted, it is auto-resolved from: --commit flag > CODETAP_COMMIT
env > ~/.codetap/.commit > local "code --version" > latest stable from Microsoft.

Flags:`)
		printFlags(fs)
	}

	name := fs.String("name", "", "session name (default: hostname)")
	commitFlag := fs.String("commit", "", "version, commit hash, or \"latest\" (auto-resolved if omitted)")
	folder := fs.String("folder", "", "workspace folder path (default: cwd)")
	socketDir := fs.String("socket-dir", "", "socket directory (default: /dev/shm/codetap)")
	stdio := fs.Bool("stdio", false, "relay traffic over stdin/stdout instead of /dev/shm")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}

	log := logger.NewStderr()

	plat, err := platform.New()
	if err != nil {
		fatal(err)
	}

	arch, err := plat.DetectArch()
	if err != nil {
		fatal(err)
	}

	// Commit resolution chain: flag → env → file → code --version → latest from API
	rawCommit, err := plat.ResolveCommit(*commitFlag)
	if err != nil {
		fatal(err)
	}

	resolver := commit.NewResolver(arch)

	resolvedCommit, err := resolver.Resolve(rawCommit)
	if err != nil {
		fatal(err)
	}

	if resolvedCommit == "" {
		if probe, _ := commit.ProbeCodeCLI(); probe != "" {
			log.Info("detected commit from local VS Code", "commit", probe[:12])
			resolvedCommit = probe
		}
	}

	if resolvedCommit == "" {
		log.Info("no commit specified, fetching latest stable from Microsoft")
		resolvedCommit, err = resolver.Resolve("latest")
		if err != nil {
			fatal(fmt.Errorf("auto-resolve commit: %w\n\nTo run offline, provide --commit, set CODETAP_COMMIT, or write a value to ~/.codetap/.commit", err))
		}
		log.Info("resolved latest stable", "commit", resolvedCommit[:12])
	}

	resolvedName := *name
	if resolvedName == "" {
		resolvedName = defaultName()
	}

	resolvedFolder := *folder
	if resolvedFolder == "" {
		resolvedFolder, _ = os.Getwd()
	}

	sockDir := plat.ResolveSocketDir(*socketDir)
	cacheDir := plat.CacheDir()
	repoDir := plat.RepositoryDir()

	dl := downloader.NewHTTPDownloader(cacheDir, log)
	ext := extractor.NewTarExtractor(repoDir, log)
	runner := server.NewProcessRunner(log)
	st := store.NewFileStore(sockDir)
	tg := token.NewRandomGenerator()

	svc := app.NewService(dl, ext, ext, runner, st, tg, log)

	cfg := app.Config{
		Name:      resolvedName,
		Commit:    resolvedCommit,
		Arch:      arch,
		Folder:    resolvedFolder,
		SocketDir: sockDir,
	}

	if *stdio {
		if err := svc.RunStdio(cfg, os.Stdin, os.Stdout); err != nil {
			fatal(err)
		}
	} else {
		if err := svc.Run(cfg); err != nil {
			fatal(err)
		}
	}
}

func listCmd(args []string) {
	fs := flag.NewFlagSet("codetap list", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, `List all discovered CodeTap sessions.

Usage:
  codetap list [flags]

Flags:`)
		printFlags(fs)
	}

	socketDir := fs.String("socket-dir", "", "socket directory (default: /dev/shm/codetap)")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}

	plat, err := platform.New()
	if err != nil {
		fatal(err)
	}

	sockDir := plat.ResolveSocketDir(*socketDir)
	st := store.NewFileStore(sockDir)
	log := logger.NewStderr()
	tg := token.NewRandomGenerator()

	svc := app.NewService(nil, nil, nil, nil, st, tg, log)

	entries, err := svc.List()
	if err != nil {
		fatal(err)
	}

	if len(entries) == 0 {
		fmt.Println("No sessions found.")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tCOMMIT\tFOLDER\tPID\tSTATUS\tSTARTED")
	for _, e := range entries {
		status := "dead"
		if e.Alive {
			status = "alive"
		}
		commitShort := e.Metadata.Commit
		if len(commitShort) > 12 {
			commitShort = commitShort[:12]
		}
		started := e.Metadata.StartedAt.Format(time.DateTime)
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\t%s\n",
			e.Name, commitShort, e.Metadata.Folder,
			e.Metadata.PID, status, started)
	}
	w.Flush()
}

func cleanCmd(args []string) {
	fs := flag.NewFlagSet("codetap clean", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, `Remove stale sessions whose sockets are no longer alive.

Usage:
  codetap clean [flags]

Flags:`)
		printFlags(fs)
	}

	socketDir := fs.String("socket-dir", "", "socket directory (default: /dev/shm/codetap)")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}

	log := logger.NewStderr()

	plat, err := platform.New()
	if err != nil {
		fatal(err)
	}

	sockDir := plat.ResolveSocketDir(*socketDir)
	st := store.NewFileStore(sockDir)
	tg := token.NewRandomGenerator()

	svc := app.NewService(nil, nil, nil, nil, st, tg, log)

	if err := svc.Clean(); err != nil {
		fatal(err)
	}
}

func relayCmd(args []string) {
	fs := flag.NewFlagSet("codetap relay", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, `Relay a remote codetap session over stdio.

Creates a Unix socket in /dev/shm/codetap/ on the host and spawns a remote
command that runs "codetap run --stdio". Traffic is multiplexed between the
local socket and the remote process via stdin/stdout.

Usage:
  codetap relay [flags] -- COMMAND [ARGS...]

Examples:
  codetap relay --name dev -- docker exec -i ctr codetap run --stdio --commit abc123
  codetap relay --name srv -- ssh host codetap run --stdio --commit abc123
  codetap relay --name pod -- kubectl exec -i pod -- codetap run --stdio --commit abc123

Flags:`)
		printFlags(fs)
	}

	name := fs.String("name", "", "session name (default: hostname)")
	commit := fs.String("commit", "", "commit hash for metadata")
	folder := fs.String("folder", "", "workspace folder for metadata (default: cwd)")
	socketDir := fs.String("socket-dir", "", "socket directory (default: /dev/shm/codetap)")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}

	remaining := fs.Args()
	if len(remaining) == 0 {
		fs.Usage()
		os.Exit(1)
	}

	// Strip leading "--" separator if present
	if remaining[0] == "--" {
		remaining = remaining[1:]
	}
	if len(remaining) == 0 {
		fmt.Fprintln(os.Stderr, "codetap relay: missing command after --")
		os.Exit(1)
	}

	log := logger.NewStderr()

	plat, err := platform.New()
	if err != nil {
		fatal(err)
	}

	sockDir := plat.ResolveSocketDir(*socketDir)
	resolvedName := *name
	if resolvedName == "" {
		resolvedName = defaultName()
	}

	st := store.NewFileStore(sockDir)
	socketPath := st.SocketPath(resolvedName)

	resolvedFolder := *folder
	if resolvedFolder == "" {
		resolvedFolder, _ = os.Getwd()
	}

	arch, _ := plat.DetectArch()
	tokenPath := filepath.Join(sockDir, resolvedName+".token")
	if err := os.Remove(tokenPath); err != nil && !os.IsNotExist(err) {
		fatal(fmt.Errorf("remove stale token: %w", err))
	}

	meta := domain.Metadata{
		Name:      resolvedName,
		Commit:    *commit,
		Arch:      arch,
		Folder:    resolvedFolder,
		PID:       os.Getpid(),
		StartedAt: time.Now(),
	}
	if err := st.WriteMetadata(meta); err != nil {
		fatal(err)
	}

	defer func() {
		log.Info("cleaning up relay session", "name", resolvedName)
		if err := st.Remove(resolvedName); err != nil {
			log.Error("relay cleanup failed", "name", resolvedName, "err", err)
		}
	}()

	if err := relay.HostSide(socketPath, remaining, log); err != nil {
		fatal(err)
	}
}

func defaultName() string {
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("codetap-%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("codetap-%x", b)
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "codetap: %v\n", err)
	os.Exit(1)
}
