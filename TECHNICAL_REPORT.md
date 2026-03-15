# gorge-file-storage 技术报告

## 1. 概述

gorge-file-storage 是 Gorge 平台中的统一文件存储网关微服务，为 Phorge（Phabricator 社区维护分支）提供文件上传、读取和删除的 HTTP API。

该服务的核心目标是将 Phorge PHP 端的文件存储引擎层（`PhabricatorFileStorageEngine` 及其子类）抽取为独立的 Go HTTP 服务。Phorge 原有的文件存储逻辑分散在多个 PHP 类中：`PhabricatorLocalDiskFileStorageEngine` 处理本地磁盘存储，`PhabricatorMySQLFileStorageEngine` 处理 MySQL Blob 存储，`PhabricatorS3FileStorageEngine` 处理 S3 存储。gorge-file-storage 将这三种存储后端统一在一套 REST API 之后，由 PHP 端的 `PhabricatorGoFileStorageClient` 通过 HTTP 调用，保持与原有存储行为完全兼容。

## 2. 设计动机

### 2.1 原有方案的问题

Phorge 的文件存储引擎嵌入在 PHP 应用中：

1. **PHP 进程模型限制**：PHP 的请求-响应模型下，每次文件操作都需要在当前请求上下文中完成。对于 S3 等外部存储，每次操作都要初始化 SDK 客户端、建立连接，无法复用客户端实例和连接池。
2. **存储引擎与应用耦合**：存储引擎代码与 Phorge 应用代码绑定在同一部署单元中，无法独立扩缩容。文件存储是 I/O 密集型操作，与 CPU 密集型的应用逻辑有不同的资源需求。
3. **MySQL 连接争用**：MySQL Blob 存储引擎与应用共享同一个 MySQL 连接，大文件的写入会长时间占用连接，影响其他数据库操作。
4. **运维复杂性**：本地磁盘存储要求 PHP 应用容器挂载持久化卷，S3 存储要求 PHP 容器安装 AWS SDK 依赖，增加了 PHP 侧的部署复杂度。

### 2.2 gorge-file-storage 的解决思路

将文件存储功能抽取为独立的 Go HTTP 微服务：

- **连接池复用**：Go 常驻进程维护 MySQL 连接池和 S3 客户端实例，避免重复建连的开销。
- **独立部署**：作为独立容器运行，可根据文件 I/O 负载独立扩缩容，不依赖 PHP 运行时。
- **关注点分离**：本地磁盘挂载和 S3 SDK 依赖从 PHP 容器转移到专用的文件存储容器。
- **统一 API**：三种存储后端通过统一的 HTTP API 暴露，PHP 端只需维护一个 HTTP 客户端类。
- **行为兼容**：Handle 格式、S3 key 命名规则与 PHP 端保持一致，确保存量数据可被正确读取。

## 3. 系统架构

### 3.1 在 Gorge 平台中的位置

```
┌──────────────────────────────────────────────────────────┐
│                       Gorge 平台                          │
│                                                          │
│  ┌──────────┐  ┌───────────┐  ┌────────────────────────┐ │
│  │  Phorge  │  │  gorge-   │  │  其他 Go 服务           │ │
│  │   PHP    │  │  conduit  │  │                        │ │
│  └────┬─────┘  └───────────┘  └────────────────────────┘ │
│       │                                                  │
│       │ HTTP + X-Service-Token                           │
│       ▼                                                  │
│  ┌────────────────────────────────┐                      │
│  │    gorge-file-storage          │                      │
│  │    :8100                       │                      │
│  │                                │                      │
│  │    Token Auth Middleware       │                      │
│  │    Engine Router               │                      │
│  │    Upload / Read / Delete API  │                      │
│  └───────┬────────┬────────┬─────┘                      │
│          │        │        │                             │
│          ▼        ▼        ▼                             │
│    ┌────────┐ ┌───────┐ ┌──────┐                        │
│    │ MySQL  │ │ Local │ │  S3  │                        │
│    │ Blob   │ │ Disk  │ │/MinIO│                        │
│    └────────┘ └───────┘ └──────┘                        │
└──────────────────────────────────────────────────────────┘
```

### 3.2 模块划分

项目采用 Go 标准布局，分为三个内部模块：

