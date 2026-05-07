package vfs

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// SFTPConfig 存储 SFTP 连接的配置信息。
type SFTPConfig struct {
	Host       string // 远程主机地址
	Port       int    // SSH 端口，默认 22
	Username   string // 登录用户名
	Password   string // 密码认证时的密码
	AuthMethod string // 认证方式："password" 或 "key"
	KeyPath    string // 密钥文件路径（authMethod=key 时使用）
	KeyData    string // 密钥内容原文（不通过文件读取时使用）
}

// SFTPFS 通过 SSH/SFTP 协议操作远程文件系统，实现 FileSystem 接口。
type SFTPFS struct {
	config  SFTPConfig
	root    string // 远程根目录
	mu      sync.Mutex
	sshCli  *ssh.Client
	sftpCli *sftp.Client
}

// TestSFTPConnection 测试 SFTP 连接是否可用，成功返回 nil，失败返回错误原因。
func TestSFTPConnection(config SFTPConfig) error {
	addr := net.JoinHostPort(config.Host, strconv.Itoa(config.Port))
	if config.Port == 0 {
		addr = net.JoinHostPort(config.Host, "22")
	}

	auth, err := buildAuthForConfig(config)
	if err != nil {
		return fmt.Errorf("构建 SSH 认证失败: %w", err)
	}

	sshConfig := &ssh.ClientConfig{
		User:            config.Username,
		Auth:            auth,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	sshCli, err := ssh.Dial("tcp", addr, sshConfig)
	if err != nil {
		return err
	}
	defer sshCli.Close()

	sftpCli, err := sftp.NewClient(sshCli)
	if err != nil {
		return fmt.Errorf("SFTP 初始化失败: %w", err)
	}
	sftpCli.Close()
	return nil
}

// buildAuthForConfig 根据配置构建 SSH 认证方式，供外部测试使用。
func buildAuthForConfig(config SFTPConfig) ([]ssh.AuthMethod, error) {
	if config.AuthMethod == "key" {
		var keyData []byte
		if config.KeyData != "" {
			keyData = []byte(config.KeyData)
		} else if config.KeyPath != "" {
			var err error
			keyData, err = os.ReadFile(config.KeyPath)
			if err != nil {
				return nil, fmt.Errorf("读取密钥文件失败: %w", err)
			}
		} else {
			return nil, errors.New("密钥认证需要提供 keyPath 或 keyData")
		}
		signer, err := ssh.ParsePrivateKey(keyData)
		if err != nil {
			return nil, fmt.Errorf("解析 SSH 密钥失败: %w", err)
		}
		return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil
	}
	return []ssh.AuthMethod{ssh.Password(config.Password)}, nil
}

// NewSFTPFS 创建 SFTP 文件系统实例，root 为远程根目录路径。
func NewSFTPFS(config SFTPConfig, root string) FileSystem {
	if config.Port == 0 {
		config.Port = 22
	}
	return &SFTPFS{
		config: config,
		root:   root,
	}
}

// connect 建立 SSH 连接并初始化 SFTP 客户端。
func (s *SFTPFS) connect() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.sftpCli != nil {
		_, _, err := s.sshCli.SendRequest("keepalive", true, nil)
		if err == nil {
			return nil
		}
		s.close()
	}

	addr := net.JoinHostPort(s.config.Host, strconv.Itoa(s.config.Port))
	auth, err := s.buildAuth()
	if err != nil {
		return fmt.Errorf("构建 SSH 认证失败: %w", err)
	}

	sshConfig := &ssh.ClientConfig{
		User:            s.config.Username,
		Auth:            auth,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	sshCli, err := ssh.Dial("tcp", addr, sshConfig)
	if err != nil {
		return fmt.Errorf("SSH 连接失败: %w", err)
	}
	s.sshCli = sshCli

	sftpCli, err := sftp.NewClient(sshCli)
	if err != nil {
		sshCli.Close()
		return fmt.Errorf("SFTP 初始化失败: %w", err)
	}
	s.sftpCli = sftpCli

	return nil
}

// buildAuth 根据配置构建 SSH 认证方式。
func (s *SFTPFS) buildAuth() ([]ssh.AuthMethod, error) {
	return buildAuthForConfig(s.config)
}

// close 关闭 SSH 和 SFTP 连接。
func (s *SFTPFS) close() {
	if s.sftpCli != nil {
		s.sftpCli.Close()
		s.sftpCli = nil
	}
	if s.sshCli != nil {
		s.sshCli.Close()
		s.sshCli = nil
	}
}

// resolve 验证已拼入 root 的绝对路径是否在访问范围内，不做二次拼接。
// 传入的 absolutePath 来自 resolveFS（main.go），已是 root + relativePath 的结果。
func (s *SFTPFS) resolve(absolutePath string) (string, error) {
	cleanPath := path.Clean(absolutePath)
	cleanRoot := path.Clean(s.root)
	if cleanPath == cleanRoot {
		return cleanPath, nil
	}
	rootPrefix := cleanRoot
	if rootPrefix != "/" {
		rootPrefix += "/"
	}
	if !strings.HasPrefix(cleanPath, rootPrefix) {
		return "", errors.New("路径越权访问被拒绝")
	}
	return cleanPath, nil
}

// withClient 获取 SFTP 客户端并执行操作，自动处理重连。
func (s *SFTPFS) withClient(fn func(*sftp.Client) error) error {
	if err := s.connect(); err != nil {
		return err
	}
	return fn(s.sftpCli)
}

func (s *SFTPFS) ReadFile(filePath string) ([]byte, error) {
	var data []byte
	err := s.withClient(func(cli *sftp.Client) error {
		real, err := s.resolve(filePath)
		if err != nil {
			return err
		}
		f, err := cli.Open(real)
		if err != nil {
			return err
		}
		defer f.Close()
		data, err = io.ReadAll(f)
		return err
	})
	return data, err
}

func (s *SFTPFS) WriteFile(filePath string, data []byte) error {
	return s.withClient(func(cli *sftp.Client) error {
		real, err := s.resolve(filePath)
		if err != nil {
			return err
		}
		if err := mkdirAllSFTP(cli, path.Dir(real)); err != nil {
			return err
		}
		f, err := cli.Create(real)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = f.Write(data)
		return err
	})
}

func (s *SFTPFS) Remove(filePath string) error {
	return s.withClient(func(cli *sftp.Client) error {
		real, err := s.resolve(filePath)
		if err != nil {
			return err
		}
		return cli.Remove(real)
	})
}

func (s *SFTPFS) RemoveAll(filePath string) error {
	return s.withClient(func(cli *sftp.Client) error {
		real, err := s.resolve(filePath)
		if err != nil {
			return err
		}
		return removeAllSFTP(cli, real)
	})
}

func (s *SFTPFS) ReadDir(dirPath string) ([]os.DirEntry, error) {
	var entries []os.DirEntry
	err := s.withClient(func(cli *sftp.Client) error {
		real, err := s.resolve(dirPath)
		if err != nil {
			return err
		}
		remoteInfos, err := cli.ReadDir(real)
		if err != nil {
			return err
		}
		entries = make([]os.DirEntry, 0, len(remoteInfos))
		for _, ri := range remoteInfos {
			entries = append(entries, &sftpDirEntry{info: ri})
		}
		return nil
	})
	return entries, err
}

func (s *SFTPFS) Stat(filePath string) (os.FileInfo, error) {
	var info os.FileInfo
	err := s.withClient(func(cli *sftp.Client) error {
		real, err := s.resolve(filePath)
		if err != nil {
			return err
		}
		info, err = cli.Stat(real)
		return err
	})
	return info, err
}

func (s *SFTPFS) Lstat(filePath string) (os.FileInfo, error) {
	var info os.FileInfo
	err := s.withClient(func(cli *sftp.Client) error {
		real, err := s.resolve(filePath)
		if err != nil {
			return err
		}
		info, err = cli.Lstat(real)
		return err
	})
	return info, err
}

func (s *SFTPFS) MkdirAll(dirPath string, _ fs.FileMode) error {
	return s.withClient(func(cli *sftp.Client) error {
		real, err := s.resolve(dirPath)
		if err != nil {
			return err
		}
		return mkdirAllSFTP(cli, real)
	})
}

func (s *SFTPFS) CreateFile(filePath string) error {
	return s.withClient(func(cli *sftp.Client) error {
		real, err := s.resolve(filePath)
		if err != nil {
			return err
		}
		// 确保父目录存在
		if err := mkdirAllSFTP(cli, path.Dir(real)); err != nil {
			return err
		}
		f, err := cli.OpenFile(real, os.O_CREATE|os.O_EXCL|os.O_WRONLY)
		if err != nil {
			return err
		}
		return f.Close()
	})
}

func (s *SFTPFS) Rename(oldPath, newPath string) error {
	return s.withClient(func(cli *sftp.Client) error {
		realOld, err := s.resolve(oldPath)
		if err != nil {
			return err
		}
		realNew, err := s.resolve(newPath)
		if err != nil {
			return err
		}
		if err := mkdirAllSFTP(cli, path.Dir(realNew)); err != nil {
			return err
		}
		return cli.Rename(realOld, realNew)
	})
}

func (s *SFTPFS) Chmod(filePath string, mode fs.FileMode) error {
	return s.withClient(func(cli *sftp.Client) error {
		real, err := s.resolve(filePath)
		if err != nil {
			return err
		}
		return cli.Chmod(real, mode)
	})
}

func (s *SFTPFS) ReadFileHeader(filePath string, n int) ([]byte, error) {
	var data []byte
	err := s.withClient(func(cli *sftp.Client) error {
		real, err := s.resolve(filePath)
		if err != nil {
			return err
		}
		f, err := cli.Open(real)
		if err != nil {
			return err
		}
		defer f.Close()
		buf := make([]byte, n)
		count, err := f.Read(buf)
		if err != nil && err != io.EOF {
			return err
		}
		data = buf[:count]
		return nil
	})
	return data, err
}

func (s *SFTPFS) Copy(src, dst string) error {
	return s.withClient(func(cli *sftp.Client) error {
		realSrc, err := s.resolve(src)
		if err != nil {
			return err
		}
		realDst, err := s.resolve(dst)
		if err != nil {
			return err
		}
		return copyPathSFTP(cli, realSrc, realDst)
	})
}

// sftpDirEntry 实现 os.DirEntry 接口，包装 sftp.FileInfo。
type sftpDirEntry struct {
	info os.FileInfo
}

func (e *sftpDirEntry) Name() string               { return e.info.Name() }
func (e *sftpDirEntry) IsDir() bool                 { return e.info.IsDir() }
func (e *sftpDirEntry) Type() fs.FileMode           { return e.info.Mode().Type() }
func (e *sftpDirEntry) Info() (os.FileInfo, error)  { return e.info, nil }

// removeAllSFTP 递归删除远程路径。
func removeAllSFTP(cli *sftp.Client, remotePath string) error {
	info, err := cli.Lstat(remotePath)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return cli.Remove(remotePath)
	}

	entries, err := cli.ReadDir(remotePath)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		childPath := path.Join(remotePath, entry.Name())
		if entry.IsDir() {
			if err := removeAllSFTP(cli, childPath); err != nil {
				return err
			}
		} else {
			if err := cli.Remove(childPath); err != nil {
				return err
			}
		}
	}
	return cli.RemoveDirectory(remotePath)
}

