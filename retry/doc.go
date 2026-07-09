// Package retry runs operations with bounded retries, backoff, and jitter.
//
// The package includes generic retry helpers and a small HTTP client wrapper.
// HTTP retries use conservative defaults: retry transient transport errors and
// common retryable status codes, honor Retry-After, and only replay request
// bodies when the request is already replayable.
package retry
