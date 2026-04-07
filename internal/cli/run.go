package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"

	vaultagent "phi/internal/agent"
	"phi/internal/app"
	"phi/internal/buildinfo"
	"phi/internal/config"
	"phi/internal/crypto"
	"phi/internal/daemon"
	"phi/internal/platform"
	syncer "phi/internal/sync"
	"phi/internal/ui"
)

func Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	service := app.New(stdout, stderr)
	if len(args) == 0 {
		printUsage(stdout)
		return nil
	}

	switch args[0] {
	case "help", "-h", "--help":
		printUsage(stdout)
		return nil
	case "init":
		return runInit(ctx, service, args[1:], stdout)
	case "unlock":
		if err := noExtraArgs(args[1:]); err != nil {
			return err
		}
		var passphrase []byte
		if runtime.GOOS == "windows" {
			autoUnlock, _, _, err := service.WindowsStartupStatus(ctx)
			if err != nil {
				return err
			}
			if !autoUnlock {
				passphrase, err = readPassphrase("Vault passphrase: ", stdout)
				if err != nil {
					return err
				}
				defer crypto.Zero(passphrase)
			}
		} else {
			var err error
			passphrase, err = readPassphrase("Vault passphrase: ", stdout)
			if err != nil {
				return err
			}
			defer crypto.Zero(passphrase)
		}
		status, err := service.Unlock(ctx, passphrase)
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, app.FormatStatus(status))
		return nil
	case "lock":
		if err := noExtraArgs(args[1:]); err != nil {
			return err
		}
		if err := service.Lock(ctx); err != nil {
			return err
		}
		fmt.Fprintln(stdout, "locked")
		return nil
	case "passwd":
		if err := noExtraArgs(args[1:]); err != nil {
			return err
		}
		passphrase, err := readConfirmedPassphrase(stdout)
		if err != nil {
			return err
		}
		defer crypto.Zero(passphrase)
		if err := service.ChangePassphrase(ctx, passphrase); err != nil {
			return err
		}
		fmt.Fprintln(stdout, "passphrase updated")
		return nil
	case "status":
		if err := noExtraArgs(args[1:]); err != nil {
			return err
		}
		status, err := service.Status(ctx)
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, app.FormatStatus(status))
		return nil
	case "env":
		return runEnv(ctx, service, args[1:], stdout)
	case "version":
		if err := noExtraArgs(args[1:]); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "phi %s\n", buildinfo.Version)
		return nil
	case "key":
		return runKey(ctx, service, args[1:], stdout)
	case "sync":
		return runSync(ctx, service, args[1:], stdout)
	case "startup":
		return runStartup(ctx, service, args[1:], stdout)
	case "__daemon":
		return runDaemon(ctx, args[1:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runInit(ctx context.Context, service *app.Service, args []string, stdout io.Writer) error {
	if err := noExtraArgs(args); err != nil {
		return err
	}
	configPath := platform.DefaultConfigPath()
	configExisted := platform.Exists(configPath)
	result, err := service.Init(ctx, nil)
	if errors.Is(err, app.ErrInitPassphraseRequired) {
		passphrase, readErr := readConfirmedPassphrase(stdout)
		if readErr != nil {
			return readErr
		}
		defer crypto.Zero(passphrase)
		result, err = service.Init(ctx, passphrase)
	}
	if err != nil {
		return err
	}
	configState := "exists"
	if !configExisted {
		configState = "created"
	}
	vaultState := "exists"
	if result.VaultCreated {
		vaultState = "created"
	}
	fmt.Fprintf(stdout, "config: %s (%s)\n", configPath, configState)
	fmt.Fprintf(stdout, "vault: %s (%s)\n", platform.DefaultVaultPath(), vaultState)
	return nil
}

func runKey(ctx context.Context, service *app.Service, args []string, stdout io.Writer) error {
	if len(args) == 0 {
		printKeyUsage(stdout)
		return nil
	}
	switch args[0] {
	case "help", "-h", "--help":
		printKeyUsage(stdout)
		return nil
	case "list":
		if err := noExtraArgs(args[1:]); err != nil {
			return err
		}
		keys, err := service.ListKeys(ctx)
		if err != nil {
			return err
		}
		if len(keys) == 0 {
			fmt.Fprintln(stdout, "no keys")
			return nil
		}
		for _, key := range keys {
			fmt.Fprintf(stdout, "%s\t%s\t%s\n", key.ID, key.Algorithm, key.Name)
		}
		return nil
	case "gen":
		rest := args[1:]
		if len(rest) != 1 {
			return errors.New("usage: phi key gen <name>")
		}
		summary, err := service.GenerateKey(ctx, rest[0])
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "generated: %s\t%s\t%s\n", summary.ID, summary.Algorithm, summary.Name)
		return nil
	case "pub":
		rest := args[1:]
		if len(rest) != 1 {
			return errors.New("usage: phi key pub <id-or-name>")
		}
		summary, err := service.PublicKey(ctx, rest[0])
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, summary.PublicKey)
		return nil
	case "copy-pub":
		port, rest, err := portFlagRest(args[1:])
		if err != nil {
			return err
		}
		if len(rest) != 2 {
			return errors.New("usage: phi key copy-pub [--port PORT|-p PORT] <id-or-name> <user@host>")
		}
		result, err := service.CopyPublicKey(ctx, rest[0], rest[1], port)
		if err != nil {
			return err
		}
		if result.AlreadyExists {
			fmt.Fprintf(stdout, "public key already exists: %s\t%s\t%s\t%s\n", result.Summary.ID, result.Summary.Algorithm, result.Summary.Name, rest[1])
			return nil
		}
		fmt.Fprintf(stdout, "copied public key: %s\t%s\t%s\t%s\n", result.Summary.ID, result.Summary.Algorithm, result.Summary.Name, rest[1])
		return nil
	case "import":
		rest := args[1:]
		if len(rest) != 2 {
			return errors.New("usage: phi key import <name> <private-key-path>")
		}
		summary, err := service.ImportKey(ctx, rest[0], rest[1])
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "imported: %s\t%s\t%s\n", summary.ID, summary.Algorithm, summary.Name)
		return nil
	case "delete":
		rest := args[1:]
		if len(rest) != 1 {
			return errors.New("usage: phi key delete <id-or-name>")
		}
		summary, err := service.DeleteKey(ctx, rest[0])
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "deleted: %s	%s	%s\n", summary.ID, summary.Algorithm, summary.Name)
		return nil
	default:
		return fmt.Errorf("unknown key command %q", args[0])
	}
}

