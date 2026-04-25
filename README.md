# 在线文件编辑器

一个基于 Web 的在线文件编辑器。前端使用 Vue 3、Element Plus 和 Monaco Editor，后端使用纯 Go 标准库实现，不依赖第三方 Go 包，也不使用 CGO。

项目当前以“常驻目录 + 文件树右键菜单”为主要交互方式，适合在受控目录内快速浏览、编辑和管理文本文件。

## 功能特性

- 多常驻目录：支持添加、移除、编辑别名，文件树按根目录懒加载。
- Monaco 编辑器：支持语法高亮、代码折叠、Tab 多文件编辑、`Ctrl+S` 保存。
- 右键菜单操作：
  - 文件：详细信息、重命名、创建副本、删除、复制、移动、权限。
  - 普通目录：详细信息、新建文件、新建目录、重命名、创建副本、删除、复制、移动、权限、刷新、添加为常驻目录。
  - 常驻根目录：详细信息、新建文件、新建目录、刷新，并提供别名编辑和移除快捷按钮。
- 创建副本：目录在当前目录生成 `_backup` 副本，文件生成 `.backup` 副本；冲突时自动追加序号。
- 详细信息：显示名称、类型、所属根目录、相对路径、绝对路径、大小、修改时间、权限，以及平台身份信息。
  - Linux：显示 UID/GID，并尽量解析用户名和组名。
  - Windows：显示所有者、所属组及 SID。
- 文本文件保护：后端按内容、扩展名和文件名判断是否可作为文本打开；非文本文件会先询问是否强制按文本打开。
- 加载失败保护：非文本取消、文件过大、读取失败等没有成功加载的文件不会遗留空编辑 Tab，避免误保存覆盖原文件。
- 文件管理安全：路径限制在常驻根目录内，复制/移动目录时禁止放入自身或子目录，创建/复制默认不覆盖已有文件。

## 技术架构

```text
┌──────────────────────────────┐
│ Vue 3 + Vite + Element Plus  │
│ Monaco Editor                │
└───────────────┬──────────────┘
                │ /api, X-Auth-Token
┌───────────────▼──────────────┐
│ Go HTTP Server               │
│ token auth / static hosting  │
└───────────────┬──────────────┘
                │
┌───────────────▼──────────────┐
│ 文件系统 rootPaths           │
└──────────────────────────────┘
```

## 项目结构

```text
file-editor/
├── backend-go/
│   ├── main.go                    # Go 后端入口和 API
│   ├── file_identity_linux.go     # Linux UID/GID 解析
│   ├── file_identity_windows.go   # Windows Owner/Group/SID 解析
│   ├── file_identity_other.go     # 其他平台身份信息回退
│   ├── go.mod
│   ├── dist/                      # 前端生产构建产物目录
│   └── README.md
├── frontend/
│   ├── src/
│   │   ├── App.vue                # 主界面、文件树、右键菜单和弹窗
│   │   ├── MonacoEditor.vue       # Monaco 编辑器封装
│   │   ├── api.js                 # API 封装
│   │   └── main.js
│   ├── vite.config.js             # 开发代理到 localhost:3002
│   └── package.json
└── README.md
```

## 配置

后端从可执行文件所在目录读取 `config.json`。如果不存在，会自动生成默认配置。

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

字段说明：

| 字段 | 说明 |
| --- | --- |
| `token` | API 访问令牌。前端会通过 URL 中的 `?token=` 读取，并放入 `X-Auth-Token` 请求头。 |
| `port` | 后端监听端口，默认 `3002`。 |
| `rootPath` | 旧版单目录配置字段，仅用于兼容历史配置。 |
| `rootPaths` | 常驻根目录列表，支持字符串数组或 `{ "path": "...", "alias": "..." }` 对象数组。 |
| `excludedNames` | 文件树中过滤的文件名或目录名，按名称精确匹配。 |
| `excludeHidden` | 是否过滤以 `.` 开头的隐藏文件和目录。 |
| `textExtensions` | 额外按文本处理的扩展名。 |
| `textFileNames` | 额外按文本处理的文件名，适合无后缀配置文件。 |
| `binaryExtensions` | 明确按二进制处理的扩展名。 |
| `binaryFileNames` | 明确按二进制处理的文件名。 |

