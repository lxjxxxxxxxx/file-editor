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
        <div class="sidebar-header">
          <span>文件树</span>
          <el-button
            type="primary"
            size="small"
            :icon="Plus"
            circle
            class="add-root-btn"
            @click="showAddRootDialog"
            title="添加常驻目录"
          />
        </div>
        <el-scrollbar class="tree-scroll">
          <!-- 空状态提示 -->
          <div v-if="rootPaths.length === 0 && !treeLoading" class="empty-roots">
            <el-empty description="暂无常驻目录">
              <template #default>
                <div class="empty-hint">
                  <p>点击上方 + 按钮添加目录</p>
                </div>
              </template>
            </el-empty>
          </div>
          <!-- tree 始终渲染，使用 data 控制内容 -->
          <el-tree
            :data="treeData"
            :props="treeProps"
            node-key="treeKey"
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
              <span class="tree-node" :class="{ 'is-root': data.isRoot }">
                <el-icon v-if="data.isDirectory"><Folder /></el-icon>
                <el-icon v-else><Document /></el-icon>
                <span class="tree-label">
                  <span v-if="data.alias" class="root-alias" :title="data.name">{{ data.alias }}</span>
                  <span v-else>{{ data.name }}</span>
                </span>
                <!-- 根目录显示操作按钮 -->
                <el-button
                  v-if="data.isRoot"
                  type="primary"
                  size="small"
                  :icon="Edit"
                  circle
                  class="edit-alias-btn"
                  @click.stop="showEditAliasDialog(data)"
                  title="编辑别名"
                />
                <el-button
                  v-if="data.isRoot"
                  type="info"
                  size="small"
                  :icon="InfoFilled"
                  circle
                  class="info-root-btn"
                  @click.stop="showRootInfo(data)"
                  title="查看信息"
                />
                <el-button
                  v-if="data.isRoot"
                  type="danger"
                  size="small"
                  :icon="Close"
                  circle
                  class="remove-root-btn"
                  @click.stop="handleRemoveRoot(data.rootIndex)"
                  title="从文件树移除"
                />
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
            :key="tab.rootIndex + '-' + tab.path"
            :label="tab.name"
            :name="tab.rootIndex + '-' + tab.path"
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
              :path="parseActiveTab(activeTab).path"
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
      <span v-if="activeTab">📄 {{ parseActiveTab(activeTab).path }}</span>
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
    <el-dialog v-model="copyDialogVisible" title="复制" width="550px">
      <el-form>
        <el-form-item label="来源">
          <el-input :model-value="selectedPath" disabled />
        </el-form-item>
        <el-form-item class="vertical-form-item">
          <div class="form-label">目标目录</div>
          <div class="dir-tree-container">
            <el-scrollbar class="tree-scroll">
              <el-tree
                :key="targetTreeKey"
                :data="targetTreeData"
                :props="treeProps"
                node-key="treeKey"
                :load="loadTargetNode"
                lazy
                highlight-current
                :expand-on-click-node="false"
                @node-click="handleTargetSelect"
                class="tree"
                ref="targetTreeRef"
              >
                <template #default="{ data }">
                  <span class="tree-node" :class="{ 'is-root': data.isRoot }">
                    <el-icon v-if="data.isRoot"><Monitor /></el-icon>
                    <el-icon v-else><Folder /></el-icon>
                    <span class="tree-label">{{ data.name }}</span>
                  </span>
                </template>
              </el-tree>
            </el-scrollbar>
          </div>
          <el-text size="small" type="info">{{ copyTargetDisplay }}</el-text>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="copyDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleCopy" :disabled="!hasSelectedTarget">确认复制</el-button>
      </template>
    </el-dialog>

    <!-- 移动对话框 -->
    <el-dialog v-model="moveDialogVisible" title="移动" width="550px">
      <el-form>
        <el-form-item label="来源">
          <el-input :model-value="selectedPath" disabled />
        </el-form-item>
        <el-form-item class="vertical-form-item">
          <div class="form-label">目标目录</div>
          <div class="dir-tree-container">
            <el-scrollbar class="tree-scroll">
              <el-tree
                :key="targetTreeKey"
                :data="targetTreeData"
                :props="treeProps"
                node-key="treeKey"
                :load="loadTargetNode"
                lazy
                highlight-current
                :expand-on-click-node="false"
                @node-click="handleTargetSelect"
                class="tree"
                ref="targetTreeRef"
              >
                <template #default="{ data }">
                  <span class="tree-node" :class="{ 'is-root': data.isRoot }">
                    <el-icon v-if="data.isRoot"><Monitor /></el-icon>
                    <el-icon v-else><Folder /></el-icon>
                    <span class="tree-label">{{ data.name }}</span>
                  </span>
                </template>
              </el-tree>
            </el-scrollbar>
          </div>
          <el-text size="small" type="info">{{ moveTargetDisplay }}</el-text>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="moveDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleMove" :disabled="!hasSelectedTarget">确认移动</el-button>
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

    <!-- 添加常驻目录对话框 -->
    <el-dialog v-model="addRootDialogVisible" title="添加常驻目录" width="500px">
      <el-form>
        <el-form-item>
          <div class="form-label">目录路径</div>
          <el-input
            v-model="newRootPath"
            placeholder="如: /home/user/projects 或 C:\\Users\\user\\Documents"
            @keyup.enter="handleAddRoot"
          />
          <el-text size="small" type="info" style="margin-top: 8px; display: block;">
            请输入服务器上的绝对路径，该目录将被添加到左侧文件树中。
          </el-text>
        </el-form-item>
        <el-form-item style="margin-top: 16px;">
          <div class="form-label">别名（可选）</div>
          <el-input
            v-model="newRootAlias"
            placeholder="如: 工作项目、个人文档（留空则显示目录名）"
            @keyup.enter="handleAddRoot"
          />
          <el-text size="small" type="info" style="margin-top: 8px; display: block;">
            别名会显示在文件树中，方便识别不同目录。
          </el-text>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="addRootDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleAddRoot">添加</el-button>
      </template>
    </el-dialog>

    <!-- 编辑别名对话框 -->
    <el-dialog v-model="editAliasDialogVisible" title="编辑别名" width="450px">
      <el-form>
        <el-form-item label="目录路径">
          <el-input :model-value="editAliasPath" disabled />
        </el-form-item>
        <el-form-item>
          <div class="form-label">别名</div>
          <el-input
            v-model="editAliasValue"
            placeholder="如: 工作项目、个人文档（留空则显示目录名）"
            @keyup.enter="handleEditAlias"
          />
          <el-text size="small" type="info" style="margin-top: 8px; display: block;">
            设置别名后，文件树中将显示别名而不是目录名。
          </el-text>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="editAliasDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleEditAlias">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted, watch, computed } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import MonacoEditor from './MonacoEditor.vue'
