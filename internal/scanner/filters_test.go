package scanner

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeFilterOptionsClampsAndCanonicalizes(t *testing.T) {
	t.Parallel()

	options := normalizeFilterOptions(ScanOptions{
		MinFileSize:        -5,
		ExcludedExtensions: []string{" TMP ", ".log", "log", "", ".", ".LOG"},
		ExcludedPaths:      []string{"", "   "},
	})

	if options.MinFileSize != 0 {
		t.Fatalf("expected negative MinFileSize to clamp to 0, got %d", options.MinFileSize)
	}
	want := []string{".tmp", ".log"}
	if len(options.ExcludedExtensions) != len(want) {
		t.Fatalf("expected %v, got %v", want, options.ExcludedExtensions)
	}
	for index, extension := range want {
		if options.ExcludedExtensions[index] != extension {
			t.Fatalf("expected %v, got %v", want, options.ExcludedExtensions)
		}
	}
	if len(options.ExcludedPaths) != 0 {
		t.Fatalf("expected blank exclusion paths to be dropped, got %v", options.ExcludedPaths)
	}
}

func TestNormalizeFilterOptionsMakesExclusionPathsAbsolute(t *testing.T) {
	t.Parallel()

	relative := filepath.Join("some", "relative", "dir")
	options := normalizeFilterOptions(ScanOptions{ExcludedPaths: []string{relative, relative}})

	if len(options.ExcludedPaths) != 1 {
		t.Fatalf("expected duplicate exclusions to collapse, got %v", options.ExcludedPaths)
	}
	if !filepath.IsAbs(options.ExcludedPaths[0]) {
		t.Fatalf("expected absolute exclusion path, got %q", options.ExcludedPaths[0])
	}
}

func TestPathWithinFold(t *testing.T) {
	t.Parallel()

	sep := string(filepath.Separator)
	root := filepath.Join("C:\\", "Data")
	cases := []struct {
		name   string
		path   string
		prefix string
		want   bool
	}{
		{"exact match", root, root, true},
		{"child file", filepath.Join(root, "a.txt"), root, true},
		{"nested child", filepath.Join(root, "sub", "b.txt"), root, true},
		{"case-insensitive", strings.ToUpper(root) + sep + "c.txt", root, true},
		{"sibling name prefix", filepath.Join("C:\\", "Database", "d.txt"), root, false},
		{"unrelated", filepath.Join("D:", "Other", "e.txt"), root, false},
		{"empty prefix", filepath.Join(root, "f.txt"), "", false},
		{"path shorter than prefix", "C:" + sep, root, false},
	}
	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			got := pathWithinFold(testCase.path, testCase.prefix)
			if got != testCase.want {
				t.Fatalf("pathWithinFold(%q, %q) = %v, want %v", testCase.path, testCase.prefix, got, testCase.want)
			}
		})
	}
}

func TestRecordPassesFilters(t *testing.T) {
	t.Parallel()

	base := filepath.Join("C:\\", "Users", "example", "Documents")
	record := func(path string, size int64) FileRecord {
		return FileRecord{Path: path, Size: size}
	}

	cases := []struct {
		name    string
		record  FileRecord
		options ScanOptions
		want    bool
	}{
		{
			"no filters pass everything",
			record(filepath.Join(base, "a.txt"), 1),
			ScanOptions{},
			true,
		},
		{
			"below size floor",
			record(filepath.Join(base, "a.txt"), 999),
			ScanOptions{MinFileSize: 1000},
			false,
		},
		{
			"at size floor",
			record(filepath.Join(base, "a.txt"), 1000),
			ScanOptions{MinFileSize: 1000},
			true,
		},
		{
			"excluded extension case-insensitive",
			record(filepath.Join(base, "movie.MP4"), 1),
			ScanOptions{ExcludedExtensions: []string{".mp4"}},
			false,
		},
		{
			"other extension passes",
			record(filepath.Join(base, "movie.mkv"), 1),
			ScanOptions{ExcludedExtensions: []string{".mp4"}},
			true,
		},
		{
			"excluded path subtree",
			record(filepath.Join(base, "cache", "x.txt"), 1),
			ScanOptions{ExcludedPaths: []string{filepath.Join(base, "cache")}},
			false,
		},
		{
			"outside excluded path",
			record(filepath.Join(base, "keep", "x.txt"), 1),
			ScanOptions{ExcludedPaths: []string{filepath.Join(base, "cache")}},
			true,
		},
	}
	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			got := recordPassesFilters(testCase.record, testCase.options)
			if got != testCase.want {
				t.Fatalf("recordPassesFilters(%q) = %v, want %v", testCase.record.Path, got, testCase.want)
			}
		})
	}
}

func TestFilterRecordsByOptionsAppliesUserFilters(t *testing.T) {
	t.Parallel()

	base := filepath.Join("C:\\", "Users", "example", "Documents")
	files := []FileRecord{
		{Path: filepath.Join(base, "keep.txt"), Size: 4096, Category: CategoryText},
		{Path: filepath.Join(base, "small.txt"), Size: 10, Category: CategoryText},
		{Path: filepath.Join(base, "skip.log"), Size: 4096, Category: CategoryText},
		{Path: filepath.Join(base, "cache", "cached.txt"), Size: 4096, Category: CategoryText},
	}
	options := NormalizeScanOptions(ScanOptions{
		Categories:         AllFileCategories(),
		MinFileSize:        1024,
		ExcludedExtensions: []string{"log"},
		ExcludedPaths:      []string{filepath.Join(base, "cache")},
	})

	filtered := filterRecordsByOptions(files, options)
	if len(filtered) != 1 {
		t.Fatalf("expected exactly one surviving record, got %d: %v", len(filtered), filtered)
	}
	if filtered[0].Path != files[0].Path {
		t.Fatalf("expected %q to survive, got %q", files[0].Path, filtered[0].Path)
	}
}
