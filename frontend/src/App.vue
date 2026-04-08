<template>
  <div class="app-container">
    <!-- 顶部工具栏 -->
    <el-header height="50px" class="toolbar">
      <div class="toolbar-left">
        <span class="app-title">📂 在线文件编辑器</span>
        <el-button-group size="small" class="action-btns">
          <el-button @click="refreshTree" :icon="Refresh" title="刷新">刷新</el-button>
          <el-button @click="showNewDialog('file')" :icon="DocumentAdd" title="新建文件">新文件</el-button>
          <el-button @click="showNewDialog('directory')" :icon="FolderAdd" title="新建文件夹">新文件夹</el-button>
          <el-button @click="handleDeleteItem" :disabled="!selectedPath" :icon="Delete" title="删除">删除</el-button>
          <el-button @click="showCopyDialog" :disabled="!selectedPath" :icon="CopyDocument" title="复制">复制</el-button>
          <el-button @click="showMoveDialog" :disabled="!selectedPath" :icon="Position" title="移动">移动</el-button>
          <el-button @click="showPermDialog" :disabled="!selectedPath" :icon="Lock" title="权限">权限</el-button>
        </el-button-group>
      </div>
    </el-header>

    <el-container class="main-container">
      <!-- 左侧文件树 -->
      <el-aside width="280px" class="sidebar">
        <div class="sidebar-header">文件树</div>
        <el-scrollbar class="tree-scroll">
          <el-tree
            :data="treeData"
            :props="treeProps"
            node-key="path"
            :load="loadNode"
            lazy
            highlight-current
            :expand-on-click-node="false"
            @node-click="handleNodeClick"
            @current-change="handleCurrentChange"
            class="tree"
            ref="treeRef"
          >
            <template #default="{ data }">
              <span class="tree-node">
                <el-icon v-if="data.isDirectory"><Folder /></el-icon>
                <el-icon v-else><Document /></el-icon>
                <span class="tree-label">{{ data.name }}</span>
              </span>
            </template>
          </el-tree>
        </el-scrollbar>
      </el-aside>

      <!-- 右侧编辑器区域 -->
      <el-main class="editor-area">
        <!-- Tabs -->
        <el-tabs
          v-if="openTabs.length"
          v-model="activeTab"
          type="card"
          closable
          @tab-click="onTabClick"
          @tab-remove="onTabRemove"
          class="editor-tabs"
        >
          <el-tab-pane
            v-for="tab in openTabs"
            :key="tab.path"
            :label="tab.name"
            :name="tab.path"
          />
        </el-tabs>

        <!-- Monaco Editor -->
        <div v-show="activeTab" class="monaco-wrapper">
          <!-- 加载遮罩 -->
          <div v-if="fileLoading" class="loading-overlay">
            <el-icon class="loading-icon" :size="32"><Loading /></el-icon>
            <span class="loading-text">正在加载 {{ fileLoadingPath.split('/').pop() }}...</span>
          </div>
          <div class="editor-content">
            <MonacoEditor
              v-if="activeTab"
              :key="editorKey"
              :path="activeTab"
              :code="editorContent"
              @change="onCodeChange"
              :options="editorOptions"
              height="100%"
              ref="monacoRef"
            />
            <div v-else class="editor-placeholder">
              <el-empty description="点击左侧文件开始编辑" />
            </div>
          </div>
          <div class="editor-bar">
            <el-button
              type="primary"
              size="small"
              :icon="Finished"
              @click="handleSave"
              :loading="saving"
              :disabled="!activeTab || !isModified"
            >保存</el-button>
            <span class="cursor-pos" v-if="cursorPos">行 {{ cursorPos.lineNumber }}，列 {{ cursorPos.column }}</span>
          </div>
        </div>

        <!-- 未打开文件时 -->
        <div v-if="!openTabs.length" class="welcome">
          <el-empty description="点击左侧文件树中的文件进行编辑" :image-size="80">
            <template #image>
              <el-icon :size="80" color="#909399"><EditPen /></el-icon>
            </template>
          </el-empty>
        </div>
      </el-main>
    </el-container>

    <!-- 底部状态栏 -->
    <el-footer height="28px" class="statusbar">
      <span v-if="activeTab">📄 {{ activeTab }}</span>
      <span v-if="currentFileStat"> | {{ formatSize(currentFileStat.size) }} | 权限: {{ currentFileStat.mode }}</span>
      <span class="statusbar-right">🐮 阿牛在线文件编辑器</span>
    </el-footer>

    <!-- 新建文件/文件夹对话框 -->
    <el-dialog v-model="newDialogVisible" :title="newType === 'file' ? '新建文件' : '新建文件夹'" width="450px">
      <el-form>
        <el-form-item label="父目录">
          <el-input :model-value="newParentPath" disabled />
        </el-form-item>
        <el-form-item :label="newType === 'file' ? '文件名' : '文件夹名'">
          <el-input v-model="newPath" :placeholder="newType === 'file' ? '如: newfile.txt' : '如: newfolder'" @keyup.enter="handleCreate" ref="nameInputRef" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="newDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleCreate">确认创建</el-button>
      </template>
    </el-dialog>

    <!-- 复制对话框 -->
    <el-dialog v-model="copyDialogVisible" title="复制" width="450px">
      <el-form>
        <el-form-item label="来源">
          <el-input :model-value="selectedPath" disabled />
        </el-form-item>
        <el-form-item label="目标路径">
          <el-input v-model="copyTarget" placeholder="目标路径" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="copyDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleCopy">确认复制</el-button>
      </template>
    </el-dialog>

    <!-- 移动对话框 -->
    <el-dialog v-model="moveDialogVisible" title="移动" width="450px">
      <el-form>
        <el-form-item label="来源">
          <el-input :model-value="selectedPath" disabled />
        </el-form-item>
        <el-form-item label="目标路径">
          <el-input v-model="moveTarget" placeholder="目标路径" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="moveDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleMove">确认移动</el-button>
      </template>
    </el-dialog>

    <!-- 权限对话框 -->
    <el-dialog v-model="permDialogVisible" title="文件权限" width="450px">
      <div v-if="fileStat">
        <p><strong>文件:</strong> {{ fileStat.name }}</p>
        <p><strong>类型:</strong> {{ fileStat.isDirectory ? '文件夹' : '文件' }}</p>
        <p><strong>大小:</strong> {{ formatSize(fileStat.size) }}</p>
        <p><strong>修改时间:</strong> {{ fileStat.mtime }}</p>
        <p><strong>当前权限:</strong> {{ fileStat.mode }}</p>
        <el-divider />
        <el-form>
          <el-form-item label="新权限 (八进制)">
            <el-input v-model="permInput" placeholder="如 644, 755" maxlength="4" />
            <el-text size="small" type="info" style="margin-top:4px">
              常用: 644(文件), 755(目录/脚本), 600(私密)
            </el-text>
          </el-form-item>
        </el-form>
      </div>
      <template #footer>
        <el-button @click="permDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleSetPerm">确认</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted, watch, nextTick } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import MonacoEditor from './MonacoEditor.vue'
