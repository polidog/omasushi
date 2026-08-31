package main

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

// captureStdio returns what f wrote to stdout and to stderr.
func captureStdio(t *testing.T, f func()) (string, string) {
	t.Helper()
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW
	defer func() { os.Stdout, os.Stderr = oldOut, oldErr }()
	f()
	outW.Close()
	errW.Close()
	out, _ := io.ReadAll(outR)
	errOut, _ := io.ReadAll(errR)
	return string(out), string(errOut)
}

// A failing action does not take the ones behind it with it: everything runs,
// and the failures are named again at the end.
func TestRunActionsCarriesOnPastFailure(t *testing.T) {
	var ran []string
	act := func(kind, desc string, err error) Action {
		return Action{Kind: kind, Desc: desc, Run: func() error {
			ran = append(ran, desc)
			return err
		}}
	}
	actions := []Action{
		act("omarchy-add", "add plugin", errors.New("id already used")),
		act("file-link", "link bindings.lua", nil),
		act("aur", "install thing", errors.New("no such package")),
		act("hypr-reload", "hyprctl reload", nil),
	}

	var failed int
	out, errOut := captureStdio(t, func() { failed = runActions(actions) })

	if failed != 2 {
		t.Errorf("failed = %d, want 2", failed)
	}
	if len(ran) != 4 {
		t.Errorf("ran %v, want every action to run", ran)
	}
	for _, want := range []string{"add plugin", "link bindings.lua", "install thing", "hyprctl reload"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q was not announced:\n%s", want, out)
		}
	}
	if !strings.Contains(errOut, "2 of 4 actions failed") {
		t.Errorf("want a summary of the failures:\n%s", errOut)
	}
	for _, want := range []string{"id already used", "no such package"} {
		if strings.Count(errOut, want) != 2 { // once where it happened, once in the summary
			t.Errorf("want %q reported where it happened and in the summary:\n%s", want, errOut)
		}
	}
}

// Nothing to say when the whole plan goes through.
func TestRunActionsQuietWhenAllSucceed(t *testing.T) {
	actions := []Action{{Kind: "file-link", Desc: "link a", Run: func() error { return nil }}}
	var failed int
	_, errOut := captureStdio(t, func() { failed = runActions(actions) })
	if failed != 0 || errOut != "" {
		t.Errorf("failed = %d, stderr = %q, want 0 and nothing", failed, errOut)
	}
}
