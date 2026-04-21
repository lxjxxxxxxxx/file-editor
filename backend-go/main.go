package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// maxFileSize 表示允许在线编辑的单个文件大小上限。
	maxFileSize = 2 * 1024 * 1024
	// maxBodySize 表示接口允许接收的 JSON 请求体大小上限。
	maxBodySize = 10 * 1024 * 1024
)

var (
	// configPath 表示当前 Go 后端读取和保存配置文件的位置。
	configPath       = filepath.Join(".", "config.json")
	// frontendDistPath 表示前端构建产物目录，用于生产模式下的静态托管。
	frontendDistPath = filepath.Join("..", "frontend", "dist")
	// textExtensions 用于快速判断带扩展名文件是否按文本文件处理。
	textExtensions   = map[string]struct{}{
		".txt": {}, ".md": {}, ".log": {},
		".js": {}, ".ts": {}, ".jsx": {}, ".tsx": {}, ".vue": {},
		".py": {}, ".rb": {}, ".java": {}, ".kt": {}, ".go": {}, ".rs": {}, ".lua": {}, ".pl": {},
		".c": {}, ".cpp": {}, ".h": {}, ".hpp": {},
		".html": {}, ".htm": {}, ".xml": {}, ".svg": {},
		".css": {}, ".scss": {}, ".sass": {}, ".less": {},
		".json": {}, ".json5": {}, ".toml": {}, ".yaml": {}, ".yml": {},
		".ini": {}, ".cfg": {}, ".conf": {},
		".sh": {}, ".bash": {}, ".zsh": {}, ".bat": {}, ".ps1": {},
		".sql": {},
		".env": {},
		".csv": {}, ".tsv": {},
		".php": {},
	}
	// specialTextNames 用于识别没有常规扩展名但通常应按文本处理的文件名。
	specialTextNames = map[string]struct{}{
		"dockerfile":  {},
		"makefile":    {},
		"vagrantfile": {},
	}
	// binarySignatures 用于通过文件头特征快速排除常见二进制文件。
	binarySignatures = [][]byte{
		{0x89, 0x50, 0x4E, 0x47},
		{0xFF, 0xD8, 0xFF},
		{0x47, 0x49, 0x46},
		{0x50, 0x4B},
		{0x25, 0x50, 0x44, 0x46},
	}
	// defaultExcludedNames 表示未配置排除列表时的默认忽略项。
	defaultExcludedNames = []string{}
)

// rootPathEntry 表示单个根目录配置项。
type rootPathEntry struct {
	// Path 表示根目录路径。
	Path  string `json:"path"`
	// Alias 表示根目录在前端展示时使用的别名。
	Alias string `json:"alias"`
}

// config 表示配置文件在内存中的结构。
type config struct {
	// Token 表示 API 鉴权令牌。
	Token         string          `json:"token"`
	// Port 表示服务监听端口。
	Port          int             `json:"port"`
	// RootPath 表示旧版单目录配置字段，仅用于兼容读取。
	RootPath      string          `json:"rootPath,omitempty"`
	// RootPathsRaw 表示原始的 rootPaths JSON 内容，用于兼容多种格式。
	RootPathsRaw  json.RawMessage `json:"rootPaths"`
	// ExcludedNames 表示需要在文件树中过滤掉的名称列表。
	ExcludedNames []string        `json:"excludedNames"`
	// ExcludeHidden 表示是否过滤隐藏文件和隐藏目录。
	ExcludeHidden *bool           `json:"excludeHidden"`
	// RootPaths 表示标准化后的根目录配置。
	RootPaths     []rootPathEntry `json:"-"`
}

// rootInfo 表示返回给前端的根目录元数据。
type rootInfo struct {
	// Index 表示根目录在当前配置数组中的索引。
	Index   int    `json:"index"`
	// Path 表示配置中保存的原始路径。
	Path    string `json:"path"`
	// Name 表示前端展示用名称，优先使用别名。
	Name    string `json:"name"`
	// AbsPath 表示根目录的绝对路径。
	AbsPath string `json:"absPath"`
	// Alias 表示根目录别名。
	Alias   string `json:"alias"`
}

// fileNode 表示文件树中的单个节点。
type fileNode struct {
	// Name 表示当前节点名称。
	Name        string      `json:"name"`
	// Path 表示相对于根目录的路径。
	Path        string      `json:"path"`
	// RootIndex 表示当前节点所属的根目录索引。
	RootIndex   int         `json:"rootIndex"`
	// AbsPath 表示根节点时返回的绝对路径。
	AbsPath     string      `json:"absPath,omitempty"`
	// Alias 表示根节点的别名。
	Alias       string      `json:"alias,omitempty"`
	// IsDirectory 表示当前节点是否为目录。
	IsDirectory bool        `json:"isDirectory"`
	// IsFile 表示当前节点是否为普通文件。
	IsFile      bool        `json:"isFile"`
	// Size 表示文件或目录的大小。
	Size        int64       `json:"size"`
	// Mtime 表示最后修改时间的毫秒时间戳。
	Mtime       int64       `json:"mtime"`
	// Mode 表示权限位的八进制字符串。
	Mode        string      `json:"mode"`
	// Children 表示目录节点的子节点，占位或实际内容。
	Children    interface{} `json:"children,omitempty"`
}

// apiResponse 表示统一的接口响应结构。
type apiResponse struct {
	// Success 表示接口调用是否成功。
	Success bool        `json:"success"`
	// Error 表示失败时的错误信息。
	Error   string      `json:"error,omitempty"`
	// Message 表示成功时的提示信息。
	Message string      `json:"message,omitempty"`
	// Data 表示具体的业务数据。
	Data    interface{} `json:"data,omitempty"`
}

