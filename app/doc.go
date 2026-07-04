// Package app runs Cobra commands with environment-backed flags, structured
// logging, and signal-aware context cancellation.
//
// RunCobraCommand executes commands synchronously. Command handlers are
// responsible for stopping when their context is cancelled.
package app