| 模块 | 路径 | 职责 |
|---|---|---|
| config | `internal/config/` | 环境变量配置加载、引擎启用判断、DSN 构建 |
| engine | `internal/engine/` | StorageEngine 接口定义、三种引擎实现、路由选择 |
| httpapi | `internal/httpapi/` | HTTP 路由注册、Token 认证中间件、上传/读取/删除处理器 |

入口程序 `cmd/server/main.go` 负责串联三个模块：加载配置 -> 初始化存储引擎 -> 构建引擎路由 -> 启动 HTTP 服务。

### 3.3 请求处理流水线

一个文件上传请求经过的完整处理链路：

```
客户端请求 POST /api/file/upload
       │
       ▼
┌─ Echo 框架层 ─────────────────────────────────┐
│  RequestLogger   记录请求日志                    │
│       │                                        │
│       ▼                                        │
│  Recover         捕获 panic，防止进程崩溃         │
└───────┼────────────────────────────────────────┘
        │
        ▼
┌─ 路由组 /api/file ────────────────────────────┐
│  tokenAuth       校验 X-Service-Token          │
│       │                                        │
│       ▼                                        │
│  upload handler                                │
│       │                                        │
│       ├─ 解析 JSON 请求体                        │
│       ├─ Base64 解码得到 []byte                  │
│       ├─ 选择存储引擎（指定 or 自动）              │
│       ├─ 调用 engine.WriteFile()                │
│       └─ 返回 handle + engine + size            │
└───────┼────────────────────────────────────────┘
        │
        ▼
  JSON 响应返回客户端
```

## 4. 核心实现分析

### 4.1 StorageEngine 接口

接口定义位于 `internal/engine/engine.go`，是整个系统的核心抽象：

```go
type StorageEngine interface {
    Identifier() string
    Priority() int
    CanWrite() bool
    HasSizeLimit() bool
    MaxFileSize() int64

    WriteFile(ctx context.Context, data []byte, params WriteParams) (handle string, err error)
    ReadFile(ctx context.Context, handle string) (io.ReadCloser, error)
    DeleteFile(ctx context.Context, handle string) error
}
```

接口设计要点：

- **Identifier**：引擎的唯一标识符（`blob`、`local-disk`、`amazon-s3`），用于读取和删除时定位引擎。PHP 端存储文件时会同时记录 handle 和 engine identifier，后续操作通过两者联合定位。
- **Priority**：数值越小越优先。写入时优先使用低 priority 引擎，实现小文件存 MySQL、大文件存 S3 的分级策略。
- **CanWrite / HasSizeLimit / MaxFileSize**：供路由器判断引擎是否可写以及能否承载给定大小的文件。
- **WriteFile 返回 handle**：handle 是引擎返回的不透明标识符，不同引擎格式完全不同。调用方不应解析 handle 内容，仅需持久化并在后续操作中原样传回。
- **ReadFile 返回 io.ReadCloser**：统一的流式读取接口，本地磁盘返回 `*os.File`，MySQL Blob 返回 `io.NopCloser(bytes.NewReader)`，S3 返回 `GetObject` 的 Body。

辅助类型 `WriteParams` 携带可选的文件名和 MIME 类型，`FileInfo` 用于返回文件元信息。

### 4.2 引擎路由

路由模块位于 `internal/engine/router.go`，`Router` 是连接 HTTP 层与存储引擎的核心组件。

#### 4.2.1 初始化排序

```go
func NewRouter(engines []StorageEngine) *Router {
    sorted := make([]StorageEngine, len(engines))
    copy(sorted, engines)
    sort.Slice(sorted, func(i, j int) bool {
        return sorted[i].Priority() < sorted[j].Priority()
    })
    byID := make(map[string]StorageEngine, len(engines))
    for _, e := range engines {
        byID[e.Identifier()] = e
    }
    return &Router{engines: sorted, byID: byID}
}
```

`NewRouter` 在初始化时完成两项工作：

1. 将引擎列表按 Priority 升序排列（优先级数值小的在前），后续 `SelectForWrite` 遍历时自然获得正确的优先级顺序。
2. 构建 `byID` 索引映射，使 `GetEngine` 可以 O(1) 定位引擎。

排序使用副本而非原地排序，避免修改调用方传入的 slice。

