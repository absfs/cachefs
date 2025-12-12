package cachefs

import (
	"testing"

	"github.com/absfs/fstesting"
	"github.com/absfs/memfs"
)

// TestCacheFSSuite runs the standard fstesting suite against cachefs wrapping memfs.
func TestCacheFSSuite(t *testing.T) {
	// Create the underlying memfs
	underlying, err := memfs.NewFS()
	if err != nil {
		t.Fatalf("failed to create memfs: %v", err)
	}

	// Create cachefs wrapper
	fs := New(underlying)

	suite := &fstesting.Suite{
		FS: fs,
		Features: fstesting.Features{
			Symlinks:      false, // CacheFS doesn't implement symlinks (use SymlinkCacheFS)
			HardLinks:     false,
			Permissions:   true,
			Timestamps:    true,
			CaseSensitive: true,
			AtomicRename:  true,
			SparseFiles:   false,
			LargeFiles:    true,
		},
	}

	suite.Run(t)
}

// TestSymlinkCacheFSSuite runs the standard fstesting suite against SymlinkCacheFS.
func TestSymlinkCacheFSSuite(t *testing.T) {
	// Create the underlying memfs
	underlying, err := memfs.NewFS()
	if err != nil {
		t.Fatalf("failed to create memfs: %v", err)
	}

	// Create SymlinkCacheFS wrapper
	fs := NewSymlinkFS(underlying)

	suite := &fstesting.Suite{
		FS: fs,
		Features: fstesting.Features{
			Symlinks:      true,
			HardLinks:     false,
			Permissions:   true,
			Timestamps:    true,
			CaseSensitive: true,
			AtomicRename:  true,
			SparseFiles:   false,
			LargeFiles:    true,
		},
	}

	suite.Run(t)
}
