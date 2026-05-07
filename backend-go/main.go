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
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"file-editor/backend-go/vfs"
	"github.com/pkg/sftp"
)

const (
	// maxFileSize 表示允许在线编辑的单个文件大小上限。
	maxFileSize = 2 * 1024 * 1024
	// maxBodySize 表示接口允许接收的 JSON 请求体大小上限。
	maxBodySize = 10 * 1024 * 1024
	// systemdServiceName 表示 Linux 安装模式下写入的 systemd 服务名。
	systemdServiceName = "file-editor-backend.service"
)

const defaultConfigFileContent = `{
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
`

var (
	// configPath 表示当前 Go 后端读取和保存配置文件的位置。
	configPath = filepath.Join(baseDir(), "config", "config.json")
	// frontendDistPath 表示前端构建产物目录，用于生产模式下的静态托管。
	frontendDistPath = filepath.Join(baseDir(), "dist")
	// textExtensions 用于快速判断带扩展名文件是否按文本文件处理。
	defaultTextExtensions = []string{
		".txt", ".md", ".log",
		".js", ".ts", ".jsx", ".tsx", ".vue",
		".py", ".rb", ".java", ".kt", ".go", ".rs", ".lua", ".pl",
		".c", ".cpp", ".h", ".hpp",
		".html", ".htm", ".xml", ".svg",
		".css", ".scss", ".sass", ".less",
		".json", ".json5", ".toml", ".yaml", ".yml",
		".ini", ".cfg", ".conf",
		".sh", ".bash", ".zsh", ".bat", ".ps1",
		".sql",
		".env",
		".csv", ".tsv",
		".php",
	}
	// specialTextNames 用于识别没有常规扩展名但通常应按文本处理的文件名。
	defaultTextFileNames = []string{
		"dockerfile",
		"makefile",
		"vagrantfile",
		".env",
		".gitignore",
		".gitattributes",
		".editorconfig",
		"nginx.conf",
		"fstab",
		"hosts",
		"hostname",
		"resolv.conf",
		"environment",
		"authorized_keys",
		"known_hosts",
		"config",
	}
	// binarySignatures 用于通过文件头特征快速排除常见二进制文件。
	binarySignatures = [][]byte{
		{0x89, 0x50, 0x4E, 0x47},
		{0xFF, 0xD8, 0xFF},
		{0x47, 0x49, 0x46},
		{0x50, 0x4B},
		{0x25, 0x50, 0x44, 0x46},
	}
	// defaultBinaryExtensions 用于快速排除常见二进制文件扩展名。
	defaultBinaryExtensions = []string{
		".png", ".jpg", ".jpeg", ".gif", ".webp", ".ico", ".bmp",
		".pdf", ".zip", ".gz", ".tar", ".tgz", ".7z", ".rar",
		".exe", ".dll", ".so", ".dylib", ".bin", ".class", ".jar",
		".woff", ".woff2", ".ttf", ".otf", ".eot",
		".mp3", ".wav", ".flac", ".mp4", ".mov", ".avi", ".mkv",
	}
	// defaultExcludedNames 表示未配置排除列表时的默认忽略项。
	defaultExcludedNames = []string{}
)

// baseDir 返回当前可执行程序所在目录；开发时回退到源码文件所在目录。
func baseDir() string {
	if exePath, err := os.Executable(); err == nil {
		if resolvedExe, err := filepath.EvalSymlinks(exePath); err == nil {
			return filepath.Dir(resolvedExe)
		}
		return filepath.Dir(exePath)
	}

	_, currentFile, _, ok := runtime.Caller(0)
	if ok {
		return filepath.Dir(currentFile)
	}
	return "."
}

// rootPathEntry 表示单个根目录配置项。
type rootPathEntry struct {
	// Type 表示协议类型："local"、"sftp"、"webdav"、"smb"、"ftp"。空值等效于 "local"。
	Type string `json:"type"`
	// Path 表示本地根目录路径（仅 type=local 时使用）。
	Path string `json:"path"`
	// Alias 表示根目录在前端展示时使用的别名。
	Alias string `json:"alias"`
	// Host 表示远程主机地址（非 local 时使用）。
	Host string `json:"host,omitempty"`
	// Port 表示远程端口号。
	Port int `json:"port,omitempty"`
	// Username 表示远程登录用户名。
	Username string `json:"username,omitempty"`
	// Password 表示远程登录密码。
	Password string `json:"password,omitempty"`
	// AuthMethod 表示远程认证方式："password" 或 "key"。
	AuthMethod string `json:"authMethod,omitempty"`
	// KeyPath 表示 SSH 密钥文件路径（SFTP 使用）。
	KeyPath string `json:"keyPath,omitempty"`
	// RootPath 表示远程根目录路径（非 local 时使用）。
	RootPath string `json:"rootPath,omitempty"`
}

// pinnedPath 表示一条收藏路径记录。
type pinnedPath struct {
	RootIndex int    `json:"rootIndex"`
	Path      string `json:"path"`
}

// config 表示配置文件在内存中的结构。
type config struct {
	// Token 表示 API 鉴权令牌。
	Token string `json:"token"`
	// Port 表示服务监听端口。
	Port int `json:"port"`
	// RootPath 表示旧版单目录配置字段，仅用于兼容读取。
	RootPath string `json:"rootPath,omitempty"`
	// RootPathsRaw 表示原始的 rootPaths JSON 内容，用于兼容多种格式。
	RootPathsRaw json.RawMessage `json:"rootPaths"`
	// ExcludedNames 表示需要在文件树中过滤掉的名称列表。
	ExcludedNames []string `json:"excludedNames"`
	// ExcludeHidden 表示是否过滤隐藏文件和隐藏目录。
	ExcludeHidden *bool `json:"excludeHidden"`
	// TextExtensions 表示额外允许按文本打开的扩展名列表。
	TextExtensions []string `json:"textExtensions"`
	// TextFileNames 表示额外允许按文本打开的文件名列表。
	TextFileNames []string `json:"textFileNames"`
	// BinaryExtensions 表示明确按二进制处理的扩展名列表。
	BinaryExtensions []string `json:"binaryExtensions"`
	// BinaryFileNames 表示明确按二进制处理的文件名列表。
	BinaryFileNames []string `json:"binaryFileNames"`
	// PinnedPaths 表示收藏的文件或目录路径列表。
	PinnedPaths []pinnedPath `json:"pinnedPaths"`
	// RootPaths 表示标准化后的根目录配置。
	RootPaths []rootPathEntry `json:"-"`
}

