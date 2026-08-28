//go:build e2e

package main

import (
	"log"
	"os"
)

// shouldSkipLocalFence is the E2E-only failure-injection variant. It is
// compiled ONLY with `-tags=e2e`, so the production binary cannot be made to
// skip local fencing by any environment variable.
//
// The cross-process abort-fencing E2E (service app/e2e) builds the node with
// this tag to prove that central delegation is retained while the node's local
// fence write has not succeeded.
func shouldSkipLocalFence(deploymentID string) bool {
	target := os.Getenv("E2E_FAIL_LOCAL_FENCE_DEPLOYMENT")
	if target == "" || target != deploymentID {
		return false
	}
	log.Printf("e2e build: skipping local fence write for deployment %s", deploymentID)
	return true
}