import {
  Refresh, DocumentAdd, FolderAdd, Delete, CopyDocument,
  Position, Lock, Folder, Document, Finished, EditPen, Loading,
  Plus, Close, InfoFilled, Monitor, Edit
} from '@element-plus/icons-vue'
import api from './api.js'

const treeData = ref([])
const treeProps = {
  label: 'name',
  children: 'children',
  isLeaf: (data) => data.isFile && !data.isRoot,
}
const treeRef = ref(null)
const selectedPath = ref('')
const selectedNodeData = ref(null)
const selectedRootIndex = ref(0)  // 当前选中的根目录索引

// 常驻目录管理
const rootPaths = ref([])  // 常驻目录列表
const treeLoading = ref(false)  // 树加载状态
const addRootDialogVisible = ref(false)
const newRootPath = ref('')
const newRootAlias = ref('')  // 新常驻目录别名

// 编辑别名
const editAliasDialogVisible = ref(false)
const editAliasIndex = ref(0)
const editAliasPath = ref('')
const editAliasValue = ref('')

const openTabs = ref([])
const activeTab = ref('')
const editorContent = ref('')
const editorKey = ref(0)
const saving = ref(false)
const currentFileStat = ref(null)
const cursorPos = ref(null)
const monacoRef = ref(null)

// 辅助函数：解析 activeTab 获取 rootIndex 和 path
function parseActiveTab(tabKey) {
  if (!tabKey) return { rootIndex: 0, path: '' }
  const match = tabKey.match(/^(\d+)-(.+)$/)
  if (match) {
    return { rootIndex: parseInt(match[1], 10), path: match[2] }
  }
  // 兼容旧格式
  return { rootIndex: 0, path: tabKey }
}