import {
  Refresh, DocumentAdd, FolderAdd, Delete, CopyDocument,
  Position, Lock, Folder, Document, Finished, EditPen, Loading
} from '@element-plus/icons-vue'
import api from './api.js'

const treeData = ref([])
const treeProps = {
  label: 'name',
  children: 'children',
  isLeaf: (data) => data.isFile,
}
const treeRef = ref(null)
const selectedPath = ref('')
const selectedNodeData = ref(null)

const openTabs = ref([])
const activeTab = ref('')
const editorContent = ref('')
const editorKey = ref(0)
const isModified = ref(false)
const saving = ref(false)
const currentFileStat = ref(null)
const cursorPos = ref(null)
const monacoRef = ref(null)

const editorOptions = {
  automaticLayout: true,
  minimap: { enabled: true },
  fontSize: 14,
  tabSize: 2,
  wordWrap: 'on',
  scrollBeyondLastLine: false,
  folding: true,
  lineNumbers: 'on',
  renderWhitespace: 'selection',
}

// Dialogs
const newDialogVisible = ref(false)
const newType = ref('file')
const newPath = ref('')
const newParentPath = ref('')
const copyDialogVisible = ref(false)
const copyTarget = ref('')
const moveDialogVisible = ref(false)
const moveTarget = ref('')
const permDialogVisible = ref(false)
const fileStat = ref(null)
const permInput = ref('')

// 懒加载：加载节点数据
async function loadNode(node, resolve) {
  const path = node.level === 0 ? '' : node.data.path
  const res = await api.getTree(path)
  if (res.success) {
    resolve(res.data)
  } else {
    resolve([])
    ElMessage.error('加载失败: ' + (res.error || '未知错误'))
  }
}

