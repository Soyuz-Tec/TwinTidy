package scanner

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// KeepStrategy chooses which member of a duplicate group is kept when a
// rule-based selection runs.
type KeepStrategy string

const (
	// KeepNewest keeps the most recently modified member.
	KeepNewest KeepStrategy = "keep-newest"
	// KeepOldest keeps the least recently modified member.
	KeepOldest KeepStrategy = "keep-oldest"
)

// SelectionPolicy describes one rule-based selection across duplicate
// groups. PriorityFolders is an ordered list: when any group member sits
// below an earlier-listed folder, the keeper is chosen among those members
// only, so users can express "keep the copy in D:\Photos, mark the ones in
// Downloads". Strategy breaks ties inside the winning folder tier and
// selects the keeper outright when no priority folder matches.
type SelectionPolicy struct {
	Strategy        KeepStrategy
	PriorityFolders []string
}

// GroupSelection is the outcome of applying a policy to one duplicate
// group. Keep is always a real member of the group and never appears in
// Marked; selection only ever proposes, it does not act.
type GroupSelection struct {
	Group  DuplicateGroup
	Keep   FileRecord
	Marked []FileRecord
}

// ErrInvalidSelectionPolicy identifies a policy that cannot run at all, as
// opposed to one that simply selects nothing.
var ErrInvalidSelectionPolicy = fmt.Errorf("invalid selection policy")

// NormalizeSelectionPolicy canonicalizes a policy: the strategy must be a
// known value and priority folders become cleaned absolute paths, dropping
// blank entries. An unresolvable folder entry is rejected rather than
// silently ignored, because a silently dropped priority could flip which
// file gets marked.
func NormalizeSelectionPolicy(policy SelectionPolicy) (SelectionPolicy, error) {
	switch policy.Strategy {
	case KeepNewest, KeepOldest:
	case "":
		policy.Strategy = KeepNewest
	default:
		return SelectionPolicy{}, fmt.Errorf("%w: unknown strategy %q", ErrInvalidSelectionPolicy, policy.Strategy)
	}

	if len(policy.PriorityFolders) > 0 {
		normalized := make([]string, 0, len(policy.PriorityFolders))
		for _, folder := range policy.PriorityFolders {
			folder = strings.TrimSpace(folder)
			if folder == "" {
				continue
			}
			absolute, err := filepath.Abs(folder)
			if err != nil {
				return SelectionPolicy{}, fmt.Errorf("%w: priority folder %q: %v", ErrInvalidSelectionPolicy, folder, err)
			}
			normalized = append(normalized, filepath.Clean(absolute))
		}
		policy.PriorityFolders = normalized
	}
	return policy, nil
}

// ApplySelectionPolicy proposes a keeper and marked members for every
// duplicate group. The invariant enforced here — not in any caller or UI —
// is that each returned selection keeps exactly one real group member and
// marks strictly fewer members than the group holds. Groups with fewer than
// two members yield no selection because there is nothing to mark.
func ApplySelectionPolicy(groups []DuplicateGroup, policy SelectionPolicy) ([]GroupSelection, error) {
	policy, err := NormalizeSelectionPolicy(policy)
	if err != nil {
		return nil, err
	}

	selections := make([]GroupSelection, 0, len(groups))
	for _, group := range groups {
		if len(group.Files) < 2 {
			continue
		}
		keeperIndex := selectKeeperIndex(group.Files, policy)
		marked := make([]FileRecord, 0, len(group.Files)-1)
		for index, file := range group.Files {
			if index == keeperIndex {
				continue
			}
			marked = append(marked, file)
		}
		selection := GroupSelection{Group: group, Keep: group.Files[keeperIndex], Marked: marked}
		if err := validateSelectionInvariant(selection); err != nil {
			return nil, err
		}
		selections = append(selections, selection)
	}
	return selections, nil
}

// selectKeeperIndex picks the index of the member to keep. Candidates
// narrow to the earliest priority folder that contains at least one member;
// the strategy then orders the candidates, with the full path as the final
// deterministic tiebreaker.
func selectKeeperIndex(files []FileRecord, policy SelectionPolicy) int {
	candidates := make([]int, 0, len(files))
	for _, folder := range policy.PriorityFolders {
		for index, file := range files {
			if pathWithinFold(file.Path, folder) {
				candidates = append(candidates, index)
			}
		}
		if len(candidates) > 0 {
			break
		}
	}
	if len(candidates) == 0 {
		for index := range files {
			candidates = append(candidates, index)
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		a, b := files[candidates[i]], files[candidates[j]]
		if !a.ModifiedAt.Equal(b.ModifiedAt) {
			if policy.Strategy == KeepOldest {
				return a.ModifiedAt.Before(b.ModifiedAt)
			}
			return a.ModifiedAt.After(b.ModifiedAt)
		}
		return a.Path < b.Path
	})
	return candidates[0]
}

// validateSelectionInvariant fail-closes on any selection that could mark
// every member of a group. It re-derives membership from paths rather than
// trusting the construction above, so a future refactoring mistake cannot
// silently weaken the guarantee.
func validateSelectionInvariant(selection GroupSelection) error {
	if len(selection.Group.Files) < 2 {
		return fmt.Errorf("selection produced for group with %d member(s)", len(selection.Group.Files))
	}
	if len(selection.Marked) >= len(selection.Group.Files) {
		return fmt.Errorf("selection marks %d of %d members", len(selection.Marked), len(selection.Group.Files))
	}

	members := make(map[string]struct{}, len(selection.Group.Files))
	for _, file := range selection.Group.Files {
		members[file.Path] = struct{}{}
	}
	if _, ok := members[selection.Keep.Path]; !ok {
		return fmt.Errorf("keeper %q is not a group member", selection.Keep.Path)
	}
	for _, file := range selection.Marked {
		if _, ok := members[file.Path]; !ok {
			return fmt.Errorf("marked file %q is not a group member", file.Path)
		}
		if file.Path == selection.Keep.Path {
			return fmt.Errorf("keeper %q is also marked", file.Path)
		}
	}
	return nil
}