// mkdirAllSFTP 递归创建远程目录。
func mkdirAllSFTP(cli *sftp.Client, remotePath string) error {
	if remotePath == "." || remotePath == "/" {
		return nil
	}

	info, err := cli.Stat(remotePath)
	if err == nil && info.IsDir() {
		return nil
	}

	parent := path.Dir(remotePath)
	if parent != remotePath {
		if err := mkdirAllSFTP(cli, parent); err != nil {
			return err
		}
	}

	return cli.Mkdir(remotePath)
}

// copyPathSFTP 递归复制远程路径。
func copyPathSFTP(cli *sftp.Client, src, dst string) error {
	info, err := cli.Lstat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return copyDirSFTP(cli, src, dst, info.Mode())
	}
	return copyFileSFTP(cli, src, dst, info.Mode())
}

// copyDirSFTP 递归复制远程目录。
func copyDirSFTP(cli *sftp.Client, src, dst string, mode fs.FileMode) error {
	if err := mkdirAllSFTP(cli, dst); err != nil {
		return err
	}
	if err := cli.Chmod(dst, mode.Perm()); err != nil {
		return err
	}
	entries, err := cli.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		srcChild := path.Join(src, entry.Name())
		dstChild := path.Join(dst, entry.Name())
		if err := copyPathSFTP(cli, srcChild, dstChild); err != nil {
			return err
		}
	}
	return nil
}

// copyFileSFTP 复制单个远程文件。
func copyFileSFTP(cli *sftp.Client, src, dst string, mode fs.FileMode) error {
	if err := mkdirAllSFTP(cli, path.Dir(dst)); err != nil {
		return err
	}
	srcFile, err := cli.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := cli.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return err
	}
	return dstFile.Chmod(mode.Perm())
}