// rootInfo 表示返回给前端的根目录元数据。
type rootInfo struct {
	// Index 表示根目录在当前配置数组中的索引。
	Index int `json:"index"`
	// Type 表示协议类型。
	Type string `json:"type"`
	// Path 表示配置中保存的原始路径。
	Path string `json:"path"`
	// Name 表示前端展示用名称，优先使用别名。
	Name string `json:"name"`
	// AbsPath 表示根目录的绝对路径。
	AbsPath string `json:"absPath"`
	// Alias 表示根目录别名。
	Alias string `json:"alias"`
	// Host 表示远程主机地址。
	Host string `json:"host,omitempty"`
	// Port 表示远程端口号。
	Port int `json:"port,omitempty"`
	// Username 表示远程登录用户名。
	Username string `json:"username,omitempty"`
	// AuthMethod 表示远程认证方式。
	AuthMethod string `json:"authMethod,omitempty"`
	// RootPath 表示远程根目录路径。
	RootPath string `json:"rootPath,omitempty"`
}

// fileNode 表示文件树中的单个节点。
type fileNode struct {
	// Name 表示当前节点名称。
	Name string `json:"name"`
	// Path 表示相对于根目录的路径。
	Path string `json:"path"`
	// RootIndex 表示当前节点所属的根目录索引。
	RootIndex int `json:"rootIndex"`
	// AbsPath 表示根节点时返回的绝对路径。
	AbsPath string `json:"absPath,omitempty"`
	// Alias 表示根节点的别名。
	Alias string `json:"alias,omitempty"`
	// IsDirectory 表示当前节点是否为目录。
	IsDirectory bool `json:"isDirectory"`
	// IsFile 表示当前节点是否为普通文件。
	IsFile bool `json:"isFile"`
	// Size 表示文件或目录的大小。
	Size int64 `json:"size"`
	// Mtime 表示最后修改时间的毫秒时间戳。
	Mtime int64 `json:"mtime"`
	// Mode 表示权限位的八进制字符串。
	Mode string `json:"mode"`
	// Type 表示根节点的协议类型（仅根节点有效）。
	Type string `json:"type,omitempty"`
	// Pinned 表示当前节点是否被收藏。
	Pinned bool `json:"pinned,omitempty"`
	// Children 表示目录节点的子节点，占位或实际内容。
	Children interface{} `json:"children,omitempty"`
}

// apiResponse 表示统一的接口响应结构。
type apiResponse struct {
	// Success 表示接口调用是否成功。
	Success bool `json:"success"`
	// Error 表示失败时的错误信息。
	Error string `json:"error,omitempty"`
	// Message 表示成功时的提示信息。
	Message string `json:"message,omitempty"`
	// Data 表示具体的业务数据。
	Data interface{} `json:"data,omitempty"`
}

// fileContentData 表示读取文件内容接口的返回体。
type fileContentData struct {
	// Content 表示文件文本内容。
	Content string `json:"content"`
	// Size 表示文件大小。
	Size int64 `json:"size"`
	// Path 表示文件相对路径。
	Path string `json:"path"`
}

// fileStatData 表示文件状态信息。
type fileStatData struct {
	// Name 表示文件名或目录名。
	Name string `json:"name"`
	// Path 表示文件相对路径。
	Path string `json:"path"`
	// IsFile 表示是否为普通文件。
	IsFile bool `json:"isFile"`
	// IsDirectory 表示是否为目录。
	IsDirectory bool `json:"isDirectory"`
	// Size 表示文件大小。
	Size int64 `json:"size"`
	// Mode 表示权限位八进制字符串。
	Mode string `json:"mode"`
	// Mtime 表示格式化后的修改时间。
	Mtime string `json:"mtime"`
	// UID 表示 Linux 下的文件所有者 ID；Windows 下保留为 0。
	UID int `json:"uid"`
	// GID 表示 Linux 下的文件所属组 ID；Windows 下保留为 0。
	GID int `json:"gid"`
	// IdentityPlatform 表示身份信息来源平台，如 linux 或 windows。
	IdentityPlatform string `json:"identityPlatform"`
	// Owner 表示解析出的所有者名称。
	Owner string `json:"owner,omitempty"`
	// Group 表示解析出的所属组名称。
	Group string `json:"group,omitempty"`
	// OwnerID 表示平台相关的所有者标识，Linux 为 UID，Windows 为 SID。
	OwnerID string `json:"ownerId,omitempty"`
	// GroupID 表示平台相关的所属组标识，Linux 为 GID，Windows 为 SID。
	GroupID string `json:"groupId,omitempty"`
}

// fileIdentityData 表示按平台解析出的文件身份信息。
type fileIdentityData struct {
	UID              int
	GID              int
	IdentityPlatform string
	Owner            string
	Group            string
	OwnerID          string
	GroupID          string
}

// rootFS 表示运行期单个根目录的运行时状态，包含配置条目和对应的文件系统实现。
type rootFS struct {
	// entry 表示该根目录的原始配置。
	entry rootPathEntry
	// fs 表示该根目录对应的文件系统实现。
	fs vfs.FileSystem
	// rootPath 表示解析后的根目录基准路径（本地为绝对路径，远程为远程根路径）。
	rootPath string
	// isLocal 标记是否为本地文件系统，用于路径处理和身份信息获取。
	isLocal bool
}

// app 表示整个后端服务的运行时状态。
type app struct {
	// mu 用于保护配置和运行时派生数据的并发读写。
	mu sync.RWMutex

	// config 表示当前生效的原始配置。
	config config
	// roots 表示运行期使用的根目录列表。
	roots []rootFS
	// excludedNames 表示运行期使用的排除名称集合。
	excludedNames map[string]struct{}
	// excludeHidden 表示运行期是否排除隐藏文件。
	excludeHidden bool
	// textExtensions 表示运行期文本扩展名集合。
	textExtensions map[string]struct{}
	// textFileNames 表示运行期文本文件名集合。
	textFileNames map[string]struct{}
	// binaryExtensions 表示运行期二进制扩展名集合。
	binaryExtensions map[string]struct{}
	// binaryFileNames 表示运行期二进制文件名集合。
	binaryFileNames map[string]struct{}
	// pinnedPaths 表示运行期收藏路径集合，用于快速判断。
	pinnedPaths map[pinnedPath]struct{}
	// staticEnabled 表示是否启用前端静态文件托管。
	staticEnabled bool
}

