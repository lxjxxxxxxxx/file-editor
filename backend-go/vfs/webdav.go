package vfs

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"
)

// WebDAVConfig 存储 WebDAV 连接的配置信息。
type WebDAVConfig struct {
	URL      string // 基础 URL，如 http://example.com/remote.php/dav/files/user
	Username string // 基本认证用户名
	Password string // 基本认证密码
}

// WebDAVFS 通过 WebDAV 协议操作远程文件系统，实现 FileSystem 接口。
type WebDAVFS struct {
	config WebDAVConfig
	base   *url.URL  // 解析后的基础 URL
	root   string    // 远程根路径
	client *http.Client
}

// TestWebDAVConnection 测试 WebDAV 连接是否可用，成功返回 nil。
func TestWebDAVConnection(config WebDAVConfig) error {
	u, err := url.Parse(config.URL)
	if err != nil {
		return fmt.Errorf("URL 格式错误: %w", err)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("PROPFIND", u.String(), nil)
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}
	if config.Username != "" {
		req.SetBasicAuth(config.Username, config.Password)
	}
	req.Header.Set("Depth", "0")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("连接失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMultiStatus && resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("服务器返回错误: %s %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

// NewWebDAVFS 创建 WebDAV 文件系统实例，root 为远程根路径。
func NewWebDAVFS(config WebDAVConfig, root string) (FileSystem, error) {
	u, err := url.Parse(config.URL)
	if err != nil {
		return nil, fmt.Errorf("解析 WebDAV URL 失败: %w", err)
	}
	return &WebDAVFS{
		config: config,
		base:   u,
		root:   cleanPath(root),
		client: &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// cleanPath 清理路径，确保以 "/" 开头且不以 "/" 结尾（根 "/" 除外）。
func cleanPath(p string) string {
	p = path.Clean("/" + p)
	if p == "/" {
		return p
	}
	return strings.TrimSuffix(p, "/")
}

// joinURL 将路径段拼接到基础 URL 后。
func (w *WebDAVFS) joinURL(elem ...string) string {
	elem2 := make([]string, 0, len(elem)+1)
	elem2 = append(elem2, strings.TrimLeft(w.root, "/"))
	for _, e := range elem {
		e = strings.TrimLeft(e, "/")
		if e != "" {
			elem2 = append(elem2, e)
		}
	}
	joined := strings.Join(elem2, "/")
	return w.base.JoinPath(joined).String()
}

// resolve 验证路径是否在根路径范围内。
func (w *WebDAVFS) resolve(absolutePath string) (string, error) {
	cleanPath := path.Clean(absolutePath)
	cleanRoot := path.Clean(w.root)
	if cleanPath == cleanRoot {
		return cleanPath, nil
	}
	rootPrefix := cleanRoot
	if rootPrefix != "/" {
		rootPrefix += "/"
	}
	if !strings.HasPrefix(cleanPath, rootPrefix) {
		return "", fmt.Errorf("路径越权访问被拒绝")
	}
	return cleanPath, nil
}

// newRequest 创建带认证的 HTTP 请求。
func (w *WebDAVFS) newRequest(method, urlStr, body string) (*http.Request, error) {
	var reqBody io.Reader
	if body != "" {
		reqBody = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, urlStr, reqBody)
	if err != nil {
		return nil, err
	}
	if w.config.Username != "" {
		req.SetBasicAuth(w.config.Username, w.config.Password)
	}
	return req, nil
}

// doRequest 执行 HTTP 请求并返回响应体。
func (w *WebDAVFS) doRequest(req *http.Request) (*http.Response, error) {
	resp, err := w.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("WebDAV 请求失败: %w", err)
	}
	return resp, nil
}

// readBody 读取响应体并返回字节数据。
func readBody(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// propfind 发送 PROPFIND 请求并返回 XML 多状态响应。
func (w *WebDAVFS) propfind(urlStr string, depth string) ([]webdavResponse, error) {
	req, err := w.newRequest("PROPFIND", urlStr, `<?xml version="1.0"?>
<d:propfind xmlns:d="DAV:">
  <d:prop>
    <d:displayname/>
    <d:getcontentlength/>
    <d:getcontenttype/>
    <d:resourcetype/>
    <d:getlastmodified/>
  </d:prop>
</d:propfind>`)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Depth", depth)
	req.Header.Set("Content-Type", "application/xml; charset=utf-8")

	resp, err := w.doRequest(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMultiStatus && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("WebDAV PROPFIND 失败: %s %s", resp.Status, strings.TrimSpace(string(body)))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取 WebDAV 响应失败: %w", err)
	}

	stripped := stripDAVNS(body)

	var ms multistatus
	if err := xml.Unmarshal(stripped, &ms); err != nil {
		return nil, fmt.Errorf("解析 WebDAV XML 失败: %w", err)
	}

	return ms.Responses, nil
}

// ---- XML 结构 ----

type multistatus struct {
	XMLName   xml.Name          `xml:"multistatus"`
	Responses []webdavResponse `xml:"response"`
}

type webdavResponse struct {
	Href     string         `xml:"href"`
	Propstat []propstatItem `xml:"propstat"`
}

type propstatItem struct {
	Prop   propItem `xml:"prop"`
	Status string   `xml:"status"`
}

type propItem struct {
	DisplayName   string       `xml:"displayname"`
	ContentLength string       `xml:"getcontentlength"`
	ContentType   string       `xml:"getcontenttype"`
	ResourceType  *resType     `xml:"resourcetype"`
	LastModified  string       `xml:"getlastmodified"`
}

type resType struct {
	Collection string `xml:"collection"`
}

// stripDAVNS 移除 XML 中的 DAV: 命名空间前缀和声明，简化解析。
func stripDAVNS(data []byte) []byte {
	data = bytes.ReplaceAll(data, []byte("<d:"), []byte("<"))
	data = bytes.ReplaceAll(data, []byte("</d:"), []byte("</"))
	data = bytes.ReplaceAll(data, []byte("<D:"), []byte("<"))
	data = bytes.ReplaceAll(data, []byte("</D:"), []byte("</"))
	data = bytes.ReplaceAll(data, []byte(` xmlns:d="DAV:"`), []byte(""))
	data = bytes.ReplaceAll(data, []byte(` xmlns:D="DAV:"`), []byte(""))
	data = bytes.ReplaceAll(data, []byte(` xmlns="DAV:"`), []byte(""))
	return data
}

// ---- 辅助方法 ----

func (r *webdavResponse) isDirectory() bool {
	// 标准 WebDAV 服务器通过 getcontenttype 标识目录
	for _, ps := range r.Propstat {
		if ps.Prop.ContentType == "httpd/unix-directory" {
			return true
		}
	}
	// 容错：目录 href 以 / 结尾
	if strings.HasSuffix(r.Href, "/") {
		return true
	}
	return false
}

func (r *webdavResponse) contentLength() int64 {
	for _, ps := range r.Propstat {
		if ps.Prop.ContentLength != "" {
			n, _ := strconv.ParseInt(ps.Prop.ContentLength, 10, 64)
			return n
		}
	}
	return 0
}

func (r *webdavResponse) modTime() time.Time {
	for _, ps := range r.Propstat {
		if ps.Prop.LastModified != "" {
			t, err := time.Parse(time.RFC1123, ps.Prop.LastModified)
			if err != nil {
				t, err = time.Parse("2006-01-02T15:04:05Z", ps.Prop.LastModified)
				if err != nil {
					return time.Time{}
				}
			}
			return t
		}
	}
	return time.Time{}
}

// ---- FileSystem 接口实现 ----

func (w *WebDAVFS) ReadFile(filePath string) ([]byte, error) {
	real, err := w.resolve(filePath)
	if err != nil {
		return nil, err
	}
	req, err := w.newRequest("GET", w.joinURL(real), "")
	if err != nil {
		return nil, err
	}
	resp, err := w.doRequest(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("WebDAV GET 失败: %s %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return readBody(resp)
}

func (w *WebDAVFS) ReadFileHeader(filePath string, n int) ([]byte, error) {
	real, err := w.resolve(filePath)
	if err != nil {
		return nil, err
	}
	req, err := w.newRequest("GET", w.joinURL(real), "")
	if err != nil {
		return nil, err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=0-%d", n-1))

	resp, err := w.doRequest(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("WebDAV GET 失败: %s %s", resp.Status, strings.TrimSpace(string(body)))
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if len(data) > n {
		data = data[:n]
	}
	return data, nil
}

func (w *WebDAVFS) WriteFile(filePath string, data []byte) error {
	real, err := w.resolve(filePath)
	if err != nil {
		return err
	}
	if err := w.MkdirAll(path.Dir(real), 0o755); err != nil {
		return err
	}
	req, err := w.newRequest("PUT", w.joinURL(real), "")
	if err != nil {
		return err
	}
	req.Body = io.NopCloser(bytes.NewReader(data))
	req.ContentLength = int64(len(data))
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := w.doRequest(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("WebDAV PUT 失败: %s %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

func (w *WebDAVFS) Remove(filePath string) error {
	real, err := w.resolve(filePath)
	if err != nil {
		return err
	}
	req, err := w.newRequest("DELETE", w.joinURL(real), "")
	if err != nil {
		return err
	}
	resp, err := w.doRequest(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("WebDAV DELETE 失败: %s %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

func (w *WebDAVFS) RemoveAll(filePath string) error {
	return w.Remove(filePath)
}

func (w *WebDAVFS) ReadDir(dirPath string) ([]os.DirEntry, error) {
	real, err := w.resolve(dirPath)
	if err != nil {
		return nil, err
	}
	responses, err := w.propfind(w.joinURL(real), "1")
	if err != nil {
		return nil, err
	}

	entries := make([]os.DirEntry, 0, len(responses))

	// 从第一个响应条目（被查询的目录本身）提取基准路径
	var basePath string
	if len(responses) > 0 {
		href := responses[0].Href
		if u, err := url.Parse(href); err == nil && u.Path != "" {
			basePath = strings.TrimSuffix(u.Path, "/") + "/"
		} else if p, err := url.PathUnescape(href); err == nil {
			basePath = strings.TrimSuffix(p, "/") + "/"
		}
	}
	for _, r := range responses {
		hrefRaw, _ := url.PathUnescape(r.Href)
		hrefURL, err := url.Parse(hrefRaw)
		href := hrefRaw
		if err == nil && hrefURL.Path != "" {
			href = hrefURL.Path
		}
		relPath := strings.TrimPrefix(href, basePath)
		relPath = strings.TrimPrefix(relPath, "/")
		relPath = strings.TrimSuffix(relPath, "/")

		if relPath == "" {
			continue
		}
		if strings.Contains(relPath, "/") {
			continue
		}

		entries = append(entries, &webdavDirEntry{
			name:    relPath,
			size:    r.contentLength(),
			isDir:   r.isDirectory(),
			modTime: r.modTime(),
		})
	}
		return entries, nil
}

func (w *WebDAVFS) Stat(filePath string) (os.FileInfo, error) {
	real, err := w.resolve(filePath)
	if err != nil {
		return nil, err
	}
	req, err := w.newRequest("PROPFIND", w.joinURL(real), "")
	if err != nil {
		return nil, err
	}
	req.Header.Set("Depth", "0")

	resp, err := w.doRequest(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, os.ErrNotExist
	}
	if resp.StatusCode != http.StatusMultiStatus {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("WebDAV PROPFIND 失败: %s %s", resp.Status, strings.TrimSpace(string(body)))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取 WebDAV 响应失败: %w", err)
	}
	body = stripDAVNS(body)

	var ms multistatus
	if err := xml.Unmarshal(body, &ms); err != nil {
		return nil, fmt.Errorf("解析 WebDAV XML 失败: %w", err)
	}
	if len(ms.Responses) == 0 {
		return nil, os.ErrNotExist
	}

	r := ms.Responses[0]
	href, _ := url.PathUnescape(r.Href)
	_, name := path.Split(strings.TrimRight(href, "/"))
	if name == "" {
		name = "/"
	}
	return &webdavFileInfo{
		name:    name,
		size:    r.contentLength(),
		isDir:   r.isDirectory(),
		modTime: r.modTime(),
	}, nil
}

func (w *WebDAVFS) Lstat(filePath string) (os.FileInfo, error) {
	return w.Stat(filePath)
}

func (w *WebDAVFS) MkdirAll(dirPath string, _ fs.FileMode) error {
	real, err := w.resolve(dirPath)
	if err != nil {
		return err
	}
	// 递归创建各级目录
	parts := strings.Split(strings.Trim(real, "/"), "/")
	current := w.root
	for _, part := range parts {
		current = path.Join(current, part)
		exists, err := w.exists(current)
		if err != nil {
			return err
		}
		if exists {
			continue
		}
		req, err := w.newRequest("MKCOL", w.joinURL(current), "")
		if err != nil {
			return err
		}
		resp, err := w.doRequest(req)
		if err != nil {
			return err
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusMethodNotAllowed {
			return fmt.Errorf("WebDAV MKCOL 失败: %s", resp.Status)
		}
	}
	return nil
}

func (w *WebDAVFS) CreateFile(filePath string) error {
	return w.WriteFile(filePath, []byte{})
}

func (w *WebDAVFS) Rename(oldPath, newPath string) error {
	realOld, err := w.resolve(oldPath)
	if err != nil {
		return err
	}
	realNew, err := w.resolve(newPath)
	if err != nil {
		return err
	}
	if err := w.MkdirAll(path.Dir(realNew), 0o755); err != nil {
		return err
	}
	req, err := w.newRequest("MOVE", w.joinURL(realOld), "")
	if err != nil {
		return err
	}
	req.Header.Set("Destination", w.joinURL(realNew))
	req.Header.Set("Overwrite", "F")

	resp, err := w.doRequest(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("WebDAV MOVE 失败: %s %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

func (w *WebDAVFS) Chmod(_ string, _ fs.FileMode) error {
	return fmt.Errorf("WebDAV 不支持修改权限")
}

func (w *WebDAVFS) Copy(src, dst string) error {
	realSrc, err := w.resolve(src)
	if err != nil {
		return err
	}
	realDst, err := w.resolve(dst)
	if err != nil {
		return err
	}
	if err := w.MkdirAll(path.Dir(realDst), 0o755); err != nil {
		return err
	}
	req, err := w.newRequest("COPY", w.joinURL(realSrc), "")
	if err != nil {
		return err
	}
	req.Header.Set("Destination", w.joinURL(realDst))
	req.Header.Set("Overwrite", "F")
	req.Header.Set("Depth", "infinity")

	resp, err := w.doRequest(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("WebDAV COPY 失败: %s %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

// exists 检查路径是否已存在。
func (w *WebDAVFS) exists(dirPath string) (bool, error) {
	req, err := w.newRequest("PROPFIND", w.joinURL(dirPath), "")
	if err != nil {
		return false, err
	}
	req.Header.Set("Depth", "0")
	resp, err := w.doRequest(req)
	if err != nil {
		return false, err
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusMultiStatus || resp.StatusCode == http.StatusOK, nil
}

// ---- os.DirEntry 实现 ----

type webdavDirEntry struct {
	name    string
	size    int64
	isDir   bool
	modTime time.Time
}

func (e *webdavDirEntry) Name() string               { return e.name }
func (e *webdavDirEntry) IsDir() bool                 { return e.isDir }
func (e *webdavDirEntry) Type() fs.FileMode           { return 0 }
func (e *webdavDirEntry) Info() (os.FileInfo, error)  { return &webdavFileInfo{name: e.name, size: e.size, isDir: e.isDir, modTime: e.modTime}, nil }

// ---- os.FileInfo 实现 ----

type webdavFileInfo struct {
	name    string
	size    int64
	isDir   bool
	modTime time.Time
}

func (fi *webdavFileInfo) Name() string      { return fi.name }
func (fi *webdavFileInfo) Size() int64        { return fi.size }
func (fi *webdavFileInfo) Mode() fs.FileMode  {
	if fi.isDir {
		return fs.ModeDir | 0o755
	}
	return 0o644
}
func (fi *webdavFileInfo) ModTime() time.Time { return fi.modTime }
func (fi *webdavFileInfo) IsDir() bool        { return fi.isDir }
func (fi *webdavFileInfo) Sys() interface{}   { return nil }