// 辅助函数：创建 activeTab key
function makeTabKey(rootIndex, path) {
  return `${rootIndex}-${path}`
}

function createTabRecord(filePath, fileName, rootIndex) {
  return {
    path: filePath,
    name: fileName,
    rootIndex,
    content: '',
    savedContent: '',
    isModified: false,
    isLoaded: false,
    stat: null,
  }
}

function getTabRecord(tabKey) {
  const { rootIndex, path } = parseActiveTab(tabKey)
  return openTabs.value.find(tab => tab.path === path && tab.rootIndex === rootIndex) || null
}

function syncEditorWithTab(tab) {
  if (!tab) {
    editorContent.value = ''
    currentFileStat.value = null
    return
  }
  editorContent.value = tab.content
  currentFileStat.value = tab.stat
}

const isModified = computed(() => {
  const tab = getTabRecord(activeTab.value)
  return !!tab?.isModified
})

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
const copyTargetRootIndex = ref(0)  // 目标根目录索引
const moveDialogVisible = ref(false)
const moveTarget = ref('')
const moveTargetRootIndex = ref(0)  // 目标根目录索引
const permDialogVisible = ref(false)
const fileStat = ref(null)
const permInput = ref('')

// 复制/移动目标路径显示
const copyTargetDisplay = computed(() => {
  if (!hasSelectedTarget.value) return '请点击选择目标目录'
  const rootName = rootPaths.value.find(r => r.rootIndex === copyTargetRootIndex.value)?.name || '未知'
  const displayPath = copyTarget.value || '(根目录)'
  return `目标: [${rootName}] ${displayPath}/${selectedItemName.value}`
})
const moveTargetDisplay = computed(() => {
  if (!hasSelectedTarget.value) return '请点击选择目标目录'
  const rootName = rootPaths.value.find(r => r.rootIndex === moveTargetRootIndex.value)?.name || '未知'
  const displayPath = moveTarget.value || '(根目录)'
  return `目标: [${rootName}] ${displayPath}/${selectedItemName.value}`
})

// 目标目录树（用于复制/移动）
const targetTreeData = ref([])
const targetTreeRef = ref(null)
const selectedItemName = ref('')  // 当前选中的文件/目录名
const operationType = ref('')     // 'copy' 或 'move'
const targetTreeKey = ref(0)      // 用于强制重新渲染树组件

// 懒加载：加载节点数据
async function loadNode(node, resolve) {
  if (node.level === 0) {
    treeLoading.value = true
    // 第一层：加载所有常驻目录（根目录列表）
    const res = await api.getTree('', 0, true)  // 第三个参数表示获取根列表
    treeLoading.value = false
    if (res.success) {
      // 为每个根节点添加标记
      const roots = res.data.map(item => ({
        ...item,
        isRoot: true,
        treeKey: `root-${item.rootIndex}`,
      }))
      rootPaths.value = roots
      // 如果没有常驻目录，显示空列表
      resolve(roots)
    } else {
      rootPaths.value = []
      resolve([])
      ElMessage.error('加载常驻目录失败: ' + (res.error || '未知错误'))
    }
  } else {
    // 子目录：使用节点的 rootIndex
    const path = node.data.path
    const rootIndex = node.data.rootIndex || 0
    const res = await api.getTree(path, rootIndex)
    if (res.success) {
      // 为子节点添加 rootIndex
      const children = res.data.map(item => ({
        ...item,
        rootIndex: rootIndex,
        treeKey: `${rootIndex}-${item.path}`,
      }))
      resolve(children)
    } else {
      resolve([])
      ElMessage.error('加载失败: ' + (res.error || '未知错误'))
    }
  }
}

// 刷新当前选中的目录
async function refreshTree() {
  if (!treeRef.value) return

  // 多根目录模式下，直接刷新根节点
  treeRef.value.store.root.loaded = false
  treeRef.value.store.root.expand()
}