#### 4.2.2 写入选择策略

```go
func (r *Router) SelectForWrite(size int64) (StorageEngine, error) {
    for _, eng := range r.engines {
        if !eng.CanWrite() {
            continue
        }
        if eng.HasSizeLimit() && size > eng.MaxFileSize() {
            continue
        }
        return eng, nil
    }
    return nil, fmt.Errorf("no writable engine available for file of size %d", size)
}
```

选择逻辑按排序后的顺序遍历，跳过不可写引擎和无法承载当前文件大小的引擎，返回第一个满足条件的引擎。这意味着：

- 小文件（<= 1MB）优先存 MySQL Blob（Priority 1），减少文件系统碎片
- 中等文件走本地磁盘（Priority 5），性能好且成本低
- 大文件或无本地磁盘时走 S3（Priority 100），作为兜底存储

如果上传请求显式指定了 `engine` 字段，HTTP 层会绕过 `SelectForWrite`，直接通过 `GetEngine` 定位指定引擎。

### 4.3 MySQL Blob 引擎

MySQL Blob 引擎位于 `internal/engine/mysqlblob.go`，将文件数据以 BLOB 形式存储在 MySQL 表中。

#### 4.3.1 存储模型

数据存储在 `file_storageblob` 表中（Phorge 数据库 `{namespace}_file` 下），列包括：

| 列 | 类型 | 说明 |
|---|---|---|
| `id` | 自增主键 | 作为 handle 返回 |
| `data` | BLOB | 文件二进制数据 |
| `dateCreated` | INT | Unix 时间戳，创建时间 |
| `dateModified` | INT | Unix 时间戳，修改时间 |

#### 4.3.2 写入流程

```go
func (e *MySQLBlobEngine) WriteFile(ctx context.Context, data []byte, _ WriteParams) (string, error) {
    if int64(len(data)) > e.maxSize {
        return "", fmt.Errorf("file size %d exceeds MySQL blob max %d", len(data), e.maxSize)
    }
    now := time.Now().Unix()
    res, err := e.db.ExecContext(ctx,
        "INSERT INTO file_storageblob (data, dateCreated, dateModified) VALUES (?, ?, ?)",
        data, now, now)
    // ...
    id, err := res.LastInsertId()
    return fmt.Sprintf("%d", id), nil
}
```

写入前先检查文件大小是否超过 `maxSize` 限制。Handle 使用自增主键 ID 的字符串表示（如 `"42"`），与 PHP 端 `PhabricatorMySQLFileStorageEngine` 行为一致。

#### 4.3.3 读取与删除

- **ReadFile**：通过 `SELECT data FROM file_storageblob WHERE id = ?` 读取 BLOB 数据，包装为 `io.NopCloser(bytes.NewReader(data))` 返回。未找到时返回明确的 "not found" 错误。
- **DeleteFile**：通过 `DELETE FROM file_storageblob WHERE id = ?` 删除记录，检查 `RowsAffected` 确认记录存在。

#### 4.3.4 引擎特性

- **Identifier**：`"blob"`
- **Priority**：1（最高优先级）
- **CanWrite**：当 `maxSize > 0` 时为 true
- **HasSizeLimit**：true
- **MaxFileSize**：由 `MYSQL_BLOB_MAX_SIZE` 环境变量控制，默认 1000000 字节（约 1MB）

### 4.4 本地磁盘引擎

本地磁盘引擎位于 `internal/engine/localdisk.go`，将文件存储在服务器本地文件系统中。

#### 4.4.1 Handle 生成与分桶存储

```go
func generateLocalHandle() (string, error) {
    b := make([]byte, 16)
    if _, err := rand.Read(b); err != nil {
        return "", fmt.Errorf("generate random handle: %w", err)
    }
    h := hex.EncodeToString(b)
    return fmt.Sprintf("%s/%s/%s", h[:2], h[2:4], h[4:]), nil
}
```

Handle 生成策略：

1. 使用 `crypto/rand` 生成 16 字节（128 位）随机数
2. 编码为 32 字符的十六进制字符串
3. 按 `ab/cd/ef...28chars` 格式分桶，前两级目录各 2 字符，文件名 28 字符

