package workspace

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"sort"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"github.com/baalimago/slivingdoc/internal/git"
)

// ErrSymlink reports a symbolic link encountered in the visible directory
// (architecture section 7.1). Symlinks are rejected on every host.
var ErrSymlink = errors.New("workspace: symbolic link rejected")

// ErrUnsupportedFile reports a visible entry that is not a regular file or
// directory: a device, socket, named pipe, or other unsupported mode.
var ErrUnsupportedFile = errors.New("workspace: unsupported file")

// ErrInvalidContent reports visible content that is not valid UTF-8 text
// without U+0000 (architecture section 7.1).
var ErrInvalidContent = errors.New("workspace: invalid text content")

// Snapshot scans the visible directory and returns the complete notebook
// state: every regular file with valid UTF-8 text content, in normalized
// slash-separated relative form. Empty files are valid and bytes and line
// endings are preserved. The scan never follows symlinks and rejects
// special files, invalid names, content, and file-versus-directory
// ambiguity. A workspace that requires recovery refuses to scan.
func (w *Workspace) Snapshot(ctx context.Context) (git.Snapshot, error) {
	var snap git.Snapshot
	err := w.withOpLock(ctx, false, func() error {
		var err error
		snap, err = w.scanLocked(ctx)
		return err
	})
	return snap, err
}

// scanLocked walks the visible directory. The caller holds the operation
// lock and has already refused a recovery-required workspace.
func (w *Workspace) scanLocked(ctx context.Context) (git.Snapshot, error) {
	if fp := w.failpoints; fp != nil && fp.Scan != nil {
		if err := fp.Scan(); err != nil {
			return git.Snapshot{}, fmt.Errorf("workspace: scan: injected: %w", err)
		}
	}
	if _, err := w.root.Lstat(w.rel); errors.Is(err, fs.ErrNotExist) {
		if err := w.root.MkdirAll(w.rel, 0o755); err != nil {
			return git.Snapshot{}, fmt.Errorf("workspace: create visible directory: %w", err)
		}
		return git.Snapshot{}, nil
	}
	var files []git.File
	if err := w.scanWalk(ctx, w.rel, "", &files); err != nil {
		return git.Snapshot{}, err
	}
	snap := git.Snapshot{Files: files}
	if err := git.ValidateSnapshot(snap); err != nil {
		return git.Snapshot{}, fmt.Errorf("workspace: scan: %w", err)
	}
	return snap, nil
}

// scanWalk recursively reads one directory. dirRel is the raw on-disk path
// relative to the workspace root; prefix is the normalized internal path of
// the directory. Names are normalized to Unicode NFC for internal paths
// while on-disk operations keep the raw names, so an NFD on-disk name and
// an NFC on-disk name that normalize to the same internal path are
// detected as a file-versus-directory ambiguity.
func (w *Workspace) scanWalk(ctx context.Context, dirRel, prefix string, files *[]git.File) error {
	entries, err := w.readDir(dirRel)
	if err != nil {
		return fmt.Errorf("workspace: scan %q: %w", dirRel, err)
	}
	var dirPaths, filePaths map[string]bool
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("workspace: scan: %w", err)
		}
		raw := e.Name()
		if !utf8.ValidString(raw) {
			return fmt.Errorf("workspace: scan %q: %w: name is not valid UTF-8", dirRel+"/"+raw, ErrInvalidPath)
		}
		norm := norm.NFC.String(raw)
		path := prefix + norm
		if err := git.ValidatePath(path); err != nil {
			return fmt.Errorf("workspace: scan %q: %w: %w", dirRel+"/"+raw, ErrInvalidPath, err)
		}
		info, err := e.Info() // Lstat semantics: a symlink reports itself
		if err != nil {
			return fmt.Errorf("workspace: scan %q: %w", dirRel+"/"+raw, err)
		}
		mode := info.Mode()
		switch {
		case mode&fs.ModeSymlink != 0:
			return fmt.Errorf("workspace: scan %q: %w", path, ErrSymlink)
		case mode.IsDir():
			if filePaths[path] {
				return fmt.Errorf("workspace: scan %q: %w: path is both a file and a directory", path, ErrInvalidPath)
			}
			if dirPaths[path] {
				return fmt.Errorf("workspace: scan %q: duplicate directory", path)
			}
			if dirPaths == nil {
				dirPaths = map[string]bool{}
			}
			dirPaths[path] = true
			if err := w.scanWalk(ctx, joinRel(dirRel, raw), path+"/", files); err != nil {
				return err
			}
		case mode.IsRegular():
			if dirPaths[path] {
				return fmt.Errorf("workspace: scan %q: %w: path is both a file and a directory", path, ErrInvalidPath)
			}
			if filePaths[path] {
				return fmt.Errorf("workspace: scan %q: duplicate path", path)
			}
			if filePaths == nil {
				filePaths = map[string]bool{}
			}
			filePaths[path] = true
			data, err := w.readVisibleFile(ctx, joinRel(dirRel, raw), path)
			if err != nil {
				return err
			}
			*files = append(*files, git.File{Path: path, Data: data})
		default:
			return fmt.Errorf("workspace: scan %q: %w (%s)", path, ErrUnsupportedFile, mode)
		}
	}
	return nil
}

