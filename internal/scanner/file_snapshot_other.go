//go:build !windows

package scanner

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
)

// The production safety contract is implemented with Windows file IDs. Other
// platforms retain a deterministic best-effort identity so the scanner and its
// tests remain buildable; destructive operations must remain disabled there.
func platformFileSnapshot(_ *os.File, path string) (FileIdentity, uint32, uint32, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return FileIdentity{}, 0, 0, err
	}
	digest := sha256.Sum256([]byte(filepath.Clean(absPath)))
	identity := FileIdentity{VolumeSerial: 1}
	copy(identity.FileID[:], digest[:len(identity.FileID)])
	return identity, 1, 0, nil
}

func platformPathIdentity(_ *os.File, path string) (FileIdentity, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return FileIdentity{}, err
	}
	digest := sha256.Sum256([]byte(filepath.Clean(absPath)))
	identity := FileIdentity{VolumeSerial: 1}
	copy(identity.FileID[:], digest[:len(identity.FileID)])
	return identity, nil
}

func openVerificationFile(path string, _ bool) (*os.File, error) {
	return os.Open(path)
}

func pathIsTraversalReparsePoint(path string) (bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return false, err
	}
	return info.Mode()&os.ModeSymlink != 0, nil
}

func validateNoTraversalReparseComponents(path string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	for current := filepath.Clean(absPath); ; {
		unsafeTraversal, err := pathIsTraversalReparsePoint(current)
		if err != nil {
			return err
		}
		if unsafeTraversal {
			return fmt.Errorf("%w: path component %q redirects traversal", errReparsePoint, current)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return nil
		}
		current = parent
	}
}

func finalPathForOpenFile(file *os.File) (string, error) {
	resolved, err := filepath.EvalSymlinks(file.Name())
	if err != nil {
		return "", err
	}
	absPath, err := filepath.Abs(resolved)
	if err != nil {
		return "", err
	}
	return filepath.Clean(absPath), nil
}