// createRequest 表示创建文件或目录接口的请求体。
type createRequest struct {
	// Path 表示要创建的目标相对路径。
	Path string `json:"path"`
	// Type 表示创建类型，directory 表示目录，其余按文件处理。
	Type string `json:"type"`
	// RootIndex 表示目标所属根目录索引。
	RootIndex *int `json:"rootIndex"`
	// Root 表示兼容旧前端时使用的根目录索引字段。
	Root *int `json:"root"`
}

// saveRequest 表示保存文件接口的请求体。
type saveRequest struct {
	// Path 表示要保存的文件相对路径。
	Path string `json:"path"`
	// Content 表示待写入的文件内容。
	Content string `json:"content"`
	// RootIndex 表示目标所属根目录索引。
	RootIndex *int `json:"rootIndex"`
	// Root 表示兼容旧前端时使用的根目录索引字段。
	Root *int `json:"root"`
}

// copyMoveRequest 表示复制或移动接口的请求体。
type copyMoveRequest struct {
	// From 表示源路径。
	From string `json:"from"`
	// To 表示目标路径。
	To string `json:"to"`
	// FromRoot 表示源路径所属根目录索引。
	FromRoot *int `json:"fromRoot"`
	// ToRoot 表示目标路径所属根目录索引。
	ToRoot *int `json:"toRoot"`
	// RootIndex 表示兼容单根目录时的索引字段。
	RootIndex *int `json:"rootIndex"`
	// Root 表示兼容旧前端时使用的索引字段。
	Root *int `json:"root"`
}

// permissionsRequest 表示修改权限接口的请求体。
type permissionsRequest struct {
	// Path 表示目标文件相对路径。
	Path string `json:"path"`
	// Mode 表示待设置的八进制权限字符串。
	Mode string `json:"mode"`
	// RootIndex 表示目标所属根目录索引。
	RootIndex *int `json:"rootIndex"`
	// Root 表示兼容旧前端时使用的根目录索引字段。
	Root *int `json:"root"`
}

// addRootRequest 表示新增常驻目录接口的请求体。
type addRootRequest struct {
	// Type 表示协议类型。
	Type string `json:"type"`
	// Path 表示要加入常驻列表的目录路径（本地）。
	Path string `json:"path"`
	// Alias 表示该目录的展示别名。
	Alias string `json:"alias"`
	// Host 表示远程主机地址。
	Host string `json:"host,omitempty"`
	// Port 表示远程端口号。
	Port int `json:"port,omitempty"`
	// Username 表示远程登录用户名。
	Username string `json:"username,omitempty"`
	// Password 表示远程登录密码。
	Password string `json:"password,omitempty"`
	// AuthMethod 表示远程认证方式。
	AuthMethod string `json:"authMethod,omitempty"`
	// KeyPath 表示 SSH 密钥文件路径。
	KeyPath string `json:"keyPath,omitempty"`
	// RootPath 表示远程根目录路径。
	RootPath string `json:"rootPath,omitempty"`
}

// updateRootRequest 表示更新常驻目录别名接口的请求体。
type updateRootRequest struct {
	// Index 表示待修改的根目录索引。
	Index int `json:"index"`
	// Alias 表示新的展示别名。
	Alias string `json:"alias"`
}

// main 是程序入口，负责加载配置、注册路由并启动 HTTP 服务。
func main() {
	if hasCLIArg("install") {
		if err := installService(); err != nil {
			log.Fatalf("安装服务失败: %v", err)
		}
		return
	}
	if hasCLIArg("uninstall") {
		if err := uninstallService(); err != nil {
			log.Fatalf("卸载服务失败: %v", err)
		}
		return
	}

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
	mux.HandleFunc("/api/files/pin", a.withAPIAuth(a.handleFilesPin))
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

// hasCLIArg 判断命令行参数中是否包含指定参数。
func hasCLIArg(target string) bool {
	for _, arg := range os.Args[1:] {
		if strings.EqualFold(strings.TrimSpace(arg), target) {
			return true
		}
	}
	return false
}

// installService 在 Linux 环境下安装 systemd 服务并设置开机启动。
func installService() error {
	if runtime.GOOS != "linux" {
		log.Printf("install 参数仅在 Linux 环境下用于安装 systemd 服务，当前系统为 %s，已跳过。", runtime.GOOS)
		return nil
	}
	if os.Geteuid() != 0 {
		return errors.New("install 需要 root 权限，请使用 sudo 运行")
	}
	if err := ensureConfigFileExists(); err != nil {
		return fmt.Errorf("初始化配置文件失败: %w", err)
	}

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取程序路径失败: %w", err)
	}
	if resolvedPath, err := filepath.EvalSymlinks(exePath); err == nil {
		exePath = resolvedPath
	}

	servicePath := systemdServicePath()
	serviceContent := buildSystemdServiceContent(exePath, baseDir())
	if err := os.WriteFile(servicePath, []byte(serviceContent), 0o644); err != nil {
		return fmt.Errorf("写入服务文件失败: %w", err)
	}

	if err := runCommand("systemctl", "daemon-reload"); err != nil {
		return err
	}
	if err := runCommand("systemctl", "enable", "--now", systemdServiceName); err != nil {
		return err
	}

	log.Printf("systemd 服务安装完成: %s", servicePath)
	log.Printf("已设置开机启动并尝试立即启动: %s", systemdServiceName)
	return nil
}

// uninstallService 在 Linux 环境下停止并移除已安装的 systemd 服务。
func uninstallService() error {
	if runtime.GOOS != "linux" {
		log.Printf("uninstall 参数仅在 Linux 环境下用于卸载 systemd 服务，当前系统为 %s，已跳过。", runtime.GOOS)
		return nil
	}
	if os.Geteuid() != 0 {
		return errors.New("uninstall 需要 root 权限，请使用 sudo 运行")
	}

	servicePath := systemdServicePath()
	if err := runCommand("systemctl", "disable", "--now", systemdServiceName); err != nil {
		log.Printf("停止或禁用服务时提示: %v", err)
	}

	if err := os.Remove(servicePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("删除服务文件失败: %w", err)
	}

	if err := runCommand("systemctl", "daemon-reload"); err != nil {
		return err
	}
	if err := runCommand("systemctl", "reset-failed", systemdServiceName); err != nil {
		log.Printf("清理失败状态时提示: %v", err)
	}

	log.Printf("systemd 服务已卸载: %s", systemdServiceName)
	return nil
}

