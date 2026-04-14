const express = require('express');
const cors = require('cors');
const path = require('path');
const fs = require('fs');
const fsPromises = require('fs').promises;
const mime = require('mime-types');

// ====== 加载配置 ======
const CONFIG_PATH = path.join(__dirname, 'config.json');
let CONFIG = {};
function loadConfig() {
  try {
    CONFIG = JSON.parse(fs.readFileSync(CONFIG_PATH, 'utf8'));
    // 兼容旧配置：将 rootPath 转为 rootPaths
    if (CONFIG.rootPath && !CONFIG.rootPaths) {
      CONFIG.rootPaths = [CONFIG.rootPath];
      delete CONFIG.rootPath;
    }
    console.log('✅ 配置文件加载成功');
  } catch (e) {
    console.error('❌ 配置文件加载失败:', e.message);
    process.exit(1);
  }
}
loadConfig();

const AUTH_TOKEN = CONFIG.token || 'default-token';
const PORT = CONFIG.port || 3002;
const ROOT_PATHS = CONFIG.rootPaths || ['./'];
const MAX_FILE_SIZE = 2 * 1024 * 1024; // 2MB

// 从配置读取排除规则
const EXCLUDED_NAMES = new Set(CONFIG.excludedNames || ['node_modules', '.openclaw']);
const EXCLUDE_HIDDEN = CONFIG.excludeHidden !== false; // 默认排除隐藏文件

const app = express();
app.use(cors({
  exposedHeaders: ['X-Auth-Token'],
}));
app.use(express.json({ limit: '10mb' }));

// ====== Token 验证中间件 ======
function authMiddleware(req, res, next) {
  const token = req.query.token || req.headers['x-auth-token'];
  if (!token) {
    return res.status(401).json({ success: false, error: '缺少访问令牌 (token)，请在 URL 中添加 ?token=your-token' });
  }
  if (token !== AUTH_TOKEN) {
    return res.status(403).json({ success: false, error: '访问令牌无效，请检查 token 是否正确' });
  }
  next();
}

// 所有 API 路由都需要验证
app.use('/api', authMiddleware);

const TEXT_EXTENSIONS = new Set([
  '.txt', '.md', '.log',
  '.js', '.ts', '.jsx', '.tsx', '.vue',
  '.py', '.rb', '.java', '.kt', '.go', '.rs', '.lua', '.pl',
  '.c', '.cpp', '.h', '.hpp',
  '.html', '.htm', '.xml', '.svg',
  '.css', '.scss', '.sass', '.less',
  '.json', '.json5', '.toml', '.yaml', '.yml',
  '.ini', '.cfg', '.conf',
  '.sh', '.bash', '.zsh', '.bat', '.ps1',
  '.sql',
  '.env',
  '.csv', '.tsv',
  '.php',
]);

const BINARY_SIGNATURES = [
  Buffer.from([0x89, 0x50, 0x4E, 0x47]),
  Buffer.from([0xFF, 0xD8, 0xFF]),
  Buffer.from([0x47, 0x49, 0x46]),
  Buffer.from([0x50, 0x4B]),
  Buffer.from([0x25, 0x50, 0x44, 0x46]),
];

// ====== 工具函数 ======
// 验证路径是否在允许的根目录范围内
function resolvePath(filePath, rootIndex = 0) {
  const rootDir = ROOT_PATHS[rootIndex] || ROOT_PATHS[0];
  const resolvedRoot = path.resolve(rootDir);
  const resolved = path.resolve(rootDir, filePath);
  if (!resolved.startsWith(resolvedRoot + path.sep) && resolved !== resolvedRoot) {
    throw new Error('路径越权访问被拒绝');
  }
  return resolved;
}

// 获取所有根目录的绝对路径
function getRootPathsInfo() {
  return ROOT_PATHS.map((rp, index) => {
    const absPath = path.resolve(rp);
    return {
      index,
      path: rp,
      name: path.basename(absPath) || rp,
      absPath: absPath
    };
  });
}

// 保存配置到文件
function saveConfig() {
  try {
    fs.writeFileSync(CONFIG_PATH, JSON.stringify(CONFIG, null, 2), 'utf8');
    return true;
  } catch (e) {
    console.error('保存配置失败:', e.message);
    return false;
  }
}

async function isTextFile(realPath, filePath) {
  const ext = path.extname(filePath).toLowerCase();
  const baseName = path.basename(filePath).toLowerCase();
  if (TEXT_EXTENSIONS.has(ext)) return true;
  if (['dockerfile', 'makefile', 'vagrantfile'].includes(baseName)) return true;
  if (!ext) {
    try {
      const header = await fsPromises.readFile(realPath, { start: 0, end: 15 });
      for (const sig of BINARY_SIGNATURES) {
        if (header.slice(0, sig.length).equals(sig)) return false;
      }
      header.toString('utf8');
      return true;
    } catch {
      return false;
    }
  }
  const mimeType = mime.lookup(filePath);
  return !!(mimeType && mimeType.startsWith('text/'));
}

