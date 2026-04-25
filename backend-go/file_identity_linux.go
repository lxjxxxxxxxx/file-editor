//go:build linux

package main

import (
	"os"
	"os/user"
	"runtime"
	"strconv"
	"syscall"
)

func getFileIdentity(realPath string, info os.FileInfo) fileIdentityData {
	identity := fileIdentityData{IdentityPlatform: runtime.GOOS}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return identity
	}

	identity.UID = int(stat.Uid)
	identity.GID = int(stat.Gid)
	identity.OwnerID = strconv.FormatUint(uint64(stat.Uid), 10)
	identity.GroupID = strconv.FormatUint(uint64(stat.Gid), 10)

	if owner, err := user.LookupId(identity.OwnerID); err == nil {
		identity.Owner = owner.Username
	}
	if group, err := user.LookupGroupId(identity.GroupID); err == nil {
		identity.Group = group.Name
	}
	return identity
}
