package tsexec

import (
	"strings"
	"testing"
	"time"
)

// TestRunBootstrapTimeoutDoesNotHang verifies that a bootstrap which never
// exits is killed at the deadline and that RunBootstrap returns promptly.
//
// Regression test: `npx` is a shim that spawns a child `node`; killing only the
// shim left the child holding the stdout/stderr pipes, so cmd.Wait() blocked
// forever and the timeout never surfaced.
func TestRunBootstrapTimeoutDoesNotHang(t *testing.T) {
	start := time.Now()
	_, _, err := RunBootstrap("krate-test-hang", "setInterval(function () {}, 1000);", t.TempDir(), 3*time.Second)
	elapsed := time.Since(start)

	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected a timeout error, got %v (after %v)", err, elapsed)
	}
	if elapsed > 45*time.Second {
		t.Fatalf("RunBootstrap took %v; the timeout was not enforced", elapsed)
	}
}