func runSync(ctx context.Context, service *app.Service, args []string, stdout io.Writer) error {
	if len(args) == 0 {
		printSyncUsage(stdout)
		return nil
	}
	switch args[0] {
	case "help", "-h", "--help":
		printSyncUsage(stdout)
		return nil
	case "config":
		if err := noExtraArgs(args[1:]); err != nil {
			return err
		}
		return runSyncConfig(ctx, stdout)
	case "status":
		if err := noExtraArgs(args[1:]); err != nil {
			return err
		}
		result, err := service.SyncStatus(ctx)
		if err != nil {
			return err
		}
		fmt.Fprint(stdout, formatSyncStatus(result))
		return nil
	case "once":
		if err := noExtraArgs(args[1:]); err != nil {
			return err
		}
		result, err := service.SyncOnce(ctx)
		if err != nil {
			return err
		}
		switch result.Action {
		case syncer.OncePush:
			fmt.Fprintln(stdout, "sync once completed (pushed local vault)")
		case syncer.OncePull:
			fmt.Fprintln(stdout, "sync once completed (pulled remote vault)")
		default:
			fmt.Fprintln(stdout, "sync once completed (already up to date)")
		}
		return nil
	case "push":
		if err := noExtraArgs(args[1:]); err != nil {
			return err
		}
		if err := service.SyncPush(ctx); err != nil {
			return err
		}
		fmt.Fprintln(stdout, "sync push completed")
		return nil
	case "pull":
		if err := noExtraArgs(args[1:]); err != nil {
			return err
		}
		if err := service.SyncPull(ctx); err != nil {
			return err
		}
		fmt.Fprintln(stdout, "sync pull completed")
		return nil
	default:
		return fmt.Errorf("unknown sync command %q", args[0])
	}
}

