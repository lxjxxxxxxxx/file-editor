# Go Backend

这是一个使用纯 Go 标准库实现的后端版本，不依赖第三方包，也不使用 CGO。

## 目录说明

- `main.go`: 服务入口
- `config.example.json`: 配置示例
- `go.mod`: Go 模块定义

## 配置

将 `config.example.json` 复制为 `config.json` 后再启动。

支持字段：

- `token`: API 访问令牌
- `port`: 监听端口
- `rootPath`: 旧版单目录配置，启动时会兼容转换
- `rootPaths`: 目录列表，支持字符串数组或对象数组
- `excludedNames`: 排除的文件/目录名
- `excludeHidden`: 是否排除隐藏文件

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
cp config.example.json config.json
go run .
```

如果存在 `../frontend/dist`，服务会自动托管前端静态文件。