// fileContentData 表示读取文件内容接口的返回体。
type fileContentData struct {
	// Content 表示文件文本内容。
	Content string `json:"content"`
	// Size 表示文件大小。
	Size    int64  `json:"size"`
	// Path 表示文件相对路径。
	Path    string `json:"path"`
}

// fileStatData 表示文件状态信息。
type fileStatData struct {
	// Name 表示文件名或目录名。
	Name        string `json:"name"`
	// Path 表示文件相对路径。
	Path        string `json:"path"`
	// IsFile 表示是否为普通文件。
	IsFile      bool   `json:"isFile"`
	// IsDirectory 表示是否为目录。
	IsDirectory bool   `json:"isDirectory"`
	// Size 表示文件大小。
	Size        int64  `json:"size"`
	// Mode 表示权限位八进制字符串。
	Mode        string `json:"mode"`
	// Mtime 表示格式化后的修改时间。
	Mtime       string `json:"mtime"`
	// UID 表示文件所有者 ID，目前跨平台统一返回 0。
	UID         int    `json:"uid"`
	// GID 表示文件所属组 ID，目前跨平台统一返回 0。
	GID         int    `json:"gid"`
}

// app 表示整个后端服务的运行时状态。
type app struct {
	// mu 用于保护配置和运行时派生数据的并发读写。
	mu sync.RWMutex

	// config 表示当前生效的原始配置。
	config        config
	// rootPaths 表示运行期使用的根目录路径列表。
	rootPaths     []string
	// excludedNames 表示运行期使用的排除名称集合。
	excludedNames map[string]struct{}
	// excludeHidden 表示运行期是否排除隐藏文件。
	excludeHidden bool
	// staticEnabled 表示是否启用前端静态文件托管。
	staticEnabled bool
}

// createRequest 表示创建文件或目录接口的请求体。
type createRequest struct {
	// Path 表示要创建的目标相对路径。
	Path      string `json:"path"`
	// Type 表示创建类型，directory 表示目录，其余按文件处理。
	Type      string `json:"type"`
	// RootIndex 表示目标所属根目录索引。
	RootIndex *int   `json:"rootIndex"`
	// Root 表示兼容旧前端时使用的根目录索引字段。
	Root      *int   `json:"root"`
}

// saveRequest 表示保存文件接口的请求体。
type saveRequest struct {
	// Path 表示要保存的文件相对路径。
	Path      string `json:"path"`
	// Content 表示待写入的文件内容。
	Content   string `json:"content"`
	// RootIndex 表示目标所属根目录索引。
	RootIndex *int   `json:"rootIndex"`
	// Root 表示兼容旧前端时使用的根目录索引字段。
	Root      *int   `json:"root"`
}

// copyMoveRequest 表示复制或移动接口的请求体。
type copyMoveRequest struct {
	// From 表示源路径。
	From      string `json:"from"`
	// To 表示目标路径。
	To        string `json:"to"`
	// FromRoot 表示源路径所属根目录索引。
	FromRoot  *int   `json:"fromRoot"`
	// ToRoot 表示目标路径所属根目录索引。
	ToRoot    *int   `json:"toRoot"`
	// RootIndex 表示兼容单根目录时的索引字段。
	RootIndex *int   `json:"rootIndex"`
	// Root 表示兼容旧前端时使用的索引字段。
	Root      *int   `json:"root"`
}

// permissionsRequest 表示修改权限接口的请求体。
type permissionsRequest struct {
	// Path 表示目标文件相对路径。
	Path      string `json:"path"`
	// Mode 表示待设置的八进制权限字符串。
	Mode      string `json:"mode"`
	// RootIndex 表示目标所属根目录索引。
	RootIndex *int   `json:"rootIndex"`
	// Root 表示兼容旧前端时使用的根目录索引字段。
	Root      *int   `json:"root"`
}

// addRootRequest 表示新增常驻目录接口的请求体。
type addRootRequest struct {
	// Path 表示要加入常驻列表的目录路径。
	Path  string `json:"path"`
	// Alias 表示该目录的展示别名。
	Alias string `json:"alias"`
}

// updateRootRequest 表示更新常驻目录别名接口的请求体。
type updateRootRequest struct {
	// Index 表示待修改的根目录索引。
	Index int    `json:"index"`
	// Alias 表示新的展示别名。
	Alias string `json:"alias"`
}

