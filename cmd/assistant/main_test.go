package main

import (
	"reflect"
	"testing"

	"github.com/dfc-coder/xarlatan/internal/config"
)

func TestBuildRegistryDefaultExposesOnlyWebTools(t *testing.T) {
	cfg := &config.Config{Tools: config.ToolsConfig{WebSearch: config.WebSearchConfig{Provider: "duckduckgo"}}}
	registry, err := buildRegistry(cfg)
	if err != nil {
		t.Fatalf("buildRegistry() error = %v", err)
	}
	want := []string{"web_search", "web_fetch"}
	if got := registry.Names(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Names() = %v, want %v", got, want)
	}
}

func TestBuildRegistryReadOnlyFilesystem(t *testing.T) {
	cfg := &config.Config{Tools: config.ToolsConfig{
		Filesystem: config.FilesystemConfig{Enabled: true, Root: t.TempDir()},
		WebSearch:  config.WebSearchConfig{Provider: "duckduckgo"},
	}}
	registry, err := buildRegistry(cfg)
	if err != nil {
		t.Fatalf("buildRegistry() error = %v", err)
	}
	want := []string{"fs_read", "fs_list", "fs_stat", "web_search", "web_fetch"}
	if got := registry.Names(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Names() = %v, want %v", got, want)
	}
}

func TestBuildRegistryAllowsMutationsOnlyWhenExplicit(t *testing.T) {
	cfg := &config.Config{Tools: config.ToolsConfig{
		Filesystem: config.FilesystemConfig{Enabled: true, Root: t.TempDir(), AllowMutations: true},
		WebSearch:  config.WebSearchConfig{Provider: "duckduckgo"},
	}}
	registry, err := buildRegistry(cfg)
	if err != nil {
		t.Fatalf("buildRegistry() error = %v", err)
	}
	want := []string{"fs_read", "fs_write", "fs_list", "fs_delete", "fs_stat", "fs_mkdir", "web_search", "web_fetch"}
	if got := registry.Names(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Names() = %v, want %v", got, want)
	}
}