分桶存储的目的是避免单目录下文件数量过多导致文件系统性能下降。两级目录提供 256 * 256 = 65536 个分桶，每个分桶内文件名 28 字符 hex（112 位随机性），碰撞概率极低。

完整的文件路径为 `{root}/ab/cd/ef1234567890abcdef12345678`，写入前自动创建父目录。

#### 4.4.2 路径穿越防护

```go
var localHandlePattern = regexp.MustCompile(`^[a-f0-9]{2}/[a-f0-9]{2}/[a-f0-9]{28}$`)
```

`ReadFile` 和 `DeleteFile` 在执行文件操作前，用正则严格校验 handle 格式。只有匹配 `^[a-f0-9]{2}/[a-f0-9]{2}/[a-f0-9]{28}$` 的 handle 才会被接受。

这防止了路径穿越攻击——攻击者无法通过 `../../../etc/passwd` 这样的 handle 读取系统文件。正则在包加载时编译一次，后续调用零分配。

#### 4.4.3 根路径校验

```go
func NewLocalDiskEngine(root string) (*LocalDiskEngine, error) {
    if root == "" || root == "/" || root[0] != '/' {
        return nil, fmt.Errorf("local disk root must be an absolute path, got %q", root)
    }
    if err := os.MkdirAll(root, 0o755); err != nil {
        return nil, fmt.Errorf("create storage root: %w", err)
    }
    return &LocalDiskEngine{root: root}, nil
}
```

初始化时拒绝三种危险的根路径：

- 空字符串：未配置
- `"/"`：根目录，可能误删系统文件
- 相对路径（首字符不是 `/`）：行为不可预测

通过 `os.MkdirAll` 确保存储根目录存在，权限设置为 `0755`。

#### 4.4.4 删除幂等性

```go
func (e *LocalDiskEngine) DeleteFile(_ context.Context, handle string) error {
    // ... handle 校验 ...
    if _, err := os.Stat(fullPath); os.IsNotExist(err) {
        return nil
    }
    return os.Remove(fullPath)
}
```

删除操作是幂等的——如果文件已不存在，返回 `nil` 而非错误。这避免了重复删除请求导致的错误，符合 HTTP DELETE 的幂等性语义。

#### 4.4.5 引擎特性

- **Identifier**：`"local-disk"`
- **Priority**：5
- **CanWrite**：始终为 true
- **HasSizeLimit**：false，无大小上限

### 4.5 S3 引擎

S3 引擎位于 `internal/engine/s3.go`，通过 AWS SDK v2 与 S3 兼容存储（包括 MinIO）交互。

#### 4.5.1 客户端初始化

```go
func NewS3Engine(cfg S3Config) (*S3Engine, error) {
    client := s3.New(s3.Options{
        Region:       cfg.Region,
        BaseEndpoint: aws.String(cfg.Endpoint),
        Credentials:  credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
        UsePathStyle: true,
    })
    return &S3Engine{client: client, bucket: cfg.Bucket, instanceName: cfg.InstanceName}, nil
}
```

配置要点：

- **UsePathStyle**：设为 true 以兼容 MinIO 等自托管 S3 服务。Path-style URL 格式为 `http://endpoint/bucket/key`，而非 virtual-hosted-style 的 `http://bucket.endpoint/key`。
- **StaticCredentialsProvider**：使用固定的 Access Key / Secret Key，不依赖 AWS 实例角色或环境变量链。
- **客户端复用**：`s3.Client` 在引擎初始化时创建一次，后续所有请求复用同一实例。SDK 内部维护 HTTP 连接池，避免重复握手。

#### 4.5.2 Key 生成规则

```go
func (e *S3Engine) generateKey() (string, error) {
    b := make([]byte, 10)
    if _, err := rand.Read(b); err != nil {
        return "", fmt.Errorf("generate random key: %w", err)
    }
    seed := hex.EncodeToString(b)
    parts := "phabricator"
    if e.instanceName != "" {
        parts += "/" + e.instanceName
    }
    parts += fmt.Sprintf("/%s/%s/%s", seed[:2], seed[2:4], seed[4:])
    return parts, nil
}
```

S3 key 格式与 PHP 端 `PhabricatorS3FileStorageEngine` 完全一致：

- 固定前缀 `phabricator`
- 可选的实例名段（用于多实例部署时隔离文件）
- 两级分桶目录 + 文件名，由 10 字节（20 字符 hex）随机数生成

