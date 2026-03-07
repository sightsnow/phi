//go:build !windows

package platform

func defaultWindowsNamedPipeName(base string) string {
	return base
}

func CurrentUserPipeSecurityDescriptor() string {
	return ""
}
