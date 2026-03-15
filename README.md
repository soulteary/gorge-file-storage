# gorge-file-storage

Go 实现的 Phorge 统一文件存储网关，提供本地磁盘、MySQL Blob、Amazon S3 三种存储后端的 HTTP 接口。

替代 PHP 侧 `PhabricatorFileStorageEngine` 的各个子类（`PhabricatorLocalDiskFileStorageEngine`、`PhabricatorMySQLFileStorageEngine`、`PhabricatorS3FileStorageEngine`），由 `PhabricatorGoFileStorageClient` 通过 HTTP 调用。

## 特性

- 三种存储后端：MySQL Blob（小文件）、本地磁盘、Amazon S3 / MinIO
- 按优先级自动选择存储引擎，支持手动指定
- 文件上传、读取、删除的统一 HTTP API
- 与 PHP 端完全兼容的 handle 格式和 S3 key 规则
- Token 认证中间件，支持请求头和查询参数两种方式
- 本地磁盘引擎内置路径穿越防护
- 环境变量驱动配置，按需启用引擎
- 静态编译，Docker 多阶段构建，镜像极轻量
- 内置健康检查端点，适配容器编排

## 快速开始

### 本地运行

```bash
# 本地磁盘模式
LOCAL_DISK_PATH=/tmp/phorge-files go run ./cmd/server

# MySQL Blob 模式
MYSQL_HOST=127.0.0.1 MYSQL_PASS=secret go run ./cmd/server
```

服务默认监听 `:8100`。

### Docker 运行

```bash
docker build -t gorge-file-storage .
docker run -p 8100:8100 -e LOCAL_DISK_PATH=/data -v /host/files:/data gorge-file-storage
```

### 带完整配置运行

```bash
export LISTEN_ADDR=:8100
export SERVICE_TOKEN=your_service_token
export MYSQL_HOST=127.0.0.1
export MYSQL_PORT=3306
export MYSQL_USER=phorge
export MYSQL_PASS=your_password
export STORAGE_NAMESPACE=phorge
export MYSQL_BLOB_MAX_SIZE=1000000
export LOCAL_DISK_PATH=/var/lib/phorge/files
export S3_BUCKET=phorge-files
export S3_ACCESS_KEY=AKID
export S3_SECRET_KEY=secret
export S3_REGION=us-east-1
export S3_ENDPOINT=https://s3.amazonaws.com
go run ./cmd/server
```

## 配置

全部通过环境变量配置。

### 服务配置

| 变量 | 默认值 | 说明 |
|---|---|---|
| `LISTEN_ADDR` | `:8100` | HTTP 监听地址 |
| `SERVICE_TOKEN` | (空) | API 鉴权 token，空则跳过鉴权 |
| `INSTANCE_NAME` | (空) | 集群实例名，用于 S3 key 前缀 |

### MySQL 配置

| 变量 | 默认值 | 说明 |
|---|---|---|
| `MYSQL_HOST` | `127.0.0.1` | MySQL 地址 |
| `MYSQL_PORT` | `3306` | MySQL 端口 |
| `MYSQL_USER` | `phorge` | MySQL 用户 |
| `MYSQL_PASS` | (空) | MySQL 密码 |
| `STORAGE_NAMESPACE` | `phorge` | 数据库名前缀（使用 `{ns}_file` 库） |
| `MYSQL_BLOB_MAX_SIZE` | `1000000` | MySQL blob 最大字节数，0 = 禁用 blob 引擎 |

### 本地磁盘配置

| 变量 | 默认值 | 说明 |
|---|---|---|
| `LOCAL_DISK_PATH` | (空) | 本地磁盘根路径，空 = 禁用 |

### S3 配置

| 变量 | 默认值 | 说明 |
|---|---|---|
| `S3_BUCKET` | (空) | S3 bucket 名称 |
| `S3_ACCESS_KEY` | (空) | S3 Access Key |
| `S3_SECRET_KEY` | (空) | S3 Secret Key |
| `S3_REGION` | (空) | S3 区域 |
| `S3_ENDPOINT` | (空) | S3 端点 URL |

五个 S3 变量全部非空时启用 S3 引擎。

## 存储引擎

| 引擎 | 标识符 | 优先级 | 大小限制 | 说明 |
|---|---|---|---|---|
| MySQL Blob | `blob` | 1 | 有（默认 ~1MB） | 小文件，存储在 `file_storageblob` 表 |
| Local Disk | `local-disk` | 5 | 无 | 本地磁盘，需挂载持久化卷 |
| Amazon S3 | `amazon-s3` | 100 | 无 | S3 / MinIO 兼容对象存储 |

