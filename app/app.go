package app

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorCyan   = "\033[36m"
	colorGray   = "\033[90m"
	colorGreen  = "\033[32m"
)

// paddedLevelEncoder keeps level names aligned for easier scanning
func paddedLevelEncoder(level zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
	enc.AppendString(fmt.Sprintf("%-5s", level.CapitalString()))
}

// colorLevelEncoder adds ANSI colors for verbose logging
func colorLevelEncoder(level zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
	var ansi string
	switch level {
	case zapcore.DebugLevel:
		ansi = colorCyan
	case zapcore.InfoLevel:
		ansi = colorGreen
	case zapcore.WarnLevel:
		ansi = colorYellow
	case zapcore.ErrorLevel:
		ansi = colorRed
	case zapcore.FatalLevel, zapcore.PanicLevel:
		ansi = colorRed
	default:
		ansi = colorGray
	}
	enc.AppendString(fmt.Sprintf("%s%-5s%s", ansi, level.CapitalString(), colorReset))
}

func colorTimeEncoder(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
	enc.AppendString(colorGray + t.Format("2006-01-02 15:04:05.000") + colorReset)
}

func colorCallerEncoder(caller zapcore.EntryCaller, enc zapcore.PrimitiveArrayEncoder) {
	enc.AppendString(colorBlue + caller.TrimmedPath() + colorReset)
}

// newStructuredLogger builds a structured console logger tuned for readability
func newStructuredLogger(level zapcore.Level, debugFormat bool) *zap.Logger {
	atomicLevel := zap.NewAtomicLevelAt(level)

	encoderConfig := zapcore.EncoderConfig{
		TimeKey:          "time",
		LevelKey:         "level",
		NameKey:          "",
		CallerKey:        "",
		MessageKey:       "msg",
		StacktraceKey:    "stack",
		LineEnding:       zapcore.DefaultLineEnding,
		EncodeLevel:      paddedLevelEncoder,
		EncodeTime:       zapcore.TimeEncoderOfLayout("2006-01-02 15:04:05.000"),
		EncodeDuration:   zapcore.MillisDurationEncoder,
		EncodeCaller:     zapcore.ShortCallerEncoder,
		EncodeName:       zapcore.FullNameEncoder,
		ConsoleSeparator: " ",
	}

	if debugFormat || level <= zapcore.DebugLevel {
		// Remove timestamps and add one blank line between log entries with wider separators
		encoderConfig.TimeKey = ""
		encoderConfig.LineEnding = "\n\n"
		encoderConfig.ConsoleSeparator = "  "
	}

	opts := []zap.Option{
		zap.ErrorOutput(zapcore.Lock(os.Stderr)),
		zap.AddStacktrace(zapcore.ErrorLevel),
	}

	if level <= zapcore.DebugLevel {
		encoderConfig.CallerKey = "caller"
		encoderConfig.EncodeLevel = colorLevelEncoder
		encoderConfig.EncodeTime = colorTimeEncoder
		encoderConfig.EncodeCaller = colorCallerEncoder
		opts = append(opts, zap.AddCaller())
	}

	core := zapcore.NewCore(
		zapcore.NewConsoleEncoder(encoderConfig),
		zapcore.Lock(os.Stdout),
		atomicLevel,
	)

	return zap.New(core, opts...)
}

// RunCobraCommand takes the *cobra.Command
// and wraps it with proper startup/shutdown, error and env to flag behavior.
func RunCobraCommand(rootCtx context.Context, root *cobra.Command) error {
	return runCommand(rootCtx, root)
}

