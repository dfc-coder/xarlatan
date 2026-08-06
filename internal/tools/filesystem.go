package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultMaxReadBytes  int64 = 1 << 20
	defaultMaxWriteBytes int64 = 1 << 20
)

// FilesystemErrorCode identifies a sandbox policy or boundary failure.
type FilesystemErrorCode string

const (
	FilesystemInvalidRoot   FilesystemErrorCode = "invalid_root"
	FilesystemInvalidPath   FilesystemErrorCode = "invalid_path"
	FilesystemPathEscape    FilesystemErrorCode = "path_escape"
	FilesystemRootProtected FilesystemErrorCode = "root_protected"
	FilesystemReadLimit     FilesystemErrorCode = "read_limit"
	FilesystemWriteLimit    FilesystemErrorCode = "write_limit"
)

// FilesystemError is returned before an unsafe filesystem operation occurs.
type FilesystemError struct {
	Code FilesystemErrorCode
	Path string
	Err  error
}

func (e *FilesystemError) Error() string {
	if e == nil {
		return "filesystem error"
	}
	if e.Err != nil {
		return fmt.Sprintf("filesystem %s for %q: %v", e.Code, e.Path, e.Err)
	}
	return fmt.Sprintf("filesystem %s for %q", e.Code, e.Path)
}

func (e *FilesystemError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// IsFilesystemError reports whether err has the requested filesystem code.
func IsFilesystemError(err error, code FilesystemErrorCode) bool {
	var target *FilesystemError
	return errors.As(err, &target) && target.Code == code
}

// FilesystemSandbox owns the canonical root and hard payload limits shared by
// all filesystem tools in one registry.
type FilesystemSandbox struct {
	root          string
	maxReadBytes  int64
	maxWriteBytes int64
}

// NewFilesystemSandbox resolves root once and rejects missing, non-directory,
// or filesystem-root sandboxes.
func NewFilesystemSandbox(root string, maxReadBytes, maxWriteBytes int64) (*FilesystemSandbox, error) {
	if strings.TrimSpace(root) == "" {
		return nil, &FilesystemError{Code: FilesystemInvalidRoot, Path: root, Err: errors.New("sandbox root is required")}
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, &FilesystemError{Code: FilesystemInvalidRoot, Path: root, Err: err}
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, &FilesystemError{Code: FilesystemInvalidRoot, Path: root, Err: err}
	}
	canonical = filepath.Clean(canonical)
	volumeRoot := filepath.VolumeName(canonical) + string(os.PathSeparator)
	if canonical == volumeRoot {
		return nil, &FilesystemError{Code: FilesystemInvalidRoot, Path: root, Err: errors.New("filesystem root cannot be used as sandbox")}
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return nil, &FilesystemError{Code: FilesystemInvalidRoot, Path: root, Err: err}
	}
	if !info.IsDir() {
		return nil, &FilesystemError{Code: FilesystemInvalidRoot, Path: root, Err: errors.New("sandbox root is not a directory")}
	}
	if maxReadBytes <= 0 {
		maxReadBytes = defaultMaxReadBytes
	}
	if maxWriteBytes <= 0 {
		maxWriteBytes = defaultMaxWriteBytes
	}
	return &FilesystemSandbox{root: canonical, maxReadBytes: maxReadBytes, maxWriteBytes: maxWriteBytes}, nil
}

func (s *FilesystemSandbox) resolve(path string, mutation bool) (string, error) {
	if s == nil {
		return "", &FilesystemError{Code: FilesystemInvalidRoot, Path: path, Err: errors.New("sandbox is nil")}
	}
	trimmed := strings.TrimSpace(path)
	var candidate string
	if filepath.IsAbs(trimmed) {
		candidate = filepath.Clean(trimmed)
	} else {
		candidate = filepath.Clean(filepath.Join(s.root, trimmed))
	}
	if !withinRoot(s.root, candidate) {
		return "", &FilesystemError{Code: FilesystemPathEscape, Path: path, Err: errors.New("path escapes sandbox root")}
	}
	if mutation && (trimmed == "" || candidate == s.root) {
		return "", &FilesystemError{Code: FilesystemRootProtected, Path: path, Err: errors.New("sandbox root cannot be mutated")}
	}

	resolved, err := resolveExistingOrParent(candidate)
	if err != nil {
		return "", &FilesystemError{Code: FilesystemInvalidPath, Path: path, Err: err}
	}
	if !withinRoot(s.root, resolved) {
		return "", &FilesystemError{Code: FilesystemPathEscape, Path: path, Err: errors.New("resolved path escapes sandbox root")}
	}
	if mutation {
		if info, lstatErr := os.Lstat(candidate); lstatErr == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", &FilesystemError{Code: FilesystemInvalidPath, Path: path, Err: errors.New("mutating a symbolic link is not allowed")}
		} else if lstatErr != nil && !errors.Is(lstatErr, os.ErrNotExist) {
			return "", &FilesystemError{Code: FilesystemInvalidPath, Path: path, Err: lstatErr}
		}
	}
	return resolved, nil
}

