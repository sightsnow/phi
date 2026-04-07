# phi

> 一个带本地 SSH agent 的加密 SSH 密钥保管工具，可直接服务于 `ssh`、`git` 和 `VSCode Remote SSH`。

[English](../README.md) | 简体中文

`phi` 是一个轻量的 SSH 密钥保管工具。

它会把私钥保存在加密的 `vault.phi` 中，解锁后通过本地 SSH agent 暴露给 `ssh`、`git`、`scp` 和 `VSCode Remote SSH`，并支持通过 `S3` 或 `WebDAV` 手动同步整个 Vault。

## 特性

- 用加密的本地 Vault 保存 SSH 私钥
- 内置本地 SSH agent，兼容 `OpenSSH`、`git` 和 `VSCode Remote SSH`
- 支持生成新密钥或导入已有私钥
- 支持查看公钥或复制公钥到远端主机
- 同步始终手动触发，支持 `S3` 和 `WebDAV`

## 当前支持

- Windows
- Linux

## 快速开始

```bash
phi init
phi unlock
phi key gen [name]
phi key list
phi key pub [name]
phi status
phi version
```

导入已有私钥：

```bash
phi key import [name] ~/.ssh/id_ed25519
```


复制公钥到远端主机：

```bash
phi key copy-pub [name] apple@example.com
phi key copy-pub -p PORT [name] apple@example.com
```

## SSH Agent 配置

执行 `phi unlock` 后，让 `OpenSSH` 指向本地 `phi` agent 即可。

### Windows

Windows 下默认 pipe 名会按当前用户隔离。执行 `phi unlock` 后先运行 `phi status`，再使用输出里的 `agent:` 值。

在 `ssh_config` 和 VSCode Remote SSH 里，`IdentityAgent` 要写成 `//./pipe/...` 形式：

```sshconfig
Host appledev
    HostName me.sightsnow.cn
    User apple
    IdentityAgent //./pipe/phi-agent-<user-sid>
```

在 PowerShell 的 `SSH_AUTH_SOCK` 里，使用 `\\.\pipe\...` 形式：

```powershell
$env:SSH_AUTH_SOCK='\\.\pipe\phi-agent-<user-sid>'
```
### Linux

直接设置 agent socket：

```bash
export SSH_AUTH_SOCK="$HOME/.phi/agent.sock"
```

或者让 `phi` 直接输出当前 shell 可执行的设置命令：

```bash
eval "$(phi env)"
```

或者写入 `~/.ssh/config`：

```sshconfig
Host work
    HostName example.com
    User apple
    IdentityAgent ~/.phi/agent.sock
```

## Windows 启动配置

在 Windows 下，`phi` 可以通过 `DPAPI` 实现免输入主密钥解锁，也可以配置为用户登录后自动启动。

```powershell
phi startup windows-auto-unlock on
phi startup windows-launch-at-login on
phi startup status
```

- `windows-auto-unlock on` 会把受 `DPAPI` 保护的密文保存到 `$HOME/.phi/auto-unlock.dpapi`
- 只要 `auto-unlock.dpapi` 存在，`phi unlock` 和 daemon 启动时都会直接使用它，不再回退提示输入主密钥
- `windows-launch-at-login on` 会写入当前用户的 `Run` 注册表项
- `startup status` 会直接检查自动解锁文件和当前用户的 `Run` 注册表项

## 同步

所有同步操作都是手动的。

```bash
phi sync config
phi sync status
phi sync once
phi sync push
phi sync pull
```

- `sync config`：配置 `S3` 或 `WebDAV`
- `sync status`：查看本地和远端状态，以及建议的同步方向
- `sync once`：自动判断执行一次 push 或 pull
- `sync push`：强制本地 → 远端
- `sync pull`：强制远端 → 本地

## 命令

### 基础

- `phi init` 初始化 `config.toml` 并创建 `vault.phi`
- `phi unlock` 解锁 Vault，并启动本地 daemon 和 SSH agent
- `phi lock` 锁定 Vault，并停止本地 daemon 和 SSH agent
- `phi passwd` 修改 Vault 口令
- `phi status` 查看 daemon、解锁状态、control 和 agent 状态
- `phi env` 输出一条用于设置 `SSH_AUTH_SOCK` 的 shell 命令
- `phi version` 查看构建版本

### 密钥

- `phi key list` 列出已保存的 key，包括 id、算法和名称
- `phi key gen <name>` 直接在 Vault 中生成新的私钥
- `phi key import <name> <private-key-path>` 将已有私钥导入 Vault
- `phi key pub <id-or-name>` 输出指定 key 的公钥
- `phi key copy-pub [-p PORT] <id-or-name> <user@host>` 将公钥复制到远端主机的 `authorized_keys`
- `phi key rename <id-or-name> <new-name>` 重命名已保存的 key
- `phi key delete <id-or-name>` 按 id 或名称删除一个已保存的 key

### 同步

- `phi sync config` 配置 `S3` 或 `WebDAV` 同步后端
- `phi sync status` 查看本地和远端 Vault 状态，以及建议的同步动作
- `phi sync once` 比较本地和远端 Vault，并自动选择方向执行一次同步
- `phi sync push` 强制把本地 Vault 上传到远端
- `phi sync pull` 强制把远端 Vault 下载到本地

### 启动

- `phi startup status` 查看 Windows 自动解锁和登录后自动启动状态
- `phi startup windows-auto-unlock <on|off>` 配置 `DPAPI` 自动解锁
- `phi startup windows-launch-at-login <on|off>` 配置 Windows 登录后自动启动 daemon

## 构建

```bash
go run scripts/build/build.go
```

默认会生成：

- `dist/bin/phi-linux-amd64`
- `dist/bin/phi-linux-arm64`
- `dist/bin/phi-windows-amd64.exe`
- `dist/bin/phi-windows-arm64.exe`
