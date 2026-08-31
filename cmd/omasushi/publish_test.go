package main

import (
	"strings"
	"testing"
)

func TestCanonicalRepoURL(t *testing.T) {
	ok := map[string]string{
		"https://github.com/Polidog/My-Omakase.git": "https://github.com/Polidog/My-Omakase",
		"https://github.com/polidog/omakase/":       "https://github.com/polidog/omakase",
		"git@github.com:polidog/omakase.git":        "https://github.com/polidog/omakase",
		"ssh://git@gitlab.com/polidog/omakase.git":  "https://gitlab.com/polidog/omakase",
		"https://www.gitlab.com/polidog/omakase":    "https://gitlab.com/polidog/omakase",
		"http://GitHub.com/polidog/omakase":         "https://github.com/polidog/omakase",
	}
	for in, want := range ok {
		got, err := canonicalRepoURL(in)
		if err != nil || got != want {
			t.Errorf("canonicalRepoURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{
		"https://codeberg.org/polidog/omakase",
		"git@github.com:polidog",
		"https://github.com/polidog/omakase/tree/main",
		"/home/me/omakase",
		"",
	} {
		if got, err := canonicalRepoURL(bad); err == nil {
			t.Errorf("canonicalRepoURL(%q) = %q; want error", bad, got)
		}
	}
}

func TestSubmitIssueURL(t *testing.T) {
	got, err := submitIssueURL("polidog/omasushi", "https://github.com/polidog/omakase")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "https://github.com/polidog/omasushi/issues/new?") {
		t.Errorf("got %q; want the issues/new URL", got)
	}
	for _, want := range []string{
		"template=submit-omakase.yml",
		"repo=https%3A%2F%2Fgithub.com%2Fpolidog%2Fomakase",
		"title=Submit%3A+github.com%2Fpolidog%2Fomakase",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("got %q; want it to contain %q", got, want)
		}
	}

	for _, bad := range []string{"", "polidog", "polidog/", "/omasushi"} {
		if got, err := submitIssueURL(bad, "https://github.com/a/b"); err == nil {
			t.Errorf("submitIssueURL(%q, …) = %q; want error", bad, got)
		}
	}
}
