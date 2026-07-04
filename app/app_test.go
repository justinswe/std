package app

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestRunCommand(t *testing.T) {
	t.Chdir(t.TempDir())
	unsetEnv(t, _bazelWorkspaceDirectoryEnv)
	unsetEnv(t, "MESSAGE")

	wantErr := errors.New("command failed")
	tests := []struct {
		name    string
		runE    func(*cobra.Command, []string) error
		wantErr error
	}{
		{
			name: "success",
			runE: func(*cobra.Command, []string) error { return nil },
		},
		{
			name:    "command error",
			runE:    func(*cobra.Command, []string) error { return wantErr },
			wantErr: wantErr,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := quietCommand("service")
			root.RunE = tt.runE

			err := runCommand(context.Background(), root, testRunConfig(nil))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("runCommand() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestRunCobraCommand(t *testing.T) {
	t.Chdir(t.TempDir())
	unsetEnv(t, _bazelWorkspaceDirectoryEnv)

	called := false
	root := quietCommand("service")
	root.SetArgs([]string{})
	root.Run = func(*cobra.Command, []string) { called = true }
	if err := RunCobraCommand(context.Background(), root); err != nil {
		t.Fatalf("RunCobraCommand() error = %v", err)
	}
	if !called {
		t.Fatal("command was not called")
	}
}

func TestRunCommandConfigurationPrecedence(t *testing.T) {
	tests := []struct {
		name       string
		fileValue  string
		envValue   *string
		args       []string
		programmed *string
		want       string
	}{
		{name: "default", want: "default"},
		{name: "dotenv", fileValue: "file", want: "file"},
		{name: "process environment", fileValue: "file", envValue: stringPointer("env"), want: "env"},
		{name: "empty process environment", fileValue: "file", envValue: stringPointer(""), want: ""},
		{name: "CLI", fileValue: "file", envValue: stringPointer("env"), args: []string{"--message=cli"}, want: "cli"},
		{name: "programmatic", fileValue: "file", envValue: stringPointer("env"), programmed: stringPointer("set"), want: "set"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			unsetEnv(t, _bazelWorkspaceDirectoryEnv)
			unsetEnv(t, "MESSAGE")
			if tt.fileValue != "" {
				writeFile(t, ".env", "MESSAGE="+tt.fileValue+"\n")
			}
			if tt.envValue != nil {
				t.Setenv("MESSAGE", *tt.envValue)
			}

			root := quietCommand("service")
			root.PersistentFlags().String("message", "default", "test message")
			if tt.programmed != nil {
				if err := root.PersistentFlags().Set("message", *tt.programmed); err != nil {
					t.Fatal(err)
				}
			}
			root.SetArgs(tt.args)
			var got string
			root.RunE = func(cmd *cobra.Command, _ []string) error {
				got, _ = cmd.Flags().GetString("message")
				return nil
			}

			if err := runCommand(context.Background(), root, testRunConfig(nil)); err != nil {
				t.Fatalf("runCommand() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("message = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRunCommandInitializesLoggerBeforeHooks(t *testing.T) {
	t.Chdir(t.TempDir())
	unsetEnv(t, _bazelWorkspaceDirectoryEnv)

	core, logs := observer.New(zapcore.DebugLevel)
	logger := zap.New(core)
	root := quietCommand("service")
	child := quietCommand("child")
	root.AddCommand(child)
	root.SetArgs([]string{"child", "--log-level=-1"})

	var order []string
	root.PersistentPreRun = func(*cobra.Command, []string) {
		order = append(order, "persistent")
		zap.L().Info("persistent hook")
	}
	child.PreRunE = func(*cobra.Command, []string) error {
		order = append(order, "pre")
		zap.L().Info("pre hook")
		return nil
	}
	child.RunE = func(*cobra.Command, []string) error {
		order = append(order, "run")
		zap.L().Info("run hook")
		return nil
	}

	before := zap.L()
	if err := runCommand(context.Background(), root, testRunConfig(logger)); err != nil {
		t.Fatalf("runCommand() error = %v", err)
	}
	if zap.L() != before {
		t.Fatal("global logger was not restored")
	}
	if !reflect.DeepEqual(order, []string{"persistent", "pre", "run"}) {
		t.Errorf("hook order = %v", order)
	}
	if got := logs.FilterMessage("persistent hook").Len(); got != 1 {
		t.Errorf("persistent hook log count = %d, want 1", got)
	}
	if got := logs.FilterMessage("Service completed successfully").Len(); got != 1 {
		t.Errorf("completion log count = %d, want 1", got)
	}
}

func TestRunCommandMapsNestedRequiredFlag(t *testing.T) {
	t.Chdir(t.TempDir())
	unsetEnv(t, _bazelWorkspaceDirectoryEnv)
	t.Setenv("CHILD_VALUE", "configured")

	root := quietCommand("service")
	child := quietCommand("child")
	child.Flags().String("child-value", "", "required child value")
	if err := child.MarkFlagRequired("child-value"); err != nil {
		t.Fatal(err)
	}
	var got string
	child.RunE = func(cmd *cobra.Command, _ []string) error {
		got, _ = cmd.Flags().GetString("child-value")
		return nil
	}
	root.AddCommand(child)
	root.SetArgs([]string{"child"})

	if err := runCommand(context.Background(), root, testRunConfig(nil)); err != nil {
		t.Fatalf("runCommand() error = %v", err)
	}
	if got != "configured" {
		t.Errorf("child-value = %q, want configured", got)
	}
}

func TestRunCommandRestoresHooks(t *testing.T) {
	t.Chdir(t.TempDir())
	unsetEnv(t, _bazelWorkspaceDirectoryEnv)

	original := func(*cobra.Command, []string) {}
	root := quietCommand("service")
	root.PreRun = original
	root.Run = func(*cobra.Command, []string) {}
	originalPointer := reflect.ValueOf(root.PreRun).Pointer()

	if err := runCommand(context.Background(), root, testRunConfig(nil)); err != nil {
		t.Fatal(err)
	}
	if root.PreRun == nil || reflect.ValueOf(root.PreRun).Pointer() != originalPointer {
		t.Fatal("PreRun hook was not restored")
	}
	if root.PreRunE != nil {
		t.Fatal("injected PreRunE hook was not removed")
	}
}

func TestRunCommandCancellation(t *testing.T) {
	t.Chdir(t.TempDir())
	unsetEnv(t, _bazelWorkspaceDirectoryEnv)

	parent, cancel := context.WithCancel(context.Background())
	root := quietCommand("service")
	started := make(chan struct{})
	root.RunE = func(cmd *cobra.Command, _ []string) error {
		close(started)
		<-cmd.Context().Done()
		return nil
	}

	done := make(chan error, 1)
	go func() {
		done <- runCommand(parent, root, testRunConfig(nil))
	}()
	<-started
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("runCommand() error = %v", err)
	}
}

func TestRunCommandRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name string
		ctx  context.Context
		root *cobra.Command
	}{
		{name: "nil context", root: quietCommand("service")},
		{name: "nil command", ctx: context.Background()},
		{name: "cancelled context", ctx: cancelledContext(), root: quietCommand("service")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := runCommand(tt.ctx, tt.root, testRunConfig(nil)); err == nil {
				t.Fatal("runCommand() error = nil, want error")
			}
		})
	}
}

func TestRunCommandRejectsInvalidLoggingFlags(t *testing.T) {
	t.Chdir(t.TempDir())
	unsetEnv(t, _bazelWorkspaceDirectoryEnv)

	t.Run("range", func(t *testing.T) {
		root := quietCommand("service")
		root.SetArgs([]string{"--log-level=6"})
		root.Run = func(*cobra.Command, []string) {}
		if err := runCommand(context.Background(), root, testRunConfig(nil)); err == nil {
			t.Fatal("runCommand() error = nil, want error")
		}
	})

	t.Run("local collision", func(t *testing.T) {
		root := quietCommand("service")
		root.Flags().Int(_logLevelFlag, 0, "collision")
		if err := runCommand(context.Background(), root, testRunConfig(nil)); err == nil {
			t.Fatal("runCommand() error = nil, want error")
		}
	})

	t.Run("wrong persistent type", func(t *testing.T) {
		root := quietCommand("service")
		root.PersistentFlags().String(_logLevelFlag, "info", "collision")
		if err := runCommand(context.Background(), root, testRunConfig(nil)); err == nil {
			t.Fatal("runCommand() error = nil, want error")
		}
	})

	t.Run("invalid environment value", func(t *testing.T) {
		t.Setenv("DEBUG_FORMAT", "sometimes")
		root := quietCommand("service")
		if err := runCommand(context.Background(), root, testRunConfig(nil)); err == nil {
			t.Fatal("runCommand() error = nil, want error")
		}
	})
}

func TestRunCommandPropagatesPreRunError(t *testing.T) {
	t.Chdir(t.TempDir())
	unsetEnv(t, _bazelWorkspaceDirectoryEnv)

	wantErr := errors.New("pre-run failed")
	root := quietCommand("service")
	root.PersistentPreRunE = func(*cobra.Command, []string) error { return wantErr }
	root.Run = func(*cobra.Command, []string) { t.Error("Run called after pre-run error") }

	err := runCommand(context.Background(), root, testRunConfig(nil))
	if !errors.Is(err, wantErr) {
		t.Fatalf("runCommand() error = %v, want %v", err, wantErr)
	}
}

func quietCommand(use string) *cobra.Command {
	return &cobra.Command{Use: use, SilenceErrors: true, SilenceUsage: true}
}

func testRunConfig(logger *zap.Logger) runConfig {
	if logger == nil {
		logger = zap.NewNop()
	}
	return runConfig{
		notifyContext: func(ctx context.Context, _ ...os.Signal) (context.Context, context.CancelFunc) {
			return context.WithCancel(ctx)
		},
		newLogger: func(zapcore.Level, bool) *zap.Logger { return logger },
	}
}

func cancelledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func stringPointer(value string) *string {
	return &value
}

func unsetEnv(t *testing.T, key string) {
	t.Helper()
	value, found := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if found {
			_ = os.Setenv(key, value)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}

func writeFile(t *testing.T, name, contents string) {
	t.Helper()
	if err := os.WriteFile(name, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
