package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// safePath resolves p inside rootDir and rejects path-traversal attempts.
// rootDir="" disables the check (unrestricted mode).
func safePath(rootDir, p string) (string, error) {
	if rootDir == "" {
		return filepath.Clean(p), nil
	}
	root := filepath.Clean(rootDir)
	abs := filepath.Clean(filepath.Join(root, p))
	if abs != root && !strings.HasPrefix(abs, root+string(os.PathSeparator)) {
		return "", fmt.Errorf("path %q escapes allowed root %q", p, rootDir)
	}
	return abs, nil
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

// unmarshal is a tiny DRY helper used by every Execute method.
func unmarshal(raw json.RawMessage, v any) error { return json.Unmarshal(raw, v) }

// ─── fs_read ──────────────────────────────────────────────────────────────────

// FSRead reads the content of a file.
type FSRead struct{ RootDir string }

func (t FSRead) Name() string        { return "fs_read" }
func (t FSRead) Description() string { return "Read a file's text content." }
func (t FSRead) Schema() ParameterSchema {
	return NewSchema([]string{"path"}, map[string]Property{
		"path":      {Type: "string", Description: "File path to read"},
		"max_bytes": {Type: "integer", Description: "Bytes to return (default 32768)"},
	})
}
func (t FSRead) Execute(_ context.Context, raw json.RawMessage) Result {
	var a struct {
		Path     string `json:"path"`
		MaxBytes int    `json:"max_bytes"`
	}
	if err := unmarshal(raw, &a); err != nil {
		return Errorf("invalid args: %v", err)
	}
	if a.MaxBytes <= 0 {
		a.MaxBytes = 32768
	}
	p, err := safePath(t.RootDir, a.Path)
	if err != nil {
		return Errorf("%v", err)
	}
	f, err := os.Open(p)
	if err != nil {
		return Errorf("open %q: %v", p, err)
	}
	defer f.Close()

	buf := make([]byte, a.MaxBytes)
	n, _ := f.Read(buf)
	suffix := ""
	if info, _ := f.Stat(); info != nil && info.Size() > int64(a.MaxBytes) {
		suffix = fmt.Sprintf("\n[truncated: %d/%d bytes]", n, info.Size())
	}
	return Result{Content: string(buf[:n]) + suffix}
}

// ─── fs_write ─────────────────────────────────────────────────────────────────

// FSWrite writes or appends content to a file.
type FSWrite struct{ RootDir string }

func (t FSWrite) Name() string { return "fs_write" }
func (t FSWrite) Description() string {
	return "Write or append text to a file. Creates parent dirs as needed."
}
func (t FSWrite) Schema() ParameterSchema {
	return NewSchema([]string{"path", "content"}, map[string]Property{
		"path":    {Type: "string", Description: "Destination file path"},
		"content": {Type: "string", Description: "Text to write"},
		"append":  {Type: "boolean", Description: "Append instead of overwrite (default false)"},
	})
}
func (t FSWrite) Execute(_ context.Context, raw json.RawMessage) Result {
	var a struct {
		Path    string `json:"path"`
		Content string `json:"content"`
		Append  bool   `json:"append"`
	}
	if err := unmarshal(raw, &a); err != nil {
		return Errorf("invalid args: %v", err)
	}
	p, err := safePath(t.RootDir, a.Path)
	if err != nil {
		return Errorf("%v", err)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return Errorf("mkdir: %v", err)
	}
	flags := os.O_CREATE | os.O_WRONLY
	if a.Append {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	f, err := os.OpenFile(p, flags, 0o644)
	if err != nil {
		return Errorf("open: %v", err)
	}
	defer f.Close()
	n, err := f.WriteString(a.Content)
	if err != nil {
		return Errorf("write: %v", err)
	}
	return Result{Content: fmt.Sprintf("wrote %d bytes to %q", n, p)}
}

// ─── fs_list ──────────────────────────────────────────────────────────────────

// FSList lists a directory.
type FSList struct{ RootDir string }

func (t FSList) Name() string        { return "fs_list" }
func (t FSList) Description() string { return "List files and directories at a path." }
func (t FSList) Schema() ParameterSchema {
	return NewSchema([]string{"path"}, map[string]Property{
		"path":      {Type: "string", Description: "Directory to list"},
		"recursive": {Type: "boolean", Description: "Recurse subdirectories (default false)"},
		"max_items": {Type: "integer", Description: "Max entries (default 200)"},
	})
}
func (t FSList) Execute(_ context.Context, raw json.RawMessage) Result {
	var a struct {
		Path      string `json:"path"`
		Recursive bool   `json:"recursive"`
		MaxItems  int    `json:"max_items"`
	}
	if err := unmarshal(raw, &a); err != nil {
		return Errorf("invalid args: %v", err)
	}
	if a.MaxItems <= 0 {
		a.MaxItems = 200
	}
	p, err := safePath(t.RootDir, a.Path)
	if err != nil {
		return Errorf("%v", err)
	}

	var lines []string
	filepath.WalkDir(p, func(path string, d fs.DirEntry, err error) error {
		if err != nil || len(lines) >= a.MaxItems {
			return nil
		}
		rel, _ := filepath.Rel(p, path)
		if rel == "." {
			return nil
		}
		size := ""
		if !d.IsDir() {
			if info, _ := d.Info(); info != nil {
				size = humanSize(info.Size())
			}
		}
		suffix := ""
		if d.IsDir() {
			suffix = "/"
			if !a.Recursive {
				defer func() {}() // keep walking but skip contents
				return fs.SkipDir
			}
		}
		lines = append(lines, fmt.Sprintf("%s%s\t%s", rel, suffix, size))
		return nil
	})

	if len(lines) == 0 {
		return Result{Content: "(empty)"}
	}
	out := strings.Join(lines, "\n")
	if len(lines) >= a.MaxItems {
		out += fmt.Sprintf("\n[limited to %d entries]", a.MaxItems)
	}
	return Result{Content: out}
}

// ─── fs_delete ────────────────────────────────────────────────────────────────

// FSDelete deletes a file or directory.
type FSDelete struct{ RootDir string }

func (t FSDelete) Name() string        { return "fs_delete" }
func (t FSDelete) Description() string { return "Delete a file or directory." }
func (t FSDelete) Schema() ParameterSchema {
	return NewSchema([]string{"path"}, map[string]Property{
		"path":      {Type: "string", Description: "Path to delete"},
		"recursive": {Type: "boolean", Description: "Remove directories recursively"},
	})
}
func (t FSDelete) Execute(_ context.Context, raw json.RawMessage) Result {
	var a struct {
		Path      string `json:"path"`
		Recursive bool   `json:"recursive"`
	}
	if err := unmarshal(raw, &a); err != nil {
		return Errorf("invalid args: %v", err)
	}
	p, err := safePath(t.RootDir, a.Path)
	if err != nil {
		return Errorf("%v", err)
	}
	if a.Recursive {
		err = os.RemoveAll(p)
	} else {
		err = os.Remove(p)
	}
	if err != nil {
		return Errorf("%v", err)
	}
	return Result{Content: fmt.Sprintf("deleted %q", p)}
}

// ─── fs_stat ──────────────────────────────────────────────────────────────────

// FSStat returns metadata about a path.
type FSStat struct{ RootDir string }

func (t FSStat) Name() string { return "fs_stat" }
func (t FSStat) Description() string {
	return "Get size, permissions, and modification time of a file or directory."
}
func (t FSStat) Schema() ParameterSchema {
	return NewSchema([]string{"path"}, map[string]Property{
		"path": {Type: "string", Description: "Path to inspect"},
	})
}
func (t FSStat) Execute(_ context.Context, raw json.RawMessage) Result {
	var a struct {
		Path string `json:"path"`
	}
	if err := unmarshal(raw, &a); err != nil {
		return Errorf("invalid args: %v", err)
	}
	p, err := safePath(t.RootDir, a.Path)
	if err != nil {
		return Errorf("%v", err)
	}
	info, err := os.Stat(p)
	if err != nil {
		return Errorf("%v", err)
	}
	kind := "file"
	if info.IsDir() {
		kind = "directory"
	}
	return Result{Content: fmt.Sprintf("type: %s\nsize: %s\nmode: %s\nmodified: %s",
		kind, humanSize(info.Size()), info.Mode(), info.ModTime().Format(time.RFC3339))}
}

// ─── fs_mkdir ─────────────────────────────────────────────────────────────────

// FSMkdir creates a directory tree.
type FSMkdir struct{ RootDir string }

func (t FSMkdir) Name() string        { return "fs_mkdir" }
func (t FSMkdir) Description() string { return "Create a directory and any missing parents." }
func (t FSMkdir) Schema() ParameterSchema {
	return NewSchema([]string{"path"}, map[string]Property{
		"path": {Type: "string", Description: "Directory path to create"},
	})
}
func (t FSMkdir) Execute(_ context.Context, raw json.RawMessage) Result {
	var a struct {
		Path string `json:"path"`
	}
	if err := unmarshal(raw, &a); err != nil {
		return Errorf("invalid args: %v", err)
	}
	p, err := safePath(t.RootDir, a.Path)
	if err != nil {
		return Errorf("%v", err)
	}
	if err := os.MkdirAll(p, 0o755); err != nil {
		return Errorf("%v", err)
	}
	return Result{Content: fmt.Sprintf("created %q", p)}
}
