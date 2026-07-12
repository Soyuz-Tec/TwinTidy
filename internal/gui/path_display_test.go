package gui

import (
	"strings"
	"testing"
)

func TestDisplayFilesystemPathEscapesBidiAndControlCharacters(t *testing.T) {
	raw := "C:\\Data\\report\u202Efdp.docx\t"
	displayed := displayFilesystemPath(raw)

	if !strings.HasPrefix(displayed, leftToRightIsolate) || !strings.HasSuffix(displayed, popDirectionalIsolate) {
		t.Fatalf("path is not directionally isolated: %q", displayed)
	}
	if strings.Contains(displayed, "\u202E") || strings.Contains(displayed, "\t") {
		t.Fatalf("path retained an active bidi or control character: %q", displayed)
	}
	if !strings.Contains(displayed, `\u202e`) || !strings.Contains(displayed, `\t`) {
		t.Fatalf("path did not expose escaped bidi/control characters: %q", displayed)
	}
}

func TestDisplayFilesystemPathKeepsOrdinaryUnicodeReadableAndIsolated(t *testing.T) {
	raw := `C:\Users\Alice\Documents\Résumé العربية.txt`
	displayed := displayFilesystemPath(raw)
	want := leftToRightIsolate + raw + popDirectionalIsolate
	if displayed != want {
		t.Fatalf("displayFilesystemPath() = %q, want %q", displayed, want)
	}
}
