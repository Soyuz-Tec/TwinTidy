package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"image/png"
	"io"
	"os"
	"strings"
	"testing"
)

func TestArchitectureResourceObjects(t *testing.T) {
	tests := []struct {
		path    string
		machine uint16
	}{
		{path: "rsrc_windows_amd64.syso", machine: 0x8664},
		{path: "rsrc_windows_arm64.syso", machine: 0xaa64},
	}

	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			data, err := os.ReadFile(test.path)
			if err != nil {
				t.Fatalf("read resource object: %v", err)
			}
			if len(data) < 20 {
				t.Fatalf("resource object is too short: %d bytes", len(data))
			}
			if got := binary.LittleEndian.Uint16(data[:2]); got != test.machine {
				t.Fatalf("COFF machine = 0x%04x, want 0x%04x", got, test.machine)
			}
			if !bytes.Contains(data, []byte("Microsoft.Windows.Common-Controls")) {
				t.Fatal("resource object does not contain the required Common Controls manifest")
			}
		})
	}
}

func TestManifestSourceContainsProductionSettings(t *testing.T) {
	manifest, err := os.ReadFile("twintidy.manifest")
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}

	for _, required := range [][]byte{
		[]byte("Microsoft.Windows.Common-Controls"),
		[]byte("PerMonitorV2, PerMonitor"),
		[]byte("longPathAware"),
	} {
		if !bytes.Contains(manifest, required) {
			t.Fatalf("manifest does not contain %q", required)
		}
	}
	if err := validateManifestExecutionPolicy(bytes.NewReader(manifest)); err != nil {
		t.Fatalf("manifest execution policy: %v", err)
	}
}

func TestManifestExecutionPolicyRejectsCommentBypass(t *testing.T) {
	manifest := `<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0">
	<!-- requestedExecutionLevel level="asInvoker" uiAccess="false" -->
	<trustInfo xmlns="urn:schemas-microsoft-com:asm.v3"><security><requestedPrivileges>
	<requestedExecutionLevel level="requireAdministrator" uiAccess="true"/>
	</requestedPrivileges></security></trustInfo></assembly>`
	if err := validateManifestExecutionPolicy(strings.NewReader(manifest)); err == nil {
		t.Fatal("unsafe active policy was accepted because safe settings appeared only in a comment")
	}
}

func TestManifestExecutionPolicyRejectsDuplicateElements(t *testing.T) {
	manifest := `<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0">
	<trustInfo xmlns="urn:schemas-microsoft-com:asm.v3"><security><requestedPrivileges>
	<requestedExecutionLevel level="asInvoker" uiAccess="false"/>
	<requestedExecutionLevel level="asInvoker" uiAccess="false"/>
	</requestedPrivileges></security></trustInfo></assembly>`
	if err := validateManifestExecutionPolicy(strings.NewReader(manifest)); err == nil {
		t.Fatal("duplicate requestedExecutionLevel elements were accepted")
	}
}

func TestWinresSourceIncludesIconManifestAndVersion(t *testing.T) {
	data, err := os.ReadFile("winres/winres.json")
	if err != nil {
		t.Fatalf("read winres config: %v", err)
	}

	var config map[string]json.RawMessage
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("parse winres config: %v", err)
	}
	for _, resourceType := range []string{"RT_GROUP_ICON", "RT_MANIFEST", "RT_VERSION"} {
		if len(config[resourceType]) == 0 {
			t.Fatalf("winres config is missing %s", resourceType)
		}
	}

	icon, err := os.Stat("winres/icon.png")
	if err != nil {
		t.Fatalf("stat icon source: %v", err)
	}
	if icon.Size() == 0 {
		t.Fatal("icon source is empty")
	}
}