示例 key：`phabricator/myinstance/a1/b2/c3d4e5f6789012345678`

这种兼容设计确保 Go 服务写入的文件可以被 PHP 端正确读取，反之亦然。迁移时无需移动或重命名已有文件。

#### 4.5.3 读写删除

三个操作直接映射到 S3 API：

- **WriteFile**：调用 `PutObject`，body 为 `bytes.NewReader(data)`
- **ReadFile**：调用 `GetObject`，返回 `out.Body`（`io.ReadCloser`）
- **DeleteFile**：调用 `DeleteObject`

Handle 即完整的 S3 key，读取和删除时原样使用。

#### 4.5.4 引擎特性

- **Identifier**：`"amazon-s3"`
- **Priority**：100（最低优先级，作为兜底存储）
- **CanWrite**：始终为 true
- **HasSizeLimit**：false，无大小上限

### 4.6 HTTP 层

HTTP 层位于 `internal/httpapi/handlers.go`，基于 Echo v4 框架。

#### 4.6.1 路由设计

```go
func RegisterRoutes(e *echo.Echo, deps *Deps) {
    e.GET("/", healthPing())
    e.GET("/healthz", healthPing())

    g := e.Group("/api/file")
    g.Use(tokenAuth(deps))

    g.POST("/upload", upload(deps))
    g.POST("/read", readFile(deps))
    g.POST("/delete", deleteFile(deps))
    g.GET("/engines", listEngines(deps))
}
```

路由设计要点：

- **健康检查独立**：`/` 和 `/healthz` 不经过认证中间件，确保 Docker HEALTHCHECK 和负载均衡器的探测不受影响。
- **路由组中间件**：`tokenAuth` 作为 `/api/file` 路由组的中间件，仅对受保护端点生效。
- **POST 读取和删除**：读取和删除使用 POST 而非 GET/DELETE，因为请求体中需要携带 `handle` 和 `engine` 参数，与 PHP 端调用方式保持一致。

#### 4.6.2 认证中间件

```go
func tokenAuth(deps *Deps) echo.MiddlewareFunc {
    return func(next echo.HandlerFunc) echo.HandlerFunc {
        return func(c echo.Context) error {
            if deps.Token == "" {
                return next(c)
            }
            token := c.Request().Header.Get("X-Service-Token")
            if token == "" {
                token = c.QueryParam("token")
            }
            if token == "" || token != deps.Token {
                return c.JSON(http.StatusUnauthorized, &apiResponse{
                    Error: &apiError{Code: "ERR_UNAUTHORIZED", Message: "missing or invalid service token"},
                })
            }
            return next(c)
        }
    }
}
```

设计要点：

- **可选鉴权**：当 `SERVICE_TOKEN` 为空时，中间件直接放行所有请求。这允许开发环境无需配置 token 即可使用。
- **双通道获取**：支持 `X-Service-Token` 请求头和 `?token=` 查询参数两种方式。请求头适合服务间调用，查询参数适合调试。
- **值校验**：同时检查 token 非空且值匹配，防止空 token 绕过认证。

#### 4.6.3 上传处理器

```go
func upload(deps *Deps) echo.HandlerFunc {
    return func(c echo.Context) error {
        var req uploadRequest
        if err := c.Bind(&req); err != nil {
            return respondErr(c, http.StatusBadRequest, "ERR_BAD_REQUEST", err.Error())
        }
        data, err := base64.StdEncoding.DecodeString(req.DataBase64)
        // ...
        var eng engine.StorageEngine
        if req.Engine != "" {
            eng, err = deps.Router.GetEngine(req.Engine)
        } else {
            eng, err = deps.Router.SelectForWrite(int64(len(data)))
        }
        handle, err := eng.WriteFile(c.Request().Context(), data, params)
        return respondOK(c, &uploadResponse{Handle: handle, Engine: eng.Identifier(), ...})
    }
}
```

上传流程：

1. 绑定 JSON 请求体，解析 `dataBase64`、`engine`、`name`、`mimeType` 字段
2. Base64 解码得到原始文件数据 `[]byte`
3. 引擎选择：若请求指定了 `engine`，通过 `GetEngine` 定位；否则通过 `SelectForWrite` 按优先级自动选择
4. 调用所选引擎的 `WriteFile`，传入 context、数据和参数
5. 返回 handle、engine identifier、文件大小和 MIME 类型

