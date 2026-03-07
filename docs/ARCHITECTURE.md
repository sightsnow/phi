# phi 架构

`phi` 是一个单二进制、跨平台的 SSH 密钥保管工具。

当前只聚焦三件事：

- 保存 SSH 密钥对
- 通过本地 SSH agent 为 `OpenSSH`、`git`、`VSCode Remote SSH` 提供身份
- 通过 `S3` 或 `WebDAV` 手动同步整个 `vault.phi`

## 核心对象

运行时最重要的对象只有三个：

- `phi`：唯一可执行文件
- `config.toml`：本地配置，主要保存同步配置
- `vault.phi`：加密后的 Vault 数据库

约束：

- Vault 口令不落盘
- Vault 口令不通过命令行参数传递
- 同步凭据保存在 `config.toml`
- 私钥保存在 `vault.phi` 的加密记录中

## 运行模型

`phi` 采用“前台 CLI + 后台 daemon + 本地 agent”的模型：

1. `phi init`
   - 初始化配置
   - 创建 `vault.phi`
   - 设置 Vault 口令

2. `phi unlock`
   - 启动后台 daemon
   - 解锁 Vault
   - 启动本地 SSH agent

3. `phi lock`
   - 清除内存中的解锁态
   - 停止本地 agent
   - 退出 daemon

4. `phi passwd`
   - 在已解锁状态下重封装 Vault
   - 更新口令相关参数

设计原则：

- 不做整库明文缓存
- 私钥只在需要签名时按需解密
- daemon 只保存最小运行状态

## 控制面与 Agent

CLI 与后台 daemon 通过本地控制面通信：

- Linux / macOS：Unix socket
- Windows：Named Pipe
- 协议：极简 JSON request / response

解锁成功后，daemon 会启动本地 SSH agent。

agent 只提供只读能力：

- 列出 Vault 中的公钥身份
- 根据公钥找到对应私钥
- 按需解密并完成签名

不支持通过 agent 协议动态添加、删除或修改外部 key。

默认端点：

- Linux / macOS：`~/.phi/agent.sock`
- Windows：`\\.\pipe\phi-agent`

因此只要让 `OpenSSH` 指向该端点，`ssh`、`git`、`scp`、`VSCode Remote SSH` 就可以直接复用 `phi`。

## 同步模型

同步对象始终是单个文件：`vault.phi`。

支持后端：

- `S3`
- `WebDAV`

同步行为始终是手动触发：

- `phi sync config`：配置同步后端
- `phi sync status`：查看本地和远端状态，以及建议动作
- `phi sync once`：自动判断推送或拉取一次
- `phi sync push`：强制以上传本地为准
- `phi sync pull`：强制以下载远端为准

冲突检测依赖文件摘要与 `meta.revision`。

所有会修改 Vault 内容的操作都会递增 `revision`。

## 数据边界

Vault 当前只有两类核心数据：

- `meta`：Vault 级元信息
- `keys`：密钥记录

其中：

- `keys.id` 使用公钥 `SHA256 fingerprint`
- `keys.ciphertext` 保存完整加密记录
- 明文元信息尽量少，敏感 SSH 数据放在密文中

更详细的表结构见 `docs/SCHEMA.md`。

## 代码模块

当前主要模块如下：

- `internal/cli`：命令解析、交互输入、输出
- `internal/app`：用例编排
- `internal/control`：本地控制面协议
- `internal/daemon`：后台解锁态与服务循环
- `internal/agent`：SSH agent 协议实现
- `internal/store/sqlite`：Vault 存储
- `internal/crypto`：KDF、封装、记录加解密
- `internal/sync`：S3 / WebDAV 整文件同步
- `internal/platform`：路径、socket、named pipe 等平台差异

## 平台范围

当前优先支持：

- Windows
- Linux