func resolveExistingOrParent(candidate string) (string, error) {
	current := filepath.Clean(candidate)
	missing := make([]string, 0, 4)
	for {
		_, err := os.Lstat(current)
		switch {
		case err == nil:
			resolved, evalErr := filepath.EvalSymlinks(current)
			if evalErr != nil {
				return "", evalErr
			}
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return filepath.Clean(resolved), nil
		case errors.Is(err, os.ErrNotExist):
			parent := filepath.Dir(current)
			if parent == current {
				return "", err
			}
			missing = append(missing, filepath.Base(current))
			current = parent
		default:
			return "", err
		}
	}
}

func withinRoot(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator)))
}

// safePath is kept as the package-level boundary helper used by tests and
// legacy callers. Runtime construction should share one FilesystemSandbox.
func safePath(rootDir, path string) (string, error) {
	sandbox, err := NewFilesystemSandbox(rootDir, defaultMaxReadBytes, defaultMaxWriteBytes)
	if err != nil {
		return "", err
	}
	return sandbox.resolve(path, false)
}

func sandboxFor(root string, maxReadBytes, maxWriteBytes int64, existing *FilesystemSandbox) (*FilesystemSandbox, error) {
	if existing != nil {
		return existing, nil
	}
	return NewFilesystemSandbox(root, maxReadBytes, maxWriteBytes)
}