使用 Base64 编码传输文件数据而非 multipart form，是因为 PHP 端 Conduit API 的调用约定基于 JSON，Base64 是在 JSON 中嵌入二进制数据的标准方式。

#### 4.6.4 读取处理器

```go
func readFile(deps *Deps) echo.HandlerFunc {
    return func(c echo.Context) error {
        // ... 解析 handle 和 engine ...
        eng, err := deps.Router.GetEngine(req.Engine)
        rc, err := eng.ReadFile(c.Request().Context(), req.Handle)
        defer func() { _ = rc.Close() }()
        data, err := io.ReadAll(rc)
        return respondOK(c, map[string]string{
            "dataBase64": base64.StdEncoding.EncodeToString(data),
        })
    }
}
```

读取时必须同时提供 `handle` 和 `engine`，因为不同引擎的 handle 格式和寻址方式完全不同——MySQL Blob 的 handle 是数字 ID，本地磁盘的 handle 是相对路径，S3 的 handle 是完整的 object key。

读取到的数据通过 `io.ReadAll` 全部加载到内存后 Base64 编码返回。这对于 Phorge 的典型文件大小（头像、附件、diff 等，通常在几十 KB 到几 MB 之间）是合理的。

#### 4.6.5 统一响应结构

```go
type apiResponse struct {
    Data  any       `json:"data,omitempty"`
    Error *apiError `json:"error,omitempty"`
}

type apiError struct {
    Code    string `json:"code"`
    Message string `json:"message"`
}
```

所有 API 使用统一的信封结构：成功时 `data` 字段携带数据，失败时 `error` 字段携带错误码和消息。这与 Phorge Conduit API 的响应格式保持一致。

### 4.7 配置管理

配置模块位于 `internal/config/config.go`，全部通过环境变量加载。

#### 4.7.1 配置结构

```go
type Config struct {
    MySQLHost string
    MySQLPort int
    MySQLUser string
    MySQLPass string
    Namespace string

    ListenAddr   string
    ServiceToken string

    LocalDiskPath string
    MySQLMaxSize  int64
    InstanceName  string
    StorageRoot   string

    S3Bucket    string
    S3AccessKey string
    S3SecretKey string
    S3Region    string
    S3Endpoint  string
}
```

配置分为四组：MySQL 连接参数、服务参数、本地磁盘参数和 S3 参数。每组参数控制对应引擎的启用与行为。

#### 4.7.2 引擎启用判断

```go
func (c *Config) S3Enabled() bool {
    return c.S3Bucket != "" && c.S3AccessKey != "" && c.S3SecretKey != "" &&
        c.S3Region != "" && c.S3Endpoint != ""
}

func (c *Config) LocalDiskEnabled() bool {
    return c.LocalDiskPath != ""
}

func (c *Config) MySQLBlobEnabled() bool {
    return c.MySQLMaxSize > 0
}
```

三种引擎的启用逻辑：

- **MySQL Blob**：`MYSQL_BLOB_MAX_SIZE > 0`。默认值为 1000000（约 1MB），因此 MySQL Blob 引擎默认启用。设为 0 可显式禁用。
- **Local Disk**：`LOCAL_DISK_PATH` 非空。默认为空，需显式配置路径才启用。
- **S3**：五个 S3 相关变量全部非空。缺少任意一个则不启用，避免部分配置导致的运行时错误。

#### 4.7.3 DSN 构建

```go
func (c *Config) FileDSN() string {
    return fmt.Sprintf(
        "%s:%s@tcp(%s:%d)/%s_file?parseTime=true&timeout=5s&readTimeout=30s&writeTimeout=30s",
        c.MySQLUser, c.MySQLPass, c.MySQLHost, c.MySQLPort, c.Namespace,
    )
}
```

DSN 格式遵循 `go-sql-driver/mysql` 的规范。数据库名使用 `{namespace}_file` 前缀，对应 Phorge 的 `phorge_file` 数据库。连接参数包括 5 秒连接超时、30 秒读写超时和 `parseTime=true`。

### 4.8 应用生命周期

#### 4.8.1 启动顺序