// systemdServicePath 返回 systemd unit 文件路径。
func systemdServicePath() string {
	return filepath.Join("/etc", "systemd", "system", systemdServiceName)
}

// buildSystemdServiceContent 生成 systemd unit 文件内容。
func buildSystemdServiceContent(exePath, workingDir string) string {
	return fmt.Sprintf(`[Unit]
Description=File Editor Backend
After=network.target

[Service]
Type=simple
WorkingDirectory=%s
ExecStart=%s
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
`, workingDir, exePath)
}

// runCommand 执行外部命令，并在失败时带上输出内容。
func runCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(output))
		if text == "" {
			return fmt.Errorf("%s %s 执行失败: %w", name, strings.Join(args, " "), err)
		}
		return fmt.Errorf("%s %s 执行失败: %w: %s", name, strings.Join(args, " "), err, text)
	}
	return nil
}

// withCORS 为所有请求追加跨域响应头，并处理预检请求。
func (a *app) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Auth-Token")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Expose-Headers", "X-Auth-Token")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
			w.Header().Set("Pragma", "no-cache")
			w.Header().Set("Expires", "0")
		}
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
		a.mu.RLock()
		tree := make([]fileNode, 0, len(a.roots))
		for i, rf := range a.roots {
			info := a.getRootInfoLocked(i)
			if info == nil {
				continue
			}
			node := fileNode{
				Name:        info.Name,
				Path:        "",
				RootIndex:   info.Index,
				AbsPath:     info.AbsPath,
				Alias:       info.Alias,
				Type:        info.Type,
				IsDirectory: true,
				IsFile:      false,
				Mode:        "755",
			}
			if rf.isLocal {
				stat, err := rf.fs.Stat(rf.rootPath)
				if err == nil {
					node.IsDirectory = stat.IsDir()
					node.IsFile = !stat.IsDir()
					node.Size = stat.Size()
					node.Mtime = stat.ModTime().UnixMilli()
					node.Mode = formatMode(stat.Mode())
				}
			}
			if node.IsDirectory {
				node.Children = []fileNode{}
			}
			tree = append(tree, node)
		}
		a.mu.RUnlock()

		writeRawJSON(w, http.StatusOK, map[string]interface{}{
			"success":     true,
			"data":        tree,
			"isMultiRoot": true,
		})
		return
	}

	fs, real, err := a.resolveFS(subDir, rootIndex)
	if err != nil {
		writeAppError(w, err)
		return
	}

	entries, err := fs.ReadDir(real)
	if err != nil {
		writeAppError(w, wrapFSError(err))
		return
	}

	tree := make([]fileNode, 0, len(entries) + 8)
	existingPaths := make(map[string]bool, len(entries))

	for _, entry := range entries {
		name := entry.Name()
		if a.shouldExclude(name) {
			continue
		}

		full := path.Join(real, name)
		stat, err := fs.Lstat(full)
		if err != nil {
			continue
		}

		rel := name
		if subDir != "" {
			rel = pathJoin(subDir, name)
		}
		existingPaths[rel] = true

		pp := pinnedPath{RootIndex: rootIndex, Path: rel}
		a.mu.RLock()
		_, pinned := a.pinnedPaths[pp]
		a.mu.RUnlock()

		node := fileNode{
			Name:        name,
			Path:        rel,
			RootIndex:   rootIndex,
			IsDirectory: stat.IsDir(),
			IsFile:      !stat.IsDir(),
			Size:        stat.Size(),
			Mtime:       stat.ModTime().UnixMilli(),
			Mode:        formatMode(stat.Mode()),
			Pinned:      pinned,
		}
		if stat.IsDir() {
			node.Children = []fileNode{}
		}
		tree = append(tree, node)
	}

	// 展开根节点时，额外注入不在当前目录下的收藏路径作为快捷方式
	if subDir == "" {
		var pinnedEntries []pinnedPath
		a.mu.RLock()
		for _, pp := range a.config.PinnedPaths {
			if pp.RootIndex == rootIndex && !existingPaths[pp.Path] {
				pinnedEntries = append(pinnedEntries, pp)
			}
		}
		a.mu.RUnlock()

		for _, pp := range pinnedEntries {
			pinReal := path.Join(real, pp.Path)
			stat, err := fs.Lstat(pinReal)
			if err != nil {
				continue
			}
			tree = append(tree, fileNode{
				Name:        path.Base(pp.Path),
				Path:        pp.Path,
				RootIndex:   rootIndex,
				IsDirectory: stat.IsDir(),
				IsFile:      !stat.IsDir(),
				Size:        stat.Size(),
				Mtime:       stat.ModTime().UnixMilli(),
				Mode:        formatMode(stat.Mode()),
				Pinned:      true,
			})
		}
	}

	sortPinnedNodes(tree)
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
	forceText := r.URL.Query().Get("forceText") == "true"
	if filePath == "" {
		writeJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "缺少 path"})
		return
	}

	fs, real, err := a.resolveFS(filePath, rootIndex)
	if err != nil {
		writeAppError(w, err)
		return
	}

	stat, err := fs.Stat(real)
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

	header, _ := fs.ReadFileHeader(real, 4096)
	ok, err := a.isTextFile(filePath, header)
	if err != nil {
		writeAppError(w, err)
		return
	}
	if !ok && !forceText {
		writeJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "不支持编辑此文件类型"})
		return
	}

	content, err := fs.ReadFile(real)
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
	fs, real, err := a.resolveFS(req.Path, rootIndex)
	if err != nil {
		writeAppError(w, err)
		return
	}

	if len(req.Content) > maxFileSize {
		writeJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "超过 2MB 限制"})
		return
	}

	if stat, err := fs.Stat(real); err == nil && stat.IsDir() {
		writeJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "不能覆盖目录"})
		return
	}

	if err := fs.MkdirAll(path.Dir(real), 0o755); err != nil {
		writeAppError(w, wrapFSError(err))
		return
	}
	if err := fs.WriteFile(real, []byte(req.Content)); err != nil {
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
	fs, real, err := a.resolveFS(req.Path, rootIndex)
	if err != nil {
		writeAppError(w, err)
		return
	}

	if _, err := fs.Stat(real); err == nil {
		writeJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "目标已存在"})
		return
	} else if !vfs.IsNotExist(err) {
		writeAppError(w, wrapFSError(err))
		return
	}

	if req.Type == "directory" {
		if err := fs.MkdirAll(real, 0o755); err != nil {
			writeAppError(w, wrapFSError(err))
			return
		}
	} else {
		if err := fs.MkdirAll(path.Dir(real), 0o755); err != nil {
			writeAppError(w, wrapFSError(err))
			return
		}
		if err := fs.CreateFile(real); err != nil {
			writeAppError(w, wrapFSError(err))
			return
		}
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

	fs, real, err := a.resolveFS(filePath, rootIndex)
	if err != nil {
		writeAppError(w, err)
		return
	}

	stat, err := fs.Stat(real)
	if err != nil {
		writeAppError(w, wrapFSError(err))
		return
	}

	if stat.IsDir() {
		if err := fs.RemoveAll(real); err != nil {
			writeAppError(w, wrapFSError(err))
			return
		}
	} else {
		if err := fs.Remove(real); err != nil {
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
	fromFS, fromReal, err := a.resolveFS(req.From, fromRoot)
	if err != nil {
		writeAppError(w, err)
		return
	}
	toFS, toReal, err := a.resolveFS(req.To, toRoot)
	if err != nil {
		writeAppError(w, err)
		return
	}

	fromStat, err := fromFS.Lstat(fromReal)
	if err != nil {
		writeAppError(w, wrapFSError(err))
		return
	}

	if fromRoot == toRoot {
		if fromStat.IsDir() && vfs.IsPathInsideOrSame(fromReal, toReal) {
			writeJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "不能复制目录到自身或其子目录中"})
			return
		}
		if _, err := toFS.Stat(toReal); err == nil {
			writeJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "目标已存在"})
			return
		} else if !vfs.IsNotExist(err) {
			writeAppError(w, wrapFSError(err))
			return
		}
		if err := fromFS.Copy(fromReal, toReal); err != nil {
			writeAppError(w, wrapFSError(err))
			return
		}
	} else {
		if err := crossFSCopy(fromFS, fromReal, toFS, toReal); err != nil {
			writeAppError(w, wrapFSError(err))
			return
		}
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
	fromFS, fromReal, err := a.resolveFS(req.From, fromRoot)
	if err != nil {
		writeAppError(w, err)
		return
	}
	toFS, toReal, err := a.resolveFS(req.To, toRoot)
	if err != nil {
		writeAppError(w, err)
		return
	}

	fromStat, err := fromFS.Lstat(fromReal)
	if err != nil {
		writeAppError(w, wrapFSError(err))
		return
	}

	if fromRoot == toRoot {
		if fromStat.IsDir() && vfs.IsPathInsideOrSame(fromReal, toReal) {
			writeJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "不能移动目录到自身或其子目录中"})
			return
		}
		if _, err := toFS.Stat(toReal); err == nil {
			writeJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "目标已存在"})
			return
		} else if !vfs.IsNotExist(err) {
			writeAppError(w, wrapFSError(err))
			return
		}
		if err := vfs.Move(fromFS, fromReal, toReal); err != nil {
			writeAppError(w, wrapFSError(err))
			return
		}
	} else {
		if err := crossFSCopy(fromFS, fromReal, toFS, toReal); err != nil {
			writeAppError(w, wrapFSError(err))
			return
		}
		if err := fromFS.RemoveAll(fromReal); err != nil {
			writeAppError(w, wrapFSError(err))
			return
		}
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

	fs, real, err := a.resolveFS(filePath, rootIndex)
	if err != nil {
		writeAppError(w, err)
		return
	}

	stat, err := fs.Stat(real)
	if err != nil {
		writeAppError(w, wrapFSError(err))
		return
	}

	name := path.Base(filePath)
	if filePath == "" {
		name = path.Base(real)
		if name == "." || name == "/" || name == "" {
			name = real
		}
	}
	identity := a.getIdentityForFS(fs, real, stat)
	writeJSON(w, http.StatusOK, apiResponse{
		Success: true,
		Data: fileStatData{
			Name:             name,
			Path:             filePath,
			IsFile:           !stat.IsDir(),
			IsDirectory:      stat.IsDir(),
			Size:             stat.Size(),
			Mode:             formatMode(stat.Mode()),
			Mtime:            stat.ModTime().Format("2006-01-02 15:04:05"),
			UID:              identity.UID,
			GID:              identity.GID,
			IdentityPlatform: identity.IdentityPlatform,
			Owner:            identity.Owner,
			Group:            identity.Group,
			OwnerID:          identity.OwnerID,
			GroupID:          identity.GroupID,
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
	fs, real, err := a.resolveFS(req.Path, rootIndex)
	if err != nil {
		writeAppError(w, err)
		return
	}

	modeVal, _ := strconv.ParseUint(req.Mode, 8, 32)
	if err := fs.Chmod(real, os.FileMode(modeVal)); err != nil {
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

// pinRequest 表示添加收藏的请求体。
type pinRequest struct {
	RootIndex *int   `json:"rootIndex"`
	Path      string `json:"path"`
}

// handleFilesPin 根据 HTTP 方法分发收藏相关操作。
func (a *app) handleFilesPin(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.handlePinsGet(w, r)
	case http.MethodPost:
		a.handlePinAdd(w, r)
	case http.MethodDelete:
		a.handlePinRemove(w, r)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "方法不允许"})
	}
}

// handlePinsGet 返回所有收藏路径。
func (a *app) handlePinsGet(w http.ResponseWriter, r *http.Request) {
	a.mu.RLock()
	pins := a.config.PinnedPaths
	a.mu.RUnlock()

	writeJSON(w, http.StatusOK, apiResponse{Success: true, Data: pins})
}

// handlePinAdd 添加一条收藏路径。
func (a *app) handlePinAdd(w http.ResponseWriter, r *http.Request) {
	var req pinRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: err.Error()})
		return
	}
	if req.RootIndex == nil || req.Path == "" {
		writeJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "缺少 rootIndex 或 path"})
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	pp := pinnedPath{RootIndex: *req.RootIndex, Path: req.Path}
	if _, exists := a.pinnedPaths[pp]; exists {
		writeJSON(w, http.StatusOK, apiResponse{Success: true, Message: "已收藏"})
		return
	}

	a.config.PinnedPaths = append(a.config.PinnedPaths, pp)
	a.pinnedPaths[pp] = struct{}{}

	if err := a.saveConfigLocked(); err != nil {
		writeJSON(w, http.StatusInternalServerError, apiResponse{Success: false, Error: "保存配置失败"})
		return
	}

	writeJSON(w, http.StatusOK, apiResponse{Success: true, Message: "收藏成功"})
}

