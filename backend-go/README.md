# Go Backend

这是一个使用纯 Go 标准库实现的后端版本，不依赖第三方包，也不使用 CGO。

## 目录说明

- `main.go`: 服务入口
- `file_identity_linux.go`: Linux 下解析 UID/GID、用户名和组名
- `file_identity_windows.go`: Windows 下解析所有者、所属组和 SID
- `file_identity_other.go`: 其他平台身份信息回退
- `go.mod`: Go 模块定义
- `dist/`: 前端生产构建产物目录，存在时由 Go 后端托管

## 配置

后端会从程序目录读取 `config.json`。

如果启动时没有检测到 `config.json`，程序会自动在当前程序目录生成一份完整结构的默认配置文件。

当前配置文件示例：

```json
{
  "token": "file-editor-2024-secret-token",
  "port": 3002,
  "rootPaths": [],
  "excludedNames": [],
  "excludeHidden": false,
  "textExtensions": [],
  "textFileNames": [],
  "binaryExtensions": [],
  "binaryFileNames": []
}
```

支持字段：

- `token`: API 访问令牌。前端请求会通过 URL 参数或 `X-Auth-Token` 请求头携带这个值。
- `port`: 后端监听端口，默认 `3002`。
- `rootPath`: 旧版单目录配置字段，仅用于兼容历史配置。启动时会自动转换到 `rootPaths`。
- `rootPaths`: 根目录列表。前端文件树会以这些目录作为常驻根目录。
- `excludedNames`: 需要在文件树中过滤掉的文件名或目录名，按名称精确匹配。
- `excludeHidden`: 是否过滤以 `.` 开头的隐藏文件和隐藏目录。
- `textExtensions`: 额外允许按文本文件打开的扩展名列表。
- `textFileNames`: 额外允许按文本文件打开的文件名列表，适合无后缀配置文件。
- `binaryExtensions`: 明确按二进制文件处理的扩展名列表。
- `binaryFileNames`: 明确按二进制文件处理的文件名列表。

常驻目录相关接口会把新增、删除、别名修改写回 `config.json`。

文本文件识别策略：

- 后端默认优先按文件内容判断是否为文本，而不是只依赖扩展名。
- 常见文本扩展名、无后缀配置文件名会被优先识别为文本。
- 命中二进制扩展名、二进制文件名、二进制签名或明显包含空字节的文件会被拒绝按文本打开。
- 如果某类文件识别不符合你的场景，可以通过 `textExtensions`、`textFileNames`、`binaryExtensions`、`binaryFileNames` 进行补充。

`rootPaths` 支持两种格式：

```json
[
  "/data/project-a",
  "/data/project-b"
]
```

或：

```json
[
  { "path": "/data/project-a", "alias": "A" },
  { "path": "/data/project-b", "alias": "B" }
]
```

## 启动

```bash
cd backend-go
go run .
```

前端开发模式默认运行在 `http://localhost:5174`，并通过 Vite 代理访问 `http://localhost:3002/api`。

生产模式下，将前端构建产物输出到 `backend-go/dist` 后，Go 后端会直接托管前端页面和 API。

## API 概览

所有 `/api` 接口都需要 token，可通过 URL 参数 `token` 或请求头 `X-Auth-Token` 提供。

| 接口 | 方法 | 说明 |
| --- | --- | --- |
| `/api/files/tree` | GET | 获取常驻根目录列表或指定目录子节点。 |
| `/api/files/content` | GET | 读取文本文件内容，支持 `forceText=true`。 |
| `/api/files/save` | POST | 保存文本文件内容。 |
| `/api/files/create` | POST | 创建空文件或目录。 |
| `/api/files/delete` | DELETE | 删除文件或目录。 |
| `/api/files/copy` | POST | 复制文件或目录。 |
| `/api/files/move` | POST | 移动文件或目录，跨设备时会复制后删除。 |
| `/api/files/stat` | GET | 获取文件、目录或根目录详细信息。 |
| `/api/files/permissions` | POST | 修改权限位。 |
| `/api/roots` | GET/POST/PUT/DELETE | 查询、添加、修改别名、移除常驻目录。 |

`/api/files/stat` 会返回基础元数据和平台身份信息：

- Linux：UID、GID，并尽量解析用户名和组名。
- Windows：所有者、所属组和 SID。
- 其他平台：返回平台标识，不伪造身份信息。

`path=""` 可用于查询指定 `rootIndex` 对应的常驻根目录自身信息。

## 安全边界

- 路径会被解析并限制在对应常驻根目录内，越权路径会被拒绝。
- 单文件在线编辑限制为 2MB。
- JSON 请求体限制为 10MB，并拒绝未知字段。
- 非文本文件默认拒绝读取，前端可在用户确认后传入 `forceText=true` 强制按文本打开。
- 创建文件和复制文件使用排他创建，默认不会覆盖已有目标。
- 复制或移动目录时，禁止目标为源目录自身或其子目录。
- `/api/roots` 只修改常驻目录配置，不删除真实目录。

## 安装为 systemd 服务

程序支持命令行参数 `install` 和 `uninstall`。

- 在 Linux 环境下，执行 `install` 会：
  - 自动确保程序目录下存在 `config.json`
  - 将当前程序安装为 systemd 服务
  - 写入 `/etc/systemd/system/file-editor-backend.service`
  - 执行 `systemctl daemon-reload`
  - 执行 `systemctl enable --now file-editor-backend.service`
- 在非 Linux 环境下，程序会打印提示并跳过，不会报错退出。

示例：

```bash
sudo ./file-editor-backend install
```

如果直接用 `go run . install`，由于 Go 会把临时编译产物放到临时目录，systemd 中记录的 `ExecStart` 也会指向临时路径，因此更推荐先编译成固定二进制后再执行安装。

卸载示例：

```bash
sudo ./file-editor-backend uninstall
```

执行 `uninstall` 时，在 Linux 环境下会：

- 执行 `systemctl disable --now file-editor-backend.service`
- 删除 `/etc/systemd/system/file-editor-backend.service`
- 执行 `systemctl daemon-reload`
- 尝试执行 `systemctl reset-failed file-editor-backend.service`

如果当前环境不是 Linux，程序会打印提示并跳过。

静态前端资源会从程序目录下的 `dist` 读取。

也就是说，当前目录结构应类似于：

```text
backend-go/
  main.go
  config.json
  dist/
```

后端现在按“程序目录”而不是“工作目录”定位 `config.json` 和 `dist`，因此即使从别的目录启动，只要可执行文件位于 `backend-go` 目录，仍然会读取同目录下的配置和前端构建产物。
