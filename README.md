# 📂 在线文件编辑器

一个基于 Web 的在线文件编辑器，支持浏览、编辑、创建、删除、复制、移动文件和文件夹，以及修改文件权限。采用 Vue 3 + Element Plus + Monaco Editor 构建前端，Node.js + Express 构建后端。

## ✨ 功能特性

- 📁 **文件树浏览** - 懒加载模式，支持大目录快速浏览
- 📝 **代码编辑** - 集成 Monaco Editor，支持语法高亮、代码折叠、自动补全
- 💾 **文件操作** - 创建、删除、复制、移动文件和文件夹
- 🔒 **权限管理** - 查看和修改文件权限（chmod）
- 🎨 **深色主题** - 舒适的深色界面，适合长时间编辑
- ⌨️ **快捷键** - Ctrl+S 保存文件
- 📊 **文件信息** - 显示文件大小、权限、修改时间
- 🔍 **多语言支持** - 支持 30+ 种编程语言的语法高亮

## 🏗️ 技术架构

```
┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐
│   Vue 3 + Vite  │────▶│  Express API    │────▶│   文件系统      │
│  Element Plus   │◀────│   (Node.js)     │◀────│  (rootPath)     │
│ Monaco Editor   │     │   Token Auth    │     │                 │
└─────────────────┘     └─────────────────┘     └─────────────────┘
        │                                               │
        └──────────────── Proxy (开发时) ───────────────┘
```

### 前端技术栈
- **Vue 3** - 渐进式 JavaScript 框架
- **Element Plus** - UI 组件库
- **Monaco Editor** - VS Code 同款代码编辑器
- **Vite** - 快速构建工具
- **Axios** - HTTP 客户端

### 后端技术栈
- **Node.js** - JavaScript 运行时
- **Express** - Web 框架
- **CORS** - 跨域支持
- **mime-types** - MIME 类型检测

## 📁 项目结构

```
file-editor/
├── backend/                    # 后端服务
│   ├── index.js               # 主入口，API 路由
│   ├── package.json           # 依赖配置
│   ├── config.json            # 运行时配置（需创建）
│   ├── config.example.json    # 配置示例
│   └── node_modules/          # 依赖目录
│
├── frontend/                   # 前端应用
│   ├── src/
│   │   ├── App.vue            # 主组件
│   │   ├── MonacoEditor.vue   # Monaco 编辑器封装
│   │   ├── api.js             # API 接口封装
│   │   └── main.js            # 入口文件
│   ├── index.html             # HTML 模板
│   ├── vite.config.js         # Vite 配置
│   ├── package.json           # 依赖配置
│   └── node_modules/          # 依赖目录
│
└── README.md                   # 本文档
```

## 🚀 快速开始

### 环境要求

- Node.js >= 16.x
- npm >= 8.x

### 1. 克隆项目

```bash
cd file-editor
```

### 2. 配置后端

```bash
cd backend

# 复制配置示例
cp config.example.json config.json

# 编辑配置（根据实际情况修改）
vim config.json
```

**config.json 说明：**

| 字段 | 类型 | 说明 | 示例 |
|------|------|------|------|
| `token` | string | 访问令牌，用于 API 认证 | `"your-secret-token"` |
| `port` | number | 后端监听端口（可选，默认 3002） | `3002` |
| `rootPath` | string | 允许编辑的根目录路径 | `"/home/user/projects"` |
| `excludedNames` | array | 排除的文件/目录名 | `["node_modules", ".git"]` |
| `excludeHidden` | boolean | 是否排除隐藏文件（以.开头） | `true` |

### 3. 安装依赖

**后端：**
```bash
cd backend
npm install
```

**前端：**
```bash
cd frontend
npm install
```

### 4. 启动服务

**开发模式（推荐）：**

```bash
# 终端 1：启动后端
cd backend
npm start
# 或
node index.js

# 终端 2：启动前端
cd frontend
npm run dev
```

**生产模式：**

后端已支持静态文件托管，只需构建前端后启动后端即可。

```bash
# 构建前端（构建产物位于 frontend/dist 目录）
cd frontend
npm run build

# 启动后端（自动托管 frontend/dist 目录）
cd ../backend
npm start
```