// main 是程序入口，负责加载配置、注册路由并启动 HTTP 服务。
func main() {
	a := &app{}
	if err := a.loadConfig(); err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	if _, err := os.Stat(frontendDistPath); err == nil {
		a.staticEnabled = true
		log.Printf("前端静态文件托管已启用: %s", frontendDistPath)
	} else {
		log.Printf("前端构建目录不存在，跳过静态文件托管: %s", frontendDistPath)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/files/tree", a.withAPIAuth(a.handleFilesTree))
	mux.HandleFunc("/api/files/content", a.withAPIAuth(a.handleFilesContent))
	mux.HandleFunc("/api/files/save", a.withAPIAuth(a.handleFilesSave))
	mux.HandleFunc("/api/files/create", a.withAPIAuth(a.handleFilesCreate))
	mux.HandleFunc("/api/files/delete", a.withAPIAuth(a.handleFilesDelete))
	mux.HandleFunc("/api/files/copy", a.withAPIAuth(a.handleFilesCopy))
	mux.HandleFunc("/api/files/move", a.withAPIAuth(a.handleFilesMove))
	mux.HandleFunc("/api/files/stat", a.withAPIAuth(a.handleFilesStat))
	mux.HandleFunc("/api/files/permissions", a.withAPIAuth(a.handleFilesPermissions))
	mux.HandleFunc("/api/roots", a.withAPIAuth(a.handleRoots))
	mux.HandleFunc("/", a.handleRoot)

	port := a.currentPort()
	addr := fmt.Sprintf("0.0.0.0:%d", port)
	log.Printf("Go backend listening on http://%s", addr)
	log.Printf("Root paths: %s", strings.Join(a.currentRootPaths(), ", "))

	server := &http.Server{
		Addr:              addr,
		Handler:           a.withCORS(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Fatal(server.ListenAndServe())
}

// withCORS 为所有请求追加跨域响应头，并处理预检请求。
func (a *app) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Auth-Token")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Expose-Headers", "X-Auth-Token")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// withAPIAuth 为 API 路由追加 token 鉴权逻辑。
func (a *app) withAPIAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		if token == "" {
			token = r.Header.Get("X-Auth-Token")
		}
		if token == "" {
			writeJSON(w, http.StatusUnauthorized, apiResponse{
				Success: false,
				Error:   "缺少访问令牌 (token)，请在 URL 中添加 ?token=your-token",
			})
			return
		}
		a.mu.RLock()
		expected := a.config.Token
		a.mu.RUnlock()
		if token != expected {
			writeJSON(w, http.StatusForbidden, apiResponse{
				Success: false,
				Error:   "访问令牌无效，请检查 token 是否正确",
			})
			return
		}
		next(w, r)
	}
}

// handleRoot 处理非 API 请求，优先返回静态资源，否则回退到前端入口文件。
func (a *app) handleRoot(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api") {
		writeJSON(w, http.StatusNotFound, apiResponse{Success: false, Error: "接口不存在"})
		return
	}
	if !a.staticEnabled {
		writeJSON(w, http.StatusNotFound, apiResponse{Success: false, Error: "前端构建产物不存在"})
		return
	}

	relativePath := strings.TrimPrefix(filepath.Clean("/"+r.URL.Path), string(os.PathSeparator))
	requestPath := filepath.Join(frontendDistPath, relativePath)
	distRoot, _ := filepath.Abs(frontendDistPath)
	target, _ := filepath.Abs(requestPath)
	if !strings.HasPrefix(target, distRoot+string(os.PathSeparator)) && target != distRoot {
		http.NotFound(w, r)
		return
	}

	if info, err := os.Stat(target); err == nil && !info.IsDir() {
		http.ServeFile(w, r, target)
		return
	}
	http.ServeFile(w, r, filepath.Join(frontendDistPath, "index.html"))
}

// handleFilesTree 返回根目录列表或指定目录下的一层文件树节点。
func (a *app) handleFilesTree(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "方法不允许"})
		return
	}

	subDir := r.URL.Query().Get("path")
	rootIndex := parseIntWithDefault(r.URL.Query().Get("rootIndex"), -1)
	if rootIndex == -1 {
		rootIndex = parseIntWithDefault(r.URL.Query().Get("root"), 0)
	}
	listRoots := r.URL.Query().Get("listRoots") == "true"

	if subDir == "" && listRoots {
		rootInfos := a.getRootInfos()
		tree := make([]fileNode, 0, len(rootInfos))
		for _, info := range rootInfos {
			stat, err := os.Lstat(info.AbsPath)
			if err != nil {
				continue
			}
			node := fileNode{
				Name:        info.Name,
				Path:        "",
				RootIndex:   info.Index,
				AbsPath:     info.AbsPath,
				Alias:       info.Alias,
				IsDirectory: stat.IsDir(),
				IsFile:      !stat.IsDir(),
				Size:        stat.Size(),
				Mtime:       stat.ModTime().UnixMilli(),
				Mode:        formatMode(stat.Mode()),
			}
			if stat.IsDir() {
				node.Children = []fileNode{}
			}
			tree = append(tree, node)
		}

		writeRawJSON(w, http.StatusOK, map[string]interface{}{
			"success":     true,
			"data":        tree,
			"isMultiRoot": true,
		})
		return
	}

	real, err := a.resolvePath(subDir, rootIndex)
	if err != nil {
		writeAppError(w, err)
		return
	}

	entries, err := os.ReadDir(real)
	if err != nil {
		writeAppError(w, wrapFSError(err))
		return
	}

	tree := make([]fileNode, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if a.shouldExclude(name) {
			continue
		}

		full := filepath.Join(real, name)
		stat, err := os.Lstat(full)
		if err != nil {
			continue
		}

		rel := name
		if subDir != "" {
			rel = pathJoin(subDir, name)
		}

		node := fileNode{
			Name:        name,
			Path:        rel,
			RootIndex:   rootIndex,
			IsDirectory: stat.IsDir(),
			IsFile:      !stat.IsDir(),
			Size:        stat.Size(),
			Mtime:       stat.ModTime().UnixMilli(),
			Mode:        formatMode(stat.Mode()),
		}
		if stat.IsDir() {
			node.Children = []fileNode{}
		}
		tree = append(tree, node)
	}

	sortNodes(tree)
	writeJSON(w, http.StatusOK, apiResponse{Success: true, Data: tree})
}

