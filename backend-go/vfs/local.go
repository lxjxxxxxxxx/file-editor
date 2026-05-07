package vfs

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// LocalFS 是对本地文件系统 os 操作的封装，实现 FileSystem 接口。
type LocalFS struct{}

// NewLocalFS 创建本地文件系统实例。
func NewLocalFS() FileSystem {
	return &LocalFS{}
}

func (l *LocalFS) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func (l *LocalFS) WriteFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o644)
}

func (l *LocalFS) Remove(path string) error {
	return os.Remove(path)
}

func (l *LocalFS) RemoveAll(path string) error {
	return os.RemoveAll(path)
}

func (l *LocalFS) ReadDir(path string) ([]os.DirEntry, error) {
	return os.ReadDir(path)
}

func (l *LocalFS) Stat(path string) (os.FileInfo, error) {
	return os.Stat(path)
}

func (l *LocalFS) Lstat(path string) (os.FileInfo, error) {
	return os.Lstat(path)
}

func (l *LocalFS) MkdirAll(path string, perm fs.FileMode) error {
	return os.MkdirAll(path, perm)
}

func (l *LocalFS) CreateFile(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	return f.Close()
}

func (l *LocalFS) Rename(oldPath, newPath string) error {
	return os.Rename(oldPath, newPath)
}

func (l *LocalFS) Chmod(path string, mode fs.FileMode) error {
	return os.Chmod(path, mode)
}

func (l *LocalFS) ReadFileHeader(path string, n int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, n)
	count, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return nil, err
	}
	return buf[:count], nil
}

// Copy 使用 os 原生方式递归复制文件或目录。
func (l *LocalFS) Copy(src, dst string) error {
	stat, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if stat.IsDir() {
		return copyDir(src, dst, stat.Mode())
	}
	return copyFile(src, dst, stat.Mode())
}

// IsCrossDeviceError 判断错误是否由跨设备移动导致。
func IsCrossDeviceError(err error) bool {
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
		if err := copyDirOrFile(srcChild, dstChild); err != nil {
			return err
		}
	}
	return nil
}

// copyDirOrFile 根据源类型递归复制文件或目录。
func copyDirOrFile(src, dst string) error {
	stat, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if stat.IsDir() {
		return copyDir(src, dst, stat.Mode())
	}
	return copyFile(src, dst, stat.Mode())
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

// Move 是 Rename 的增强版，同设备时直接重命名，跨设备时回退为复制后删除。
func Move(fs FileSystem, src, dst string) error {
	if err := fs.MkdirAll(dirOf(dst), 0o755); err != nil {
		return err
	}
	if err := fs.Rename(src, dst); err == nil {
		return nil
	} else if !IsCrossDeviceError(err) {
		return err
	}
	if err := fs.Copy(src, dst); err != nil {
		return err
	}
	return fs.RemoveAll(src)
}

// dirOf 返回路径的父目录。
func dirOf(path string) string {
	idx := strings.LastIndexAny(path, "/\\")
	if idx == -1 {
		return "."
	}
	return path[:idx]
}

// IsPathInsideOrSame 判断子路径是否等于父路径或位于父路径内部。
// 适用于本地（Linux 前斜杠）和远程路径（前斜杠）。
func IsPathInsideOrSame(parent, child string) bool {
	cleanParent := filepath.Clean(parent)
	cleanChild := filepath.Clean(child)
	if cleanParent == cleanChild {
		return true
	}
	sep := "/"
	if !strings.HasSuffix(cleanParent, sep) {
		cleanParent += sep
	}
	return strings.HasPrefix(cleanChild, cleanParent)
}

// IsNotExist 判断错误是否为"路径不存在"。
func IsNotExist(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, os.ErrNotExist) || strings.Contains(strings.ToLower(err.Error()), "does not exist") ||
		strings.Contains(strings.ToLower(err.Error()), "no such file")
}
