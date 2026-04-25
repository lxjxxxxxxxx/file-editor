//go:build windows

package main

import (
	"os"
	"runtime"
	"syscall"
	"unsafe"
)

const (
	seFileObject               = 1
	ownerSecurityInformation   = 0x00000001
	groupSecurityInformation   = 0x00000002
	ownerAndGroupSecurityQuery = ownerSecurityInformation | groupSecurityInformation
)

var (
	advapi32                   = syscall.NewLazyDLL("advapi32.dll")
	kernel32                   = syscall.NewLazyDLL("kernel32.dll")
	procGetNamedSecurityInfoW  = advapi32.NewProc("GetNamedSecurityInfoW")
	procLookupAccountSidW      = advapi32.NewProc("LookupAccountSidW")
	procConvertSidToStringSidW = advapi32.NewProc("ConvertSidToStringSidW")
	procLocalFree              = kernel32.NewProc("LocalFree")
)

func getFileIdentity(realPath string, info os.FileInfo) fileIdentityData {
	identity := fileIdentityData{IdentityPlatform: runtime.GOOS}
	pathPtr, err := syscall.UTF16PtrFromString(realPath)
	if err != nil {
		return identity
	}

	var ownerSid uintptr
	var groupSid uintptr
	var securityDescriptor uintptr
	ret, _, _ := procGetNamedSecurityInfoW.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		uintptr(seFileObject),
		uintptr(ownerAndGroupSecurityQuery),
		uintptr(unsafe.Pointer(&ownerSid)),
		uintptr(unsafe.Pointer(&groupSid)),
		0,
		0,
		uintptr(unsafe.Pointer(&securityDescriptor)),
	)
	if ret != 0 {
		return identity
	}
	if securityDescriptor != 0 {
		defer procLocalFree.Call(securityDescriptor)
	}

	if ownerSid != 0 {
		identity.Owner, identity.OwnerID = lookupAccountSid(ownerSid)
	}
	if groupSid != 0 {
		identity.Group, identity.GroupID = lookupAccountSid(groupSid)
	}
	return identity
}

func lookupAccountSid(sid uintptr) (string, string) {
	sidString := sidToString(sid)

	var nameLen uint32
	var domainLen uint32
	var sidUse uint32
	procLookupAccountSidW.Call(
		0,
		sid,
		0,
		uintptr(unsafe.Pointer(&nameLen)),
		0,
		uintptr(unsafe.Pointer(&domainLen)),
		uintptr(unsafe.Pointer(&sidUse)),
	)
	if nameLen == 0 {
		return "", sidString
	}

	name := make([]uint16, nameLen)
	namePtr := uintptr(unsafe.Pointer(&name[0]))
	var domain []uint16
	var domainPtr uintptr
	if domainLen > 0 {
		domain = make([]uint16, domainLen)
		domainPtr = uintptr(unsafe.Pointer(&domain[0]))
	}

	ret, _, _ := procLookupAccountSidW.Call(
		0,
		sid,
		namePtr,
		uintptr(unsafe.Pointer(&nameLen)),
		domainPtr,
		uintptr(unsafe.Pointer(&domainLen)),
		uintptr(unsafe.Pointer(&sidUse)),
	)
	if ret == 0 {
		return "", sidString
	}

	accountName := syscall.UTF16ToString(name)
	domainName := syscall.UTF16ToString(domain)
	if domainName != "" {
		accountName = domainName + `\` + accountName
	}
	return accountName, sidString
}

func sidToString(sid uintptr) string {
	var sidStringPtr uintptr
	ret, _, _ := procConvertSidToStringSidW.Call(sid, uintptr(unsafe.Pointer(&sidStringPtr)))
	if ret == 0 || sidStringPtr == 0 {
		return ""
	}
	defer procLocalFree.Call(sidStringPtr)
	return utf16PtrToString((*uint16)(unsafe.Pointer(sidStringPtr)))
}

func utf16PtrToString(ptr *uint16) string {
	if ptr == nil {
		return ""
	}

	var data []uint16
	for offset := uintptr(0); ; offset += unsafe.Sizeof(*ptr) {
		value := *(*uint16)(unsafe.Pointer(uintptr(unsafe.Pointer(ptr)) + offset))
		if value == 0 {
			break
		}
		data = append(data, value)
	}
	return syscall.UTF16ToString(data)
}
