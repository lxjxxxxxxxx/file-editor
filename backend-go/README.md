# Go Backend

这是一个使用纯 Go 标准库实现的后端版本，不依赖第三方包，也不使用 CGO。

## 目录说明

- `main.go`: 服务入口
- `config.example.json`: 配置示例
- `go.mod`: Go 模块定义

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

静态前端资源会从程序目录下的 `dist` 读取。

也就是说，当前目录结构应类似于：

```text
backend-go/
  main.go
  config.json
  dist/
```

后端现在按“程序目录”而不是“工作目录”定位 `config.json` 和 `dist`，因此即使从别的目录启动，只要可执行文件位于 `backend-go` 目录，仍然会读取同目录下的配置和前端构建产物。
