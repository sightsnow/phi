//go:build windows

package platform

import (
	"strings"

	"golang.org/x/sys/windows"
)

func defaultWindowsNamedPipeName(base string) string {
	suffix := currentUserSID()
	if suffix == "" {
		return base
	}
	return base + "-" + suffix
}

func CurrentUserPipeSecurityDescriptor() string {
	sid := currentUserSID()
	if sid == "" {
		return ""
	}
	return "D:P(A;;GA;;;" + sid + ")"
}

func currentUserSID() string {
	tokenUser, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil || tokenUser == nil || tokenUser.User.Sid == nil {
		return ""
	}
	return strings.TrimSpace(tokenUser.User.Sid.String())
}