```
LoadFromEnv() → MySQLBlobEnabled? → openDB + NewMySQLBlobEngine
             → LocalDiskEnabled? → NewLocalDiskEngine
             → S3Enabled?        → NewS3Engine
             → len(engines)==0?  → Exit(1)
             → NewRouter(engines)
             → Echo + Logger + Recover
             → RegisterRoutes
             → e.Start(addr)
```

启动流程是线性的：

1. 加载环境变量配置
2. 按 MySQL Blob -> Local Disk -> S3 的顺序，逐个检查是否启用并初始化引擎
3. 如果没有任何引擎被启用，打印错误信息并退出
4. 用所有已启用的引擎构建 Router
5. 创建 Echo 实例，添加日志和 Recover 中间件
6. 注册 HTTP 路由
7. 启动 HTTP 服务

每个引擎初始化失败时立即退出，采用 fail-fast 策略——宁可启动失败，也不让服务在引擎缺失的情况下运行。

#### 4.8.2 MySQL 连接池

```go
func openDB(dsn string) (*sql.DB, error) {
    db, err := sql.Open("mysql", dsn)
    // ...
    db.SetMaxOpenConns(25)
    db.SetMaxIdleConns(5)
    db.SetConnMaxLifetime(5 * time.Minute)

    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    if err := db.PingContext(ctx); err != nil {
        return nil, fmt.Errorf("ping db: %w", err)
    }
    return db, nil
}
```

连接池配置：

- **MaxOpenConns=25**：限制对 MySQL 的最大并发连接数，防止在高并发上传时耗尽 MySQL 连接
- **MaxIdleConns=5**：保持少量空闲连接以减少重建开销
- **ConnMaxLifetime=5min**：防止长时间空闲连接被 MySQL 的 `wait_timeout` 断开
- **Ping 超时 10s**：启动时验证数据库可达性，超时则启动失败

## 5. Handle 设计

Handle 是 gorge-file-storage 中最关键的设计决策之一。三种引擎使用完全不同的 handle 格式，这是与 PHP 端兼容的必然选择。

### 5.1 各引擎的 Handle 格式

| 引擎 | 格式 | 示例 | 来源 |
|---|---|---|---|
| MySQL Blob | 纯数字字符串 | `"42"` | MySQL 自增主键 ID |
| Local Disk | 32 字符 hex 分桶路径 | `"a1/b2/c3d4e5f6789012345678901234"` | 16 字节 `crypto/rand` |
| S3 | 带前缀的分桶路径 | `"phabricator/inst/a1/b2/c3d4e5f60123456789"` | 10 字节 `crypto/rand` |

### 5.2 Handle 的不透明性

Handle 对 HTTP 层和调用方是不透明的——调用方不应解析 handle 内容，仅需在上传时持久化返回的 `handle + engine`，后续读取和删除时原样传回。这使得每个引擎可以独立选择最适合自己的标识格式：

- MySQL Blob 用自增 ID，查询快且唯一
- Local Disk 用分桶路径，直接映射文件系统目录结构
- S3 用带前缀的分桶路径，兼容 PHP 端的 key 命名规则

### 5.3 读取/删除时必须指定引擎

由于不同引擎的 handle 格式和寻址方式完全不同，读取和删除时必须同时提供 `handle` 和 `engine`。这避免了需要在多个后端中搜索的歧义和开销。

## 6. 安全设计

### 6.1 Token 认证

API 端点通过 `X-Service-Token` 请求头或 `token` 查询参数进行认证。Token 值与 `SERVICE_TOKEN` 环境变量比对。

设计为可选认证：当 `SERVICE_TOKEN` 为空时，中间件直接放行。这适合开发环境和受信任的内部网络部署。生产环境应始终配置 token。

### 6.2 路径穿越防护

本地磁盘引擎是三种引擎中唯一涉及文件系统路径操作的，因此需要特别的安全措施：

1. **Handle 格式校验**：正则 `^[a-f0-9]{2}/[a-f0-9]{2}/[a-f0-9]{28}$` 严格限制 handle 只能包含小写十六进制字符和正斜杠，彻底排除 `..`、绝对路径等攻击向量。
2. **根路径校验**：拒绝空路径、`"/"` 和相对路径，防止配置错误导致的安全问题。
3. **写入时不校验**：`WriteFile` 不做 handle 校验，因为 handle 由 `generateLocalHandle` 生成，其输出始终符合格式要求。