// handlePinRemove 移除一条收藏路径。
func (a *app) handlePinRemove(w http.ResponseWriter, r *http.Request) {
	rootIndex := parseIntWithDefault(r.URL.Query().Get("rootIndex"), -1)
	filePath := r.URL.Query().Get("path")
	if rootIndex < 0 || filePath == "" {
		writeJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "缺少 rootIndex 或 path"})
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	pp := pinnedPath{RootIndex: rootIndex, Path: filePath}

	filtered := make([]pinnedPath, 0, len(a.config.PinnedPaths))
	for _, p := range a.config.PinnedPaths {
		if p.RootIndex != rootIndex || p.Path != filePath {
			filtered = append(filtered, p)
		}
	}
	a.config.PinnedPaths = filtered
	delete(a.pinnedPaths, pp)

	if err := a.saveConfigLocked(); err != nil {
		writeJSON(w, http.StatusInternalServerError, apiResponse{Success: false, Error: "保存配置失败"})
		return
	}

	writeJSON(w, http.StatusOK, apiResponse{Success: true, Message: "已取消收藏"})
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

	rootType := strings.ToLower(strings.TrimSpace(req.Type))
	if rootType == "" || rootType == "local" {
		if req.Path == "" {
			writeJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "缺少 path"})
			return
		}
		absPath, err := filepath.Abs(req.Path)
		if err != nil {
			writeAppError(w, errInvalidPath)
			return
		}
		localFS := vfs.NewLocalFS()
		stat, err := localFS.Stat(absPath)
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
			if !strings.EqualFold(strings.TrimSpace(item.Type), "sftp") {
				existingAbs, _ := filepath.Abs(item.Path)
				if samePath(existingAbs, absPath) {
					writeJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "该目录已在常驻列表中"})
					return
				}
			}
		}

		a.config.RootPaths = append(a.config.RootPaths, rootPathEntry{
			Path:  req.Path,
			Alias: strings.TrimSpace(req.Alias),
		})
	} else {
		if req.Host == "" {
			writeJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "远程主机地址不能为空"})
			return
		}

		// 添加前先测试连接
		if err := testSFTPConnection(req); err != nil {
			writeJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "连接验证失败: " + err.Error()})
			return
		}

		a.mu.Lock()
		defer a.mu.Unlock()

		a.config.RootPaths = append(a.config.RootPaths, rootPathEntry{
			Type:       req.Type,
			Alias:      strings.TrimSpace(req.Alias),
			Host:       req.Host,
			Port:       req.Port,
			Username:   req.Username,
			Password:   req.Password,
			AuthMethod: req.AuthMethod,
			KeyPath:    req.KeyPath,
			RootPath:   strings.TrimSpace(req.RootPath),
		})
	}

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
	if err := ensureConfigFileExists(); err != nil {
		return err
	}

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