// readVisibleFile opens one visible file with symlink and special-file
// protection and returns its bytes after content validation. The open uses
// O_NOFOLLOW where the host supports it, and the opened descriptor is
// re-checked against the mode seen by the walk, so a substituted symlink,
// FIFO, or device cannot be read or block the scan.
func (w *Workspace) readVisibleFile(ctx context.Context, rawRel, path string) ([]byte, error) {
	f, err := w.root.OpenFile(rawRel, os.O_RDONLY|noFollowFlag, 0)
	if err != nil {
		return nil, fmt.Errorf("workspace: scan %q: %w", path, err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("workspace: scan %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("workspace: scan %q: %w (%s)", path, ErrUnsupportedFile, info.Mode())
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("workspace: scan %q: %w", path, err)
	}
	if err := git.ValidateContent(data); err != nil {
		return nil, fmt.Errorf("workspace: scan %q: %w: %w", path, ErrInvalidContent, err)
	}
	return data, nil
}

// collectVisible walks the visible directory without opening anything and
// returns every file path and directory path relative to the workspace
// root, in slash form. It is used to remove obsolete entries during a
// materialization; removing a symlink removes the link, never the target,
// so no entry is opened.
func (w *Workspace) collectVisible(ctx context.Context, dirRel, prefix string, files, dirs *[]string) error {
	entries, err := w.readDir(dirRel)
	if err != nil {
		return fmt.Errorf("workspace: collect %q: %w", dirRel, err)
	}
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("workspace: collect: %w", err)
		}
		path := prefix + e.Name()
		if e.IsDir() {
			*dirs = append(*dirs, path)
			if err := w.collectVisible(ctx, joinRel(dirRel, e.Name()), path+"/", files, dirs); err != nil {
				return err
			}
			continue
		}
		*files = append(*files, path)
	}
	return nil
}

// readDir opens the directory relative to the workspace root and returns
// its entries sorted by name. The entries carry Lstat semantics: a symlink
// reports itself, so the scan rejects it before any open.
func (w *Workspace) readDir(rel string) ([]fs.DirEntry, error) {
	f, err := w.root.Open(rel)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.ReadDir(-1)
}

// joinRel joins two slash-separated relative path fragments, treating "."
// and the empty string as the empty prefix.
func joinRel(a, b string) string {
	if a == "" || a == "." {
		return b
	}
	return a + "/" + b
}

// diffSnapshots compares the current visible snapshot against the baseline
// snapshot and reports local additions, modifications, and deletions, each
// sorted by path.
func diffSnapshots(base, cur git.Snapshot) Diff {
	baseData := make(map[string][]byte, len(base.Files))
	for _, f := range base.Files {
		baseData[f.Path] = f.Data
	}
	curData := make(map[string][]byte, len(cur.Files))
	for _, f := range cur.Files {
		curData[f.Path] = f.Data
	}
	var d Diff
	for path, data := range curData {
		old, ok := baseData[path]
		switch {
		case !ok:
			d.Added = append(d.Added, path)
		case !bytes.Equal(old, data):
			d.Modified = append(d.Modified, path)
		}
	}
	for path := range baseData {
		if _, ok := curData[path]; !ok {
			d.Deleted = append(d.Deleted, path)
		}
	}
	sort.Strings(d.Added)
	sort.Strings(d.Modified)
	sort.Strings(d.Deleted)
	return d
}