// handleFilesContent 读取文本文件内容并返回给前端编辑器。
func (a *app) handleFilesContent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "方法不允许"})
		return
	}

	filePath := r.URL.Query().Get("path")
	rootIndex := parseRootIndexFromQuery(r)
	if filePath == "" {
		writeJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "缺少 path"})
		return
	}

	real, err := a.resolvePath(filePath, rootIndex)
	if err != nil {
		writeAppError(w, err)
		return
	}

	stat, err := os.Stat(real)
	if err != nil {
		writeAppError(w, wrapFSError(err))
		return
	}
	if stat.IsDir() {
		writeJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "不是文件"})
		return
	}
	if stat.Size() > maxFileSize {
		writeJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "超过 2MB 限制"})
		return
	}

	ok, err := isTextFile(real, filePath)
	if err != nil {
		writeAppError(w, err)
		return
	}
	if !ok {
		writeJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "不支持编辑此文件类型"})
		return
	}

	content, err := os.ReadFile(real)
	if err != nil {
		writeAppError(w, wrapFSError(err))
		return
	}

	writeJSON(w, http.StatusOK, apiResponse{
		Success: true,
		Data: fileContentData{
			Content: string(content),
			Size:    stat.Size(),
			Path:    filePath,
		},
	})
}

// handleFilesSave 将前端编辑后的文本内容写回文件系统。
func (a *app) handleFilesSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "方法不允许"})
		return
	}

	var req saveRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: err.Error()})
		return
	}
	if req.Path == "" {
		writeJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "缺少 path"})
		return
	}

	rootIndex := pickInt(req.RootIndex, req.Root)
	real, err := a.resolvePath(req.Path, rootIndex)
	if err != nil {
		writeAppError(w, err)
		return
	}

	if stat, err := os.Stat(real); err == nil && stat.IsDir() {
		writeJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "不能覆盖目录"})
		return
	}

	if err := os.MkdirAll(filepath.Dir(real), 0o755); err != nil {
		writeAppError(w, wrapFSError(err))
		return
	}
	if err := os.WriteFile(real, []byte(req.Content), 0o644); err != nil {
		writeAppError(w, wrapFSError(err))
		return
	}

	writeJSON(w, http.StatusOK, apiResponse{Success: true, Message: "保存成功"})
}

// handleFilesCreate 根据请求创建空文件或目录。
func (a *app) handleFilesCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "方法不允许"})
		return
	}

	var req createRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: err.Error()})
		return
	}
	if req.Path == "" {
		writeJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "缺少 path"})
		return
	}

	rootIndex := pickInt(req.RootIndex, req.Root)
	real, err := a.resolvePath(req.Path, rootIndex)
	if err != nil {
		writeAppError(w, err)
		return
	}

	if _, err := os.Stat(real); err == nil {
		writeJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "目标已存在"})
		return
	} else if !errors.Is(err, os.ErrNotExist) {
		writeAppError(w, wrapFSError(err))
		return
	}

	if req.Type == "directory" {
		if err := os.MkdirAll(real, 0o755); err != nil {
			writeAppError(w, wrapFSError(err))
			return
		}
	} else {
		if err := os.MkdirAll(filepath.Dir(real), 0o755); err != nil {
			writeAppError(w, wrapFSError(err))
			return
		}
		f, err := os.OpenFile(real, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			writeAppError(w, wrapFSError(err))
			return
		}
		_ = f.Close()
	}

	writeJSON(w, http.StatusOK, apiResponse{Success: true, Message: "创建成功"})
}

// handleFilesDelete 删除指定文件或目录。
func (a *app) handleFilesDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "方法不允许"})
		return
	}

	filePath := r.URL.Query().Get("path")
	rootIndex := parseRootIndexFromQuery(r)
	if filePath == "" {
		writeJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "缺少 path"})
		return
	}

	real, err := a.resolvePath(filePath, rootIndex)
	if err != nil {
		writeAppError(w, err)
		return
	}

	stat, err := os.Stat(real)
	if err != nil {
		writeAppError(w, wrapFSError(err))
		return
	}

	if stat.IsDir() {
		if err := os.RemoveAll(real); err != nil {
			writeAppError(w, wrapFSError(err))
			return
		}
	} else {
		if err := os.Remove(real); err != nil {
			writeAppError(w, wrapFSError(err))
			return
		}
	}

	writeJSON(w, http.StatusOK, apiResponse{Success: true, Message: "删除成功"})
}

// handleFilesCopy 复制文件或目录到目标位置。
func (a *app) handleFilesCopy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "方法不允许"})
		return
	}

	var req copyMoveRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: err.Error()})
		return
	}
	if req.From == "" || req.To == "" {
		writeJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "缺少 from 或 to"})
		return
	}

	fromRoot, toRoot := parseCopyMoveRoots(req)
	fromReal, err := a.resolvePath(req.From, fromRoot)
	if err != nil {
		writeAppError(w, err)
		return
	}
	toReal, err := a.resolvePath(req.To, toRoot)
	if err != nil {
		writeAppError(w, err)
		return
	}

	if _, err := os.Stat(toReal); err == nil {
		writeJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "目标已存在"})
		return
	} else if !errors.Is(err, os.ErrNotExist) {
		writeAppError(w, wrapFSError(err))
		return
	}

	if err := copyPath(fromReal, toReal); err != nil {
		writeAppError(w, wrapFSError(err))
		return
	}

	writeJSON(w, http.StatusOK, apiResponse{Success: true, Message: "复制成功"})
}