**前端静态文件路径：** 后端会自动检测并托管 `frontend/dist` 目录（相对于后端代码的上一级目录）。

### 5. 访问应用

开发模式：
- 前端地址：`http://localhost:5174`
- 后端地址：`http://localhost:3002`

生产模式（后端托管前端）：
- 统一地址：`http://localhost:3002`

**⚠️ 注意：** 首次访问需要在 URL 中添加 token 参数：
```
http://localhost:3002/?token=your-secret-token
```

token 必须与 `config.json` 中的 `token` 字段一致。

## 🧪 测试

### 手动测试清单

#### 1. 文件树浏览
- [ ] 点击文件夹展开/折叠
- [ ] 懒加载大量文件时性能正常
- [ ] 隐藏文件根据配置正确显示/隐藏

#### 2. 文件编辑
- [ ] 点击文件打开编辑器
- [ ] 语法高亮正确显示
- [ ] 修改文件后显示未保存标记
- [ ] Ctrl+S 保存文件
- [ ] 超过 2MB 的文件无法编辑
- [ ] 二进制文件无法编辑

#### 3. 文件操作
- [ ] 新建文件
- [ ] 新建文件夹
- [ ] 删除文件/文件夹
- [ ] 复制文件/文件夹
- [ ] 移动文件/文件夹

#### 4. 权限管理
- [ ] 查看文件权限
- [ ] 修改文件权限（如 644 → 755）

#### 5. Tab 管理
- [ ] 打开多个文件显示 Tab
- [ ] 切换 Tab 正常
- [ ] 关闭 Tab 提示保存
- [ ] 关闭已保存的 Tab 无提示

### API 测试

使用 curl 测试后端 API：

```bash
# 设置 token
TOKEN="your-secret-token"

# 获取文件树
curl "http://localhost:3002/api/files/tree?token=$TOKEN"

# 获取文件内容
curl "http://localhost:3002/api/files/content?token=$TOKEN&path=test.txt"

# 保存文件
curl -X POST "http://localhost:3002/api/files/save?token=$TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"path":"test.txt","content":"Hello World"}'

# 创建文件
curl -X POST "http://localhost:3002/api/files/create?token=$TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"path":"newfile.txt","type":"file"}'

# 删除文件
curl -X DELETE "http://localhost:3002/api/files/delete?token=$TOKEN&path=test.txt"

# 获取文件状态
curl "http://localhost:3002/api/files/stat?token=$TOKEN&path=test.txt"

# 修改权限
curl -X POST "http://localhost:3002/api/files/permissions?token=$TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"path":"test.txt","mode":"755"}'
```

## 📦 部署

### 方式一：直接部署（推荐）

#### 1. 准备环境

```bash
# 安装 Node.js 16+
# Ubuntu/Debian
curl -fsSL https://deb.nodesource.com/setup_16.x | sudo -E bash -
sudo apt-get install -y nodejs

# 或使用 nvm
curl -o- https://raw.githubusercontent.com/nvm-sh/nvm/v0.39.0/install.sh | bash
nvm install 16
nvm use 16
```

#### 2. 部署后端

```bash
cd /path/to/file-editor/backend

# 安装依赖
npm install --production

# 配置
cp config.example.json config.json
vim config.json
# 修改 rootPath 为实际路径，设置安全的 token

# 测试启动
node index.js
```

#### 3. 部署前端

后端已内置静态文件托管功能，构建前端到 `frontend/dist` 目录即可。

```bash
cd /path/to/file-editor/frontend

# 安装依赖
npm install

# 构建（输出到 frontend/dist 目录）
npm run build

# 构建完成后，后端会自动托管 frontend/dist 目录
```

**静态文件路径说明：**
- 后端会自动检测 `backend/../frontend/dist` 目录
- 如果该目录存在，后端会启用静态文件托管
- 所有非 `/api` 路由的请求都会返回 `index.html`（支持前端 History 模式）

# 构建
npm run build

# 构建产物在 dist/ 目录
```

#### 4. 使用 PM2 守护进程

后端会自动托管前端静态文件，只需启动后端服务即可。

```bash
# 安装 PM2
npm install -g pm2

