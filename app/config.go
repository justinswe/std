package app

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/subosito/gotenv"
)

const (
	_bazelWorkspaceDirectoryEnv = "BUILD_WORKSPACE_DIRECTORY"
	_bazelModuleMarker          = "MODULE.bazel"
	_bazelWorkspaceMarker       = "WORKSPACE.bazel"
	_bazelLegacyWorkspaceMarker = "WORKSPACE"
)

func loadDotEnv(root *cobra.Command) (gotenv.Env, error) {
	candidates, err := envFileCandidates(root)
	if err != nil {
		return nil, err
	}

	for _, candidate := range candidates {
		contents, err := os.ReadFile(candidate)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("app: read environment file %q: %w", candidate, err)
		}

		env, err := gotenv.StrictParse(bytes.NewReader(contents))
		if err != nil {
			return nil, fmt.Errorf("app: parse environment file %q: %w", candidate, err)
		}
		return env, nil
	}

	return nil, nil
}

func envFileCandidates(command *cobra.Command) ([]string, error) {
	candidates := make([]string, 0, 4)
	candidates = append(candidates, ".env")

	workspace, found := os.LookupEnv(_bazelWorkspaceDirectoryEnv)
	if !found {
		return candidates, nil
	}

	workspace, err := validateBazelWorkspace(workspace)
	if err != nil {
		return nil, err
	}

	root := command.Root()
	if len(root.Aliases) > 0 {
		candidate, err := workspaceEnvCandidate(workspace, root.Aliases[0])
		if err != nil {
			return nil, err
		}
		candidates = appendUnique(candidates, candidate)
	}

	candidate, err := workspaceEnvCandidate(workspace, root.Name())
	if err != nil {
		return nil, err
	}
	candidates = appendUnique(candidates, candidate)
	candidates = appendUnique(candidates, filepath.Join(workspace, ".env"))

	for _, candidate := range candidates[1:] {
		if err := ensurePathWithinWorkspace(workspace, candidate); err != nil {
			return nil, err
		}
	}
	return candidates, nil
}

func validateBazelWorkspace(workspace string) (string, error) {
	if !filepath.IsAbs(workspace) {
		return "", fmt.Errorf("app: %s must be an absolute path, got %q", _bazelWorkspaceDirectoryEnv, workspace)
	}

	resolved, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		return "", fmt.Errorf("app: resolve %s %q: %w", _bazelWorkspaceDirectoryEnv, workspace, err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("app: inspect %s %q: %w", _bazelWorkspaceDirectoryEnv, resolved, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("app: %s %q is not a directory", _bazelWorkspaceDirectoryEnv, resolved)
	}

	markers := [...]string{
		_bazelModuleMarker,
		_bazelWorkspaceMarker,
		_bazelLegacyWorkspaceMarker,
	}
	for _, marker := range markers {
		info, err := os.Stat(filepath.Join(resolved, marker))
		if err == nil && !info.IsDir() {
			return resolved, nil
		}
		if err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("app: inspect Bazel workspace marker %q: %w", marker, err)
		}
	}

	return "", fmt.Errorf("app: %s %q has no Bazel workspace marker", _bazelWorkspaceDirectoryEnv, resolved)
}

func workspaceEnvCandidate(workspace, service string) (string, error) {
	if service == "" || service == "." || service == ".." || filepath.Base(service) != service {
		return "", fmt.Errorf("app: unsafe workspace service path %q", service)
	}
	return filepath.Join(workspace, service, ".env"), nil
}

func ensurePathWithinWorkspace(workspace, candidate string) error {
	pathToResolve := candidate
	if _, err := os.Lstat(candidate); os.IsNotExist(err) {
		pathToResolve = filepath.Dir(candidate)
	} else if err != nil {
		return fmt.Errorf("app: inspect environment candidate %q: %w", candidate, err)
	}

	resolved, err := filepath.EvalSymlinks(pathToResolve)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("app: resolve environment candidate %q: %w", candidate, err)
	}

	relative, err := filepath.Rel(workspace, resolved)
	if err != nil {
		return fmt.Errorf("app: compare environment candidate %q with workspace: %w", candidate, err)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("app: environment candidate %q resolves outside workspace %q", candidate, workspace)
	}
	return nil
}

func appendUnique(paths []string, candidate string) []string {
	for _, path := range paths {
		if path == candidate {
			return paths
		}
	}
	return append(paths, candidate)
}

func applyEnvironment(root *cobra.Command, fileEnv gotenv.Env) error {
	var applyErr error
	visitCommands(root, func(command *cobra.Command) {
		if applyErr != nil {
			return
		}

		apply := func(flag *pflag.Flag) {
			if applyErr != nil || flag.Changed {
				return
			}

			key := strings.ToUpper(strings.ReplaceAll(flag.Name, "-", "_"))
			value, found := os.LookupEnv(key)
			if !found {
				value, found = fileEnv[key]
			}
			if !found {
				return
			}

			if err := flag.Value.Set(value); err != nil {
				applyErr = fmt.Errorf("app: set --%s from %s: %w", flag.Name, key, err)
				return
			}
			flag.Changed = true
		}

		command.NonInheritedFlags().VisitAll(apply)
	})
	return applyErr
}