function handleCurrentChange(data) {
  if (data) {
    selectedPath.value = data.path
    selectedNodeData.value = data
    selectedRootIndex.value = data.rootIndex || 0
  } else {
    selectedPath.value = ''
    selectedNodeData.value = null
    selectedRootIndex.value = 0
  }
}

// 文件加载状态
const fileLoading = ref(false)
const fileLoadingPath = ref('')

async function openFile(filePath, fileName, rootIndex = 0) {
  const tabKey = makeTabKey(rootIndex, filePath)
  // Check if already open
  const existing = getTabRecord(tabKey)
  if (existing) {
    activeTab.value = tabKey
    return
  }
  openTabs.value.push(createTabRecord(filePath, fileName, rootIndex))
  activeTab.value = tabKey
}

async function loadFileContent(tabKey) {
  const tab = getTabRecord(tabKey)
  if (!tab) return

  if (tab.isLoaded) {
    syncEditorWithTab(tab)
    return
  }

  const { rootIndex, path: filePath } = parseActiveTab(tabKey)
  if (!filePath) return

  fileLoading.value = true
  fileLoadingPath.value = filePath
  ElMessage.info({ message: `正在加载: ${filePath.split('/').pop()}`, duration: 1500 })

  const res = await api.getContent(filePath, rootIndex)

  fileLoading.value = false
  fileLoadingPath.value = ''

  if (res.success) {
    tab.content = res.data.content
    tab.savedContent = res.data.content
    tab.isModified = false
    tab.isLoaded = true
    editorKey.value++
    syncEditorWithTab(tab)

    // 显示加载成功提示，包括文件大小
    const sizeStr = formatSize(res.data.size || 0)
    ElMessage.success({ message: `加载成功 (${sizeStr})`, duration: 2000 })

    await api.getStat(filePath, rootIndex).then(r => {
      if (r.success) {
        tab.stat = r.data
        if (activeTab.value === tabKey) {
          currentFileStat.value = r.data
        }
      }
    })
  } else {
    ElMessage.error('读取失败: ' + res.error)
  }
}

function onTabClick(tab) {
  // Already handled by v-model on activeTab
}

async function onTabRemove(tabKey) {
  const { rootIndex, path: filePath } = parseActiveTab(tabKey)
  const tab = getTabRecord(tabKey)
  if (tab) {
    if (isModified.value && activeTab.value === tabKey) {
      try {
        await ElMessageBox.confirm(`${tab.name} 有未保存的更改，确定关闭？`, '警告', { type: 'warning' })
      } catch { return }
    }
    openTabs.value = openTabs.value.filter(t => !(t.path === filePath && t.rootIndex === rootIndex))
    if (activeTab.value === tabKey) {
      // 只切换 activeTab，让 watch 自动加载文件内容
      const lastTab = openTabs.value[openTabs.value.length - 1]
      activeTab.value = lastTab ? makeTabKey(lastTab.rootIndex, lastTab.path) : ''
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
  } else {
    syncEditorWithTab(null)
  }
})

function onCodeChange(newCode) {
  const tab = getTabRecord(activeTab.value)
  if (!tab) return
  tab.content = newCode
  tab.isModified = newCode !== tab.savedContent
  editorContent.value = newCode
}

