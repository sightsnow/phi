# phi SQLite Schema

MVP 只保留两张表：

- `meta`
- `keys`

## meta

`meta` 只有一行，保存整个 Vault 的公共元信息：

- `format_version`：Vault 格式版本
- `kdf_params`：口令派生参数，当前用于保存 `argon2id` 所需参数与 salt
- `wrapped_master_key`：被口令派生密钥封装后的 `Master Key`
- `revision`：Vault 逻辑版本号；生成、导入、重命名或删除密钥后递增
- `created_at`
- `updated_at`

`kdf_params` 的作用是让同一个 Vault 文件自描述其解锁参数；没有它就无法从用户口令稳定推导出正确的 KEK。

## keys

`keys` 保存所有 SSH 密钥记录：

- `id`
- `created_at`
- `updated_at`
- `ciphertext`

其中：

- `id` 使用公钥的 `SHA256 fingerprint`
- `ciphertext` 是 `KeyRecord` 的整条加密结果

## 解密后的结构

```go
type KeyRecord struct {
    Name       string
    Algorithm  string
    PublicKey  string
    PrivateKey []byte
}
```

说明：

- `Name`：展示名称；生成或导入时显式提供，后续可重命名
- `Algorithm`：如 `ssh-ed25519`
- `PublicKey`：OpenSSH authorized key 文本
- `PrivateKey`：私钥原始字节

MVP 不保存：

- host
- user
- alias
- tag
- 任意扩展 meta

## 加密边界

明文落在 SQLite 的内容只有：

- `meta` 表字段
- `keys.id`
- `keys.created_at`
- `keys.updated_at`

真正敏感的 SSH 数据都在 `keys.ciphertext` 中。

## 典型操作

- `init`：写入 `meta`，创建空 `keys`
- `unlock`：读取 `meta`，用口令解封装 `Master Key`
- `key gen`：生成新私钥，加密 `KeyRecord`，写入 `keys`，递增 `revision`
- `key pub`：解密对应 `KeyRecord`，导出其中的公钥文本
- `key copy-pub`：解密对应 `KeyRecord`，取出公钥并通过本机 `ssh` 客户端安装到远端
- `key import`：加密 `KeyRecord`，按指定名字写入 `keys`，递增 `revision`
- `key rename`：重写对应 `KeyRecord` 中的名字，递增 `revision`
- `key list`：逐条解密 `ciphertext` 后返回摘要
- `key delete <id-or-name>`：按 id 或名称删除对应记录，递增 `revision`
- `sync`：直接同步整个 `vault.phi`
