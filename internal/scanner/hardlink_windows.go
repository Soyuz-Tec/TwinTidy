//go:build windows

package scanner

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

func platformHardlinkSupported() bool {
	return true
}

// platformConsolidateTarget executes the ADR 0009 sequence for one verified
// candidate. Every destructive step acts through the candidate's verified
// handle rather than its path, so a concurrent path swap can never redirect
// the operation to a different file:
//
//  1. open the candidate with delete access and re-verify identity,
//     metadata, scope, and full content hash;
//  2. rename the candidate aside to a backup name via its handle;
//  3. create a hardlink to the keeper at the original path;
//  4. re-verify that the original path now resolves to the keeper identity;
//  5. dispose of the backup via the still-open candidate handle.
//
// A failure at any step leaves the content reachable and reports where.
func platformConsolidateTarget(ctx context.Context, keeperFile *os.File, keeperRecord FileRecord, target FileRecord, expectedHash string) HardlinkItemResult {
	targetFile, err := openVerifiedConsolidationTarget(ctx, target, expectedHash)
	if err != nil {
		return hardlinkItem(target.Path, hardlinkVerificationStatus(err), err)
	}
	defer targetFile.Close()

	keeperFinal, err := finalPathForOpenFile(keeperFile)
	if err != nil {
		return hardlinkItem(target.Path, HardlinkStatusFailed, fmt.Errorf("resolve keeper path: %w", err))
	}
	targetFinal, err := finalPathForOpenFile(targetFile)
	if err != nil {
		return hardlinkItem(target.Path, HardlinkStatusFailed, fmt.Errorf("resolve candidate path: %w", err))
	}
	backupPath, err := consolidationBackupPath(targetFinal)
	if err != nil {
		return hardlinkItem(target.Path, HardlinkStatusFailed, err)
	}
	if err := ctx.Err(); err != nil {
		return hardlinkItem(target.Path, HardlinkStatusCancelled, err)
	}

	// Step 2: rename the verified object aside, bound to its handle.
	if err := renameByHandle(targetFile, backupPath); err != nil {
		return hardlinkItem(target.Path, HardlinkStatusFailed, fmt.Errorf("rename candidate aside: %w", err))
	}

	// Step 3: link the keeper into the vacated path. On any failure the
	// handle-bound rename is reversed, restoring the original state.
	if err := ctx.Err(); err != nil {
		return restoreOrReport(targetFile, target.Path, backupPath, HardlinkStatusCancelled, err)
	}
	if err := os.Link(keeperFinal, targetFinal); err != nil {
		return restoreOrReport(targetFile, target.Path, backupPath, HardlinkStatusFailed,
			fmt.Errorf("create hardlink: %w", err))
	}

	// Step 4: prove the original path now resolves to the keeper's identity.
	// On mismatch nothing is deleted: an unknown object won the path race and
	// the user's content stays safe at the backup path.
	if err := verifyPathHasIdentity(targetFinal, keeperRecord.Identity); err != nil {
		result := hardlinkItem(target.Path, HardlinkStatusFailed,
			fmt.Errorf("post-link verification failed; original content retained at backup: %w", err))
		result.BackupPath = backupPath
		return result
	}

	// Step 5: dispose of the backup through the verified handle. A read-only
	// attribute is cleared first through the same handle; if disposal still
	// fails the backup is reported, never silently leaked.
	if err := disposeByHandle(targetFile); err != nil {
		result := hardlinkItem(target.Path, HardlinkStatusFailed,
			fmt.Errorf("hardlink created but backup could not be removed: %w", err))
		result.BackupPath = backupPath
		return result
	}
	return HardlinkItemResult{Path: target.Path, Status: HardlinkStatusConsolidated}
}

// openVerifiedConsolidationTarget mirrors openVerifiedRecord but opens the
// candidate with delete access so the rename and disposal steps stay bound to
// this exact verified object. Write and delete sharing are denied to others.
func openVerifiedConsolidationTarget(ctx context.Context, record FileRecord, expectedHash string) (*os.File, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file, snapshot, err := openScopedSnapshotWith(record, openConsolidationFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: source path disappeared", errFileChanged)
		}
		return nil, err
	}
	if snapshot.linkCount != 1 {
		_ = file.Close()
		return nil, errHardLinkedFile
	}
	if snapshot.namedStreams != 0 {
		_ = file.Close()
		return nil, errNamedStreamFile
	}
	if !recordMatchesSnapshot(record, snapshot) {
		_ = file.Close()
		return nil, errFileChanged
	}
	if err := verifyRecordContentHash(ctx, file, record, snapshot, expectedHash); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