// handleFilesMove 移动文件或目录到目标位置，并兼容跨设备移动。
func (a *app) handleFilesMove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "方法不允许"})
		return
	}

	var req copyMoveRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: err.Error()})
		return
	}
	if req.From == "" || req.To == "" {
		writeJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "缺少 from 或 to"})
		return
	}

	fromRoot, toRoot := parseCopyMoveRoots(req)
	fromReal, err := a.resolvePath(req.From, fromRoot)
	if err != nil {
		writeAppError(w, err)
		return
	}
	toReal, err := a.resolvePath(req.To, toRoot)
	if err != nil {
		writeAppError(w, err)
		return
	}

	if _, err := os.Stat(toReal); err == nil {
		writeJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "目标已存在"})
		return
	} else if !errors.Is(err, os.ErrNotExist) {
		writeAppError(w, wrapFSError(err))
		return
	}

	if err := movePath(fromReal, toReal); err != nil {
		writeAppError(w, wrapFSError(err))
		return
	}

	writeJSON(w, http.StatusOK, apiResponse{Success: true, Message: "移动成功"})
}

// handleFilesStat 返回文件或目录的基础元数据。
func (a *app) handleFilesStat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "方法不允许"})
		return
	}

	filePath := r.URL.Query().Get("path")
	rootIndex := parseRootIndexFromQuery(r)
	if filePath == "" {
		writeJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "缺少 path"})
		return
	}

	real, err := a.resolvePath(filePath, rootIndex)
	if err != nil {
		writeAppError(w, err)
		return
	}

	stat, err := os.Stat(real)
	if err != nil {
		writeAppError(w, wrapFSError(err))
		return
	}

	writeJSON(w, http.StatusOK, apiResponse{
		Success: true,
		Data: fileStatData{
			Name:        filepath.Base(filePath),
			Path:        filePath,
			IsFile:      !stat.IsDir(),
			IsDirectory: stat.IsDir(),
			Size:        stat.Size(),
			Mode:        formatMode(stat.Mode()),
			Mtime:       stat.ModTime().Format("2006-01-02 15:04:05"),
			UID:         0,
			GID:         0,
		},
	})
}

// handleFilesPermissions 修改文件或目录的权限位。
func (a *app) handleFilesPermissions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "方法不允许"})
		return
	}

	var req permissionsRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: err.Error()})
		return
	}
	if req.Path == "" {
		writeJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "缺少 path"})
		return
	}
	if !isValidMode(req.Mode) {
		writeJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "权限格式错误"})
		return
	}

	rootIndex := pickInt(req.RootIndex, req.Root)
	real, err := a.resolvePath(req.Path, rootIndex)
	if err != nil {
		writeAppError(w, err)
		return
	}

	mode, _ := strconv.ParseUint(req.Mode, 8, 32)
	if err := os.Chmod(real, fs.FileMode(mode)); err != nil {
		writeAppError(w, wrapFSError(err))
		return
	}

	writeJSON(w, http.StatusOK, apiResponse{
		Success: true,
		Message: "权限修改成功",
		Data: map[string]string{
			"mode": padMode(req.Mode),
		},
	})
}

// handleRoots 根据 HTTP 方法分发常驻目录相关操作。
func (a *app) handleRoots(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, apiResponse{Success: true, Data: a.getRootInfos()})
	case http.MethodPost:
		a.handleRootsAdd(w, r)
	case http.MethodDelete:
		a.handleRootsDelete(w, r)
	case http.MethodPut:
		a.handleRootsUpdate(w, r)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "方法不允许"})
	}
}

// handleRootsAdd 将新目录加入常驻目录配置。
func (a *app) handleRootsAdd(w http.ResponseWriter, r *http.Request) {
	var req addRootRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: err.Error()})
		return
	}
	if req.Path == "" {
		writeJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "缺少 path"})
		return
	}

	absPath, err := filepath.Abs(req.Path)
	if err != nil {
		writeAppError(w, errInvalidPath)
		return
	}

	stat, err := os.Stat(absPath)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "路径不存在或无法访问"})
		return
	}
	if !stat.IsDir() {
		writeJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "路径不是目录"})
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	for _, item := range a.config.RootPaths {
		existingAbs, _ := filepath.Abs(item.Path)
		if samePath(existingAbs, absPath) {
			writeJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "该目录已在常驻列表中"})
			return
		}
	}

	a.config.RootPaths = append(a.config.RootPaths, rootPathEntry{
		Path:  req.Path,
		Alias: strings.TrimSpace(req.Alias),
	})
	a.rebuildRuntimeConfigLocked()

	if err := a.saveConfigLocked(); err != nil {
		writeJSON(w, http.StatusInternalServerError, apiResponse{Success: false, Error: "保存配置失败"})
		return
	}

	writeJSON(w, http.StatusOK, apiResponse{Success: true, Message: "添加成功", Data: a.getRootInfosLocked()})
}

// handleRootsDelete 从配置中移除指定常驻目录，但不会删除真实目录。
func (a *app) handleRootsDelete(w http.ResponseWriter, r *http.Request) {
	index := parseIntWithDefault(r.URL.Query().Get("index"), -1)

	a.mu.Lock()
	defer a.mu.Unlock()

	if index < 0 || index >= len(a.config.RootPaths) {
		writeJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "无效的索引"})
		return
	}

	removed := a.config.RootPaths[index]
	a.config.RootPaths = append(a.config.RootPaths[:index], a.config.RootPaths[index+1:]...)
	a.rebuildRuntimeConfigLocked()

	if err := a.saveConfigLocked(); err != nil {
		writeJSON(w, http.StatusInternalServerError, apiResponse{Success: false, Error: "保存配置失败"})
		return
	}

	writeJSON(w, http.StatusOK, apiResponse{
		Success: true,
		Message: "已移除常驻目录: " + removed.Path,
		Data:    a.getRootInfosLocked(),
	})
}

