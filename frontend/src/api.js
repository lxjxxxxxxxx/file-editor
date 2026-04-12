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
  getTree(path = '') {
    return http.get('/files/tree', { params: { path } }).then(r => r.data).catch(e => ({ success: false, error: e.response?.data?.error || e.message }))
  },
  getContent(path) {
    return http.get('/files/content', { params: { path } }).then(r => r.data).catch(e => ({ success: false, error: e.response?.data?.error || e.message }))
  },
  saveFile(path, content) {
    return http.post('/files/save', { path, content }).then(r => r.data).catch(e => ({ success: false, error: e.response?.data?.error || e.message }))
  },
  createItem(path, type) {
    return http.post('/files/create', { path, type }).then(r => r.data).catch(e => ({ success: false, error: e.response?.data?.error || e.message }))
  },
  deleteItem(path) {
    return http.delete('/files/delete', { params: { path } }).then(r => r.data).catch(e => ({ success: false, error: e.response?.data?.error || e.message }))
  },
  copyFile(from, to) {
    return http.post('/files/copy', { from, to }).then(r => r.data).catch(e => ({ success: false, error: e.response?.data?.error || e.message }))
  },
  moveFile(from, to) {
    return http.post('/files/move', { from, to }).then(r => r.data).catch(e => ({ success: false, error: e.response?.data?.error || e.message }))
  },
  getStat(path) {
    return http.get('/files/stat', { params: { path } }).then(r => r.data).catch(e => ({ success: false, error: e.response?.data?.error || e.message }))
  },
  setPerm(path, mode) {
    return http.post('/files/permissions', { path, mode }).then(r => r.data).catch(e => ({ success: false, error: e.response?.data?.error || e.message }))
  },
}
