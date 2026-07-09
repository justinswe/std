# std

[![Go Reference](https://pkg.go.dev/badge/github.com/justinswe/std.svg)](https://pkg.go.dev/github.com/justinswe/std)

Fast, easy to use Go utilities for command-line applications and error handling.

## Install

Install the whole module:

```sh
go get github.com/justinswe/std
```

Or install only the package you need:

```sh
go get github.com/justinswe/std/app
go get github.com/justinswe/std/errors
go get github.com/justinswe/std/retry
```

## Packages

- [`app`](https://pkg.go.dev/github.com/justinswe/std/app) runs Cobra commands with structured logging, environment-backed flags, and graceful shutdown.
- [`errors`](https://pkg.go.dev/github.com/justinswe/std/errors) creates stack-capturing errors and provides wrapping, inspection, and aggregation helpers.
- [`retry`](https://pkg.go.dev/github.com/justinswe/std/retry) retries generic operations and HTTP requests with backoff, jitter, and conservative defaults.

### `app`

Use `RunCobraCommand` as the entry point for a Cobra application:

```go
package main

import (
	"context"
	"log"

	"github.com/justinswe/std/app"
	"github.com/spf13/cobra"
)

func main() {
	root := &cobra.Command{
		Use: "hello",
		RunE: func(cmd *cobra.Command, args []string) error {
			log.Println("hello")
			return nil
		},
	}

	if err := app.RunCobraCommand(context.Background(), root); err != nil {
		log.Fatal(err)
	}
}
```

The runner adds persistent `--log-level` and `--debug-format` flags. Numeric log levels range from `-1` (debug) through `5` (fatal). `--debug-format` removes timestamps and adds spacing intended for interactive debugging.

Every declared flag can be populated from an environment variable by converting its name to uppercase and replacing hyphens with underscores. For example, `LOG_LEVEL=-1` sets `--log-level=-1`. Configuration precedence is explicit CLI flag, process environment, the first `.env` file found, then the flag default. Invalid environment values and malformed `.env` files are returned as errors.

When `BUILD_WORKSPACE_DIRECTORY` is set, the runner also searches for `.env` beneath the Bazel workspace using the command alias, command name, and workspace root. The workspace must be an absolute directory containing `MODULE.bazel`, `WORKSPACE.bazel`, or `WORKSPACE`, and resolved `.env` paths cannot escape it. Invalid workspace configuration is an error.

`RunCobraCommand` derives a context cancelled by `SIGINT` or `SIGTERM` and executes the command synchronously. Command handlers must stop when `cmd.Context()` is cancelled; hard process termination remains the responsibility of the process supervisor.

### `errors`

Create and wrap errors while preserving compatibility with Go's standard `errors.Is` and `errors.As` traversal:

```go
package main

import (
	stderrors "errors"
	"fmt"

	apperrors "github.com/justinswe/std/errors"
)

var errNotFound = stderrors.New("not found")

func load() error {
	return apperrors.Wrap(errNotFound, "load account")
}

func main() {
	err := load()
	fmt.Println(err)                      // load account; caused by: not found
	fmt.Println(apperrors.Is(err, errNotFound)) // true
	fmt.Printf("%+v\n", err)             // message, stack, and cause
}
```

Other helpers include `New`, `Errorf`, `Wrapf`, `AsType`, `IsCanceled`, `Any`, `Join`, `Ignore`, and `IgnoreCtx`. See the [`errors` package reference](https://pkg.go.dev/github.com/justinswe/std/errors) for the complete API.

### `retry`

Use `NewHTTPClient` for HTTP retries with conservative defaults:

```go
package main

import (
	"log"
	"net/http"

	"github.com/justinswe/std/retry"
)

func main() {
	client := retry.NewHTTPClient(retry.WithMaxAttempts(3))

	resp, err := client.Do(mustRequest("https://example.com"))
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
}

func mustRequest(url string) *http.Request {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		log.Fatal(err)
	}
	return req
}
```

HTTP retries use exponential backoff with full jitter, retry transient transport errors and common retryable status codes, honor `Retry-After`, and only replay request bodies when `Request.GetBody` is available.

For a fully configured HTTP client, pass the retry options when the client is created. This example tries each request at most five times, waits with exponential backoff starting at `100ms`, caps each calculated delay at `2s`, and applies full jitter so concurrent callers do not all retry at the same instant:

```go
package main

import (
	"log"
	"net/http"
	"time"

	"github.com/justinswe/std/retry"
)

func main() {
	client := retry.NewHTTPClient(
		retry.WithMaxAttempts(5),
		retry.WithBackoff(retry.ExponentialBackoff(100*time.Millisecond, 2*time.Second)),
		retry.WithJitter(retry.FullJitter),
	)

	req, err := http.NewRequest(http.MethodGet, "https://example.com", nil)
	if err != nil {
		log.Fatal(err)
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
}
```

`WithMaxAttempts(5)` means one initial call plus up to four retries. For non-HTTP work, use `retry.Do` or `retry.DoValue`; return `retry.Retryable(err)` for failures that should be retried, `retry.Permanent(err)` for failures that must stop immediately, or `nil` for success.
