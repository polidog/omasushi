package main

import "testing"

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

func TestRegistrationURL(t *testing.T) {
	got, err := registrationURL("https://example.test/", "https://github.com/polidog/omakase")
	if err != nil {
		t.Fatal(err)
	}
	want := "https://example.test/new?url=https%3A%2F%2Fgithub.com%2Fpolidog%2Fomakase"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if _, err := registrationURL("example.test", "x"); err == nil {
		t.Error("want error for base URL without scheme")
	}
}