async function buildTree(dirPath, relativeBase, depth, maxDepth) {
  if (depth > maxDepth) return null;
  let entries;
  try {
    entries = await fsPromises.readdir(dirPath, { withFileTypes: true });
  } catch {
    return [];
  }
  const tree = [];
  for (const entry of entries) {
    // 排除隐藏文件（以.开头）
    if (EXCLUDE_HIDDEN && entry.name.startsWith('.')) continue;
    // 排除配置的目录/文件
    if (EXCLUDED_NAMES.has(entry.name)) continue;
    const full = path.join(dirPath, entry.name);
    const rel = relativeBase ? path.posix.join(relativeBase, entry.name) : entry.name;
    try {
      const stat = await fsPromises.lstat(full);
      tree.push({
        name: entry.name,
        path: rel,
        isDirectory: stat.isDirectory(),
        isFile: stat.isFile(),
        size: stat.size,
        mtime: stat.mtimeMs,
        mode: (stat.mode & 0o777).toString(8),
        children: stat.isDirectory() && depth < maxDepth
          ? await buildTree(full, rel, depth + 1, maxDepth)
          : [],
      });
    } catch { /* skip */ }
  }
  tree.sort((a, b) => {
    if (a.isDirectory !== b.isDirectory) return a.isDirectory ? -1 : 1;
    return a.name.localeCompare(b.name);
  });
  return tree;
}

// ====== API ======

// 1. 文件树 - 支持懒加载，只加载一层，支持多根目录
app.get('/api/files/tree', async (req, res) => {
  try {
    const subDir = req.query.path || '';
    const rootIndex = parseInt(req.query.rootIndex || req.query.root || '0', 10);
    const listRoots = req.query.listRoots === 'true';

    // 只有当请求的是全局根（没有指定具体根目录或明确请求根列表）时才返回所有常驻目录
    if (!subDir && listRoots) {
      const rootInfos = getRootPathsInfo();
      const tree = [];
      for (const info of rootInfos) {
        try {
          const stat = await fsPromises.lstat(info.absPath);
          tree.push({
            name: info.name,
            path: '',
            rootIndex: info.index,
            absPath: info.absPath,
            isDirectory: stat.isDirectory(),
            isFile: stat.isFile(),
            size: stat.size,
            mtime: stat.mtimeMs,
            mode: (stat.mode & 0o777).toString(8).padStart(3, '0'),
            children: stat.isDirectory() ? [] : undefined,
          });
        } catch { /* skip invalid root */ }
      }
      res.json({ success: true, data: tree, isMultiRoot: true });
      return;
    }

    const real = resolvePath(subDir, rootIndex);
    // 懒加载模式：只加载当前目录的直接子项，不递归
    const entries = await fsPromises.readdir(real, { withFileTypes: true });
    const tree = [];
    for (const entry of entries) {
      // 排除隐藏文件（以.开头）
      if (EXCLUDE_HIDDEN && entry.name.startsWith('.')) continue;
      // 排除配置的目录/文件
      if (EXCLUDED_NAMES.has(entry.name)) continue;
      const full = path.join(real, entry.name);
      const rel = subDir ? path.posix.join(subDir, entry.name) : entry.name;
      try {
        const stat = await fsPromises.lstat(full);
        tree.push({
          name: entry.name,
          path: rel,
          rootIndex: rootIndex,
          isDirectory: stat.isDirectory(),
          isFile: stat.isFile(),
          size: stat.size,
          mtime: stat.mtimeMs,
          mode: (stat.mode & 0o777).toString(8).padStart(3, '0'),
          // 目录标记为未加载（有子节点但为空数组），文件标记为叶子节点
          children: stat.isDirectory() ? [] : undefined,
        });
      } catch { /* skip */ }
    }
    tree.sort((a, b) => {
      if (a.isDirectory !== b.isDirectory) return a.isDirectory ? -1 : 1;
      return a.name.localeCompare(b.name);
    });
    res.json({ success: true, data: tree });
  } catch (e) { res.status(500).json({ success: false, error: e.message }); }
});

// 2. 读取文件
app.get('/api/files/content', async (req, res) => {
  try {
    const fp = req.query.path;
    const rootIndex = parseInt(req.query.rootIndex || req.query.root || '0', 10);
    if (!fp) return res.status(400).json({ success: false, error: '缺少 path' });
    const real = resolvePath(fp, rootIndex);
    const stat = await fsPromises.stat(real);
    if (!stat.isFile()) return res.status(400).json({ success: false, error: '不是文件' });
    if (stat.size > MAX_FILE_SIZE) return res.status(400).json({ success: false, error: '超过 2MB 限制' });
    const ok = await isTextFile(real, fp);
    if (!ok) return res.status(400).json({ success: false, error: '不支持编辑此文件类型' });
    const content = await fsPromises.readFile(real, 'utf8');
    res.json({ success: true, data: { content, size: stat.size, path: fp } });
  } catch (e) { res.status(500).json({ success: false, error: e.message }); }
});

