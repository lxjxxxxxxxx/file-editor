package vfs

import (
	"io/fs"
	"os"
)

// FileSystem 定义了所有文件系统需要实现的操作接口。
// 本地文件系统、SFTP、WebDAV、SMB、FTP 等都实现此接口。
type FileSystem interface {
	// ReadFile 读取指定路径文件的全部内容。
	ReadFile(path string) ([]byte, error)
	// WriteFile 将数据写入指定路径文件。
	WriteFile(path string, data []byte) error
	// Remove 删除指定路径的文件或空目录。
	Remove(path string) error
	// RemoveAll 递归删除指定路径的文件或目录树。
	RemoveAll(path string) error
	// ReadDir 读取目录下的所有条目。
	ReadDir(path string) ([]os.DirEntry, error)
	// Stat 获取文件或目录的状态信息。
	Stat(path string) (os.FileInfo, error)
	// Lstat 获取文件状态信息，不跟随符号链接。
	Lstat(path string) (os.FileInfo, error)
	// MkdirAll 递归创建目录。
	MkdirAll(path string, perm fs.FileMode) error
	// CreateFile 创建空文件，目标已存在时返回错误。
	CreateFile(path string) error
	// Rename 重命名或移动文件/目录（同文件系统内）。
	Rename(oldPath, newPath string) error
	// Chmod 修改文件或目录的权限。
	Chmod(path string, mode fs.FileMode) error
	// Copy 递归复制文件或目录到目标路径。
	Copy(src, dst string) error
	// ReadFileHeader 读取文件开头 n 字节，用于内容类型探测。
	ReadFileHeader(path string, n int) ([]byte, error)
}

// IdentityInfo 表示文件或目录的身份信息，与具体平台相关。
type IdentityInfo struct {
	UID              int
	GID              int
	IdentityPlatform string
	Owner            string
	Group            string
	OwnerID          string
	GroupID          string
}
