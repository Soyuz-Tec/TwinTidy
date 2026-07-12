package scanner

import "time"

type Stage string

const (
	StageIdle            Stage = "Idle"
	StageSurfaceScan     Stage = "Surface Scan"
	StageSizeMapping     Stage = "Size Mapping"
	StageBoundaryHashing Stage = "Boundary Hashing"
	StageFullHashing     Stage = "Full Hashing"
	StageDone            Stage = "Done"
)

type FileCategory string

const (
	CategoryPDF        FileCategory = "pdf"
	CategoryText       FileCategory = "text"
	CategoryWord       FileCategory = "word"
	CategoryExcel      FileCategory = "excel"
	CategoryPowerPoint FileCategory = "powerpoint"
	CategoryImages     FileCategory = "images"
	CategoryAudio      FileCategory = "audio"
	CategoryVideo      FileCategory = "video"
	CategoryArchives   FileCategory = "archives"
	CategoryOther      FileCategory = "other"
)

type FileRecord struct {
	Path         string
	Size         int64
	CreatedAt    time.Time
	ModifiedAt   time.Time
	Category     FileCategory
	Identity     FileIdentity
	LinkCount    uint32
	NamedStreams uint32
	Scope        AuthorizedScope
}

// FileIdentity identifies one physical file on a Windows machine. A file ID is
// meaningful only together with the volume serial number that issued it.
type FileIdentity struct {
	VolumeSerial uint64
	FileID       [16]byte
}

// AuthorizedScope binds a scan record to the selected root that authorized
// its discovery. The value is deliberately comparable and copy-safe so later
// preview and recycle requests can bind user intent to the same scope.
// RootFinalPath is the canonical final path observed from an open root handle;
// RootIdentity detects replacement of that filesystem object.
type AuthorizedScope struct {
	RootFinalPath string
	RootIdentity  FileIdentity
	RootIsFile    bool
}

type DuplicateGroup struct {
	Size  int64
	Hash  string
	Files []FileRecord
}

type Progress struct {
	Stage              Stage
	CurrentPath        string
	FilesProcessed     int64
	FilesTotal         int64
	DirectoriesScanned int64
	BytesHashed        int64
	GroupsFound        int
	ErrorsIgnored      int64
	SkippedSystemItems int64
	StartedAt          time.Time
	Message            string
}

type ScanOptions struct {
	Categories    map[FileCategory]bool
	UserFilesOnly bool
}

// ScanLimits cap the in-memory inventory built during one scan. Zero values
// select production defaults. Callers should normally use NewEngine; explicit
// limits primarily support controlled deployments and deterministic tests.
type ScanLimits struct {
	MaxRoots       int
	MaxDirectories int64
	MaxFiles       int64
}

type FileCategoryDefinition struct {
	Category   FileCategory
	Label      string
	Extensions []string
}

type SurfaceCategoryStats struct {
	Files int64
	Bytes int64
}

type SurfaceReport struct {
	Files              []FileRecord
	CategoryStats      map[FileCategory]SurfaceCategoryStats
	TotalFiles         int64
	TotalBytes         int64
	DirectoriesScanned int64
	ErrorsIgnored      int64
	SkippedSystemItems int64
}

type RecycleRequest struct {
	Group    DuplicateGroup
	Selected []FileRecord
}

type RecycleStatus string

const (
	RecycleStatusRecycled         RecycleStatus = "recycled"
	RecycleStatusSkippedChanged   RecycleStatus = "skipped-changed"
	RecycleStatusSkippedProtected RecycleStatus = "skipped-protected"
	RecycleStatusCancelled        RecycleStatus = "cancelled"
	RecycleStatusFailed           RecycleStatus = "failed"
)

type RecycleItemResult struct {
	Path   string
	Status RecycleStatus
	Reason string
}

type RecycleResult struct {
	Items        []RecycleItemResult
	RequestError string
}