`rootPaths` 示例：

```json
[
  "C:/projects/file-editor",
  { "path": "D:/work", "alias": "工作目录" }
]
```

## 开发运行

后端：

```bash
cd backend-go
go run .
```

前端：

```bash
cd frontend
npm install
npm run dev
```

开发环境默认：

- 前端：`http://localhost:5174`
- 后端：`http://localhost:3002`
- Vite 会把 `/api` 代理到 `http://localhost:3002`

访问时需要携带 token：

```text
http://localhost:5174/?token=file-editor-2024-secret-token
```

## 生产运行

前端提供 `build-go` 脚本，将构建产物输出到 `backend-go/dist`：

```bash
cd frontend
npm run build-go
```

然后启动 Go 后端：

```bash
cd backend-go
go run .
```

生产模式下，Go 后端会托管同目录下的 `dist`，并对非 `/api` 路由返回前端入口。

## API 概览

所有 `/api` 接口都需要 token，可通过 URL 参数 `token` 或请求头 `X-Auth-Token` 提供。

| 接口 | 方法 | 说明 |
| --- | --- | --- |
| `/api/files/tree` | GET | 获取常驻根目录列表或指定目录子节点。 |
| `/api/files/content` | GET | 读取文件内容，支持 `forceText=true`。 |
| `/api/files/save` | POST | 保存文本文件内容。 |
| `/api/files/create` | POST | 创建空文件或目录。 |
| `/api/files/delete` | DELETE | 删除文件或目录。 |
| `/api/files/copy` | POST | 跨根目录复制文件或目录。 |
| `/api/files/move` | POST | 跨根目录移动文件或目录。 |
| `/api/files/stat` | GET | 获取文件、目录或根目录详细信息。 |
| `/api/files/permissions` | POST | 修改权限位。 |
| `/api/roots` | GET/POST/PUT/DELETE | 查询、添加、修改别名、移除常驻目录。 |

常见参数：

- `path`：相对当前根目录的路径；根目录详情可传空字符串。
- `rootIndex`：常驻根目录索引。
- `fromRoot` / `toRoot`：复制、移动时的源根目录和目标根目录索引。

## 安全边界

- 后端会将所有文件路径解析到对应 `rootPaths` 下，拒绝越权访问。
- 请求体限制为 10MB，单文件在线编辑限制为 2MB。
- JSON 请求体会拒绝未知字段，减少误传参数。
- 非文本文件默认不会直接进入编辑器，需要用户确认强制按文本打开。
- 加载失败的 Tab 会自动关闭，避免空内容保存覆盖原文件。
- 复制和移动目录时禁止目标位于源目录内部。
- 常驻目录操作只修改配置，不会删除真实目录。

## systemd 支持

Go 后端支持 `install` 和 `uninstall` 参数：

```bash
sudo ./file-editor-backend install
sudo ./file-editor-backend uninstall
```

`install` 仅在 Linux 下生效，会创建并启用 `file-editor-backend.service`。建议先将后端编译为固定路径的二进制后再安装服务。

## 常见问题

### 前端打不开 API？

确认后端已启动，访问 URL 中带有正确 token，并检查 `frontend/vite.config.js` 中 `/api` 代理目标是否为当前后端地址。

### 文件无法打开？

超过 2MB 的文件不会进入编辑器。被识别为非文本的文件会弹出确认框；取消或读取失败时，Tab 会自动关闭。

### 权限修改在 Windows 上有什么差异？

当前权限接口使用 Go 的 `os.Chmod`。Windows 下权限位语义和 Linux 不完全一致；详细信息窗口会显示 Windows 的所有者、组和 SID。

### 目录大小为什么不是递归占用空间？

详细信息中的目录大小来自文件系统 `stat` 的目录项大小，不递归遍历目录，避免大目录导致界面卡顿。
