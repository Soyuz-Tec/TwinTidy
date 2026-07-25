//go:build !windows

package scanner

import (
	"context"
	"os"
)

// The consolidation safety contract is implemented with Windows file IDs and
// handle-bound rename/disposition. Other platforms stay fail-closed.
func platformHardlinkSupported() bool {
	return false
}

func platformConsolidateTarget(ctx context.Context, _ *os.File, _ FileRecord, target FileRecord, _ string) HardlinkItemResult {
	if err := ctx.Err(); err != nil {
		return hardlinkItem(target.Path, HardlinkStatusCancelled, err)
	}
	return hardlinkItem(target.Path, HardlinkStatusFailed, errHardlinkUnsupported)
}