// 3. 保存
app.post('/api/files/save', async (req, res) => {
  try {
    const { path: fp, content } = req.body;
    const rootIndex = parseInt(req.body.rootIndex || req.body.root || '0', 10);
    if (!fp) return res.status(400).json({ success: false, error: '缺少 path' });
    const real = resolvePath(fp, rootIndex);
    const ex = await fsPromises.stat(real).catch(() => null);
    if (ex && ex.isDirectory()) return res.status(400).json({ success: false, error: '不能覆盖目录' });
    await fsPromises.mkdir(path.dirname(real), { recursive: true });
    await fsPromises.writeFile(real, content, 'utf8');
    res.json({ success: true, message: '保存成功' });
  } catch (e) { res.status(500).json({ success: false, error: e.message }); }
});

// 4. 创建
app.post('/api/files/create', async (req, res) => {
  try {
    const { path: fp, type } = req.body;
    const rootIndex = parseInt(req.body.rootIndex || req.body.root || '0', 10);
    if (!fp) return res.status(400).json({ success: false, error: '缺少 path' });
    const real = resolvePath(fp, rootIndex);
    const ex = await fsPromises.stat(real).catch(() => null);
    if (ex) return res.status(400).json({ success: false, error: '目标已存在' });
    if (type === 'directory') {
      await fsPromises.mkdir(real, { recursive: true });
    } else {
      await fsPromises.mkdir(path.dirname(real), { recursive: true });
      await fsPromises.writeFile(real, '', 'utf8');
    }
    res.json({ success: true, message: '创建成功' });
  } catch (e) { res.status(500).json({ success: false, error: e.message }); }
});

// 5. 删除
app.delete('/api/files/delete', async (req, res) => {
  try {
    const fp = req.query.path || (req.body && req.body.path);
    const rootIndex = parseInt(req.query.rootIndex || req.query.root || req.body?.rootIndex || req.body?.root || '0', 10);
    if (!fp) return res.status(400).json({ success: false, error: '缺少 path' });
    const real = resolvePath(fp, rootIndex);
    const stat = await fsPromises.stat(real);
    if (stat.isDirectory()) await fsPromises.rm(real, { recursive: true });
    else await fsPromises.unlink(real);
    res.json({ success: true, message: '删除成功' });
  } catch (e) { res.status(500).json({ success: false, error: e.message }); }
});

// 6. 复制
app.post('/api/files/copy', async (req, res) => {
  try {
    const { from, to } = req.body;
    const fromRoot = parseInt(req.body.fromRoot || req.body.rootIndex || req.body.root || '0', 10);
    const toRoot = parseInt(req.body.toRoot || req.body.rootIndex || req.body.root || '0', 10);
    if (!from || !to) return res.status(400).json({ success: false, error: '缺少 from 或 to' });
    const fromR = resolvePath(from, fromRoot), toR = resolvePath(to, toRoot);
    const ex = await fsPromises.stat(toR).catch(() => null);
    if (ex) return res.status(400).json({ success: false, error: '目标已存在' });
    const stat = await fsPromises.stat(fromR);
    if (stat.isDirectory()) await fsPromises.cp(fromR, toR, { recursive: true });
    else await fsPromises.copyFile(fromR, toR);
    res.json({ success: true, message: '复制成功' });
  } catch (e) { res.status(500).json({ success: false, error: e.message }); }
});

// 7. 移动
app.post('/api/files/move', async (req, res) => {
  try {
    const { from, to } = req.body;
    const fromRoot = parseInt(req.body.fromRoot || req.body.rootIndex || req.body.root || '0', 10);
    const toRoot = parseInt(req.body.toRoot || req.body.rootIndex || req.body.root || '0', 10);
    if (!from || !to) return res.status(400).json({ success: false, error: '缺少 from 或 to' });
    const fromR = resolvePath(from, fromRoot), toR = resolvePath(to, toRoot);
    const ex = await fsPromises.stat(toR).catch(() => null);
    if (ex) return res.status(400).json({ success: false, error: '目标已存在' });
    await fsPromises.rename(fromR, toR);
    res.json({ success: true, message: '移动成功' });
  } catch (e) { res.status(500).json({ success: false, error: e.message }); }
});