// ensureConfigFileExists 确保配置文件存在，不存在时写入内置的完整模板。
func ensureConfigFileExists() error {
	if _, err := os.Stat(configPath); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(configPath, []byte(defaultConfigFileContent), 0o644)
}

// saveConfigLocked 将当前内存中的配置写回磁盘，调用方需持有写锁。
func (a *app) saveConfigLocked() error {
	cfg := a.config
	cfg.RootPath = ""
	cfg.RootPathsRaw = nil
	data, err := json.MarshalIndent(struct {
		Token            string          `json:"token"`
		Port             int             `json:"port"`
		RootPaths        []rootPathEntry `json:"rootPaths"`
		ExcludedNames    []string        `json:"excludedNames"`
		ExcludeHidden    bool            `json:"excludeHidden"`
		TextExtensions   []string        `json:"textExtensions"`
		TextFileNames    []string        `json:"textFileNames"`
		BinaryExtensions []string        `json:"binaryExtensions"`
		BinaryFileNames  []string        `json:"binaryFileNames"`
		PinnedPaths      []pinnedPath    `json:"pinnedPaths"`
	}{
		Token:            cfg.Token,
		Port:             cfg.Port,
		RootPaths:        cfg.RootPaths,
		ExcludedNames:    cfg.ExcludedNames,
		ExcludeHidden:    a.excludeHidden,
		TextExtensions:   cfg.TextExtensions,
		TextFileNames:    cfg.TextFileNames,
		BinaryExtensions: cfg.BinaryExtensions,
		BinaryFileNames:  cfg.BinaryFileNames,
		PinnedPaths:      cfg.PinnedPaths,
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, data, 0o644)
}

// rebuildRuntimeConfigLocked 基于原始配置重建运行期缓存字段，调用方需持有写锁。
func (a *app) rebuildRuntimeConfigLocked() {
	a.roots = make([]rootFS, 0, len(a.config.RootPaths))
	for _, item := range a.config.RootPaths {
		rootType := strings.ToLower(strings.TrimSpace(item.Type))
		if rootType == "" || rootType == "local" {
			localPath := strings.TrimSpace(item.Path)
			if localPath == "" {
				continue
			}
			absPath, err := filepath.Abs(localPath)
			if err != nil {
				continue
			}
			a.roots = append(a.roots, rootFS{
				entry:    item,
				fs:       vfs.NewLocalFS(),
				rootPath: absPath,
				isLocal:  true,
			})
		} else if rootType == "sftp" {
			remoteRoot := strings.TrimSpace(item.RootPath)
			if remoteRoot == "" {
				remoteRoot = "/"
			}
			a.roots = append(a.roots, rootFS{
				entry:    item,
				fs:       vfs.NewSFTPFS(toSFTPConfig(item), remoteRoot),
				rootPath: remoteRoot,
				isLocal:  false,
			})
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

	a.textExtensions = normalizeStringSet(defaultTextExtensions, true)
	mergeStringSet(a.textExtensions, cfgStringSlice(a.config.TextExtensions), true)

	a.textFileNames = normalizeStringSet(defaultTextFileNames, false)
	mergeStringSet(a.textFileNames, cfgStringSlice(a.config.TextFileNames), false)

	a.binaryExtensions = normalizeStringSet(defaultBinaryExtensions, true)
	mergeStringSet(a.binaryExtensions, cfgStringSlice(a.config.BinaryExtensions), true)

	a.binaryFileNames = normalizeStringSet(nil, false)
	mergeStringSet(a.binaryFileNames, cfgStringSlice(a.config.BinaryFileNames), false)

	a.pinnedPaths = make(map[pinnedPath]struct{}, len(a.config.PinnedPaths))
	for _, pp := range a.config.PinnedPaths {
		a.pinnedPaths[pp] = struct{}{}
	}
}

// toSFTPConfig 将 rootPathEntry 转换为 SFTP 连接配置。
func toSFTPConfig(entry rootPathEntry) vfs.SFTPConfig {
	return vfs.SFTPConfig{
		Host:       entry.Host,
		Port:       entry.Port,
		Username:   entry.Username,
		Password:   entry.Password,
		AuthMethod: entry.AuthMethod,
		KeyPath:    entry.KeyPath,
	}
}

// testSFTPConnection 测试给定配置的 SFTP 连接是否可用。
func testSFTPConnection(req addRootRequest) error {
	config := vfs.SFTPConfig{
		Host:       req.Host,
		Port:       req.Port,
		Username:   req.Username,
		Password:   req.Password,
		AuthMethod: req.AuthMethod,
		KeyPath:    req.KeyPath,
	}
	return vfs.TestSFTPConnection(config)
}

// cfgStringSlice 返回配置切片的副本，避免运行时误改原配置。
func cfgStringSlice(values []string) []string {
	out := make([]string, len(values))
	copy(out, values)
	return out
}

// normalizeStringSet 将字符串列表标准化为集合，可选按扩展名规则补点。
func normalizeStringSet(values []string, isExtension bool) map[string]struct{} {
	set := make(map[string]struct{})
	mergeStringSet(set, values, isExtension)
	return set
}

// mergeStringSet 将字符串列表合并进目标集合。
func mergeStringSet(target map[string]struct{}, values []string, isExtension bool) {
	for _, value := range values {
		normalized := normalizeFileRule(value, isExtension)
		if normalized == "" {
			continue
		}
		target[normalized] = struct{}{}
	}
}

// normalizeFileRule 统一清理配置中的扩展名和文件名。
func normalizeFileRule(value string, isExtension bool) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return ""
	}
	if isExtension {
		if !strings.HasPrefix(normalized, ".") {
			normalized = "." + normalized
		}
	}
	return normalized
}

// getRootInfos 返回适合响应给前端的根目录信息列表。
func (a *app) getRootInfos() []rootInfo {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.getRootInfosLocked()
}

// getRootInfosLocked 在已持锁前提下构建根目录信息列表。
func (a *app) getRootInfosLocked() []rootInfo {
	infos := make([]rootInfo, 0, len(a.roots))
	for i := range a.roots {
		info := a.getRootInfoLocked(i)
		if info != nil {
			infos = append(infos, *info)
		}
	}
	return infos
}

// getRootInfoLocked 在已持锁前提下返回指定索引的根目录信息。
func (a *app) getRootInfoLocked(index int) *rootInfo {
	if index < 0 || index >= len(a.roots) {
		return nil
	}
	rf := a.roots[index]
	item := rf.entry
	basePath := rf.rootPath
	name := item.Alias
	if strings.TrimSpace(name) == "" {
		if rf.isLocal {
			name = filepath.Base(basePath)
		} else {
			name = basePath
		}
		if name == "." || name == string(os.PathSeparator) || name == "" {
			name = item.Path
			if name == "" {
				name = item.Host
			}
		}
	}
	info := rootInfo{
		Index:      index,
		Type:       item.Type,
		Path:       item.Path,
		Name:       name,
		AbsPath:    basePath,
		Alias:      item.Alias,
		Host:       item.Host,
		Port:       item.Port,
		Username:   item.Username,
		AuthMethod: item.AuthMethod,
		RootPath:   item.RootPath,
	}
	return &info
}

// getIdentityForFS 根据文件系统类型获取文件身份信息。
// 本地文件系统使用平台特定实现，远程文件系统返回空信息。
func (a *app) getIdentityForFS(fs vfs.FileSystem, path string, stat os.FileInfo) fileIdentityData {
	switch fs.(type) {
	case *vfs.LocalFS:
		return getFileIdentity(path, stat)
	case *vfs.SFTPFS:
		if sys, ok := stat.Sys().(*sftp.FileStat); ok {
			return fileIdentityData{
				UID:              int(sys.UID),
				GID:              int(sys.GID),
				IdentityPlatform: "sftp",
				OwnerID:          strconv.Itoa(int(sys.UID)),
				GroupID:          strconv.Itoa(int(sys.GID)),
			}
		}
	}
	return fileIdentityData{}
}

// crossFSCopy 在不同文件系统之间复制文件或目录，使用 ReadFile/WriteFile 回退。
func crossFSCopy(srcFS vfs.FileSystem, srcPath string, dstFS vfs.FileSystem, dstPath string) error {
	srcStat, err := srcFS.Lstat(srcPath)
	if err != nil {
		return err
	}
	if srcStat.IsDir() {
		return crossFSCopyDir(srcFS, srcPath, dstFS, dstPath, srcStat.Mode())
	}
	return crossFSCopyFile(srcFS, srcPath, dstFS, dstPath, srcStat.Mode())
}

func crossFSCopyDir(srcFS vfs.FileSystem, srcPath string, dstFS vfs.FileSystem, dstPath string, mode os.FileMode) error {
	if err := dstFS.MkdirAll(dstPath, mode.Perm()); err != nil {
		return err
	}
	entries, err := srcFS.ReadDir(srcPath)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		srcChild := path.Join(srcPath, entry.Name())
		dstChild := path.Join(dstPath, entry.Name())
		if err := crossFSCopy(srcFS, srcChild, dstFS, dstChild); err != nil {
			return err
		}
	}
	return nil
}

