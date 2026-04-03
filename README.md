# phi

> Encrypted SSH key vault with a local SSH agent for `ssh`, `git`, and `VSCode Remote SSH`.

English | [简体中文](./docs/README.zh-CN.md)

`phi` is a small SSH key vault for everyday use.

It keeps private keys in an encrypted `vault.phi`, exposes them through a local SSH agent after unlock, and supports manual Vault sync through `S3` or `WebDAV`.

## Features

- Encrypted local Vault for SSH private keys
- Built-in local SSH agent for `OpenSSH`, `git`, and `VSCode Remote SSH`
- Generate new keys or import existing ones
- Print or copy public keys to remote hosts
- Manual sync only, with `S3` and `WebDAV` backends

## Supported Platforms

- Windows
- Linux

## Quick Start

```bash
phi init
phi unlock
phi key gen [name]
phi key list
phi key pub [name]
phi status
phi version
```

Import an existing key:

```bash
phi key import [name] ~/.ssh/id_ed25519
```


Copy a public key to a remote host:

```bash
phi key copy-pub [name] apple@example.com
phi key copy-pub -p PORT [name] apple@example.com
```

## SSH Agent Setup

After `phi unlock`, point `OpenSSH` to the local `phi` agent.

### Windows

On Windows, the default pipe name is isolated per user. Run `phi status` after `phi unlock`, then use the reported `agent:` value.

For `IdentityAgent` in `ssh_config` and VSCode Remote SSH, use `//./pipe/...` style:

```sshconfig
Host appledev
    HostName me.sightsnow.cn
    User apple
    IdentityAgent //./pipe/phi-agent-<user-sid>
```

For PowerShell `SSH_AUTH_SOCK`, use `\\.\pipe\...` style:

```powershell
$env:SSH_AUTH_SOCK='\\.\pipe\phi-agent-<user-sid>'
```
### Linux

Set the agent socket explicitly:

```bash
export SSH_AUTH_SOCK="$HOME/.phi/agent.sock"
```

Or let `phi` print the command for your current shell:

```bash
eval "$(phi env)"
```

Or configure it in `~/.ssh/config`:

```sshconfig
Host work
    HostName example.com
    User apple
    IdentityAgent ~/.phi/agent.sock
```

## Sync

All sync operations are manual.

```bash
phi sync config
phi sync status
phi sync once
phi sync push
phi sync pull
```

- `sync config` configures `S3` or `WebDAV`
- `sync status` shows local and remote status and the suggested sync direction
- `sync once` auto-selects push or pull once
- `sync push` forces local → remote
- `sync pull` forces remote → local

## Commands

### Core

- `phi init` initialize `config.toml` and create `vault.phi`
- `phi unlock` unlock the Vault and start the local daemon and SSH agent
- `phi lock` lock the Vault and stop the local daemon and SSH agent
- `phi passwd` change the Vault passphrase
- `phi status` show daemon, unlock, control, and agent status
- `phi env` print a shell command that sets `SSH_AUTH_SOCK`
- `phi version` show build version

### Keys

- `phi key list` list stored keys with id, algorithm, and name
- `phi key gen <name>` generate a new private key directly into the Vault
- `phi key import <name> <private-key-path>` import an existing private key into the Vault
- `phi key pub <id-or-name>` print the public key for a stored key
- `phi key copy-pub [-p PORT] <id-or-name> <user@host>` copy the public key to the remote host's `authorized_keys`
- `phi key rename <id-or-name> <new-name>` rename a stored key
- `phi key delete <id-or-name>` delete a stored key by id or name

### Sync

- `phi sync config` configure the `S3` or `WebDAV` sync backend
- `phi sync status` show local and remote Vault status and the suggested action
- `phi sync once` compare local and remote Vaults and perform one sync in the right direction
- `phi sync push` force upload the local Vault to the remote backend
- `phi sync pull` force download the remote Vault to the local machine

## Build

```bash
go run scripts/build/build.go
```

This produces:

- `dist/bin/phi-linux-amd64`
- `dist/bin/phi-linux-arm64`
- `dist/bin/phi-windows-amd64.exe`
- `dist/bin/phi-windows-arm64.exe`
