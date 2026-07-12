//go:build windows

package gui

import (
	"net/url"
	"path/filepath"
	"strings"
)

func previewArtifactFileURL(path string) string {
	clean := filepath.ToSlash(filepath.Clean(path))
	if !strings.HasPrefix(clean, "/") {
		clean = "/" + clean
	}
	return (&url.URL{Scheme: "file", Path: clean}).String()
}

func previewNavigationAllowed(requested, allowed string) bool {
	if strings.EqualFold(requested, "about:blank") {
		return true
	}
	requestedPath, requestedOK := localFilePathFromURL(requested)
	allowedPath, allowedOK := localFilePathFromURL(allowed)
	return requestedOK && allowedOK && strings.EqualFold(requestedPath, allowedPath)
}

func localFilePathFromURL(value string) (string, bool) {
	parsed, err := url.Parse(value)
	if err != nil || !strings.EqualFold(parsed.Scheme, "file") || (parsed.Host != "" && !strings.EqualFold(parsed.Host, "localhost")) {
		return "", false
	}
	path := filepath.FromSlash(parsed.Path)
	if len(path) >= 3 && (path[0] == '\\' || path[0] == '/') && path[2] == ':' {
		path = path[1:]
	}
	if !filepath.IsAbs(path) {
		return "", false
	}
	return filepath.Clean(path), true
}
