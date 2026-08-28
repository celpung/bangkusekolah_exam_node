//go:build !e2e

package main

// shouldSkipLocalFence is the production implementation: the node always
// attempts the local fence write. The failure-injection variant lives in
// fence_hook_e2e.go and is only compiled with the `e2e` build tag, so the
// production binary contains no environment-driven bypass of fencing.
func shouldSkipLocalFence(string) bool { return false }
