package opencodesdk

import (
	"slices"
	"strings"
	"testing"
)

func TestWithExtraArgs_AccumulatesEntries(t *testing.T) {
	val := "warn"

	o := apply([]Option{
		WithExtraArgs(map[string]*string{"log-level": &val}),
		WithExtraArgs(map[string]*string{"pure": nil}),
	})

	if got, ok := o.cliExtraArgs["log-level"]; !ok || got == nil || *got != "warn" {
		t.Errorf("log-level entry missing or wrong: %v", o.cliExtraArgs)
	}

	if got, ok := o.cliExtraArgs["pure"]; !ok || got != nil {
		t.Errorf("pure entry missing or wrong: %v", o.cliExtraArgs)
	}
}

func TestSubprocessArgs_NilValueRendersAsBareFlag(t *testing.T) {
	o := apply([]Option{WithExtraArgs(map[string]*string{"pure": nil})})

	args := subprocessArgs(o)
	if !slices.Contains(args, "--pure") {
		t.Errorf("expected --pure in args, got %v", args)
	}
}

func TestSubprocessArgs_ValueRendersAsKVFlag(t *testing.T) {
	val := "info"

	o := apply([]Option{WithExtraArgs(map[string]*string{"log-level": &val})})

	args := subprocessArgs(o)
	if !slices.Contains(args, "--log-level=info") {
		t.Errorf("expected --log-level=info in args, got %v", args)
	}
}

func TestSubprocessArgs_PreservesCLIFlagsOrder(t *testing.T) {
	o := apply([]Option{
		WithCLIFlags("--first", "--second"),
		WithCLIFlags("--third"),
	})

	args := subprocessArgs(o)
	if len(args) < 3 || strings.Join(args[:3], " ") != "--first --second --third" {
		t.Errorf("CLI flag order corrupted, got %v", args)
	}
}

func TestSubprocessArgs_EmptyExtraArgsReturnsCLIFlags(t *testing.T) {
	o := apply([]Option{WithCLIFlags("--only")})

	args := subprocessArgs(o)
	if len(args) != 1 || args[0] != "--only" {
		t.Errorf("expected [--only], got %v", args)
	}
}

func TestWithUser_StoresValue(t *testing.T) {
	o := apply([]Option{WithUser("alice@example.com")})
	if o.user != "alice@example.com" {
		t.Errorf("WithUser: options.user = %q, want %q", o.user, "alice@example.com")
	}
}