// openConsolidationFile opens the candidate with read and delete access while
// denying write and delete sharing: the object cannot change or be removed by
// others, and this handle alone can rename and dispose of it.
func openConsolidationFile(path string) (*os.File, error) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	// FILE_WRITE_ATTRIBUTES lets disposal clear a read-only attribute through
	// this same handle; it is not part of Windows sharing enforcement, so it
	// does not weaken the deny-write/deny-delete exclusivity below.
	handle, err := windows.CreateFile(
		pathPtr,
		windows.GENERIC_READ|windows.DELETE|windows.FILE_WRITE_ATTRIBUTES,
		windows.FILE_SHARE_READ,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_SEQUENTIAL_SCAN,
		0,
	)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(handle), path)
	if file == nil {
		_ = windows.CloseHandle(handle)
		return nil, errors.New("create consolidation file handle")
	}
	return file, nil
}

// consolidationBackupPath returns an unpredictable sibling name for the
// rename-aside step. The name stays in the candidate's directory so the
// rename never crosses volumes or scopes.
func consolidationBackupPath(finalPath string) (string, error) {
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", fmt.Errorf("generate backup name: %w", err)
	}
	dir := filepath.Dir(finalPath)
	name := fmt.Sprintf(".twintidy-consolidate-%s.bak", hex.EncodeToString(suffix[:]))
	return filepath.Join(dir, name), nil
}

// restoreOrReport undoes the rename-aside through the still-open handle. If
// even the restore fails, the backup path is reported so no content is lost.
func restoreOrReport(file *os.File, originalPath string, backupPath string, status HardlinkStatus, cause error) HardlinkItemResult {
	if err := renameByHandle(file, originalPath); err != nil {
		result := hardlinkItem(originalPath, HardlinkStatusFailed,
			fmt.Errorf("%v; restore also failed (%v); original content retained at backup", cause, err))
		result.BackupPath = backupPath
		return result
	}
	return hardlinkItem(originalPath, status, cause)
}

// verifyPathHasIdentity opens the path fresh and requires the exact identity.
func verifyPathHasIdentity(path string, expected FileIdentity) error {
	file, snapshot, err := openFileSnapshot(path)
	if err != nil {
		return err
	}
	defer file.Close()
	if snapshot.identity != expected {
		return errors.New("path does not resolve to the keeper identity")
	}
	return nil
}

// renameByHandle renames the open file to newPath (same volume) using
// FILE_RENAME_INFO, binding the rename to the verified object rather than to
// any path. ReplaceIfExists stays false: consolidation never overwrites.
func renameByHandle(file *os.File, newPath string) error {
	utf16Path, err := windows.UTF16FromString(newPath)
	if err != nil {
		return err
	}
	// FILE_RENAME_INFO layout on 64-bit: union(ReplaceIfExists/Flags) at 0,
	// RootDirectory handle at 8, FileNameLength at 16, FileName at 20.
	nameBytes := (len(utf16Path) - 1) * 2
	buffer := make([]byte, 20+nameBytes+2)
	binary.LittleEndian.PutUint32(buffer[16:], uint32(nameBytes))
	for index, wide := range utf16Path[:len(utf16Path)-1] {
		binary.LittleEndian.PutUint16(buffer[20+index*2:], wide)
	}
	return windows.SetFileInformationByHandle(
		windows.Handle(file.Fd()),
		windows.FileRenameInfo,
		&buffer[0],
		uint32(len(buffer)),
	)
}

// disposeByHandle marks the open file delete-on-close. A read-only attribute
// blocks disposition, so it is cleared first through the same handle.
func disposeByHandle(file *os.File) error {
	handle := windows.Handle(file.Fd())
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return err
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_READONLY != 0 {
		basic := fileBasicInfo{FileAttributes: info.FileAttributes &^ windows.FILE_ATTRIBUTE_READONLY}
		if err := windows.SetFileInformationByHandle(
			handle,
			windows.FileBasicInfo,
			(*byte)(unsafe.Pointer(&basic)),
			uint32(unsafe.Sizeof(basic)),
		); err != nil {
			return fmt.Errorf("clear read-only attribute: %w", err)
		}
	}
	disposition := struct{ DeleteFile byte }{DeleteFile: 1}
	return windows.SetFileInformationByHandle(
		handle,
		windows.FileDispositionInfo,
		(*byte)(unsafe.Pointer(&disposition)),
		uint32(unsafe.Sizeof(disposition)),
	)
}

// fileBasicInfo mirrors FILE_BASIC_INFO. Zero timestamps mean "leave
// unchanged", so only the attribute field is populated.
type fileBasicInfo struct {
	CreationTime   int64
	LastAccessTime int64
	LastWriteTime  int64
	ChangeTime     int64
	FileAttributes uint32
	_              uint32
}