// 刷新当前选中的目录
async function refreshTree() {
  if (!treeRef.value) return
  
  // 确定要刷新的目录路径
  let targetPath = ''
  let targetNode = null
  
  if (selectedPath.value && selectedNodeData.value) {
    if (selectedNodeData.value.isDirectory) {
      // 选中的是目录，刷新该目录
      targetPath = selectedPath.value
      targetNode = treeRef.value.getNode(targetPath)
    } else {
      // 选中的是文件，刷新文件所在的父目录
      const parts = selectedPath.value.split('/')
      parts.pop()
      targetPath = parts.join('/')
      if (targetPath) {
        targetNode = treeRef.value.getNode(targetPath)
      } else {
        // 文件在根目录，刷新根节点
        targetNode = treeRef.value.store.root
      }
    }
  } else {
    // 没有选中项，刷新根目录
    targetNode = treeRef.value.store.root
  }
  
  // 如果目标节点不存在（可能已被删除），尝试刷新上一级
  if (!targetNode && targetPath) {
    const parts = targetPath.split('/')
    parts.pop()
    const parentPath = parts.join('/')
    if (parentPath) {
      targetNode = treeRef.value.getNode(parentPath)
    } else {
      targetNode = treeRef.value.store.root
    }
  }
  
  // 执行刷新
  if (targetNode) {
    targetNode.loaded = false
    targetNode.expand()
  }
}

function handleCurrentChange(data) {
  if (data) {
    selectedPath.value = data.path
    selectedNodeData.value = data
  } else {
    selectedPath.value = ''
    selectedNodeData.value = null
  }
}

// 文件加载状态
const fileLoading = ref(false)
const fileLoadingPath = ref('')

async function openFile(filePath, fileName) {
  // Check if already open
  const existing = openTabs.value.find(t => t.path === filePath)
  if (existing) {
    activeTab.value = filePath
    return
  }
  // Open in tab - watch(activeTab) 会自动调用 loadFileContent
  openTabs.value.push({ path: filePath, name: fileName })
  activeTab.value = filePath
}

async function loadFileContent(filePath) {
  fileLoading.value = true
  fileLoadingPath.value = filePath
  ElMessage.info({ message: `正在加载: ${filePath.split('/').pop()}`, duration: 1500 })
  
  const res = await api.getContent(filePath)
  
  fileLoading.value = false
  fileLoadingPath.value = ''
  
  if (res.success) {
    editorContent.value = res.data.content
    editorKey.value++
    isModified.value = false
    currentFileStat.value = res.data
    
    // 显示加载成功提示，包括文件大小
    const sizeStr = formatSize(res.data.size || 0)
    ElMessage.success({ message: `加载成功 (${sizeStr})`, duration: 2000 })
    
    await api.getStat(filePath).then(r => {
      if (r.success) currentFileStat.value = r.data
    })
  } else {
    ElMessage.error('读取失败: ' + res.error)
  }
}

function onTabClick(tab) {
  // Already handled by v-model on activeTab
}

async function onTabRemove(filePath) {
  const tab = openTabs.value.find(t => t.path === filePath)
  if (tab) {
    if (isModified.value && activeTab.value === filePath) {
      try {
        await ElMessageBox.confirm(`${tab.name} 有未保存的更改，确定关闭？`, '警告', { type: 'warning' })
      } catch { return }
    }
    openTabs.value = openTabs.value.filter(t => t.path !== filePath)
    if (activeTab.value === filePath) {
      // 只切换 activeTab，让 watch 自动加载文件内容
      activeTab.value = openTabs.value.length ? openTabs.value[openTabs.value.length - 1].path : ''
      if (!activeTab.value) { 
        currentFileStat.value = null
        editorContent.value = ''
      }
    }
  }
}

watch(activeTab, async (newVal) => {
  if (newVal) {
    await loadFileContent(newVal)
  }
})

function onCodeChange(newCode) {
  editorContent.value = newCode
  isModified.value = true
}

async function handleSave() {
  if (!activeTab.value) return
  saving.value = true
  try {
    const res = await api.saveFile(activeTab.value, editorContent.value)
    if (res.success) {
      ElMessage.success('保存成功')
      isModified.value = false
    } else {
      ElMessage.error('保存失败: ' + res.error)
    }
  } finally {
    saving.value = false
  }
}

function formatSize(bytes) {
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB'
  return (bytes / (1024 * 1024)).toFixed(1) + ' MB'
}

// 判断选中的是文件还是目录
function handleNodeClick(data) {
  selectedPath.value = data.path
  selectedNodeData.value = data
  if (data.isFile) {
    openFile(data.path, data.name)
  }
}

