package state

import (
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

func NormalizeWorkspaceKey(value string) string {
	return normalizeWorkspaceKeyForGOOS(runtime.GOOS, value)
}

func normalizeWorkspaceKeyForGOOS(goos, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	normalized := filepath.Clean(value)
	if normalized == "." {
		return ""
	}
	normalized = filepath.ToSlash(normalized)
	if strings.EqualFold(strings.TrimSpace(goos), "windows") {
		normalized = trimWindowsExtendedPathPrefix(normalized)
	}
	return normalized
}

func trimWindowsExtendedPathPrefix(value string) string {
	if strings.HasPrefix(value, "//?/") {
		if len(value) >= len("//?/UNC/") && strings.EqualFold(value[:len("//?/UNC/")], "//?/UNC/") {
			return "//" + value[len("//?/UNC/"):]
		}
		return value[len("//?/"):]
	}
	return value
}

func ResolveWorkspaceKey(values ...string) string {
	for _, value := range values {
		if normalized := NormalizeWorkspaceKey(value); normalized != "" {
			return normalized
		}
	}
	return ""
}

func ResolveWorkspaceRootOnHost(value string) (string, error) {
	absolute, err := filepath.Abs(strings.TrimSpace(value))
	if err != nil {
		return "", err
	}
	normalized := NormalizeWorkspaceKey(absolute)
	if resolved, err := filepath.EvalSymlinks(normalized); err == nil {
		normalized = NormalizeWorkspaceKey(resolved)
	}
	return normalized, nil
}

func WorkspaceShortName(value string) string {
	key := ResolveWorkspaceKey(value)
	if key == "" {
		return ""
	}
	short := strings.TrimSpace(path.Base(key))
	if short == "" || short == "." || short == "/" {
		return key
	}
	return short
}