func checkContext(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

// humanSize formats a byte count for human reading.
func humanSize(b int64) string {
	switch {
	case b < 1024:
		return fmt.Sprintf("%d B", b)
	case b < 1<<20:
		return fmt.Sprintf("%.1f KB", float64(b)/1024)
	case b < 1<<30:
		return fmt.Sprintf("%.1f MB", float64(b)/(1<<20))
	default:
		return fmt.Sprintf("%.1f GB", float64(b)/(1<<30))
	}
}

func unmarshal(raw json.RawMessage, value any) error { return json.Unmarshal(raw, value) }

// FSRead reads the content of a file.
type FSRead struct {
	RootDir string
	MaxBytes int64
	Sandbox *FilesystemSandbox
}

func (t FSRead) Name() string        { return "fs_read" }
func (t FSRead) Description() string { return "Read a text file inside the configured sandbox." }
func (t FSRead) Schema() ParameterSchema {
	return NewSchema([]string{"path"}, map[string]Property{
		"path":      {Type: "string", Description: "File path relative to the sandbox"},
		"max_bytes": {Type: "integer", Description: "Optional response truncation below the configured hard limit"},
	})
}
func (t FSRead) Execute(ctx context.Context, raw json.RawMessage) Result {
	if err := checkContext(ctx); err != nil {
		return Errorf("read cancelled: %v", err)
	}
	var args struct {
		Path     string `json:"path"`
		MaxBytes int64  `json:"max_bytes"`
	}
	if err := unmarshal(raw, &args); err != nil {
		return Errorf("invalid args: %v", err)
	}
	sandbox, err := sandboxFor(t.RootDir, t.MaxBytes, defaultMaxWriteBytes, t.Sandbox)
	if err != nil {
		return Errorf("%v", err)
	}
	path, err := sandbox.resolve(args.Path, false)
	if err != nil {
		return Errorf("%v", err)
	}
	file, err := os.Open(path)
	if err != nil {
		return Errorf("open %q: %v", path, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return Errorf("stat %q: %v", path, err)
	}
	if !info.Mode().IsRegular() {
		return Errorf("read %q: not a regular file", path)
	}
	if info.Size() > sandbox.maxReadBytes {
		return Errorf("read limit exceeded: %d bytes exceeds %d", info.Size(), sandbox.maxReadBytes)
	}
	data, err := io.ReadAll(io.LimitReader(file, sandbox.maxReadBytes+1))
	if err != nil {
		return Errorf("read %q: %v", path, err)
	}
	if int64(len(data)) > sandbox.maxReadBytes {
		return Errorf("read limit exceeded: file grew beyond %d bytes", sandbox.maxReadBytes)
	}
	if args.MaxBytes > 0 && args.MaxBytes < int64(len(data)) {
		returned := args.MaxBytes
		return Result{Content: string(data[:returned]) + fmt.Sprintf("\n[truncated: %d/%d bytes]", returned, len(data))}
	}
	return Result{Content: string(data)}
}

// FSWrite writes or appends content atomically inside the sandbox.
type FSWrite struct {
	RootDir string
	MaxBytes int64
	Sandbox *FilesystemSandbox
}

func (t FSWrite) Name() string { return "fs_write" }
func (t FSWrite) Description() string {
	return "Atomically write or append text to a file inside the configured sandbox."
}
func (t FSWrite) Schema() ParameterSchema {
	return NewSchema([]string{"path", "content"}, map[string]Property{
		"path":    {Type: "string", Description: "Destination path relative to the sandbox"},
		"content": {Type: "string", Description: "Text to write"},
		"append":  {Type: "boolean", Description: "Append using an atomic replacement"},
	})
}
func (t FSWrite) Execute(ctx context.Context, raw json.RawMessage) Result {
	if err := checkContext(ctx); err != nil {
		return Errorf("write cancelled: %v", err)
	}
	var args struct {
		Path    string `json:"path"`
		Content string `json:"content"`
		Append  bool   `json:"append"`
	}
	if err := unmarshal(raw, &args); err != nil {
		return Errorf("invalid args: %v", err)
	}
	sandbox, err := sandboxFor(t.RootDir, defaultMaxReadBytes, t.MaxBytes, t.Sandbox)
	if err != nil {
		return Errorf("%v", err)
	}
	if int64(len(args.Content)) > sandbox.maxWriteBytes {
		return Errorf("write limit exceeded: %d bytes exceeds %d", len(args.Content), sandbox.maxWriteBytes)
	}
	path, err := sandbox.resolve(args.Path, true)
	if err != nil {
		return Errorf("%v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Errorf("mkdir: %v", err)
	}
	path, err = sandbox.resolve(args.Path, true)
	if err != nil {
		return Errorf("%v", err)
	}

	data := []byte(args.Content)
	mode := fs.FileMode(0o644)
	if info, statErr := os.Stat(path); statErr == nil {
		if !info.Mode().IsRegular() {
			return Errorf("write %q: destination is not a regular file", path)
		}
		mode = info.Mode().Perm()
		if args.Append {
			if info.Size()+int64(len(data)) > sandbox.maxWriteBytes {
				return Errorf("write limit exceeded: resulting file exceeds %d bytes", sandbox.maxWriteBytes)
			}
			existing, readErr := os.ReadFile(path)
			if readErr != nil {
				return Errorf("read existing file: %v", readErr)
			}
			data = append(existing, data...)
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return Errorf("stat destination: %v", statErr)
	}
	if err := checkContext(ctx); err != nil {
		return Errorf("write cancelled: %v", err)
	}
	if err := atomicWriteFile(path, data, mode, os.Rename); err != nil {
		return Errorf("atomic write: %v", err)
	}
	return Result{Content: fmt.Sprintf("wrote %d bytes to %q", len(args.Content), path)}
}

func atomicWriteFile(path string, data []byte, mode fs.FileMode, rename func(string, string) error) error {
	if rename == nil {
		rename = os.Rename
	}
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".xarlatan-write-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	closed := false
	defer func() {
		if !closed {
			_ = temporary.Close()
		}
		_ = os.Remove(temporaryPath)
	}()
	if err := temporary.Chmod(mode.Perm()); err != nil {
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	closed = true
	if err := rename(temporaryPath, path); err != nil {
		return err
	}
	if directoryHandle, err := os.Open(directory); err == nil {
		_ = directoryHandle.Sync()
		_ = directoryHandle.Close()
	}
	return nil
}

// FSList lists a directory without following child symlinks.
type FSList struct {
	RootDir string
	Sandbox *FilesystemSandbox
}

func (t FSList) Name() string        { return "fs_list" }
func (t FSList) Description() string { return "List files and directories inside the configured sandbox." }
func (t FSList) Schema() ParameterSchema {
	return NewSchema([]string{"path"}, map[string]Property{
		"path":      {Type: "string", Description: "Directory path relative to the sandbox"},
		"recursive": {Type: "boolean", Description: "Recurse into real subdirectories"},
		"max_items": {Type: "integer", Description: "Maximum entries, default 200"},
	})
}
func (t FSList) Execute(ctx context.Context, raw json.RawMessage) Result {
	if err := checkContext(ctx); err != nil {
		return Errorf("list cancelled: %v", err)
	}
	var args struct {
		Path      string `json:"path"`
		Recursive bool   `json:"recursive"`
		MaxItems  int    `json:"max_items"`
	}
	if err := unmarshal(raw, &args); err != nil {
		return Errorf("invalid args: %v", err)
	}
	if args.MaxItems <= 0 {
		args.MaxItems = 200
	}
	sandbox, err := sandboxFor(t.RootDir, defaultMaxReadBytes, defaultMaxWriteBytes, t.Sandbox)
	if err != nil {
		return Errorf("%v", err)
	}
	path, err := sandbox.resolve(args.Path, false)
	if err != nil {
		return Errorf("%v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return Errorf("stat %q: %v", path, err)
	}
	if !info.IsDir() {
		return Errorf("list %q: not a directory", path)
	}

	lines := make([]string, 0, args.MaxItems)
	walkErr := filepath.WalkDir(path, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := checkContext(ctx); err != nil {
			return err
		}
		if current == path {
			return nil
		}
		if len(lines) >= args.MaxItems {
			return fs.SkipAll
		}
		relative, err := filepath.Rel(path, current)
		if err != nil {
			return err
		}
		size := ""
		if entry.Type().IsRegular() {
			if entryInfo, infoErr := entry.Info(); infoErr == nil {
				size = humanSize(entryInfo.Size())
			}
		}
		suffix := ""
		if entry.IsDir() {
			suffix = "/"
		}
		lines = append(lines, fmt.Sprintf("%s%s\t%s", relative, suffix, size))
		if entry.IsDir() && !args.Recursive {
			return fs.SkipDir
		}
		return nil
	})
	if walkErr != nil {
		return Errorf("list %q: %v", path, walkErr)
	}
	if len(lines) == 0 {
		return Result{Content: "(empty)"}
	}
	output := strings.Join(lines, "\n")
	if len(lines) >= args.MaxItems {
		output += fmt.Sprintf("\n[limited to %d entries]", args.MaxItems)
	}
	return Result{Content: output}
}

// FSDelete deletes a non-root file or directory inside the sandbox.
type FSDelete struct {
	RootDir string
	Sandbox *FilesystemSandbox
}

func (t FSDelete) Name() string        { return "fs_delete" }
func (t FSDelete) Description() string { return "Delete a non-root file or directory inside the configured sandbox." }
func (t FSDelete) Schema() ParameterSchema {
	return NewSchema([]string{"path"}, map[string]Property{
		"path":      {Type: "string", Description: "Path relative to the sandbox"},
		"recursive": {Type: "boolean", Description: "Remove directories recursively"},
	})
}
func (t FSDelete) Execute(ctx context.Context, raw json.RawMessage) Result {
	if err := checkContext(ctx); err != nil {
		return Errorf("delete cancelled: %v", err)
	}
	var args struct {
		Path      string `json:"path"`
		Recursive bool   `json:"recursive"`
	}
	if err := unmarshal(raw, &args); err != nil {
		return Errorf("invalid args: %v", err)
	}
	sandbox, err := sandboxFor(t.RootDir, defaultMaxReadBytes, defaultMaxWriteBytes, t.Sandbox)
	if err != nil {
		return Errorf("%v", err)
	}
	path, err := sandbox.resolve(args.Path, true)
	if err != nil {
		return Errorf("%v", err)
	}
	if args.Recursive {
		err = os.RemoveAll(path)
	} else {
		err = os.Remove(path)
	}
	if err != nil {
		return Errorf("delete %q: %v", path, err)
	}
	return Result{Content: fmt.Sprintf("deleted %q", path)}
}

// FSStat returns metadata about a path inside the sandbox.
type FSStat struct {
	RootDir string
	Sandbox *FilesystemSandbox
}

func (t FSStat) Name() string { return "fs_stat" }
func (t FSStat) Description() string {
	return "Get size, permissions, and modification time inside the configured sandbox."
}
func (t FSStat) Schema() ParameterSchema {
	return NewSchema([]string{"path"}, map[string]Property{
		"path": {Type: "string", Description: "Path relative to the sandbox"},
	})
}
func (t FSStat) Execute(ctx context.Context, raw json.RawMessage) Result {
	if err := checkContext(ctx); err != nil {
		return Errorf("stat cancelled: %v", err)
	}
	var args struct {
		Path string `json:"path"`
	}
	if err := unmarshal(raw, &args); err != nil {
		return Errorf("invalid args: %v", err)
	}
	sandbox, err := sandboxFor(t.RootDir, defaultMaxReadBytes, defaultMaxWriteBytes, t.Sandbox)
	if err != nil {
		return Errorf("%v", err)
	}
	path, err := sandbox.resolve(args.Path, false)
	if err != nil {
		return Errorf("%v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return Errorf("stat %q: %v", path, err)
	}
	kind := "file"
	if info.IsDir() {
		kind = "directory"
	}
	return Result{Content: fmt.Sprintf("type: %s\nsize: %s\nmode: %s\nmodified: %s", kind, humanSize(info.Size()), info.Mode(), info.ModTime().Format(time.RFC3339))}
}

// FSMkdir creates a non-root directory tree inside the sandbox.
type FSMkdir struct {
	RootDir string
	Sandbox *FilesystemSandbox
}

func (t FSMkdir) Name() string        { return "fs_mkdir" }
func (t FSMkdir) Description() string { return "Create a directory tree inside the configured sandbox." }
func (t FSMkdir) Schema() ParameterSchema {
	return NewSchema([]string{"path"}, map[string]Property{
		"path": {Type: "string", Description: "Directory path relative to the sandbox"},
	})
}
func (t FSMkdir) Execute(ctx context.Context, raw json.RawMessage) Result {
	if err := checkContext(ctx); err != nil {
		return Errorf("mkdir cancelled: %v", err)
	}
	var args struct {
		Path string `json:"path"`
	}
	if err := unmarshal(raw, &args); err != nil {
		return Errorf("invalid args: %v", err)
	}
	sandbox, err := sandboxFor(t.RootDir, defaultMaxReadBytes, defaultMaxWriteBytes, t.Sandbox)
	if err != nil {
		return Errorf("%v", err)
	}
	path, err := sandbox.resolve(args.Path, true)
	if err != nil {
		return Errorf("%v", err)
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return Errorf("mkdir %q: %v", path, err)
	}
	if _, err := sandbox.resolve(args.Path, true); err != nil {
		return Errorf("%v", err)
	}
	return Result{Content: fmt.Sprintf("created %q", path)}
}