# 启动后端（自动托管 frontend/dist 目录）
cd /path/to/file-editor/backend
pm2 start index.js --name "file-editor"

# 保存配置
pm2 save
pm2 startup
```

### 方式二：Docker 部署

创建 `Dockerfile`：

```dockerfile
# 构建阶段
FROM node:18-alpine AS builder

WORKDIR /app/frontend
COPY frontend/package*.json ./
RUN npm install
COPY frontend/ ./
RUN npm run build

# 运行阶段
FROM node:18-alpine

WORKDIR /app

# 复制后端
COPY backend/package*.json ./
RUN npm install --production
COPY backend/ ./

# 复制前端构建产物
COPY --from=builder /app/frontend/dist ./public

# 暴露端口
EXPOSE 3002

# 启动
CMD ["node", "index.js"]
```

构建和运行：

```bash
# 构建镜像
docker build -t file-editor .

# 运行容器
docker run -d \
  --name file-editor \
  -p 3002:3002 \
  -v /path/to/edit:/data:rw \
  -e ROOT_PATH=/data \
  -e TOKEN=your-secret-token \
  file-editor
```

### 方式三：Nginx 反向代理

**Nginx 配置示例：**

```nginx
server {
    listen 80;
    server_name file-editor.yourdomain.com;

    # 前端静态文件
    location / {
        root /path/to/file-editor/frontend/dist;
        index index.html;
        try_files $uri $uri/ /index.html;
    }

    # API 代理
    location /api/ {
        proxy_pass http://localhost:3002;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection 'upgrade';
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_cache_bypass $http_upgrade;
        
        # 允许大文件上传
        client_max_body_size 10M;
    }
}
```

**启用 HTTPS（Let's Encrypt）：**

```bash
# 安装 certbot
sudo apt install certbot python3-certbot-nginx

# 获取证书
sudo certbot --nginx -d file-editor.yourdomain.com

# 自动续期测试
sudo certbot renew --dry-run
```

### 方式四：Systemd 服务

创建 `/etc/systemd/system/file-editor.service`：

```ini
[Unit]
Description=File Editor Backend
After=network.target

[Service]
Type=simple
User=www-data
WorkingDirectory=/path/to/file-editor/backend
ExecStart=/usr/bin/node index.js
Restart=on-failure
RestartSec=5
Environment=NODE_ENV=production

[Install]
WantedBy=multi-user.target
```

启动服务：

```bash
sudo systemctl daemon-reload
sudo systemctl enable file-editor
sudo systemctl start file-editor
sudo systemctl status file-editor
```

## 🔒 安全建议

1. **使用强 Token** - 至少 32 位随机字符串
2. **限制 rootPath** - 不要设置为系统根目录 `/`
3. **使用 HTTPS** - 生产环境必须启用 HTTPS
4. **访问控制** - 使用防火墙限制 IP 访问
5. **定期备份** - 重要文件定期备份
6. **排除敏感目录** - 在 `excludedNames` 中添加 `.ssh`, `.gnupg` 等

## 🐛 常见问题

### Q: 前端无法连接到后端？
A: 检查 `vite.config.js` 中的代理配置，确保后端地址正确。

### Q: 保存文件失败？
A: 检查后端运行用户是否有写入权限，可以使用 `chmod` 修改目录权限。

### Q: 大文件无法编辑？
A: 后端限制最大 2MB，如需修改，编辑 `backend/index.js` 中的 `MAX_FILE_SIZE`。

### Q: 如何修改端口？
A: 后端端口在 `index.js` 底部修改，前端端口在 `vite.config.js` 中修改。

### Q: 如何支持更多文件类型？
A: 编辑 `backend/index.js` 中的 `TEXT_EXTENSIONS` 集合。

## 📝 更新日志

### v1.0.0 (2024-04)
- ✨ 初始版本发布
- 📝 文件浏览、编辑、保存
- 📁 文件夹操作（创建、删除、复制、移动）
- 🔒 权限管理
- 🎨 深色主题界面

## 📄 许可证

MIT License

## 🤝 贡献

欢迎提交 Issue 和 Pull Request！

---

Made with ❤️ by 阿牛