func runSyncConfig(ctx context.Context, stdout io.Writer) error {
	resolvedPath := platform.DefaultConfigPath()
	cfg, err := config.Load(resolvedPath)
	if err != nil {
		return err
	}

	syncCfg, err := ui.PromptSyncConfig(cfg.Sync)
	if err != nil {
		return err
	}
	cfg.Sync = syncCfg

	testCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	fmt.Fprintf(stdout, "testing %s connectivity...\n", syncCfg.Backend)
	if err := syncer.TestConnection(testCtx, cfg); err != nil {
		return fmt.Errorf("sync connectivity test failed; config not written: %w", err)
	}

	if err := config.Save(resolvedPath, cfg); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "sync connectivity test passed")
	fmt.Fprintf(stdout, "config: %s\n", resolvedPath)
	fmt.Fprintf(stdout, "sync backend: %s\n", syncCfg.Backend)
	fmt.Fprintln(stdout, "sync config saved")
	return nil
}

func runEnv(ctx context.Context, service *app.Service, args []string, stdout io.Writer) error {
	if err := noExtraArgs(args); err != nil {
		return err
	}

	agentAddress := vaultagent.DefaultSocketPath(platform.DefaultControlPath())
	status, err := service.Status(ctx)
	if err == nil && status.AgentEnabled && status.AgentAddress != "" {
		agentAddress = status.AgentAddress
	}
	if agentAddress == "" {
		return errors.New("ssh agent socket is unavailable")
	}

	if runtime.GOOS == "windows" {
		fmt.Fprintf(stdout, "$env:SSH_AUTH_SOCK=%s\n", quotePowerShell(agentAddress))
		return nil
	}
	fmt.Fprintf(stdout, "export SSH_AUTH_SOCK=%s\n", quotePOSIXShell(agentAddress))
	return nil
}

func runDaemon(ctx context.Context, args []string) error {
	if err := noExtraArgs(args); err != nil {
		return err
	}
	return daemon.Run(ctx)
}

func runStartup(ctx context.Context, service *app.Service, args []string, stdout io.Writer) error {
	if len(args) == 0 {
		printStartupUsage(stdout)
		return nil
	}
	switch args[0] {
	case "help", "-h", "--help":
		printStartupUsage(stdout)
		return nil
	case "status":
		if err := noExtraArgs(args[1:]); err != nil {
			return err
		}
		if runtime.GOOS != "windows" {
			return errors.New("startup configuration is only available on windows")
		}
		autoUnlock, launchAtLogin, command, err := service.WindowsStartupStatus(ctx)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "windows auto unlock: %t\n", autoUnlock)
		if autoUnlock {
			fmt.Fprintf(stdout, "windows auto unlock file: %s\n", platform.DefaultWindowsAutoUnlockPath())
		}
		fmt.Fprintf(stdout, "windows launch at login: %t\n", launchAtLogin)
		if launchAtLogin && command != "" {
			fmt.Fprintf(stdout, "windows launch command: %s\n", command)
		}
		return nil
	case "windows-auto-unlock":
		if runtime.GOOS != "windows" {
			return errors.New("windows auto unlock is only available on windows")
		}
		if len(args) != 2 {
			return errors.New("usage: phi startup windows-auto-unlock <on|off>")
		}
		switch args[1] {
		case "on":
			passphrase, err := readPassphrase("Vault passphrase: ", stdout)
			if err != nil {
				return err
			}
			defer crypto.Zero(passphrase)
			if err := service.ConfigureWindowsAutoUnlock(ctx, passphrase, true); err != nil {
				return err
			}
			fmt.Fprintln(stdout, "windows auto unlock enabled")
			return nil
		case "off":
			if err := service.ConfigureWindowsAutoUnlock(ctx, nil, false); err != nil {
				return err
			}
			fmt.Fprintln(stdout, "windows auto unlock disabled")
			return nil
		default:
			return errors.New("usage: phi startup windows-auto-unlock <on|off>")
		}
	case "windows-launch-at-login":
		if runtime.GOOS != "windows" {
			return errors.New("windows launch at login is only available on windows")
		}
		if len(args) != 2 {
			return errors.New("usage: phi startup windows-launch-at-login <on|off>")
		}
		enabled := false
		switch args[1] {
		case "on":
			enabled = true
		case "off":
			enabled = false
		default:
			return errors.New("usage: phi startup windows-launch-at-login <on|off>")
		}
		if err := service.ConfigureWindowsLaunchAtLogin(enabled); err != nil {
			return err
		}
		if enabled {
			fmt.Fprintln(stdout, "windows launch at login enabled")
		} else {
			fmt.Fprintln(stdout, "windows launch at login disabled")
		}
		return nil
	default:
		return fmt.Errorf("unknown startup command %q", args[0])
	}
}

