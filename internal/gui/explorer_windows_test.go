//go:build windows

package gui

import (
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestTrustedExplorerPathComesFromWindowsDirectoryNotPATH(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	got, err := trustedExplorerPath()
	if err != nil {
		t.Fatal(err)
	}
	windowsDir, err := windows.GetWindowsDirectory()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(windowsDir, "explorer.exe")
	if !strings.EqualFold(got, want) {
		t.Fatalf("trustedExplorerPath() = %q, want %q", got, want)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("trusted Explorer path is not absolute: %q", got)
	}
}
