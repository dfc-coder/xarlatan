package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

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

func TestDeleteRejectsSandboxRoot(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "keep.txt")
	if err := os.WriteFile(marker, []byte("keep"), 0o600); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	result := (FSDelete{RootDir: root}).Execute(context.Background(), json.RawMessage(`{"path":".","recursive":true}`))
	if !result.IsError {
		t.Fatalf("FSDelete.Execute() = %#v, want root protection error", result)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("sandbox root was modified: %v", err)
	}
}

func TestDeleteRejectsEmptyPath(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "keep.txt")
	if err := os.WriteFile(marker, []byte("keep"), 0o600); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	result := (FSDelete{RootDir: root}).Execute(context.Background(), json.RawMessage(`{"path":"","recursive":true}`))
	if !result.IsError {
		t.Fatalf("FSDelete.Execute() = %#v, want empty path error", result)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("sandbox root was modified: %v", err)
	}
}