func portFlagRest(args []string) (int, []string, error) {
	var port int
	var rest []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--port", "-p":
			if i+1 >= len(args) {
				return 0, nil, fmt.Errorf("%s requires a port", args[i])
			}
			parsed, err := strconv.Atoi(args[i+1])
			if err != nil || parsed <= 0 || parsed > 65535 {
				return 0, nil, fmt.Errorf("invalid port %q", args[i+1])
			}
			port = parsed
			i++
		default:
			rest = append(rest, args[i])
		}
	}
	return port, rest, nil
}

func noExtraArgs(args []string) error {
	if len(args) == 0 {
		return nil
	}
	return fmt.Errorf("unexpected arguments: %s", strings.Join(args, " "))
}

func readPassphrase(prompt string, stdout io.Writer) ([]byte, error) {
	fmt.Fprint(stdout, prompt)
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return nil, errors.New("stdin is not a terminal")
	}
	value, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(stdout)
	if err != nil {
		return nil, err
	}
	return value, nil
}

func readConfirmedPassphrase(stdout io.Writer) ([]byte, error) {
	first, err := readPassphrase("New vault passphrase: ", stdout)
	if err != nil {
		return nil, err
	}
	second, err := readPassphrase("Confirm vault passphrase: ", stdout)
	if err != nil {
		crypto.Zero(first)
		return nil, err
	}
	defer crypto.Zero(second)
	if len(first) == 0 {
		crypto.Zero(first)
		return nil, errors.New("empty passphrase")
	}
	if !bytes.Equal(first, second) {
		crypto.Zero(first)
		return nil, errors.New("passphrases do not match")
	}
	return first, nil
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "phi - SSH key vault")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  phi <command>")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Core commands:")
	fmt.Fprintln(w, "  init                                 Initialize config.toml and create vault.phi")
	fmt.Fprintln(w, "  unlock                               Unlock the Vault and start the local daemon and SSH agent")
	fmt.Fprintln(w, "  lock                                 Lock the Vault and stop the local daemon and SSH agent")
	fmt.Fprintln(w, "  passwd                               Change the Vault passphrase")
	fmt.Fprintln(w, "  status                               Show daemon, unlock, control, and agent status")
	fmt.Fprintln(w, "  env                                  Print a shell command that sets SSH_AUTH_SOCK")
	fmt.Fprintln(w, "  version                              Show build version")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Key commands:")
	fmt.Fprintln(w, "  key list                             List stored keys with id, algorithm, and name")
	fmt.Fprintln(w, "  key gen <name>                       Generate a new private key directly into the Vault")
	fmt.Fprintln(w, "  key import <name> <private-key-path> Import an existing private key into the Vault")
	fmt.Fprintln(w, "  key pub <id-or-name>                 Print the public key for a stored key")
	fmt.Fprintln(w, "  key copy-pub [-p PORT] <id-or-name> <user@host>")
	fmt.Fprintln(w, "                                       Copy the public key to the remote host's authorized_keys")
	fmt.Fprintln(w, "  key rename <id-or-name> <new-name>   Rename a stored key")
	fmt.Fprintln(w, "  key delete <id-or-name>              Delete a stored key by id or name")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Sync commands:")
	fmt.Fprintln(w, "  sync config                          Configure the S3 or WebDAV sync backend")
	fmt.Fprintln(w, "  sync status                          Show local and remote Vault status and the suggested action")
	fmt.Fprintln(w, "  sync once                            Compare local and remote Vaults and sync once")
	fmt.Fprintln(w, "  sync push                            Force upload the local Vault to the remote backend")
	fmt.Fprintln(w, "  sync pull                            Force download the remote Vault to the local machine")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Startup commands:")
	fmt.Fprintln(w, "  startup status                       Show startup configuration")
	fmt.Fprintln(w, "  startup windows-auto-unlock <on|off> Configure Windows DPAPI auto unlock")
	fmt.Fprintln(w, "  startup windows-launch-at-login <on|off>")
	fmt.Fprintln(w, "                                       Configure automatic daemon launch after Windows login")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "More help:")
	fmt.Fprintln(w, "  phi key help")
	fmt.Fprintln(w, "  phi sync help")
	fmt.Fprintln(w, "  phi startup help")
}

func printKeyUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  phi key <command>")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Commands:")
	fmt.Fprintln(w, "  list                                 List stored keys with id, algorithm, and name")
	fmt.Fprintln(w, "  gen <name>                           Generate a new private key directly into the Vault")
	fmt.Fprintln(w, "  import <name> <private-key-path>     Import an existing private key into the Vault")
	fmt.Fprintln(w, "  pub <id-or-name>                     Print the public key for a stored key")
	fmt.Fprintln(w, "  copy-pub [-p PORT] <id-or-name> <user@host>")
	fmt.Fprintln(w, "                                       Copy the public key to the remote host's authorized_keys")
	fmt.Fprintln(w, "  rename <id-or-name> <new-name>       Rename a stored key")
	fmt.Fprintln(w, "  delete <id-or-name>                  Delete a stored key by id or name")
}

func printStartupUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  phi startup <command>")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Commands:")
	fmt.Fprintln(w, "  status                               Show startup configuration")
	fmt.Fprintln(w, "  windows-auto-unlock <on|off>         Configure Windows DPAPI auto unlock")
	fmt.Fprintln(w, "  windows-launch-at-login <on|off>     Configure daemon launch after Windows login")
}

func printSyncUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  phi sync <command>")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Commands:")
	fmt.Fprintln(w, "  config                               Configure the S3 or WebDAV sync backend")
	fmt.Fprintln(w, "  status                               Show local and remote Vault status and the suggested action")
	fmt.Fprintln(w, "  once                                 Compare local and remote Vaults and sync once")
	fmt.Fprintln(w, "  push                                 Force upload the local Vault to the remote backend")
	fmt.Fprintln(w, "  pull                                 Force download the remote Vault to the local machine")
}

func formatSyncStatus(result syncer.StatusResult) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "backend: %s\n", result.Backend)
	writeSyncSideStatus(&builder, "local", result.Local)
	writeSyncSideStatus(&builder, "remote", result.Remote)
	fmt.Fprintf(&builder, "action: %s\n", result.Action)
	return builder.String()
}

func writeSyncSideStatus(builder *strings.Builder, name string, status syncer.SnapshotStatus) {
	if !status.Present {
		fmt.Fprintf(builder, "%s: missing\n", name)
		return
	}
	fmt.Fprintf(builder, "%s: present\n", name)
	fmt.Fprintf(builder, "%s revision: %d\n", name, status.Meta.Revision)
	fmt.Fprintf(builder, "%s updated_at: %s\n", name, status.Meta.UpdatedAt.UTC().Format(time.RFC3339))
	fmt.Fprintf(builder, "%s digest: %s\n", name, shortDigest(status.Digest))
}

func shortDigest(value string) string {
	if len(value) <= 16 {
		return value
	}
	return value[:16]
}

func quotePOSIXShell(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func quotePowerShell(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
