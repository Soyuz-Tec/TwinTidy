package scanner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRecyclePolicyRejectsForgedDuplicateAndSelectedAllRequests(t *testing.T) {
	fixture := newRecycleFixture(t, 3)
	first := fixture.group.Files[0]
	second := fixture.group.Files[1]
	forged := first
	forged.Path = filepath.Join(fixture.root, "not-a-member.txt")
	forgedScope := first
	forgedScope.Scope = AuthorizedScope{}

	tests := []struct {
		name     string
		selected []FileRecord
	}{
		{name: "forged nonmember", selected: []FileRecord{forged}},
		{name: "duplicate selection", selected: []FileRecord{first, first}},
		{name: "every physical identity", selected: append([]FileRecord(nil), fixture.group.Files...)},
		{name: "forged metadata", selected: []FileRecord{func() FileRecord { changed := second; changed.Size++; return changed }()}},
		{name: "forged scope", selected: []FileRecord{forgedScope}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			adapter := &fakeRecycleAdapter{}
			result := recycleExactDuplicates(context.Background(), RecycleRequest{
				Group:    fixture.group,
				Selected: test.selected,
			}, adapter)
			if result.RequestError == "" {
				t.Fatalf("expected request rejection, got %#v", result)
			}
			if len(adapter.calls) != 0 {
				t.Fatalf("native adapter was called for rejected request: %v", adapter.calls)
			}
			for _, item := range result.Items {
				if item.Status != RecycleStatusFailed {
					t.Fatalf("rejected item status = %s, want failed", item.Status)
				}
			}
		})
	}
}

func TestRecyclePolicySkipsReplacedTarget(t *testing.T) {
	fixture := newRecycleFixture(t, 2)
	target := fixture.group.Files[0]
	backup := target.Path + ".original"
	if err := os.Rename(target.Path, backup); err != nil {
		t.Fatalf("Rename target failed: %v", err)
	}
	writeTestFile(t, target.Path, fixture.content)

	adapter := &fakeRecycleAdapter{recycle: removeRecycledTestFile}
	result := recycleExactDuplicates(context.Background(), recycleRequest(fixture.group, target), adapter)
	assertSingleRecycleStatus(t, result, RecycleStatusSkippedChanged)
	if len(adapter.calls) != 0 {
		t.Fatalf("adapter called for replaced target: %v", adapter.calls)
	}
}

func TestRecyclePolicySkipsWhenKeeperChanged(t *testing.T) {
	fixture := newRecycleFixture(t, 2)
	target := fixture.group.Files[0]
	keeper := fixture.group.Files[1]
	mutation := sameLengthMutation(fixture.content)
	writeTestFile(t, keeper.Path, mutation)
	if err := os.Chtimes(keeper.Path, time.Now(), keeper.ModifiedAt); err != nil {
		t.Fatalf("restore keeper modification time: %v", err)
	}

	adapter := &fakeRecycleAdapter{recycle: removeRecycledTestFile}
	result := recycleExactDuplicates(context.Background(), recycleRequest(fixture.group, target), adapter)
	assertSingleRecycleStatus(t, result, RecycleStatusSkippedChanged)
	if len(adapter.calls) != 0 {
		t.Fatalf("adapter called without a verified keeper: %v", adapter.calls)
	}
}

func TestRecyclePolicySkipsWhenKeeperMissing(t *testing.T) {
	fixture := newRecycleFixture(t, 2)
	target := fixture.group.Files[0]
	keeper := fixture.group.Files[1]
	if err := os.Remove(keeper.Path); err != nil {
		t.Fatalf("Remove keeper failed: %v", err)
	}

	adapter := &fakeRecycleAdapter{recycle: removeRecycledTestFile}
	result := recycleExactDuplicates(context.Background(), recycleRequest(fixture.group, target), adapter)
	assertSingleRecycleStatus(t, result, RecycleStatusSkippedChanged)
	if len(adapter.calls) != 0 {
		t.Fatalf("adapter called without a keeper: %v", adapter.calls)
	}
}

func TestRecyclePolicyDetectsSameSizeSameTimeMutation(t *testing.T) {
	fixture := newRecycleFixture(t, 2)
	target := fixture.group.Files[0]
	writeTestFile(t, target.Path, sameLengthMutation(fixture.content))
	if err := os.Chtimes(target.Path, time.Now(), target.ModifiedAt); err != nil {
		t.Fatalf("restore target modification time: %v", err)
	}

	adapter := &fakeRecycleAdapter{recycle: removeRecycledTestFile}
	result := recycleExactDuplicates(context.Background(), recycleRequest(fixture.group, target), adapter)
	assertSingleRecycleStatus(t, result, RecycleStatusSkippedChanged)
	if len(adapter.calls) != 0 {
		t.Fatalf("adapter called for content-mutated target: %v", adapter.calls)
	}
}

func TestRecyclePolicyDoesNotTrustSuccessWhileSourceExists(t *testing.T) {
	fixture := newRecycleFixture(t, 2)
	target := fixture.group.Files[0]
	adapter := &fakeRecycleAdapter{}

	result := recycleExactDuplicates(context.Background(), recycleRequest(fixture.group, target), adapter)
	assertSingleRecycleStatus(t, result, RecycleStatusFailed)
	if _, err := os.Stat(target.Path); err != nil {
		t.Fatalf("source unexpectedly changed after ambiguous success: %v", err)
	}
}