// handleRootsUpdate 修改指定常驻目录的展示别名。
func (a *app) handleRootsUpdate(w http.ResponseWriter, r *http.Request) {
	var req updateRootRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: err.Error()})
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if req.Index < 0 || req.Index >= len(a.config.RootPaths) {
		writeJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "无效的索引"})
		return
	}

	a.config.RootPaths[req.Index].Alias = strings.TrimSpace(req.Alias)
	a.rebuildRuntimeConfigLocked()

	if err := a.saveConfigLocked(); err != nil {
		writeJSON(w, http.StatusInternalServerError, apiResponse{Success: false, Error: "保存配置失败"})
		return
	}

	writeJSON(w, http.StatusOK, apiResponse{
		Success: true,
		Message: "别名修改成功",
		Data:    a.getRootInfosLocked(),
	})
}

// loadConfig 从磁盘读取配置，并将兼容格式归一化到运行时结构中。
func (a *app) loadConfig() error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}

	var cfg config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return err
	}

	rootPaths, err := parseRootPaths(cfg.RootPath, cfg.RootPathsRaw)
	if err != nil {
		return err
	}
	cfg.RootPaths = rootPaths
	cfg.RootPath = ""
	if cfg.Port == 0 {
		cfg.Port = 3002
	}
	if cfg.Token == "" {
		cfg.Token = "default-token"
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	a.config = cfg
	a.rebuildRuntimeConfigLocked()
	return nil
}

// saveConfigLocked 将当前内存中的配置写回磁盘，调用方需持有写锁。
func (a *app) saveConfigLocked() error {
	cfg := a.config
	cfg.RootPath = ""
	cfg.RootPathsRaw = nil
	data, err := json.MarshalIndent(struct {
		Token         string          `json:"token"`
		Port          int             `json:"port"`
		RootPaths     []rootPathEntry `json:"rootPaths"`
		ExcludedNames []string        `json:"excludedNames"`
		ExcludeHidden bool            `json:"excludeHidden"`
	}{
		Token:         cfg.Token,
		Port:          cfg.Port,
		RootPaths:     cfg.RootPaths,
		ExcludedNames: cfg.ExcludedNames,
		ExcludeHidden: a.excludeHidden,
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, data, 0o644)
}

// rebuildRuntimeConfigLocked 基于原始配置重建运行期缓存字段，调用方需持有写锁。
func (a *app) rebuildRuntimeConfigLocked() {
	a.rootPaths = make([]string, 0, len(a.config.RootPaths))
	for _, item := range a.config.RootPaths {
		if strings.TrimSpace(item.Path) != "" {
			a.rootPaths = append(a.rootPaths, item.Path)
		}
	}

	a.excludedNames = make(map[string]struct{})
	names := a.config.ExcludedNames
	if len(names) == 0 {
		names = defaultExcludedNames
	}
	for _, name := range names {
		if name = strings.TrimSpace(name); name != "" {
			a.excludedNames[name] = struct{}{}
		}
	}

	a.excludeHidden = true
	if a.config.ExcludeHidden != nil {
		a.excludeHidden = *a.config.ExcludeHidden
	}
}

// getRootInfos 返回适合响应给前端的根目录信息列表。
func (a *app) getRootInfos() []rootInfo {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.getRootInfosLocked()
}

// getRootInfosLocked 在已持锁前提下构建根目录信息列表。
func (a *app) getRootInfosLocked() []rootInfo {
	infos := make([]rootInfo, 0, len(a.config.RootPaths))
	for i, item := range a.config.RootPaths {
		absPath, _ := filepath.Abs(item.Path)
		name := item.Alias
		if strings.TrimSpace(name) == "" {
			name = filepath.Base(absPath)
			if name == "." || name == string(os.PathSeparator) || name == "" {
				name = item.Path
			}
		}
		infos = append(infos, rootInfo{
			Index:   i,
			Path:    item.Path,
			Name:    name,
			AbsPath: absPath,
			Alias:   item.Alias,
		})
	}
	return infos
}

// currentPort 返回当前配置中的监听端口。
func (a *app) currentPort() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.config.Port
}

// currentRootPaths 返回运行期根目录列表的副本，避免外部修改内部切片。
func (a *app) currentRootPaths() []string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]string, len(a.rootPaths))
	copy(out, a.rootPaths)
	return out
}

// resolvePath 将相对路径解析为绝对路径，并校验其仍位于允许的根目录范围内。
func (a *app) resolvePath(filePath string, rootIndex int) (string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if len(a.rootPaths) == 0 {
		return "", errNoRootPath
	}
	if rootIndex < 0 || rootIndex >= len(a.rootPaths) {
		rootIndex = 0
	}

	rootDir := a.rootPaths[rootIndex]
	resolvedRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return "", errInvalidPath
	}
	resolved := filepath.Join(resolvedRoot, filepath.FromSlash(filePath))
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return "", errInvalidPath
	}

	if !samePath(resolved, resolvedRoot) && !strings.HasPrefix(withTrailingSep(resolved), withTrailingSep(resolvedRoot)) {
		return "", errPathTraversal
	}
	return resolved, nil
}

// shouldExclude 判断某个文件名是否应在文件树中被过滤掉。
func (a *app) shouldExclude(name string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.excludeHidden && strings.HasPrefix(name, ".") {
		return true
	}
	_, ok := a.excludedNames[name]
	return ok
}

