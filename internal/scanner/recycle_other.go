//go:build !windows

package scanner

import (
	"context"
	"os"
)

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
