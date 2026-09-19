package config

import (
	"strings"
	"testing"
)

func TestValidateOutputMode(t *testing.T) {
	c := Default()
	c.Output = "bogus"
	if _, err := c.Validate(); err == nil {
		t.Fatal("expected error for invalid output mode")
	}
	c.Output = "static"
	if _, err := c.Validate(); err != nil {
		t.Fatalf("unexpected error for static: %v", err)
	}
}

func TestValidatePorts(t *testing.T) {
	c := Default()
	c.DevServer.Port = 70000
	if _, err := c.Validate(); err == nil {
		t.Fatal("expected error for out-of-range port")
	}
	c = Default()
	c.SSR.Timeout = -1
	if _, err := c.Validate(); err == nil {
		t.Fatal("expected error for negative timeout")
	}
}

func TestUnknownKeyWarnings(t *testing.T) {
	raw := []byte(`{"entry":"src/index.tsx","tailwnd":{},"output":"static"}`)
	warnings := UnknownKeyWarnings(raw)
	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %v", warnings)
	}
	if !strings.Contains(warnings[0], "tailwnd") {
		t.Errorf("warning should name the unknown key: %v", warnings[0])
	}
}

func TestUnknownKeyWarningsIgnoresValidate(t *testing.T) {
	raw := []byte(`{"entry":"x","validate":"function"}`)
	if w := UnknownKeyWarnings(raw); len(w) != 0 {
		t.Errorf("validate hook should not warn: %v", w)
	}
}

// TestDeprecatedEmitReactAcceptedSilently verifies the removed emitReact option
// still loads without an unknown-key warning, in either boolean state.
func TestDeprecatedEmitReactAcceptedSilently(t *testing.T) {
	for _, raw := range [][]byte{
		[]byte(`{"entry":"x","emitReact":true}`),
		[]byte(`{"entry":"x","emitReact":false}`),
	} {
		if w := UnknownKeyWarnings(raw); len(w) != 0 {
			t.Errorf("emitReact should be accepted silently, got warnings: %v", w)
		}
	}
}

// TestApplyConfigPropEmitReactNoOp verifies the tsconfig-style config loader
// accepts emitReact without error or effect.
func TestApplyConfigPropEmitReactNoOp(t *testing.T) {
	cfg := Default()
	for _, v := range []interface{}{true, false} {
		if err := applyConfigProp(cfg, "emitReact", v); err != nil {
			t.Errorf("applyConfigProp(emitReact, %v) returned error: %v", v, err)
		}
	}
}