// parseRootPaths 解析 rootPaths 配置，兼容字符串数组、对象数组和旧版 rootPath。
func parseRootPaths(rootPath string, raw json.RawMessage) ([]rootPathEntry, error) {
	if len(bytes.TrimSpace(raw)) == 0 || string(bytes.TrimSpace(raw)) == "null" {
		if strings.TrimSpace(rootPath) == "" {
			return []rootPathEntry{}, nil
		}
		return []rootPathEntry{{Path: rootPath}}, nil
	}

	var stringItems []string
	if err := json.Unmarshal(raw, &stringItems); err == nil {
		out := make([]rootPathEntry, 0, len(stringItems))
		for _, item := range stringItems {
			if strings.TrimSpace(item) != "" {
				out = append(out, rootPathEntry{Path: item})
			}
		}
		return out, nil
	}

	var objectItems []rootPathEntry
	if err := json.Unmarshal(raw, &objectItems); err == nil {
		out := make([]rootPathEntry, 0, len(objectItems))
		for _, item := range objectItems {
			if strings.TrimSpace(item.Path) != "" {
				out = append(out, rootPathEntry{
					Path:  item.Path,
					Alias: item.Alias,
				})
			}
		}
		return out, nil
	}

	return nil, errors.New("rootPaths 配置格式无效")
}

// decodeJSONBody 读取并解析 JSON 请求体，同时限制大小并拒绝未知字段。
func decodeJSONBody(r *http.Request, dst interface{}) error {
	defer r.Body.Close()

	limited := io.LimitReader(r.Body, maxBodySize+1)
	decoder := json.NewDecoder(limited)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return errors.New("请求体格式错误")
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("请求体格式错误")
	}
	return nil
}

// writeJSON 将标准响应结构编码为 JSON 输出。
func writeJSON(w http.ResponseWriter, status int, payload apiResponse) {
	writeRawJSON(w, status, payload)
}

// writeRawJSON 将任意结构编码为 JSON 输出。
func writeRawJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

// parseRootIndexFromQuery 从查询参数中解析根目录索引，并兼容旧字段名。
func parseRootIndexFromQuery(r *http.Request) int {
	rootIndex := parseIntWithDefault(r.URL.Query().Get("rootIndex"), -1)
	if rootIndex == -1 {
		rootIndex = parseIntWithDefault(r.URL.Query().Get("root"), 0)
	}
	return rootIndex
}

// parseCopyMoveRoots 从复制或移动请求中解析源和目标根目录索引。
func parseCopyMoveRoots(req copyMoveRequest) (int, int) {
	fromRoot := pickInt(req.FromRoot, req.RootIndex, req.Root)
	toRoot := pickInt(req.ToRoot, req.RootIndex, req.Root)
	return fromRoot, toRoot
}

// pickInt 返回第一个非空整数指针对应的值，用于兼容多套字段名。
func pickInt(values ...*int) int {
	for _, value := range values {
		if value != nil {
			return *value
		}
	}
	return 0
}

// parseIntWithDefault 解析字符串整数，失败时返回给定默认值。
func parseIntWithDefault(input string, fallback int) int {
	if input == "" {
		return fallback
	}
	value, err := strconv.Atoi(input)
	if err != nil {
		return fallback
	}
	return value
}

// pathJoin 使用前端约定的斜杠格式拼接相对路径。
func pathJoin(base, name string) string {
	base = strings.TrimSuffix(base, "/")
	if base == "" {
		return name
	}
	return base + "/" + name
}

// sortNodes 将目录排在前面，并按名称排序文件树节点。
func sortNodes(nodes []fileNode) {
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].IsDirectory != nodes[j].IsDirectory {
			return nodes[i].IsDirectory
		}
		return strings.ToLower(nodes[i].Name) < strings.ToLower(nodes[j].Name)
	})
}

// isTextFile 根据扩展名、文件头和内容类型判断目标是否可按文本编辑。
func isTextFile(realPath, filePath string) (bool, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	baseName := strings.ToLower(filepath.Base(filePath))
	if _, ok := textExtensions[ext]; ok {
		return true, nil
	}
	if _, ok := specialTextNames[baseName]; ok {
		return true, nil
	}

	if ext != "" {
		contentType := mime.TypeByExtension(ext)
		return strings.HasPrefix(contentType, "text/"), nil
	}

	header, err := readFileHeader(realPath, 512)
	if err != nil {
		return false, wrapFSError(err)
	}
	for _, sig := range binarySignatures {
		if len(header) >= len(sig) && bytes.Equal(header[:len(sig)], sig) {
			return false, nil
		}
	}

	contentType := http.DetectContentType(header)
	if strings.HasPrefix(contentType, "text/") {
		return true, nil
	}
	if contentType == "application/octet-stream" && isLikelyUTF8(header) {
		return true, nil
	}
	return false, nil
}

// readFileHeader 读取文件头部若干字节，用于类型探测。
func readFileHeader(path string, size int) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	buf := make([]byte, size)
	n, err := file.Read(buf)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return buf[:n], nil
}

// isLikelyUTF8 粗略判断一段字节是否更像 UTF-8 文本。
func isLikelyUTF8(data []byte) bool {
	for len(data) > 0 {
		r, size := utf8DecodeRune(data)
		if r == '\uFFFD' && size == 1 {
			return false
		}
		data = data[size:]
	}
	return true
}

