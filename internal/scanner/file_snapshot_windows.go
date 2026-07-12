//go:build windows

package scanner

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

const reparseTagNameSurrogate = 0x20000000
const maxFileStreamInfoBytes = 4 * 1024 * 1024

type windowsFileIDInfo struct {
	VolumeSerialNumber uint64
	FileID             [16]byte
}

type windowsFileAttributeTagInfo struct {
	FileAttributes uint32
	ReparseTag     uint32
}

func platformFileSnapshot(file *os.File, _ string) (FileIdentity, uint32, uint32, error) {
	handle := windows.Handle(file.Fd())
	identity, err := platformPathIdentity(file, "")
	if err != nil {
		return FileIdentity{}, 0, 0, err
	}

	var handleInfo windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &handleInfo); err != nil {
		return FileIdentity{}, 0, 0, fmt.Errorf("read link count: %w", err)
	}

	var tagInfo windowsFileAttributeTagInfo
	if err := windows.GetFileInformationByHandleEx(
		handle,
		windows.FileAttributeTagInfo,
		(*byte)(unsafe.Pointer(&tagInfo)),
		uint32(unsafe.Sizeof(tagInfo)),
	); err != nil {
		return FileIdentity{}, 0, 0, fmt.Errorf("read reparse policy: %w", err)
	}
	if tagInfo.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 &&
		tagInfo.ReparseTag&reparseTagNameSurrogate != 0 {
		return FileIdentity{}, 0, 0, errReparsePoint
	}

	namedStreams, err := countNamedStreams(handle)
	if err != nil {
		return FileIdentity{}, 0, 0, fmt.Errorf("enumerate data streams: %w", err)
	}

	return identity, handleInfo.NumberOfLinks, namedStreams, nil
}

func platformPathIdentity(file *os.File, _ string) (FileIdentity, error) {
	var idInfo windowsFileIDInfo
	if err := windows.GetFileInformationByHandleEx(
		windows.Handle(file.Fd()),
		windows.FileIdInfo,
		(*byte)(unsafe.Pointer(&idInfo)),
		uint32(unsafe.Sizeof(idInfo)),
	); err != nil {
		return FileIdentity{}, fmt.Errorf("read path identity: %w", err)
	}
	return FileIdentity{
		VolumeSerial: idInfo.VolumeSerialNumber,
		FileID:       idInfo.FileID,
	}, nil
}

func countNamedStreams(handle windows.Handle) (uint32, error) {
	for bufferSize := 4096; bufferSize <= maxFileStreamInfoBytes; bufferSize *= 2 {
		buffer := make([]byte, bufferSize)
		err := windows.GetFileInformationByHandleEx(
			handle,
			windows.FileStreamInfo,
			&buffer[0],
			uint32(len(buffer)),
		)
		if err == nil {
			return countNamedStreamsInBuffer(buffer)
		}
		if errors.Is(err, windows.ERROR_HANDLE_EOF) {
			return 0, nil
		}
		if !errors.Is(err, windows.ERROR_MORE_DATA) && !errors.Is(err, windows.ERROR_INSUFFICIENT_BUFFER) {
			return 0, err
		}
	}
	return 0, fmt.Errorf("file stream metadata exceeds %d bytes", maxFileStreamInfoBytes)
}

func countNamedStreamsInBuffer(buffer []byte) (uint32, error) {
	const fileStreamInfoHeaderBytes = 24
	var count uint32
	for offset := 0; ; {
		if offset < 0 || len(buffer)-offset < fileStreamInfoHeaderBytes {
			return 0, errors.New("invalid FILE_STREAM_INFO header")
		}

		nextOffset := binary.LittleEndian.Uint32(buffer[offset : offset+4])
		nameBytes := binary.LittleEndian.Uint32(buffer[offset+4 : offset+8])
		if nameBytes%2 != 0 || uint64(offset)+fileStreamInfoHeaderBytes+uint64(nameBytes) > uint64(len(buffer)) {
			return 0, errors.New("invalid FILE_STREAM_INFO name")
		}

		nameUnits := make([]uint16, nameBytes/2)
		nameStart := offset + fileStreamInfoHeaderBytes
		for index := range nameUnits {
			unitStart := nameStart + index*2
			nameUnits[index] = binary.LittleEndian.Uint16(buffer[unitStart : unitStart+2])
		}
		name := windows.UTF16ToString(nameUnits)
		if name != "" && !strings.EqualFold(name, "::$DATA") {
			count++
		}

		if nextOffset == 0 {
			return count, nil
		}
		if nextOffset < fileStreamInfoHeaderBytes || uint64(offset)+uint64(nextOffset) >= uint64(len(buffer)) {
			return 0, errors.New("invalid FILE_STREAM_INFO next offset")
		}
		offset += int(nextOffset)
	}
}

func openVerificationFile(path string, shareDelete bool) (*os.File, error) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	shareMode := uint32(windows.FILE_SHARE_READ)
	if shareDelete {
		// Destructive-policy tests may inject an identity-aware adapter that
		// needs delete sharing while this exact object remains open. Production
		// recycling is disabled until the native sink can preserve that identity.
		// Write sharing remains denied so bytes cannot change while hashed.
		shareMode |= windows.FILE_SHARE_DELETE
	}
	// Keeper handles omit both write and delete sharing. While held, another
	// process cannot mutate, rename, or delete the verified retained copy.
	handle, err := windows.CreateFile(
		pathPtr,
		windows.GENERIC_READ,
		shareMode,
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
		return nil, errors.New("create verification file handle")
	}
	return file, nil
}

func finalPathForOpenFile(file *os.File) (string, error) {
	return finalPathForWindowsHandle(windows.Handle(file.Fd()))
}

func finalPathForWindowsHandle(handle windows.Handle) (string, error) {
	buffer := make([]uint16, 32768)
	length, err := windows.GetFinalPathNameByHandle(handle, &buffer[0], uint32(len(buffer)), 0)
	if err != nil {
		return "", err
	}
	if length == 0 || length >= uint32(len(buffer)) {
		return "", fmt.Errorf("resolved path exceeds supported Windows path length")
	}

	resolved := windows.UTF16ToString(buffer[:length])
	resolved = strings.TrimPrefix(resolved, `\\?\`)
	if strings.HasPrefix(strings.ToUpper(resolved), `UNC\`) {
		resolved = `\\` + resolved[len(`UNC\`):]
	}
	return filepath.Clean(resolved), nil
}

func pathIsTraversalReparsePoint(path string) (bool, error) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false, err
	}
	attributes, err := windows.GetFileAttributes(pathPtr)
	if err != nil {
		return false, err
	}
	return pathHasTraversalReparsePoint(pathPtr, attributes)
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
		if sameCanonicalPath(parent, current) {
			return nil
		}
		current = parent
	}
}

func pathHasTraversalReparsePoint(pathPtr *uint16, attributes uint32) (bool, error) {
	if attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT == 0 {
		return false, nil
	}

	handle, err := windows.CreateFile(
		pathPtr,
		windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		return false, err
	}
	defer windows.CloseHandle(handle)

	var tagInfo windowsFileAttributeTagInfo
	if err := windows.GetFileInformationByHandleEx(
		handle,
		windows.FileAttributeTagInfo,
		(*byte)(unsafe.Pointer(&tagInfo)),
		uint32(unsafe.Sizeof(tagInfo)),
	); err != nil {
		return false, err
	}
	return tagInfo.ReparseTag&reparseTagNameSurrogate != 0, nil
}