func crossFSCopyFile(srcFS vfs.FileSystem, srcPath string, dstFS vfs.FileSystem, dstPath string, mode os.FileMode) error {
	if err := dstFS.MkdirAll(path.Dir(dstPath), 0o755); err != nil {
		return err
	}
	data, err := srcFS.ReadFile(srcPath)
	if err != nil {
		return err
	}
	return dstFS.WriteFile(dstPath, data)
}

// currentPort 返回当前配置中的监听端口。
func (a *app) currentPort() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.config.Port
}

// currentRootPaths 返回运行期根目录路径列表，用于启动日志。
func (a *app) currentRootPaths() []string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]string, 0, len(a.roots))
	for _, rf := range a.roots {
		out = append(out, rf.rootPath)
	}
	return out
}

// resolvePath 将相对路径解析为绝对路径，并校验其仍位于允许的根目录范围内。
// resolveFS 根据根目录索引获取对应的文件系统实现和解析后的访问路径。
func (a *app) resolveFS(filePath string, rootIndex int) (vfs.FileSystem, string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if len(a.roots) == 0 {
		return nil, "", errNoRootPath
	}
	if rootIndex < 0 || rootIndex >= len(a.roots) {
		rootIndex = 0
	}

	rf := a.roots[rootIndex]
	if rf.isLocal {
		rootDir := rf.rootPath
		resolved := filepath.Join(rootDir, filepath.FromSlash(filePath))
		resolvedAbs, err := filepath.Abs(resolved)
		if err != nil {
			return nil, "", errInvalidPath
		}
		if !samePath(resolvedAbs, rootDir) && !strings.HasPrefix(withTrailingSep(resolvedAbs), withTrailingSep(rootDir)) {
			return nil, "", errPathTraversal
		}
		return rf.fs, resolvedAbs, nil
	}

	// 远程文件系统：使用前斜杠路径拼接
	joined := path.Join(rf.rootPath, filePath)
	cleanRoot := path.Clean(rf.rootPath)
	cleanJoined := path.Clean(joined)
	if cleanJoined != cleanRoot {
		rootPrefix := cleanRoot
		if rootPrefix != "/" {
			rootPrefix += "/"
		}
		if !strings.HasPrefix(cleanJoined, rootPrefix) {
			return nil, "", errPathTraversal
		}
	}
	return rf.fs, cleanJoined, nil
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
			itemType := strings.ToLower(strings.TrimSpace(item.Type))
			// 本地目录必须要有 path，远程目录可以不设 path
			if strings.TrimSpace(item.Path) != "" || (itemType != "" && itemType != "local") {
				out = append(out, item)
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

// sortPinnedNodes 将收藏节点置顶，其余按目录优先、名称排序。
func sortPinnedNodes(nodes []fileNode) {
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].Pinned != nodes[j].Pinned {
			return nodes[i].Pinned
		}
		if nodes[i].IsDirectory != nodes[j].IsDirectory {
			return nodes[i].IsDirectory
		}
		return strings.ToLower(nodes[i].Name) < strings.ToLower(nodes[j].Name)
	})
}