func TestRecyclePolicyMapsNativeAbortFailureAndCancellation(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status RecycleStatus
	}{
		{name: "aborted", err: errRecycleAborted, status: RecycleStatusCancelled},
		{name: "native failure", err: errors.New("shell failure"), status: RecycleStatusFailed},
		{name: "context cancellation", err: context.Canceled, status: RecycleStatusCancelled},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRecycleFixture(t, 2)
			target := fixture.group.Files[0]
			adapter := &fakeRecycleAdapter{recycle: func(context.Context, string) error { return test.err }}
			result := recycleExactDuplicates(context.Background(), recycleRequest(fixture.group, target), adapter)
			assertSingleRecycleStatus(t, result, test.status)
			if _, err := os.Stat(target.Path); err != nil {
				t.Fatalf("native failure changed source: %v", err)
			}
		})
	}
}

func TestRecyclePolicyReportsOnlyVerifiedSourceRemovalAsRecycled(t *testing.T) {
	fixture := newRecycleFixture(t, 2)
	target := fixture.group.Files[0]
	keeper := fixture.group.Files[1]
	adapter := &fakeRecycleAdapter{recycle: removeRecycledTestFile}

	result := recycleExactDuplicates(context.Background(), recycleRequest(fixture.group, target), adapter)
	assertSingleRecycleStatus(t, result, RecycleStatusRecycled)
	if _, err := os.Stat(target.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("target still exists after reported recycle: %v", err)
	}
	if _, err := os.Stat(keeper.Path); err != nil {
		t.Fatalf("keeper was not preserved: %v", err)
	}
}

func TestRecyclePolicyProcessesMultipleTargetsAgainstOneKeeper(t *testing.T) {
	fixture := newRecycleFixture(t, 3)
	first := fixture.group.Files[0]
	second := fixture.group.Files[1]
	keeper := fixture.group.Files[2]
	adapter := &fakeRecycleAdapter{recycle: removeRecycledTestFile}

	result := recycleExactDuplicates(context.Background(), recycleRequest(fixture.group, first, second), adapter)
	if result.RequestError != "" {
		t.Fatalf("unexpected request error: %s", result.RequestError)
	}
	if len(result.Items) != 2 {
		t.Fatalf("result items = %#v, want two", result.Items)
	}
	for _, item := range result.Items {
		if item.Status != RecycleStatusRecycled {
			t.Fatalf("item status = %s, want recycled (reason: %s)", item.Status, item.Reason)
		}
	}
	if len(adapter.calls) != 2 {
		t.Fatalf("adapter calls = %v, want two", adapter.calls)
	}
	if _, err := os.Stat(keeper.Path); err != nil {
		t.Fatalf("sole keeper was not preserved: %v", err)
	}
}

func TestRecyclePolicyHonorsAlreadyCancelledContext(t *testing.T) {
	fixture := newRecycleFixture(t, 2)
	target := fixture.group.Files[0]
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	adapter := &fakeRecycleAdapter{recycle: removeRecycledTestFile}

	result := recycleExactDuplicates(ctx, recycleRequest(fixture.group, target), adapter)
	assertSingleRecycleStatus(t, result, RecycleStatusCancelled)
	if len(adapter.calls) != 0 {
		t.Fatalf("adapter called after cancellation: %v", adapter.calls)
	}
}

type recycleFixture struct {
	root    string
	content []byte
	group   DuplicateGroup
}

func newRecycleFixture(t *testing.T, count int) recycleFixture {
	t.Helper()
	root := userFileTestRoot(t)
	content := []byte("TwinTidy verified duplicate payload")
	for index := 0; index < count; index++ {
		writeTestFile(t, filepath.Join(root, fmt.Sprintf("copy-%02d.txt", index)), content)
	}
	groups, err := NewEngine(1).Scan(context.Background(), []string{root}, nil)
	if err != nil {
		t.Fatalf("Scan fixture failed: %v", err)
	}
	if len(groups) != 1 || len(groups[0].Files) != count {
		t.Fatalf("unexpected fixture groups: %#v", groups)
	}
	return recycleFixture{root: root, content: content, group: groups[0]}
}

func recycleRequest(group DuplicateGroup, selected ...FileRecord) RecycleRequest {
	return RecycleRequest{Group: group, Selected: selected}
}

func sameLengthMutation(original []byte) []byte {
	mutation := append([]byte(nil), original...)
	for index := range mutation {
		mutation[index] ^= 0xff
	}
	return mutation
}

func assertSingleRecycleStatus(t *testing.T, result RecycleResult, want RecycleStatus) {
	t.Helper()
	if result.RequestError != "" {
		t.Fatalf("unexpected request error: %s", result.RequestError)
	}
	if len(result.Items) != 1 {
		t.Fatalf("result items = %#v, want one", result.Items)
	}
	if result.Items[0].Status != want {
		t.Fatalf("item status = %s, want %s (reason: %s)", result.Items[0].Status, want, result.Items[0].Reason)
	}
}

type fakeRecycleAdapter struct {
	calls   []string
	recycle func(context.Context, string) error
	confirm func(*os.File, FileIdentity) (recycleReceipt, error)
}

func (f *fakeRecycleAdapter) Recycle(ctx context.Context, path string, expectedHandle *os.File, expectedIdentity FileIdentity) (recycleReceipt, error) {
	f.calls = append(f.calls, path)
	if f.recycle != nil {
		if err := f.recycle(ctx, path); err != nil {
			return recycleReceipt{}, err
		}
	}
	if f.confirm != nil {
		return f.confirm(expectedHandle, expectedIdentity)
	}
	return recycleReceipt{identity: expectedIdentity, destination: "test-recycle-receipt", confirmed: true}, nil
}

func removeRecycledTestFile(_ context.Context, path string) error {
	return os.Remove(path)
}
