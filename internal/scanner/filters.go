package scanner

import (
	"path/filepath"
	"strings"
)

// normalizeFilterOptions canonicalizes the filter fields of ScanOptions so
// later matching is deterministic: sizes clamp at zero, extensions become
// lower-case dot-prefixed, and exclusion paths become cleaned absolute paths.
// Entries that cannot be made absolute are dropped rather than matched
// loosely, because a loosely matched exclusion could silently hide results.
func normalizeFilterOptions(options ScanOptions) ScanOptions {
	if options.MinFileSize < 0 {
		options.MinFileSize = 0
	}

	if len(options.ExcludedExtensions) > 0 {
		normalized := make([]string, 0, len(options.ExcludedExtensions))
		seen := make(map[string]struct{}, len(options.ExcludedExtensions))
		for _, extension := range options.ExcludedExtensions {
			extension = strings.ToLower(strings.TrimSpace(extension))
			if extension == "" || extension == "." {
				continue
			}
			if !strings.HasPrefix(extension, ".") {
				extension = "." + extension
			}
			if _, duplicate := seen[extension]; duplicate {
				continue
			}
			seen[extension] = struct{}{}
			normalized = append(normalized, extension)
		}
		options.ExcludedExtensions = normalized
	}

	if len(options.ExcludedPaths) > 0 {
		normalized := make([]string, 0, len(options.ExcludedPaths))
		seen := make(map[string]struct{}, len(options.ExcludedPaths))
		for _, path := range options.ExcludedPaths {
			path = strings.TrimSpace(path)
			if path == "" {
				continue
			}
			absolute, err := filepath.Abs(path)
			if err != nil {
				continue
			}
			absolute = filepath.Clean(absolute)
			key := strings.ToLower(absolute)
			if _, duplicate := seen[key]; duplicate {
				continue
			}
			seen[key] = struct{}{}
			normalized = append(normalized, absolute)
		}
		options.ExcludedPaths = normalized
	}

	return options
}

// recordPassesFilters reports whether one file record survives the size,
// extension, and exclusion-path filters. Category and user-file policy checks
// stay in filterRecordsByOptions; this function covers only the newer
// user-configurable filters.
func recordPassesFilters(record FileRecord, options ScanOptions) bool {
	if options.MinFileSize > 0 && record.Size < options.MinFileSize {
		return false
	}
	if len(options.ExcludedExtensions) > 0 {
		extension := strings.ToLower(filepath.Ext(record.Path))
		for _, excluded := range options.ExcludedExtensions {
			if extension == excluded {
				return false
			}
		}
	}
	for _, excluded := range options.ExcludedPaths {
		if pathWithinFold(record.Path, excluded) {
			return false
		}
	}
	return true
}

// pathWithinFold reports whether path equals prefix or sits below it,
// comparing case-insensitively and only on whole path-segment boundaries, so
// an exclusion for C:\Data never matches C:\Database.
func pathWithinFold(path, prefix string) bool {
	if prefix == "" {
		return false
	}
	path = filepath.Clean(path)
	prefix = filepath.Clean(prefix)
	if len(path) < len(prefix) {
		return false
	}
	if !strings.EqualFold(path[:len(prefix)], prefix) {
		return false
	}
	if len(path) == len(prefix) {
		return true
	}
	if strings.HasSuffix(prefix, string(filepath.Separator)) {
		return true
	}
	return path[len(prefix)] == filepath.Separator
}