上传时若未指定引擎，按优先级升序选择第一个可写且满足大小限制的引擎。

## API

所有 `/api/file/*` 端点在配置 `SERVICE_TOKEN` 时需要认证。认证方式：

- 请求头：`X-Service-Token: <token>`
- 查询参数：`?token=<token>`

### POST /api/file/upload

上传文件。

**请求体**：

```json
{
  "dataBase64": "SGVsbG8gV29ybGQ=",
  "engine": "",
  "name": "example.txt",
  "mimeType": "text/plain"
}
```

- `dataBase64`（必填）：Base64 编码的文件数据
- `engine`（可选）：指定存储引擎标识符，不指定则自动选择
- `name`（可选）：文件名
- `mimeType`（可选）：MIME 类型

**响应** (200)：

```json
{
  "data": {
    "handle": "a1/b2/c3d4e5f6789012345678901234",
    "engine": "local-disk",
    "size": 11,
    "mimeType": "text/plain"
  }
}
```

### POST /api/file/read

读取文件。

**请求体**：

```json
{
  "handle": "a1/b2/c3d4e5f6789012345678901234",
  "engine": "local-disk"
}
```

**响应** (200)：

```json
{
  "data": {
    "dataBase64": "SGVsbG8gV29ybGQ="
  }
}
```

### POST /api/file/delete

删除文件。

**请求体**：

```json
{
  "handle": "a1/b2/c3d4e5f6789012345678901234",
  "engine": "local-disk"
}
```

**响应** (200)：

```json
{
  "data": {
    "status": "deleted"
  }
}
```

### GET /api/file/engines

列出已启用的存储引擎。

**响应** (200)：

```json
{
  "data": [
    { "identifier": "blob", "priority": 1, "canWrite": true, "sizeLimit": 1000000 },
    { "identifier": "local-disk", "priority": 5, "canWrite": true, "sizeLimit": 0 },
    { "identifier": "amazon-s3", "priority": 100, "canWrite": true, "sizeLimit": 0 }
  ]
}
```

### GET /healthz

健康检查端点，不需要认证。

**响应** (200)：

```json
{"status": "ok"}
```

### 错误响应

所有错误响应使用统一的 JSON 格式：

```json
{
  "error": {
    "code": "ERR_BAD_REQUEST",
    "message": "dataBase64 is required"
  }
}
```

| 错误码 | HTTP 状态码 | 含义 |
|---|---|---|
| `ERR_UNAUTHORIZED` | 401 | Service Token 缺失或无效 |
| `ERR_BAD_REQUEST` | 400 | 请求参数错误 |
| `ERR_NO_ENGINE` | 503 | 没有可用的存储引擎 |
| `ERR_NOT_FOUND` | 404 | 文件不存在 |
| `ERR_INTERNAL` | 500 | 内部错误 |

## 项目结构

```
gorge-file-storage/
├── cmd/server/main.go              # 服务入口
├── internal/
│   ├── config/
│   │   ├── config.go               # 环境变量配置加载
│   │   └── config_test.go          # 配置测试
│   ├── engine/
│   │   ├── engine.go               # StorageEngine 接口定义
│   │   ├── router.go               # 引擎路由与优先级选择
│   │   ├── router_test.go          # 路由测试
│   │   ├── localdisk.go            # 本地磁盘存储引擎
│   │   ├── localdisk_test.go       # 本地磁盘测试
│   │   ├── mysqlblob.go            # MySQL Blob 存储引擎
│   │   └── s3.go                   # S3 存储引擎
│   └── httpapi/
│       └── handlers.go             # HTTP 路由、认证中间件与处理器
├── Dockerfile                      # 多阶段 Docker 构建
├── go.mod
└── go.sum
```

## 开发

```bash
# 运行测试
go test ./...

# 运行测试（带详细输出）
go test -v ./...

# 构建二进制
CGO_ENABLED=0 go build -o gorge-file-storage ./cmd/server
```

## 技术栈

- **语言**：Go 1.25
- **HTTP 框架**：[Echo](https://echo.labstack.com/) v4.15.1
- **数据库驱动**：[go-sql-driver/mysql](https://github.com/go-sql-driver/mysql) v1.9.3
- **S3 客户端**：[AWS SDK for Go v2](https://github.com/aws/aws-sdk-go-v2) v1.41.4
- **许可证**：Apache License 2.0