// utf8DecodeRune 在不依赖额外包的前提下解析单个 UTF-8 rune。
func utf8DecodeRune(data []byte) (rune, int) {
	if len(data) == 0 {
		return '\uFFFD', 0
	}
	if data[0] < 0x80 {
		return rune(data[0]), 1
	}
	if len(data) >= 2 && data[0]&0xE0 == 0xC0 && data[1]&0xC0 == 0x80 {
		return rune(data[0]&0x1F)<<6 | rune(data[1]&0x3F), 2
	}
	if len(data) >= 3 && data[0]&0xF0 == 0xE0 && data[1]&0xC0 == 0x80 && data[2]&0xC0 == 0x80 {
		return rune(data[0]&0x0F)<<12 | rune(data[1]&0x3F)<<6 | rune(data[2]&0x3F), 3
	}
	if len(data) >= 4 && data[0]&0xF8 == 0xF0 && data[1]&0xC0 == 0x80 && data[2]&0xC0 == 0x80 && data[3]&0xC0 == 0x80 {
		return rune(data[0]&0x07)<<18 | rune(data[1]&0x3F)<<12 | rune(data[2]&0x3F)<<6 | rune(data[3]&0x3F), 4
	}
	return '\uFFFD', 1
}

// copyPath 根据源路径类型递归复制文件或目录。
func copyPath(src, dst string) error {
	stat, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if stat.IsDir() {
		return copyDir(src, dst, stat.Mode())
	}
	return copyFile(src, dst, stat.Mode())
}

// copyDir 递归复制整个目录树。
func copyDir(src, dst string, mode fs.FileMode) error {
	if err := os.MkdirAll(dst, mode.Perm()); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcChild := filepath.Join(src, entry.Name())
		dstChild := filepath.Join(dst, entry.Name())
		if err := copyPath(srcChild, dstChild); err != nil {
			return err
		}
	}
	return nil
}

// copyFile 复制单个文件内容并保留基础权限。
func copyFile(src, dst string, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode.Perm())
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

// movePath 优先尝试重命名，跨设备时回退为复制后删除。
func movePath(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err == nil {
		return nil
	} else if !isCrossDeviceError(err) {
		return err
	}

	if err := copyPath(src, dst); err != nil {
		return err
	}
	return os.RemoveAll(src)
}

// isCrossDeviceError 判断错误是否由跨设备移动导致。
func isCrossDeviceError(err error) bool {
	type causer interface {
		Unwrap() error
	}

	for current := err; current != nil; {
		if strings.Contains(strings.ToLower(current.Error()), "cross-device") ||
			strings.Contains(strings.ToLower(current.Error()), "invalid cross-device link") {
			return true
		}
		next, ok := current.(causer)
		if !ok {
			break
		}
		current = next.Unwrap()
	}
	return false
}

// samePath 比较两个路径在当前平台下是否表示同一位置。
func samePath(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// withTrailingSep 为路径追加结尾分隔符，便于做前缀比较。
func withTrailingSep(path string) string {
	cleaned := filepath.Clean(path)
	if strings.HasSuffix(cleaned, string(os.PathSeparator)) {
		return cleaned
	}
	return cleaned + string(os.PathSeparator)
}

// formatMode 将文件权限转换为三位八进制字符串。
func formatMode(mode fs.FileMode) string {
	return fmt.Sprintf("%03o", mode.Perm())
}

// isValidMode 校验权限字符串是否为合法的八进制格式。
func isValidMode(input string) bool {
	if len(input) < 3 || len(input) > 4 {
		return false
	}
	for _, ch := range input {
		if ch < '0' || ch > '7' {
			return false
		}
	}
	return true
}

// padMode 将权限字符串左侧补零到至少三位。
func padMode(input string) string {
	if len(input) >= 3 {
		return input
	}
	return strings.Repeat("0", 3-len(input)) + input
}

var (
	// errPathTraversal 表示请求路径越过了允许的根目录边界。
	errPathTraversal = appError{Status: http.StatusBadRequest, Message: "路径越权访问被拒绝"}
	// errNoRootPath 表示当前没有可供操作的根目录配置。
	errNoRootPath    = appError{Status: http.StatusBadRequest, Message: "未配置可用的根目录"}
	// errInvalidPath 表示路径格式本身无法被安全解析。
	errInvalidPath   = appError{Status: http.StatusBadRequest, Message: "路径无效"}
)

// appError 表示带有 HTTP 状态码的业务错误。
type appError struct {
	// Status 表示最终返回给客户端的 HTTP 状态码。
	Status  int
	// Message 表示返回给客户端的错误提示语。
	Message string
}

// Error 返回自定义业务错误的人类可读信息。
func (e appError) Error() string {
	return e.Message
}

// writeAppError 根据业务错误类型选择合适的 HTTP 状态码和返回消息。
func writeAppError(w http.ResponseWriter, err error) {
	var appErr appError
	if errors.As(err, &appErr) {
		writeJSON(w, appErr.Status, apiResponse{Success: false, Error: appErr.Message})
		return
	}
	writeJSON(w, http.StatusInternalServerError, apiResponse{Success: false, Error: err.Error()})
}

// wrapFSError 将常见文件系统错误映射为更适合前端展示的业务错误。
func wrapFSError(err error) error {
	switch {
	case errors.Is(err, os.ErrNotExist):
		return appError{Status: http.StatusNotFound, Message: "文件或目录不存在"}
	case errors.Is(err, os.ErrPermission):
		return appError{Status: http.StatusForbidden, Message: "权限不足"}
	default:
		return err
	}
}
