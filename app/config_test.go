package app

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestEnvFileCandidates(t *testing.T) {
	t.Run("without Bazel", func(t *testing.T) {
		unsetEnv(t, _bazelWorkspaceDirectoryEnv)
		got, err := envFileCandidates(&cobra.Command{Use: "service"})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, []string{".env"}) {
			t.Errorf("candidates = %v", got)
		}
	})

	markers := [...]string{
		_bazelModuleMarker,
		_bazelWorkspaceMarker,
		_bazelLegacyWorkspaceMarker,
	}
	for _, marker := range markers {
		t.Run(marker, func(t *testing.T) {
			workspace := t.TempDir()
			writeFile(t, filepath.Join(workspace, marker), "")
			t.Setenv(_bazelWorkspaceDirectoryEnv, workspace)
			root := &cobra.Command{Use: "service", Aliases: []string{"svc"}}

			got, err := envFileCandidates(root)
			if err != nil {
				t.Fatal(err)
			}
			workspace, err = filepath.EvalSymlinks(workspace)
			if err != nil {
				t.Fatal(err)
			}
			want := []string{
				".env",
				filepath.Join(workspace, "svc", ".env"),
				filepath.Join(workspace, "service", ".env"),
				filepath.Join(workspace, ".env"),
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("candidates = %v, want %v", got, want)
			}
		})
	}

	t.Run("deduplicates alias", func(t *testing.T) {
		workspace := t.TempDir()
		writeFile(t, filepath.Join(workspace, "MODULE.bazel"), "")
		t.Setenv(_bazelWorkspaceDirectoryEnv, workspace)
		root := &cobra.Command{Use: "service", Aliases: []string{"service"}}

		got, err := envFileCandidates(root)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 3 {
			t.Errorf("candidate count = %d, want 3: %v", len(got), got)
		}
	})
}

func TestEnvFileCandidatesRejectsInvalidWorkspace(t *testing.T) {
	tests := []struct {
		name      string
		workspace func(*testing.T) string
		alias     string
	}{
		{name: "relative", workspace: func(*testing.T) string { return "relative" }},
		{name: "missing", workspace: func(t *testing.T) string { return filepath.Join(t.TempDir(), "missing") }},
		{name: "not directory", workspace: func(t *testing.T) string {
			path := filepath.Join(t.TempDir(), "file")
			writeFile(t, path, "")
			return path
		}},
		{name: "missing marker", workspace: func(t *testing.T) string { return t.TempDir() }},
		{name: "unsafe alias", workspace: bazelWorkspace, alias: "../escape"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(_bazelWorkspaceDirectoryEnv, tt.workspace(t))
			root := &cobra.Command{Use: "service"}
			if tt.alias != "" {
				root.Aliases = []string{tt.alias}
			}
			if _, err := envFileCandidates(root); err == nil {
				t.Fatal("envFileCandidates() error = nil, want error")
			}
		})
	}
}

func TestEnvFileCandidatesRejectsSymlinkEscape(t *testing.T) {
	workspace := bazelWorkspace(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(workspace, "service")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	t.Setenv(_bazelWorkspaceDirectoryEnv, workspace)

	_, err := envFileCandidates(&cobra.Command{Use: "service"})
	if err == nil || !strings.Contains(err.Error(), "outside workspace") {
		t.Fatalf("envFileCandidates() error = %v, want containment error", err)
	}
}

func TestLoadDotEnv(t *testing.T) {
	t.Run("first existing candidate", func(t *testing.T) {
		workspace := bazelWorkspace(t)
		if err := os.Mkdir(filepath.Join(workspace, "service"), 0o700); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(workspace, "service", ".env"), "VALUE=workspace\n")
		t.Setenv(_bazelWorkspaceDirectoryEnv, workspace)
		t.Chdir(t.TempDir())

		env, err := loadDotEnv(&cobra.Command{Use: "service"})
		if err != nil {
			t.Fatal(err)
		}
		if got := env["VALUE"]; got != "workspace" {
			t.Errorf("VALUE = %q", got)
		}
	})

	t.Run("malformed", func(t *testing.T) {
		unsetEnv(t, _bazelWorkspaceDirectoryEnv)
		t.Chdir(t.TempDir())
		writeFile(t, ".env", "not valid = value\n")
		if _, err := loadDotEnv(&cobra.Command{Use: "service"}); err == nil {
			t.Fatal("loadDotEnv() error = nil, want error")
		}
	})
}

func bazelWorkspace(t *testing.T) string {
	t.Helper()
	workspace := t.TempDir()
	writeFile(t, filepath.Join(workspace, "MODULE.bazel"), "")
	return workspace
}