// 8. stat
app.get('/api/files/stat', async (req, res) => {
  try {
    const fp = req.query.path;
    const rootIndex = parseInt(req.query.rootIndex || req.query.root || '0', 10);
    if (!fp) return res.status(400).json({ success: false, error: '缺少 path' });
    const real = resolvePath(fp, rootIndex);
    const stat = await fsPromises.stat(real);
    res.json({ success: true, data: {
      name: path.basename(fp), path: fp,
      isFile: stat.isFile(), isDirectory: stat.isDirectory(),
      size: stat.size,
      mode: (stat.mode & 0o777).toString(8).padStart(3, '0'),
      mtime: new Date(stat.mtimeMs).toLocaleString('zh-CN'),
      uid: stat.uid, gid: stat.gid,
    }});
  } catch (e) { res.status(500).json({ success: false, error: e.message }); }
});

// 9. 修改权限
app.post('/api/files/permissions', async (req, res) => {
  try {
    const { path: fp, mode } = req.body;
    const rootIndex = parseInt(req.body.rootIndex || req.body.root || '0', 10);
    if (!fp) return res.status(400).json({ success: false, error: '缺少 path' });
    if (!mode || !/^[0-7]{3,4}$/.test(mode)) return res.status(400).json({ success: false, error: '权限格式错误' });
    const real = resolvePath(fp, rootIndex);
    await fsPromises.chmod(real, parseInt(mode, 8));
    res.json({ success: true, message: '权限修改成功', data: { mode: mode.padStart(3, '0') } });
  } catch (e) { res.status(500).json({ success: false, error: e.message }); }
});

// 10. 获取常驻目录列表
app.get('/api/roots', (req, res) => {
  res.json({ success: true, data: getRootPathsInfo() });
});

// 11. 添加常驻目录
app.post('/api/roots', async (req, res) => {
  try {
    const { path: newPath } = req.body;
    if (!newPath) return res.status(400).json({ success: false, error: '缺少 path' });

    // 验证路径是否存在且是目录
    const absPath = path.resolve(newPath);
    try {
      const stat = await fsPromises.stat(absPath);
      if (!stat.isDirectory()) {
        return res.status(400).json({ success: false, error: '路径不是目录' });
      }
    } catch {
      return res.status(400).json({ success: false, error: '路径不存在或无法访问' });
    }

    // 检查是否已存在
    if (ROOT_PATHS.some(rp => path.resolve(rp) === absPath)) {
      return res.status(400).json({ success: false, error: '该目录已在常驻列表中' });
    }

    // 添加到配置
    ROOT_PATHS.push(newPath);
    CONFIG.rootPaths = ROOT_PATHS;

    if (saveConfig()) {
      res.json({ success: true, message: '添加成功', data: getRootPathsInfo() });
    } else {
      res.status(500).json({ success: false, error: '保存配置失败' });
    }
  } catch (e) { res.status(500).json({ success: false, error: e.message }); }
});

// 12. 删除常驻目录（仅从配置中移除，不删除实际目录）
app.delete('/api/roots', (req, res) => {
  try {
    const index = parseInt(req.query.index, 10);
    if (isNaN(index) || index < 0 || index >= ROOT_PATHS.length) {
      return res.status(400).json({ success: false, error: '无效的索引' });
    }

    const removed = ROOT_PATHS.splice(index, 1);
    CONFIG.rootPaths = ROOT_PATHS;

    if (saveConfig()) {
      res.json({ success: true, message: '已移除常驻目录: ' + removed[0], data: getRootPathsInfo() });
    } else {
      res.status(500).json({ success: false, error: '保存配置失败' });
    }
  } catch (e) { res.status(500).json({ success: false, error: e.message }); }
});

// ====== 静态文件托管（放在 API 路由之后）======
const FRONTEND_DIST_PATH = path.join(__dirname, '..', 'frontend', 'dist');

// 检查是否存在前端构建目录
let hasFrontendDist = false;
try {
  fs.accessSync(FRONTEND_DIST_PATH);
  hasFrontendDist = true;
  console.log('📦 前端静态文件托管已启用:', FRONTEND_DIST_PATH);
} catch {
  console.log('ℹ️ 前端构建目录不存在，跳过静态文件托管（开发模式）');
}

// 如果有前端构建目录，启用静态文件托管
if (hasFrontendDist) {
  // 静态文件服务
  app.use(express.static(FRONTEND_DIST_PATH));

  // 处理前端路由的 history mode（所有非 API 路由返回 index.html）
  app.get(/^(?!\/api).*/, (req, res) => {
    const indexPath = path.join(FRONTEND_DIST_PATH, 'index.html');
    res.sendFile(indexPath);
  });
}

app.listen(PORT, '0.0.0.0', () => {
  console.log(`📂 Backend: http://0.0.0.0:${PORT}`);
  console.log(`📁 Root Paths: ${ROOT_PATHS.join(', ')}`);
});