async function handleSave() {
  if (!activeTab.value) return
  const { rootIndex, path: filePath } = parseActiveTab(activeTab.value)
  const tab = getTabRecord(activeTab.value)
  if (!tab) return
  saving.value = true
  try {
    const res = await api.saveFile(filePath, tab.content, rootIndex)
    if (res.success) {
      ElMessage.success('保存成功')
      tab.savedContent = tab.content
      tab.isModified = false
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
  selectedRootIndex.value = data.rootIndex || 0
  if (data.isFile) {
    openFile(data.path, data.name, data.rootIndex || 0)
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

  const res = await api.createItem(fullPath, newType.value, selectedRootIndex.value)
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

  // 保存父目录路径和根目录索引，用于删除后刷新
  const deletedPath = selectedPath.value
  const rootIdx = selectedRootIndex.value
  const parts = deletedPath.split('/')
  parts.pop()
  const parentPath = parts.join('/')

  const res = await api.deleteItem(deletedPath, rootIdx)
  if (res.success) {
    ElMessage.success('删除成功')
    // Close tab if open
    const deletedTabKey = makeTabKey(rootIdx, deletedPath)
    const idx = openTabs.value.findIndex(t => t.path === deletedPath && t.rootIndex === rootIdx)
    if (idx >= 0) {
      openTabs.value.splice(idx, 1)
      if (activeTab.value === deletedTabKey) {
        const firstTab = openTabs.value[0]
        activeTab.value = firstTab ? makeTabKey(firstTab.rootIndex, firstTab.path) : ''
        if (activeTab.value) loadFileContent(activeTab.value)
        else { currentFileStat.value = null; editorContent.value = '' }
      }
    }

    // 清空选中状态
    selectedPath.value = ''
    selectedNodeData.value = null
    selectedRootIndex.value = 0

    // 刷新父目录（如果父目录存在），否则刷新根目录
    if (treeRef.value) {
      // 对于多根目录，需要使用 treeKey 来获取节点
      const parentTreeKey = parentPath ? `${rootIdx}-${parentPath}` : `root-${rootIdx}`
      const parentNode = treeRef.value.getNode(parentTreeKey)
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
const sourceRootIndex = ref(0)  // 源文件所在的根目录索引
const hasSelectedTarget = ref(false)  // 是否已选择目标目录

function showCopyDialog() {
  operationType.value = 'copy'
  copyTarget.value = ''
  copyTargetRootIndex.value = 0
  hasSelectedTarget.value = false
  sourceRootIndex.value = selectedRootIndex.value
  selectedItemName.value = selectedPath.value ? selectedPath.value.split('/').pop() : ''
  // 直接使用 rootPaths 作为目标树的根数据
  targetTreeData.value = rootPaths.value.map(r => ({
    ...r,
    treeKey: `root-${r.rootIndex}`,
    children: []
  }))
  targetTreeKey.value++  // 强制重新渲染树组件
  copyDialogVisible.value = true
}

async function handleCopy() {
  if (!hasSelectedTarget.value) { ElMessage.warning('请选择目标目录'); return }

  const targetPath = copyTarget.value ? copyTarget.value + '/' + selectedItemName.value : selectedItemName.value
  const fromRoot = sourceRootIndex.value
  const toRoot = copyTargetRootIndex.value

  // 检查目标是否已存在
  const statRes = await api.getStat(targetPath, toRoot)
  if (statRes.success) {
    // 目标已存在，让用户选择
    try {
      await ElMessageBox.confirm(
        `目标位置已存在 "${selectedItemName.value}"，是否自动重命名？`,
        '文件已存在',
        {
          confirmButtonText: '自动重命名',
          cancelButtonText: '取消',
          type: 'warning',
        }
      )
    } catch {
      return
    }

    // 生成唯一路径
    const uniquePath = await generateUniqueTargetPath(copyTarget.value, selectedItemName.value, toRoot)
    if (!uniquePath) {
      ElMessage.error('无法生成唯一文件名')
      return
    }

    const res = await api.copyFile(selectedPath.value, uniquePath, fromRoot, toRoot)
    if (res.success) {
      ElMessage.success('复制成功（已重命名）')
      copyDialogVisible.value = false
      await refreshTree()
    } else {
      ElMessage.error('复制失败: ' + res.error)
    }
  } else {
    const res = await api.copyFile(selectedPath.value, targetPath, fromRoot, toRoot)
    if (res.success) {
      ElMessage.success('复制成功')
      copyDialogVisible.value = false
      await refreshTree()
    } else {
      ElMessage.error('复制失败: ' + res.error)
    }
  }
}

// Move
function showMoveDialog() {
  operationType.value = 'move'
  moveTarget.value = ''
  moveTargetRootIndex.value = 0
  hasSelectedTarget.value = false
  sourceRootIndex.value = selectedRootIndex.value
  selectedItemName.value = selectedPath.value ? selectedPath.value.split('/').pop() : ''
  // 直接使用 rootPaths 作为目标树的根数据
  targetTreeData.value = rootPaths.value.map(r => ({
    ...r,
    treeKey: `root-${r.rootIndex}`,
    children: []
  }))
  targetTreeKey.value++  // 强制重新渲染树组件
  moveDialogVisible.value = true
}

async function handleMove() {
  if (!hasSelectedTarget.value) { ElMessage.warning('请选择目标目录'); return }

  const targetPath = moveTarget.value ? moveTarget.value + '/' + selectedItemName.value : selectedItemName.value
  const fromRoot = sourceRootIndex.value
  const toRoot = moveTargetRootIndex.value

  // 检查目标是否已存在
  const statRes = await api.getStat(targetPath, toRoot)
  if (statRes.success) {
    // 目标已存在，让用户选择
    try {
      await ElMessageBox.confirm(
        `目标位置已存在 "${selectedItemName.value}"，是否自动重命名？`,
        '文件已存在',
        {
          confirmButtonText: '自动重命名',
          cancelButtonText: '取消',
          type: 'warning',
        }
      )
    } catch {
      return
    }

    // 生成唯一路径
    const uniquePath = await generateUniqueTargetPath(moveTarget.value, selectedItemName.value, toRoot)
    if (!uniquePath) {
      ElMessage.error('无法生成唯一文件名')
      return
    }

    const res = await api.moveFile(selectedPath.value, uniquePath, fromRoot, toRoot)
    if (res.success) {
      ElMessage.success('移动成功（已重命名）')
      moveDialogVisible.value = false
      // Update tab path
      const tab = openTabs.value.find(t => t.path === selectedPath.value && t.rootIndex === fromRoot)
      if (tab) {
        const idx = openTabs.value.indexOf(tab)
        openTabs.value[idx] = { path: uniquePath, name: uniquePath.split('/').pop(), rootIndex: toRoot }
        if (activeTab.value === makeTabKey(fromRoot, selectedPath.value)) activeTab.value = makeTabKey(toRoot, uniquePath)
      }
      selectedPath.value = ''
      await refreshTree()
    } else {
      ElMessage.error('移动失败: ' + res.error)
    }
  } else {
    const res = await api.moveFile(selectedPath.value, targetPath, fromRoot, toRoot)
    if (res.success) {
      ElMessage.success('移动成功')
      moveDialogVisible.value = false
      // Update tab path
      const tab = openTabs.value.find(t => t.path === selectedPath.value && t.rootIndex === fromRoot)
      if (tab) {
        const idx = openTabs.value.indexOf(tab)
        openTabs.value[idx] = { path: targetPath, name: targetPath.split('/').pop(), rootIndex: toRoot }
        if (activeTab.value === makeTabKey(fromRoot, selectedPath.value)) activeTab.value = makeTabKey(toRoot, targetPath)
      }
      selectedPath.value = ''
      await refreshTree()
    } else {
      ElMessage.error('移动失败: ' + res.error)
    }
  }
}

// 加载目标目录树节点（只显示目录）
async function loadTargetNode(node, resolve) {
  if (node.level === 0) {
    // 第一层：直接使用已设置的 rootPaths 数据
    resolve(targetTreeData.value)
  } else {
    // 子目录
    const path = node.data.path
    const rootIndex = node.data.rootIndex || 0
    const res = await api.getTree(path, rootIndex)
    if (res.success) {
      // 只返回目录，过滤掉文件
      const dirs = res.data
        .filter(item => item.isDirectory)
        .map(item => ({
          ...item,
          rootIndex,
          treeKey: `${rootIndex}-${item.path}`
        }))
      resolve(dirs)
    } else {
      resolve([])
    }
  }
}

// 选择目标目录
function handleTargetSelect(data) {
  if (data.isDirectory) {
    if (operationType.value === 'copy') {
      copyTarget.value = data.path
      copyTargetRootIndex.value = data.rootIndex || 0
      hasSelectedTarget.value = true
    } else if (operationType.value === 'move') {
      moveTarget.value = data.path
      moveTargetRootIndex.value = data.rootIndex || 0
      hasSelectedTarget.value = true
    }
  }
}

// 生成唯一的目标路径
async function generateUniqueTargetPath(targetDir, itemName, rootIndex = 0) {
  const basePath = targetDir ? targetDir + '/' + itemName : itemName
  const res = await api.getStat(basePath, rootIndex)
  if (!res.success) {
    return basePath
  }
  // 目标已存在，需要加后缀
  const nameParts = itemName.split('.')
  const ext = nameParts.length > 1 ? '.' + nameParts.pop() : ''
  const baseName = nameParts.join('.')

  let counter = 1
  let newPath = ''
  do {
    newPath = targetDir ? targetDir + '/' + baseName + '(' + counter + ')' + ext : baseName + '(' + counter + ')' + ext
    const checkRes = await api.getStat(newPath, rootIndex)
    if (!checkRes.success) {
      return newPath
    }
    counter++
  } while (counter < 1000)

  return null
}

// Permissions
async function showPermDialog() {
  const res = await api.getStat(selectedPath.value, selectedRootIndex.value)
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
  const res = await api.setPerm(selectedPath.value, permInput.value, selectedRootIndex.value)
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

// 显示添加常驻目录对话框
function showAddRootDialog() {
  newRootPath.value = ''
  newRootAlias.value = ''
  addRootDialogVisible.value = true
}

// 处理添加常驻目录
async function handleAddRoot() {
  const path = newRootPath.value.trim()
  const alias = newRootAlias.value.trim()
  if (!path) {
    ElMessage.warning('请输入目录路径')
    return
  }

  const res = await api.addRoot(path, alias)
  if (res.success) {
    ElMessage.success('添加常驻目录成功')
    addRootDialogVisible.value = false
    newRootPath.value = ''
    newRootAlias.value = ''
    // 刷新树
    if (treeRef.value) {
      treeRef.value.store.root.loaded = false
      treeRef.value.store.root.expand()
    }
  } else {
    ElMessage.error('添加失败: ' + res.error)
  }
}

// 处理移除常驻目录
async function handleRemoveRoot(index) {
  try {
    await ElMessageBox.confirm(
      '确定从文件树移除此目录？<br><strong style="color: #f56c6c;">注意：这不会删除实际目录，仅从文件树中移除。</strong>',
      '确认移除',
      {
        confirmButtonText: '移除',
        cancelButtonText: '取消',
        type: 'warning',
        dangerouslyUseHTMLString: true,
      }
    )
  } catch {
    return
  }

  const res = await api.removeRoot(index)
  if (res.success) {
    ElMessage.success('已移除常驻目录')
    // 刷新树
    if (treeRef.value) {
      treeRef.value.store.root.loaded = false
      treeRef.value.store.root.expand()
    }
  } else {
    ElMessage.error('移除失败: ' + res.error)
  }
}

// 显示编辑别名对话框
function showEditAliasDialog(data) {
  editAliasIndex.value = data.rootIndex
  editAliasPath.value = data.absPath || data.path || ''
  editAliasValue.value = data.alias || ''
  editAliasDialogVisible.value = true
}

// 处理编辑别名
async function handleEditAlias() {
  const alias = editAliasValue.value.trim()
  const res = await api.updateRootAlias(editAliasIndex.value, alias)
  if (res.success) {
    ElMessage.success('别名修改成功')
    editAliasDialogVisible.value = false
    // 刷新树
    if (treeRef.value) {
      treeRef.value.store.root.loaded = false
      treeRef.value.store.root.expand()
    }
  } else {
    ElMessage.error('修改失败: ' + res.error)
  }
}

// 显示常驻目录信息
function showRootInfo(data) {
  // 从 rootPaths 中找到对应的绝对路径
  const rootInfo = rootPaths.value.find(r => r.rootIndex === data.rootIndex)
  const absPath = rootInfo?.absPath || data.absPath || '未知路径'

  ElMessageBox.alert(
    `<div style="font-family: monospace; word-break: break-all; background: #1e1e1e; padding: 12px; border-radius: 4px; border: 1px solid #444;">
      ${absPath}
    </div>`,
    `📁 ${data.name}`,
    {
      confirmButtonText: '确定',
      dangerouslyUseHTMLString: true,
      customClass: 'root-info-dialog',
    }
  )
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

/* 常驻目录信息对话框样式 - 全局覆盖 */
.root-info-dialog {
  background: #252526 !important;
  border: 1px solid #444 !important;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.4) !important;
}
.root-info-dialog .el-message-box__header {
  background: #2d2d2d !important;
  border-bottom: 1px solid #444 !important;
  padding: 15px 20px !important;
}
.root-info-dialog .el-message-box__title {
  color: #eee !important;
  font-size: 15px !important;
  font-weight: 600 !important;
}
.root-info-dialog .el-message-box__content {
  color: #ccc !important;
  padding: 20px !important;
  background: #252526 !important;
}
.root-info-dialog .el-message-box__btns {
  padding: 12px 20px !important;
  background: #2d2d2d !important;
  border-top: 1px solid #444 !important;
}
.root-info-dialog .el-message-box__btns .el-button {
  background: #333 !important;
  border-color: #444 !important;
  color: #ccc !important;
}
.root-info-dialog .el-message-box__btns .el-button:hover {
  background: #444 !important;
  border-color: #0969da !important;
  color: #fff !important;
}
.root-info-dialog .el-message-box__btns .el-button--primary {
  background: #007acc !important;
  border-color: #007acc !important;
  color: #fff !important;
}
.root-info-dialog .el-message-box__btns .el-button--primary:hover {
  background: #0062a3 !important;
  border-color: #0062a3 !important;
}
.root-info-dialog .el-message-box__close {
  color: #999 !important;
}
.root-info-dialog .el-message-box__close:hover {
  color: #fff !important;
}
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
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.add-root-btn {
  width: 20px !important;
  height: 20px !important;
  padding: 0 !important;
}

.add-root-btn :deep(.el-icon) {
  font-size: 12px;
}

.tree-scroll {
  flex: 1;
  padding: 4px 0;
}

.empty-roots {
  display: flex;
  align-items: center;
  justify-content: center;
  height: 200px;
  padding: 20px;
}

.empty-roots :deep(.el-empty__description) {
  color: #999;
  font-size: 14px;
}

.empty-hint {
  text-align: center;
  margin-top: 12px;
}

.empty-hint p {
  color: #666;
  font-size: 12px;
  margin: 0;
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
  flex: 1;
  padding-right: 8px;
}

.tree-node.is-root {
  font-weight: 600;
  color: #fff;
}

.remove-root-btn {
  width: 18px !important;
  height: 18px !important;
  padding: 0 !important;
  visibility: hidden;
  margin-left: 4px;
}

.info-root-btn {
  width: 18px !important;
  height: 18px !important;
  padding: 0 !important;
  visibility: hidden;
  margin-left: 4px;
}

.edit-alias-btn {
  width: 18px !important;
  height: 18px !important;
  padding: 0 !important;
  visibility: hidden;
  margin-left: auto;
}

/* 当 tree 节点被悬停时显示按钮 */
:deep(.el-tree-node__content:hover) .remove-root-btn,
:deep(.el-tree-node__content.is-current) .remove-root-btn,
:deep(.el-tree-node__content:hover) .info-root-btn,
:deep(.el-tree-node__content.is-current) .info-root-btn,
:deep(.el-tree-node__content:hover) .edit-alias-btn,
:deep(.el-tree-node__content.is-current) .edit-alias-btn {
  visibility: visible;
}

.info-root-btn :deep(.el-icon) {
  font-size: 10px;
}

.edit-alias-btn :deep(.el-icon) {
  font-size: 10px;
}

.tree-label {
  font-size: 13px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 220px;
}

.root-alias {
  color: #409eff;
  font-weight: 500;
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

/* 表单标签样式 */
.form-label {
  font-size: 14px;
  color: #ccc;
  margin-bottom: 8px;
  font-weight: 500;
}

/* 垂直布局的表单项 */
.vertical-form-item :deep(.el-form-item__content) {
  display: block;
  width: 100%;
}

.vertical-form-item :deep(.el-form-item__label) {
  display: none;
}

/* 目标目录树容器 */
.dir-tree-container {
  height: 200px;
  border: 1px solid #444;
  border-radius: 4px;
  background: #252526;
  overflow: hidden;
}

.dir-tree-container .tree-scroll {
  height: 100%;
}

.dir-tree-container .tree {
  background: transparent !important;
}

.dir-tree-container :deep(.el-tree-node__content) {
  height: 28px;
  background: transparent !important;
}

.dir-tree-container :deep(.el-tree-node__content:hover) {
  background: #37373d !important;
}

.dir-tree-container .tree-node.is-root {
  font-weight: 600;
  color: #fff;
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