// New dialog
function showNewDialog(type) {
  newType.value = type
  const sel = selectedPath.value
  
  if (sel && selectedNodeData.value?.isDirectory) {
    // 选了目录 → 在这个目录下创建
    newParentPath.value = sel
    newPath.value = ''  // 只需输入名称
  } else if (sel && selectedNodeData.value?.isFile) {
    // 选了文件 → 在同目录下创建
    const parts = sel.split('/')
    parts.pop()
    const parent = parts.join('/')
    newParentPath.value = parent
    newPath.value = ''
  } else {
    // 没选 → 根目录
    newParentPath.value = '(根目录)'
    newPath.value = ''
  }
  newDialogVisible.value = true
}

async function handleCreate() {
  // 输入的名称必须不为空
  const name = newPath.value.trim()
  if (!name) { ElMessage.warning('请输入文件/文件夹名称'); return }
  
  // 拼接父目录 + 名称
  let fullPath
  if (newParentPath.value && newParentPath.value !== '(根目录)') {
    fullPath = newParentPath.value + '/' + name
  } else {
    fullPath = name
  }
  
  const res = await api.createItem(fullPath, newType.value)
  if (res.success) {
    ElMessage.success('创建成功')
    newDialogVisible.value = false
    await refreshTree()
  } else {
    ElMessage.error('创建失败: ' + res.error)
  }
}

// Delete
async function handleDeleteItem() {
  if (!selectedPath.value) return
  try {
    await ElMessageBox.confirm(`确定删除 ${selectedPath.value}？此操作不可撤销！`, '确认删除', { type: 'warning' })
  } catch { return }
  
  // 保存父目录路径，用于删除后刷新
  const deletedPath = selectedPath.value
  const parts = deletedPath.split('/')
  parts.pop()
  const parentPath = parts.join('/')
  
  const res = await api.deleteItem(deletedPath)
  if (res.success) {
    ElMessage.success('删除成功')
    // Close tab if open
    const idx = openTabs.value.findIndex(t => t.path === deletedPath)
    if (idx >= 0) {
      openTabs.value.splice(idx, 1)
      if (activeTab.value === deletedPath) {
        activeTab.value = openTabs.value.length ? openTabs.value[0].path : ''
        if (activeTab.value) loadFileContent(activeTab.value)
        else { currentFileStat.value = null; editorContent.value = '' }
      }
    }
    
    // 清空选中状态
    selectedPath.value = ''
    selectedNodeData.value = null
    
    // 刷新父目录（如果父目录存在），否则刷新根目录
    if (treeRef.value) {
      const parentNode = parentPath ? treeRef.value.getNode(parentPath) : treeRef.value.store.root
      if (parentNode) {
        parentNode.loaded = false
        parentNode.expand()
      } else {
        // 父目录也不存在，刷新根目录
        treeRef.value.store.root.loaded = false
        treeRef.value.store.root.expand()
      }
    }
  } else {
    ElMessage.error('删除失败: ' + res.error)
  }
}

// Copy
function showCopyDialog() {
  copyTarget.value = selectedPath.value + '_copy'
  copyDialogVisible.value = true
}

async function handleCopy() {
  if (!copyTarget.value) { ElMessage.warning('请输入目标路径'); return }
  const res = await api.copyFile(selectedPath.value, copyTarget.value)
  if (res.success) {
    ElMessage.success('复制成功')
    copyDialogVisible.value = false
    await refreshTree()
  } else {
    ElMessage.error('复制失败: ' + res.error)
  }
}

// Move
function showMoveDialog() {
  moveTarget.value = selectedPath.value
  moveDialogVisible.value = true
}

async function handleMove() {
  if (!moveTarget.value) { ElMessage.warning('请输入目标路径'); return }
  const res = await api.moveFile(selectedPath.value, moveTarget.value)
  if (res.success) {
    ElMessage.success('移动成功')
    moveDialogVisible.value = false
    // Update tab path
    const tab = openTabs.value.find(t => t.path === selectedPath.value)
    if (tab) {
      const idx = openTabs.value.indexOf(tab)
      openTabs.value[idx] = { path: moveTarget.value, name: moveTarget.value.split('/').pop() }
      if (activeTab.value === selectedPath.value) activeTab.value = moveTarget.value
    }
    selectedPath.value = ''
    await refreshTree()
  } else {
    ElMessage.error('移动失败: ' + res.error)
  }
}

// Permissions
async function showPermDialog() {
  const res = await api.getStat(selectedPath.value)
  if (res.success) {
    fileStat.value = res.data
    permInput.value = res.data.mode
    permDialogVisible.value = true
  }
}

