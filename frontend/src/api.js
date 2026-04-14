import axios from 'axios'

// 从 URL 获取 token
function getTokenFromUrl() {
  const params = new URLSearchParams(window.location.search)
  return params.get('token') || ''
}

const AUTH_TOKEN = getTokenFromUrl()

const http = axios.create({
  baseURL: '/api',
  timeout: 30000,
  headers: {
    'X-Auth-Token': AUTH_TOKEN
  }
})

export default {
  // 文件树 - 支持多根目录
  // listRoots: 是否获取根目录列表（而不是某个根目录的内容）
  getTree(path = '', rootIndex = 0, listRoots = false) {
    return http.get('/files/tree', { params: { path, rootIndex, listRoots } }).then(r => r.data).catch(e => ({ success: false, error: e.response?.data?.error || e.message }))
  },
  getContent(path, rootIndex = 0) {
    return http.get('/files/content', { params: { path, rootIndex } }).then(r => r.data).catch(e => ({ success: false, error: e.response?.data?.error || e.message }))
  },
  saveFile(path, content, rootIndex = 0) {
    return http.post('/files/save', { path, content, rootIndex }).then(r => r.data).catch(e => ({ success: false, error: e.response?.data?.error || e.message }))
  },
  createItem(path, type, rootIndex = 0) {
    return http.post('/files/create', { path, type, rootIndex }).then(r => r.data).catch(e => ({ success: false, error: e.response?.data?.error || e.message }))
  },
  deleteItem(path, rootIndex = 0) {
    return http.delete('/files/delete', { params: { path, rootIndex } }).then(r => r.data).catch(e => ({ success: false, error: e.response?.data?.error || e.message }))
  },
  copyFile(from, to, fromRoot = 0, toRoot = 0) {
    return http.post('/files/copy', { from, to, fromRoot, toRoot }).then(r => r.data).catch(e => ({ success: false, error: e.response?.data?.error || e.message }))
  },
  moveFile(from, to, fromRoot = 0, toRoot = 0) {
    return http.post('/files/move', { from, to, fromRoot, toRoot }).then(r => r.data).catch(e => ({ success: false, error: e.response?.data?.error || e.message }))
  },
  getStat(path, rootIndex = 0) {
    return http.get('/files/stat', { params: { path, rootIndex } }).then(r => r.data).catch(e => ({ success: false, error: e.response?.data?.error || e.message }))
  },
  setPerm(path, mode, rootIndex = 0) {
    return http.post('/files/permissions', { path, mode, rootIndex }).then(r => r.data).catch(e => ({ success: false, error: e.response?.data?.error || e.message }))
  },
  // 常驻目录管理
  getRoots() {
    return http.get('/roots').then(r => r.data).catch(e => ({ success: false, error: e.response?.data?.error || e.message }))
  },
  addRoot(path, alias = '') {
    return http.post('/roots', { path, alias }).then(r => r.data).catch(e => ({ success: false, error: e.response?.data?.error || e.message }))
  },
  updateRootAlias(index, alias) {
    return http.put('/roots', { index, alias }).then(r => r.data).catch(e => ({ success: false, error: e.response?.data?.error || e.message }))
  },
  removeRoot(index) {
    return http.delete('/roots', { params: { index } }).then(r => r.data).catch(e => ({ success: false, error: e.response?.data?.error || e.message }))
  },
}
