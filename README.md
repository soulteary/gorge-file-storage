# github.com/soulteary/gorge-file-storage

Go 实现的 Phorge 统一文件存储网关，提供本地磁盘、MySQL Blob、Amazon S3 三种存储后端的 HTTP 接口。

替代 PHP 侧 `PhabricatorFileStorageEngine` 的各个子类（`PhabricatorLocalDiskFileStorageEngine`、`PhabricatorMySQLFileStorageEngine`、`PhabricatorS3FileStorageEngine`），由 `PhabricatorGoFileStorageClient` 通过 HTTP 调用。

## 功能

- **上传** (`POST /api/file/upload`) — 写入文件，自动选择或指定存储引擎
- **读取** (`POST /api/file/read`) — 按 engine + handle 读取文件内容
- **删除** (`POST /api/file/delete`) — 按 engine + handle 删除文件
- **引擎列表** (`GET /api/file/engines`) — 查看已启用的存储引擎及状态

## 存储引擎

| 引擎 | 标识符 | 优先级 | 说明 |
|------|--------|--------|------|
| MySQL Blob | `blob` | 1 | 小文件，受 `MYSQL_BLOB_MAX_SIZE` 限制 |
| Local Disk | `local-disk` | 5 | 本地磁盘，需挂载持久化卷 |
| Amazon S3 | `amazon-s3` | 100 | S3 / MinIO 兼容对象存储 |

引擎选择逻辑按优先级升序，跳过不可写或超过大小限制的引擎。

## 环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `LISTEN_ADDR` | `:8100` | HTTP 监听地址 |
| `SERVICE_TOKEN` | (空) | API 鉴权 token |
| `MYSQL_HOST` | `127.0.0.1` | MySQL 地址 |
| `MYSQL_PORT` | `3306` | MySQL 端口 |
| `MYSQL_USER` | `phorge` | MySQL 用户 |
| `MYSQL_PASS` | (空) | MySQL 密码 |
| `STORAGE_NAMESPACE` | `phorge` | 数据库名前缀（使用 `{ns}_file` 库） |
| `MYSQL_BLOB_MAX_SIZE` | `1000000` | MySQL blob 最大字节数，0 = 禁用 |
| `LOCAL_DISK_PATH` | (空) | 本地磁盘根路径，空 = 禁用 |
| `S3_BUCKET` | (空) | S3 bucket 名称 |
| `S3_ACCESS_KEY` | (空) | S3 Access Key |
| `S3_SECRET_KEY` | (空) | S3 Secret Key |
| `S3_REGION` | (空) | S3 区域 |
| `S3_ENDPOINT` | (空) | S3 端点 URL |
| `INSTANCE_NAME` | (空) | 集群实例名（用于 S3 key 前缀） |

## 开发

```bash
# 运行（本地磁盘模式）
LOCAL_DISK_PATH=/tmp/phorge-files go run ./cmd/server

# 测试
go test ./...

# 构建
CGO_ENABLED=0 go build -o github.com/soulteary/gorge-file-storage ./cmd/server
```

## Docker

```bash
docker build -t github.com/soulteary/gorge-file-storage .
docker run -e LOCAL_DISK_PATH=/data -v /host/files:/data github.com/soulteary/gorge-file-storage
```
