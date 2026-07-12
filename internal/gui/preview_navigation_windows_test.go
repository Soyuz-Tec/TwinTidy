//go:build windows

package gui

import "testing"

func TestPreviewNavigationAllowsOnlyTheGeneratedComparisonArtifact(t *testing.T) {
	allowed := previewArtifactFileURL(`C:\Temp\twintidy-preview-safe\comparison.html`)
	if !previewNavigationAllowed(allowed, allowed) {
		t.Fatal("generated comparison artifact was rejected")
	}
	if !previewNavigationAllowed("about:blank", allowed) {
		t.Fatal("inert initial browser page was rejected")
	}
	for _, blocked := range []string{
		previewArtifactFileURL(`C:\Users\Alice\Documents\untrusted.docm`),
		"https://example.invalid/",
		"javascript:alert(1)",
	} {
		if previewNavigationAllowed(blocked, allowed) {
			t.Fatalf("untrusted browser navigation was accepted: %q", blocked)
		}
	}
}