### 6.3 数据库参数化查询

MySQL Blob 引擎的所有 SQL 查询使用参数化占位符（`?`），由 MySQL 驱动负责转义，防止 SQL 注入。

## 7. 部署方案

### 7.1 Docker 镜像

采用多阶段构建：

- **构建阶段**：基于 `golang:1.22-alpine`，使用 `CGO_ENABLED=0` 静态编译，`-ldflags="-s -w"` 去除调试信息和符号表以缩小二进制体积。
- **运行阶段**：基于 `alpine:3.20`，仅包含编译后的二进制和 CA 证书（S3 HTTPS 连接所需）。

### 7.2 健康检查

```dockerfile
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -qO- http://localhost:8100/healthz || exit 1
```

内置 Docker `HEALTHCHECK`，每 10 秒通过 `wget` 检查 `/healthz` 端点。启动等待 5 秒留出初始化时间，超时 3 秒，连续 3 次失败标记为不健康。

### 7.3 持久化卷

使用本地磁盘引擎时，必须将 `LOCAL_DISK_PATH` 对应的目录挂载为持久化卷，否则容器重启后文件丢失。

## 8. 依赖分析

| 依赖 | 版本 | 用途 |
|---|---|---|
| `labstack/echo/v4` | v4.15.1 | HTTP 框架，提供路由、中间件和上下文管理 |
| `go-sql-driver/mysql` | v1.9.3 | MySQL 数据库驱动 |
| `aws/aws-sdk-go-v2` | v1.41.4 | AWS SDK 核心 |
| `aws/aws-sdk-go-v2/credentials` | v1.19.12 | S3 静态凭证 |
| `aws/aws-sdk-go-v2/service/s3` | v1.97.1 | S3 API 客户端 |

直接依赖三个：Echo 框架、MySQL 驱动和 AWS SDK。存储引擎实现基于标准库和 SDK API，无额外第三方依赖。

## 9. 测试覆盖

项目包含三组测试文件：

| 测试文件 | 覆盖范围 |
|---|---|
| `config_test.go` | 默认配置验证、自定义环境变量覆盖、FileDSN 格式、三种引擎的 Enabled 判断 |
| `localdisk_test.go` | 本地磁盘读写删除全生命周期、Handle 格式校验、路径穿越拒绝、非法根路径拒绝 |
| `router_test.go` | 按优先级选择写入引擎、大文件跳过有大小限制的引擎、无可写引擎时报错、按 ID 查找引擎 |

测试设计的特点：

- **端到端验证**：`localdisk_test.go` 的 `TestLocalDiskRoundTrip` 覆盖了完整的写入-读取-删除-确认删除流程。
- **安全测试**：`TestLocalDiskRejectsBadHandle` 验证路径穿越攻击被拒绝，`TestLocalDiskBadRoot` 验证危险根路径被拒绝。
- **Mock 引擎**：`router_test.go` 使用 `mockEngine` 实现 `StorageEngine` 接口，隔离测试路由逻辑，不依赖实际存储后端。
- **表驱动测试**：配置测试覆盖默认值和自定义值两种场景，引擎启用判断覆盖启用和禁用两种状态。

## 10. 总结

gorge-file-storage 是一个职责明确的文件存储网关微服务，核心价值在于：

1. **统一抽象**：通过 `StorageEngine` 接口将 MySQL Blob、本地磁盘、S3 三种异构存储后端统一在一套 API 之后，调用方无需关心底层存储细节。
2. **智能路由**：按优先级自动选择存储引擎，小文件存数据库减少碎片，大文件存对象存储降低成本，同时支持手动指定引擎覆盖默认策略。
3. **行为兼容**：Handle 格式、S3 key 命名规则与 PHP 端保持一致，确保存量数据可正常读取，支持渐进式迁移。
4. **安全优先**：本地磁盘引擎的正则 handle 校验、根路径校验防止路径穿越；Token 认证防止未授权访问；参数化 SQL 防止注入。
5. **运维友好**：纯环境变量配置、按需启用引擎、Docker 多阶段构建、内置健康检查，开箱即用于容器化部署。
