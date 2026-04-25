//go:build !linux && !windows

package main

import (
	"os"
	"runtime"
)

func getFileIdentity(realPath string, info os.FileInfo) fileIdentityData {
	return fileIdentityData{IdentityPlatform: runtime.GOOS}
}
