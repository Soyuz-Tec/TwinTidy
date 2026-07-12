//go:build windows

package scanner

import (
	"context"
	"os"
)

// Windows Shell recycle APIs accept path-derived Shell items rather than a
// verified file handle. They can therefore act on a replacement that appears
// at the same path after verification. Production recycling remains disabled
// until the adapter can preserve file identity through the destructive sink.
type unsupportedRecycleAdapter struct{}

func newPlatformRecycleAdapter() recycleAdapter {
	return unsupportedRecycleAdapter{}
}

func platformRecycleSupported() bool {
	return false
}

func (unsupportedRecycleAdapter) Recycle(ctx context.Context, _ string, _ *os.File, _ FileIdentity) (recycleReceipt, error) {
	if err := ctx.Err(); err != nil {
		return recycleReceipt{}, err
	}
	return recycleReceipt{}, errRecycleUnsupported
}