async function handleSetPerm() {
  if (!permInput.value || !/^[0-7]{3,4}$/.test(permInput.value)) {
    ElMessage.warning('请输入有效的八进制权限')
    return
  }
  const res = await api.setPerm(selectedPath.value, permInput.value)
  if (res.success) {
    ElMessage.success('权限已修改: ' + permInput.value)
    permDialogVisible.value = false
    if (currentFileStat.value && currentFileStat.value.path === selectedPath.value) {
      currentFileStat.value.mode = permInput.value.padStart(3, '0')
    }
  } else {
    ElMessage.error('修改失败: ' + res.error)
  }
}

// Keyboard shortcut: Ctrl+S
function onKeydown(e) {
  if ((e.ctrlKey || e.metaKey) && e.key === 's') {
    e.preventDefault()
    if (activeTab.value && isModified.value) handleSave()
  }
}

onMounted(() => {
  // 懒加载模式下，el-tree 会自动调用 load 加载根节点
  // 不需要手动调用 refreshTree
  window.addEventListener('keydown', onKeydown)
})
</script>

<style>
/* 全局样式 - 应用到整个页面 */
* { box-sizing: border-box; margin: 0; padding: 0; }
html, body, #app { height: 100%; overflow: hidden; }
</style>

<style scoped>

.app-container {
  display: flex;
  flex-direction: column;
  height: 100vh;
  background: #1e1e1e;
  color: #ccc;
}

.toolbar {
  display: flex;
  align-items: center;
  background: #252526;
  border-bottom: 1px solid #333;
  padding: 0 12px;
  height: 50px !important;
}

.toolbar-left {
  display: flex;
  align-items: center;
  gap: 12px;
}

.app-title {
  font-size: 16px;
  font-weight: 600;
  color: #eee;
  white-space: nowrap;
}

.action-btns .el-button {
  background: #333;
  border-color: #444;
  color: #ccc;
}
.action-btns .el-button:hover {
  background: #444;
  border-color: #0969da;
  color: #fff;
}

.main-container {
  flex: 1;
  overflow: hidden;
}

.sidebar {
  background: #252526;
  border-right: 1px solid #333;
  display: flex;
  flex-direction: column;
}

.sidebar-header {
  padding: 8px 12px;
  font-size: 12px;
  font-weight: 600;
  color: #999;
  text-transform: uppercase;
  letter-spacing: 1px;
  border-bottom: 1px solid #333;
}

.tree-scroll {
  flex: 1;
  padding: 4px 0;
}

.tree {
  background: transparent !important;
}

.tree :deep(.el-tree-node__label) {
  color: #ccc;
}

.tree :deep(.el-tree-node__content) {
  height: 28px;
  background: transparent !important;
}

.tree :deep(.el-tree-node__content:hover) {
  background: #37373d !important;
}

.tree :deep(.el-tree-node.is-current > .el-tree-node__content) {
  background: #37373d !important;
}

.tree-node {
  display: flex;
  align-items: center;
  gap: 4px;
}

.tree-label {
  font-size: 13px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 220px;
}

.editor-area {
  padding: 0;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}

.editor-tabs {
  background: #2d2d2d;
  border-bottom: 1px solid #333;
}

.editor-tabs .el-tabs__header {
  margin: 0;
  padding: 0 4px;
}
.editor-tabs :deep(.el-tabs__item) {
  color: #999;
  font-size: 13px;
  border: none;
  background: #2d2d2d;
}
.editor-tabs :deep(.el-tabs__item.is-active) {
  color: #eee;
  background: #1e1e1e;
}
.editor-tabs :deep(.el-tabs__nav-wrap::after) {
  height: 1px;
}

.monaco-wrapper {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  position: relative;
}

/* 加载遮罩 */
.loading-overlay {
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  background: rgba(30, 30, 30, 0.85);
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  z-index: 100;
  gap: 12px;
}

.loading-icon {
  color: #007acc;
  animation: rotate 1s linear infinite;
}

@keyframes rotate {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}

.loading-text {
  color: #ccc;
  font-size: 14px;
}

.editor-content {
  flex: 1;
  overflow: hidden;
}

.editor-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 4px 12px;
  background: #007acc;
  font-size: 12px;
  color: #fff;
}

.cursor-pos {
  color: #fff;
}

.welcome {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
}

.statusbar {
  background: #007acc;
  color: #fff;
  display: flex;
  align-items: center;
  padding: 0 12px;
  font-size: 12px;
  height: 28px !important;
}

.statusbar-right {
  margin-left: auto;
}

/* Override element-plus dark theme bits */
:deep(.el-dialog) {
  background: #2d2d2d;
  color: #ccc;
}
:deep(.el-form-item__label) {
  color: #ccc;
}
:deep(.el-divider__text) {
  background: #2d2d2d;
  color: #999;
}
</style>