// isTextFile 根据文件路径规则和内容头部判断是否为文本文件。
// header 是文件开头若干字节，为空时只做规则判断。
func (a *app) isTextFile(filePath string, header []byte) (bool, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	baseName := strings.ToLower(filepath.Base(filePath))
	if a.isConfiguredBinaryFile(ext, baseName) {
		return false, nil
	}

	if len(header) > 0 {
		for _, sig := range binarySignatures {
			if len(header) >= len(sig) && bytes.Equal(header[:len(sig)], sig) {
				return false, nil
			}
		}
		if hasNullByte(header) {
			return false, nil
		}
		if isLikelyTextContent(header) {
			return true, nil
		}
	}

	if a.isConfiguredTextFile(ext, baseName) {
		return true, nil
	}

	if ext != "" {
		contentType := mime.TypeByExtension(ext)
		if strings.HasPrefix(contentType, "text/") || contentType == "application/json" || contentType == "application/xml" || strings.HasSuffix(contentType, "+xml") {
			return true, nil
		}
	}

	if len(header) > 0 {
		contentType := http.DetectContentType(header)
		if strings.HasPrefix(contentType, "text/") {
			return true, nil
		}
		if contentType == "application/octet-stream" && isLikelyUTF8(header) {
			return true, nil
		}
	}
	return false, nil
}

// isConfiguredTextFile 判断文件是否命中了文本规则。
func (a *app) isConfiguredTextFile(ext, baseName string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	_, extOK := a.textExtensions[ext]
	_, nameOK := a.textFileNames[baseName]
	return extOK || nameOK
}

// isConfiguredBinaryFile 判断文件是否命中了二进制规则。
func (a *app) isConfiguredBinaryFile(ext, baseName string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	_, extOK := a.binaryExtensions[ext]
	_, nameOK := a.binaryFileNames[baseName]
	return extOK || nameOK
}

// hasNullByte 判断内容中是否包含明显的二进制空字节。
func hasNullByte(data []byte) bool {
	for _, b := range data {
		if b == 0 {
			return true
		}
	}
	return false
}

// isLikelyTextContent 根据可打印字符比例粗略判断文本内容。
func isLikelyTextContent(data []byte) bool {
	if len(data) == 0 {
		return true
	}
	if !isLikelyUTF8(data) {
		return false
	}

	printable := 0
	for _, b := range data {
		switch {
		case b == '\n' || b == '\r' || b == '\t':
			printable++
		case b >= 0x20 && b < 0x7F:
			printable++
		case b >= 0x80:
			printable++
		}
	}

	return float64(printable)/float64(len(data)) >= 0.85
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
	errNoRootPath = appError{Status: http.StatusBadRequest, Message: "未配置可用的根目录"}
	// errInvalidPath 表示路径格式本身无法被安全解析。
	errInvalidPath = appError{Status: http.StatusBadRequest, Message: "路径无效"}
)

// appError 表示带有 HTTP 状态码的业务错误。
type appError struct {
	// Status 表示最终返回给客户端的 HTTP 状态码。
	Status int
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