func TestIconIsHighResolutionWithTransparentBackground(t *testing.T) {
	file, err := os.Open("winres/icon.png")
	if err != nil {
		t.Fatalf("open icon source: %v", err)
	}
	defer file.Close()

	icon, err := png.Decode(file)
	if err != nil {
		t.Fatalf("decode icon source: %v", err)
	}
	bounds := icon.Bounds()
	if bounds.Dx() != bounds.Dy() {
		t.Fatalf("icon dimensions are %dx%d, want square", bounds.Dx(), bounds.Dy())
	}
	if bounds.Dx() < 512 {
		t.Fatalf("icon is only %dx%d, want at least 512x512", bounds.Dx(), bounds.Dy())
	}

	corners := [][2]int{
		{bounds.Min.X, bounds.Min.Y},
		{bounds.Max.X - 1, bounds.Min.Y},
		{bounds.Min.X, bounds.Max.Y - 1},
		{bounds.Max.X - 1, bounds.Max.Y - 1},
	}
	for _, corner := range corners {
		_, _, _, alpha := icon.At(corner[0], corner[1]).RGBA()
		if alpha != 0 {
			t.Fatalf("icon corner (%d,%d) alpha = %d, want transparent", corner[0], corner[1], alpha)
		}
	}

	opaquePixels := 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			_, _, _, alpha := icon.At(x, y).RGBA()
			if alpha == 0xffff {
				opaquePixels++
			}
		}
	}
	minimumOpaquePixels := bounds.Dx() * bounds.Dy() / 100
	if opaquePixels < minimumOpaquePixels {
		t.Fatalf("icon has %d fully opaque pixels, want at least %d", opaquePixels, minimumOpaquePixels)
	}
}

func validateManifestExecutionPolicy(reader io.Reader) error {
	const (
		assemblyV1 = "urn:schemas-microsoft-com:asm.v1"
		assemblyV3 = "urn:schemas-microsoft-com:asm.v3"
	)

	decoder := xml.NewDecoder(reader)
	decoder.Strict = true
	stack := make([]xml.Name, 0, 8)
	executionLevelCount := 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("parse XML: %w", err)
		}

		switch typed := token.(type) {
		case xml.StartElement:
			stack = append(stack, typed.Name)
			if typed.Name.Local != "requestedExecutionLevel" {
				continue
			}
			executionLevelCount++
			expectedPath := []xml.Name{
				{Space: assemblyV1, Local: "assembly"},
				{Space: assemblyV3, Local: "trustInfo"},
				{Space: assemblyV3, Local: "security"},
				{Space: assemblyV3, Local: "requestedPrivileges"},
				{Space: assemblyV3, Local: "requestedExecutionLevel"},
			}
			if len(stack) != len(expectedPath) {
				return fmt.Errorf("requestedExecutionLevel is outside the active trustInfo policy path")
			}
			for index := range expectedPath {
				if stack[index] != expectedPath[index] {
					return fmt.Errorf("requestedExecutionLevel is outside the active trustInfo policy path")
				}
			}

			attributes := make(map[string]string, len(typed.Attr))
			for _, attribute := range typed.Attr {
				if attribute.Name.Space != "" || (attribute.Name.Local != "level" && attribute.Name.Local != "uiAccess") {
					return fmt.Errorf("unexpected requestedExecutionLevel attribute %q", attribute.Name.Local)
				}
				if _, exists := attributes[attribute.Name.Local]; exists {
					return fmt.Errorf("duplicate requestedExecutionLevel attribute %q", attribute.Name.Local)
				}
				attributes[attribute.Name.Local] = attribute.Value
			}
			if len(attributes) != 2 || attributes["level"] != "asInvoker" || attributes["uiAccess"] != "false" {
				return fmt.Errorf("requestedExecutionLevel must be exactly level=asInvoker and uiAccess=false")
			}
		case xml.EndElement:
			if len(stack) == 0 || stack[len(stack)-1] != typed.Name {
				return fmt.Errorf("invalid XML element nesting")
			}
			stack = stack[:len(stack)-1]
		}
	}
	if executionLevelCount != 1 {
		return fmt.Errorf("requestedExecutionLevel count is %d, want exactly 1", executionLevelCount)
	}
	return nil
}
