package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSafePathRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	if resolved, err := safePath(root, filepath.Join("..", "outside.txt")); err == nil {
		t.Fatalf("safePath() = %q, nil; want traversal error", resolved)
	}
}

func TestSafePathRejectsAbsoluteExternalPath(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if resolved, err := safePath(root, outside); err == nil {
		t.Fatalf("safePath() = %q, nil; want absolute path escape error", resolved)
	}
}

func TestSafePathRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(target, []byte("secret"), 0o600); err != nil {
		t.Fatalf("write outside target: %v", err)
	}
	if err := os.Symlink(target, filepath.Join(root, "link")); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	if resolved, err := safePath(root, "link"); err == nil {
		t.Fatalf("safePath() = %q, nil; want symlink escape error", resolved)
	}
}

func TestSafePathAllowsInternalSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target.txt")
	if err := os.WriteFile(target, []byte("safe"), 0o600); err != nil {
		t.Fatalf("write target: %v", err)
	}
	if err := os.Symlink(target, filepath.Join(root, "link")); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	resolved, err := safePath(root, "link")
	if err != nil {
		t.Fatalf("safePath() error = %v, want internal symlink allowed", err)
	}
	if resolved != target {
		t.Fatalf("safePath() = %q, want %q", resolved, target)
	}
}

func TestSafePathRejectsParentSymlinkForNewFile(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatalf("create parent symlink: %v", err)
	}

	path := filepath.Join("escape", "new.txt")
	if resolved, err := safePath(root, path); err == nil {
		t.Fatalf("safePath() = %q, nil; want parent symlink escape error", resolved)
	}
}

func TestAllFilesystemToolsRejectSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatalf("write outside target: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	tests := []struct {
		name string
		tool Tool
		args string
	}{
		{"read", FSRead{RootDir: root}, `{"path":"escape/secret.txt"}`},
		{"stat", FSStat{RootDir: root}, `{"path":"escape/secret.txt"}`},
		{"list", FSList{RootDir: root}, `{"path":"escape"}`},
		{"write", FSWrite{RootDir: root}, `{"path":"escape/new.txt","content":"unsafe"}`},
		{"mkdir", FSMkdir{RootDir: root}, `{"path":"escape/new-dir"}`},
		{"delete", FSDelete{RootDir: root}, `{"path":"escape/secret.txt"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.tool.Execute(context.Background(), json.RawMessage(tt.args))
			if !result.IsError {
				t.Fatalf("Execute() = %#v, want sandbox escape error", result)
			}
		})
	}
}

func TestMutationsRejectSandboxRoot(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "keep.txt")
	if err := os.WriteFile(marker, []byte("keep"), 0o600); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	tests := []struct {
		name string
		tool Tool
		args string
	}{
		{"delete-dot", FSDelete{RootDir: root}, `{"path":".","recursive":true}`},
		{"delete-empty", FSDelete{RootDir: root}, `{"path":"","recursive":true}`},
		{"write-dot", FSWrite{RootDir: root}, `{"path":".","content":"unsafe"}`},
		{"write-empty", FSWrite{RootDir: root}, `{"path":"","content":"unsafe"}`},
		{"mkdir-dot", FSMkdir{RootDir: root}, `{"path":"."}`},
		{"mkdir-empty", FSMkdir{RootDir: root}, `{"path":""}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.tool.Execute(context.Background(), json.RawMessage(tt.args))
			if !result.IsError || !strings.Contains(result.Content, "sandbox root") {
				t.Fatalf("Execute() = %#v, want sandbox root protection error", result)
			}
			if _, err := os.Stat(marker); err != nil {
				t.Fatalf("sandbox root was modified: %v", err)
			}
		})
	}
}

func TestReadRejectsOversizedFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "large.txt"), []byte("12345"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	result := (FSRead{RootDir: root, MaxBytes: 4}).Execute(context.Background(), json.RawMessage(`{"path":"large.txt"}`))
	if !result.IsError || !strings.Contains(result.Content, "read limit") {
		t.Fatalf("FSRead.Execute() = %#v, want read limit error", result)
	}
}

func TestWriteRejectsOversizedPayload(t *testing.T) {
	root := t.TempDir()
	result := (FSWrite{RootDir: root, MaxBytes: 4}).Execute(context.Background(), json.RawMessage(`{"path":"large.txt","content":"12345"}`))
	if !result.IsError || !strings.Contains(result.Content, "write limit") {
		t.Fatalf("FSWrite.Execute() = %#v, want write limit error", result)
	}
	if _, err := os.Stat(filepath.Join(root, "large.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("oversized write created destination: %v", err)
	}
}

func TestWriteIsAtomic(t *testing.T) {
	root := t.TempDir()
	destination := filepath.Join(root, "state.txt")
	if err := os.WriteFile(destination, []byte("old"), 0o600); err != nil {
		t.Fatalf("write destination: %v", err)
	}

	err := atomicWriteFile(destination, []byte("new"), 0o600, func(_, _ string) error {
		return errors.New("rename failed")
	})
	if err == nil {
		t.Fatal("atomicWriteFile() error = nil, want rename failure")
	}
	content, readErr := os.ReadFile(destination)
	if readErr != nil {
		t.Fatalf("read destination: %v", readErr)
	}
	if string(content) != "old" {
		t.Fatalf("destination = %q, want original content", content)
	}
	matches, globErr := filepath.Glob(filepath.Join(root, ".xarlatan-write-*"))
	if globErr != nil {
		t.Fatalf("glob temp files: %v", globErr)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary files remain after failure: %v", matches)
	}
}

func TestReadOnlyPolicyRejectsWriteDeleteAndMkdir(t *testing.T) {
	registry := NewRegistry(NewToolPolicy("fs_read", "fs_list", "fs_stat"))
	for _, tool := range []Tool{FSWrite{}, FSDelete{}, FSMkdir{}} {
		if err := registry.Register(tool); !IsToolDenied(err) {
			t.Fatalf("Register(%s) error = %v, want ToolDeniedError", tool.Name(), err)
		}
	}
}
