package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	_logLevelFlag    = "log-level"
	_debugFormatFlag = "debug-format"
)

type runConfig struct {
	notifyContext func(context.Context, ...os.Signal) (context.Context, context.CancelFunc)
	newLogger     func(zapcore.Level, bool) *zap.Logger
}

// RunCobraCommand executes root with environment-backed flags, structured
// logging, and signal-aware context cancellation.
func RunCobraCommand(ctx context.Context, root *cobra.Command) error {
	config := runConfig{
		notifyContext: signal.NotifyContext,
		newLogger: func(level zapcore.Level, debugFormat bool) *zap.Logger {
			return newStructuredLogger(level, debugFormat, os.Stdout, os.Stderr)
		},
	}

	return runCommand(ctx, root, config)
}

func runCommand(ctx context.Context, root *cobra.Command, config runConfig) error {
	if ctx == nil {
		return errors.New("app: nil context")
	}
	if root == nil {
		return errors.New("app: nil Cobra command")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := addLoggingFlags(root); err != nil {
		return err
	}

	fileEnv, err := loadDotEnv(root)
	if err != nil {
		return err
	}
	if err := applyEnvironment(root, fileEnv); err != nil {
		return err
	}

	state := loggerState{newLogger: config.newLogger}
	restoreHooks := installLoggerHooks(root, state.initialize)
	defer restoreHooks()
	defer state.restore()

	runCtx, stop := config.notifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	err = root.ExecuteContext(runCtx)
	if err != nil || !state.initialized {
		return err
	}

	if runCtx.Err() != nil {
		zap.L().Info("Service shut down successfully", zap.String("service", root.Name()))
	} else {
		zap.L().Info("Service completed successfully", zap.String("service", root.Name()))
	}
	return nil
}

func addLoggingFlags(root *cobra.Command) error {
	if err := addPersistentFlag(root, _logLevelFlag, "int", func() {
		root.PersistentFlags().Int(
			_logLevelFlag,
			int(zapcore.InfoLevel),
			"The log level (-1=DEBUG, 0=INFO, 1=WARN, 2=ERROR, 3=DPANIC, 4=PANIC, 5=FATAL)",
		)
	}); err != nil {
		return err
	}

	return addPersistentFlag(root, _debugFormatFlag, "bool", func() {
		root.PersistentFlags().Bool(
			_debugFormatFlag,
			false,
			"Enable debug log formatting (no timestamps, extra spacing)",
		)
	})
}

func addPersistentFlag(root *cobra.Command, name, wantType string, add func()) error {
	if flag := root.PersistentFlags().Lookup(name); flag != nil {
		if flag.Value.Type() != wantType {
			return fmt.Errorf("app: persistent flag --%s must have type %s, got %s", name, wantType, flag.Value.Type())
		}
		return nil
	}
	if flag := root.LocalNonPersistentFlags().Lookup(name); flag != nil {
		return fmt.Errorf("app: flag --%s must be persistent", name)
	}

	add()
	return nil
}

type loggerState struct {
	newLogger   func(zapcore.Level, bool) *zap.Logger
	undo        func()
	initialized bool
}

func (s *loggerState) initialize(command *cobra.Command) error {
	if s.initialized {
		return nil
	}

	level, err := command.Flags().GetInt(_logLevelFlag)
	if err != nil {
		return fmt.Errorf("app: read --%s: %w", _logLevelFlag, err)
	}
	if level < int(zapcore.DebugLevel) || level > int(zapcore.FatalLevel) {
		return fmt.Errorf("app: --%s must be between %d and %d, got %d", _logLevelFlag, zapcore.DebugLevel, zapcore.FatalLevel, level)
	}

	debugFormat, err := command.Flags().GetBool(_debugFormatFlag)
	if err != nil {
		return fmt.Errorf("app: read --%s: %w", _debugFormatFlag, err)
	}

	logger := s.newLogger(zapcore.Level(level), debugFormat)
	s.undo = zap.ReplaceGlobals(logger)
	s.initialized = true
	logger.Info("Starting", zap.String("service", command.Root().Name()))
	return nil
}

func (s *loggerState) restore() {
	if s.undo != nil {
		s.undo()
	}
}

type commandHooks struct {
	command           *cobra.Command
	persistentPreRun  func(*cobra.Command, []string)
	persistentPreRunE func(*cobra.Command, []string) error
	preRun            func(*cobra.Command, []string)
	preRunE           func(*cobra.Command, []string) error
}

func installLoggerHooks(root *cobra.Command, initialize func(*cobra.Command) error) func() {
	var snapshots []commandHooks
	visitCommands(root, func(command *cobra.Command) {
		snapshot := commandHooks{
			command:           command,
			persistentPreRun:  command.PersistentPreRun,
			persistentPreRunE: command.PersistentPreRunE,
			preRun:            command.PreRun,
			preRunE:           command.PreRunE,
		}
		snapshots = append(snapshots, snapshot)

		wrapPersistentPreRun(command, snapshot, initialize)
		wrapPreRun(command, snapshot, initialize)
	})

	return func() {
		for _, snapshot := range snapshots {
			snapshot.command.PersistentPreRun = snapshot.persistentPreRun
			snapshot.command.PersistentPreRunE = snapshot.persistentPreRunE
			snapshot.command.PreRun = snapshot.preRun
			snapshot.command.PreRunE = snapshot.preRunE
		}
	}
}

func wrapPersistentPreRun(command *cobra.Command, snapshot commandHooks, initialize func(*cobra.Command) error) {
	switch {
	case snapshot.persistentPreRunE != nil:
		command.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
			if err := initialize(cmd); err != nil {
				return err
			}
			return snapshot.persistentPreRunE(cmd, args)
		}
	case snapshot.persistentPreRun != nil:
		command.PersistentPreRun = nil
		command.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
			if err := initialize(cmd); err != nil {
				return err
			}
			snapshot.persistentPreRun(cmd, args)
			return nil
		}
	}
}

func wrapPreRun(command *cobra.Command, snapshot commandHooks, initialize func(*cobra.Command) error) {
	switch {
	case snapshot.preRunE != nil:
		command.PreRunE = func(cmd *cobra.Command, args []string) error {
			if err := initialize(cmd); err != nil {
				return err
			}
			return snapshot.preRunE(cmd, args)
		}
	case snapshot.preRun != nil:
		command.PreRun = nil
		command.PreRunE = func(cmd *cobra.Command, args []string) error {
			if err := initialize(cmd); err != nil {
				return err
			}
			snapshot.preRun(cmd, args)
			return nil
		}
	default:
		command.PreRunE = func(cmd *cobra.Command, _ []string) error {
			return initialize(cmd)
		}
	}
}

func visitCommands(root *cobra.Command, visit func(*cobra.Command)) {
	visit(root)
	for _, command := range root.Commands() {
		visitCommands(command, visit)
	}
}
