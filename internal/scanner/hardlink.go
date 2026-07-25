package scanner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
)

// ADR 0009: hardlink consolidation reclaims duplicate space by replacing a
// verified duplicate with a hardlink to the verified keeper. No content ever
// becomes unreachable: every failure mode leaves the bytes reachable at the
// original path or at a reported backup path.

var errHardlinkUnsupported = errors.New("hardlink consolidation is unsupported on this platform")

// HardlinkStatus reports the outcome for one consolidation candidate.
type HardlinkStatus string

const (
	HardlinkStatusConsolidated       HardlinkStatus = "consolidated"
	HardlinkStatusSkippedChanged     HardlinkStatus = "skipped-changed"
	HardlinkStatusSkippedProtected   HardlinkStatus = "skipped-protected"
	HardlinkStatusSkippedCrossVolume HardlinkStatus = "skipped-cross-volume"
	HardlinkStatusCancelled          HardlinkStatus = "cancelled"
	HardlinkStatusFailed             HardlinkStatus = "failed"
)

// HardlinkItemResult reports one candidate's outcome. BackupPath is set only
// when the operation stopped after the candidate was renamed aside: the
// user's content is intact at that path and nothing may delete it silently.
type HardlinkItemResult struct {
	Path       string
	Status     HardlinkStatus
	Reason     string
	BackupPath string
}

// HardlinkResult reports one consolidation request.
type HardlinkResult struct {
	Items        []HardlinkItemResult
	RequestError string
}

// HardlinkConsolidationSupported reports whether the platform adapter can
// bind verified file identities through every destructive step.
func HardlinkConsolidationSupported() bool {
	return platformHardlinkSupported()
}

// ConsolidateExactDuplicates replaces each selected duplicate with a hardlink
// to a verified keeper from the same group. It reuses the recycle request
// validation, so every invariant that protects deletion also protects
// consolidation: verified group hash, distinct identities, no protected
// members, and at least one distinct physical copy always retained.
func ConsolidateExactDuplicates(ctx context.Context, request RecycleRequest) HardlinkResult {
	if !platformHardlinkSupported() {
		return rejectedHardlinkResult(request, errHardlinkUnsupported)
	}
	validated, err := validateRecycleRequest(request)
	if err != nil {
		return rejectedHardlinkResult(request, err)
	}

	result := HardlinkResult{Items: make([]HardlinkItemResult, 0, len(validated.selected))}
	for _, target := range validated.selected {
		if err := ctx.Err(); err != nil {
			result.Items = append(result.Items, hardlinkItem(target.Path, HardlinkStatusCancelled, err))
			continue
		}
		result.Items = append(result.Items, consolidateOne(ctx, validated, target))
	}
	return result
}

func consolidateOne(ctx context.Context, request validatedRecycleRequest, target FileRecord) HardlinkItemResult {
	keeperFile, keeperRecord, keeperStatus, err := openVerifiedKeeperRecord(ctx, request.keepers, request.groupHash)
	if err != nil {
		return hardlinkItem(target.Path, keeperStatus, fmt.Errorf("no verified keeper remains: %w", err))
	}
	defer keeperFile.Close()

	if keeperRecord.Identity.VolumeSerial != target.Identity.VolumeSerial {
		return hardlinkItem(target.Path, HardlinkStatusSkippedCrossVolume,
			errors.New("hardlinks cannot cross volumes; the candidate is on a different volume than the keeper"))
	}
	if err := ctx.Err(); err != nil {
		return hardlinkItem(target.Path, HardlinkStatusCancelled, err)
	}
	return platformConsolidateTarget(ctx, keeperFile, keeperRecord, target, request.groupHash)
}

// openVerifiedKeeperRecord returns the first keeper whose content hash,
// identity, metadata, and scope all still verify, together with its record.
// The returned handle denies write and delete sharing, so the keeper cannot
// change or vanish while the consolidation acts on its identity.
func openVerifiedKeeperRecord(ctx context.Context, candidates []FileRecord, expectedHash string) (keeperFile *os.File, record FileRecord, status HardlinkStatus, err error) {
	var (
		lastError    error
		sawChanged   bool
		sawProtected bool
	)
	for _, candidate := range candidates {
		file, openErr := openVerifiedRecord(ctx, candidate, expectedHash, false)
		if openErr == nil {
			return file, candidate, "", nil
		}
		lastError = openErr
		switch hardlinkVerificationStatus(openErr) {
		case HardlinkStatusCancelled:
			return nil, FileRecord{}, HardlinkStatusCancelled, openErr
		case HardlinkStatusSkippedChanged:
			sawChanged = true
		case HardlinkStatusSkippedProtected:
			sawProtected = true
		}
	}
	if lastError == nil {
		lastError = errors.New("keeper set is empty")
	}
	switch {
	case sawChanged:
		return nil, FileRecord{}, HardlinkStatusSkippedChanged, lastError
	case sawProtected:
		return nil, FileRecord{}, HardlinkStatusSkippedProtected, lastError
	default:
		return nil, FileRecord{}, HardlinkStatusFailed, lastError
	}
}

// verifyRecordContentHash streams the open file and requires its SHA-256 to
// still match the duplicate group, then re-proves stability through the same
// handle. A path re-open (as the recycle policy uses) would deny the delete
// access this handle holds and conflict with itself; the handle-bound
// re-snapshot is also the stronger check, because the destructive steps act
// on this handle, not on the path.
func verifyRecordContentHash(ctx context.Context, file *os.File, record FileRecord, snapshot fileSnapshot, expectedHash string) error {
	hasher := sha256.New()
	buffer := make([]byte, fullHashBufferSize)
	if _, err := copyWithContext(ctx, hasher, file, buffer); err != nil {
		return err
	}
	if _, err := validateOpenPathScope(file, record.Path, record.Scope); err != nil {
		return err
	}
	current, err := snapshotOpenFile(file, record.Path)
	if err != nil {
		return err
	}
	if !snapshotsEqual(snapshot, current) {
		return errFileChanged
	}
	if !strings.EqualFold(hex.EncodeToString(hasher.Sum(nil)), expectedHash) {
		return fmt.Errorf("%w: SHA-256 no longer matches the duplicate group", errFileChanged)
	}
	return nil
}

func hardlinkVerificationStatus(err error) HardlinkStatus {
	switch verificationStatus(err) {
	case RecycleStatusCancelled:
		return HardlinkStatusCancelled
	case RecycleStatusSkippedChanged:
		return HardlinkStatusSkippedChanged
	case RecycleStatusSkippedProtected:
		return HardlinkStatusSkippedProtected
	default:
		return HardlinkStatusFailed
	}
}

func rejectedHardlinkResult(request RecycleRequest, err error) HardlinkResult {
	result := HardlinkResult{
		Items:        make([]HardlinkItemResult, 0, len(request.Selected)),
		RequestError: err.Error(),
	}
	for _, selected := range request.Selected {
		result.Items = append(result.Items, hardlinkItem(selected.Path, HardlinkStatusFailed, err))
	}
	return result
}

func hardlinkItem(path string, status HardlinkStatus, err error) HardlinkItemResult {
	reason := ""
	if err != nil {
		reason = err.Error()
	}
	return HardlinkItemResult{Path: path, Status: status, Reason: reason}
}