func runCommand(ctx context.Context, root *cobra.Command) error {
	// Create a cancellable context for shutdown handling
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)

	// Start signal handler goroutine
	go func() {
		sig := <-sigChan
		zap.L().Info("Received shutdown signal", zap.String("signal", sig.String()))
		cancel()
	}()

	var lvlInt int
	var err error
	if root.Flags().Lookup("log-level") == nil {
		root.Flags().IntVar(&lvlInt, "log-level", 0, "The log level (-1=DEBUG, 0=INFO, 1=WARNING, 2=ERROR, 5=FATAL)")
	} else {
		lvlInt, err = strconv.Atoi(root.Flags().Lookup("log-level").Value.String())
		if err != nil {
			lvlInt = 0
		}
	}

	var debugFormat bool
	if root.Flags().Lookup("debug-format") == nil {
		root.Flags().BoolVar(&debugFormat, "debug-format", false, "Enable debug log formatting (no timestamps, extra spacing)")
	} else {
		if v, e := strconv.ParseBool(root.Flags().Lookup("debug-format").Value.String()); e == nil {
			debugFormat = v
		}
	}

	// map the flags to ENV vars
	mapFlagsToEnv(root)

	// Re-read debug-format in case it was set via environment mapping
	if v, e := root.Flags().GetBool("debug-format"); e == nil {
		debugFormat = v
	}

	zap.ReplaceGlobals(newStructuredLogger(zapcore.Level(lvlInt), debugFormat))

	// run the cobra command
	zap.L().Info("Starting", zap.String("service", root.Name()))

	// Create a channel to receive the result of command execution
	errChan := make(chan error, 1)
	go func() {
		errChan <- root.ExecuteContext(ctx)
	}()

	// Wait for either command completion or context cancellation
	select {
	case err := <-errChan:
		if err != nil {
			zap.L().Error("Error executing app command", zap.Error(err))
			return err
		}
		zap.L().Info("Service completed successfully", zap.String("service", root.Name()))
		return nil
	case <-ctx.Done():
		zap.L().Info("Shutting down gracefully", zap.String("service", root.Name()))

		// Create a timeout context for graceful shutdown (3s gives buffer under Cloud Run's 60s limit)
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer shutdownCancel()

		// Wait for command to finish or timeout
		select {
		case err := <-errChan:
			if err != nil {
				zap.L().Error("Error during shutdown", zap.Error(err))
				return err
			}
			zap.L().Info("Service shut down successfully", zap.String("service", root.Name()))
			return nil
		case <-shutdownCtx.Done():
			zap.L().Warn("Forced shutdown after timeout", zap.String("service", root.Name()))
			return ctx.Err()
		}
	}
}

// mapFlagsToEnv maps the flags to ENV vars
func mapFlagsToEnv(command *cobra.Command) {
	v := viper.New()
	v.SetConfigType("env")
	v.AutomaticEnv()

	// Load .env from known locations (CWD, workspace/<service>, workspace root)
	candidates := envFileCandidates(command)
	_ = loadEnvFromCandidates(v, candidates)

	command.Flags().VisitAll(func(f *pflag.Flag) {
		up := strings.ToUpper(f.Name)
		formattedFlag := strings.ReplaceAll(up, "-", "_")
		if v.IsSet(formattedFlag) && v.GetString(formattedFlag) != "" {
			// fmt.Printf("Flag %s replaced with environment variable\n", f.Name)
			_ = command.Flags().Set(f.Name, v.GetString(formattedFlag))
		}
	})
	for c := command; c != nil; c = c.Parent() {
		c.PersistentFlags().VisitAll(func(f *pflag.Flag) {
			up := strings.ToUpper(f.Name)
			formattedFlag := strings.ReplaceAll(up, "-", "_")
			if v.IsSet(formattedFlag) && v.GetString(formattedFlag) != "" {
				_ = command.Flags().Set(f.Name, v.GetString(formattedFlag))
				// fmt.Printf("Flag %s replaced with environment variable\n", f.Name)
			}
		})
	}
}

// envFileCandidates returns the ordered list of .env locations to try.
func envFileCandidates(command *cobra.Command) []string {
	candidates := []string{".env"}
	if workspaceDir, found := os.LookupEnv("BUILD_WORKSPACE_DIRECTORY"); found {
		zap.L().Debug("BUILD_WORKSPACE_DIRECTORY", zap.String("dir", workspaceDir))
		if len(command.Root().Aliases) > 0 {
			candidates = append(candidates, filepath.Join(workspaceDir, command.Root().Aliases[0], ".env"))
		}
		candidates = append(candidates,
			filepath.Join(workspaceDir, command.Root().Name(), ".env"),
			filepath.Join(workspaceDir, ".env"),
		)
		if len(command.Root().Aliases) > 0 {
			candidates = append(candidates, filepath.Join(workspaceDir, command.Root().Aliases[0], ".env"))
		}
	}
	return candidates
}

// loadEnvFromCandidates loads the first existing .env into the provided viper instance.
func loadEnvFromCandidates(v *viper.Viper, candidates []string) error {
	for _, p := range candidates {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			v.SetConfigFile(p)
			if err := v.ReadInConfig(); err == nil {
				return nil
			}
		}
	}
	return nil
}
