package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func TestWebPath(t *testing.T) {
	got, err := webPath("https://example.test/", "/new", url.Values{"url": {"https://github.com/polidog/omakase"}})
	if err != nil {
		t.Fatal(err)
	}
	want := "https://example.test/new?url=https%3A%2F%2Fgithub.com%2Fpolidog%2Fomakase"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, _ := webPath("http://localhost:3000", "/api/omakase", nil); got != "http://localhost:3000/api/omakase" {
		t.Errorf("got %q", got)
	}
	if _, err := webPath("example.test", "/new", nil); err == nil {
		t.Error("want error for base URL without scheme")
	}
}

func TestRegisterOnWeb(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/omakase" || r.Method != http.MethodPost {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		switch body["repo"] {
		case "https://github.com/a/new":
			w.WriteHeader(201)
			fmt.Fprintf(w, `{"ok":true,"id":"x1","url":"http://%s/omakase/x1","created":true}`, r.Host)
		case "https://github.com/a/old":
			fmt.Fprintf(w, `{"ok":true,"id":"x2","url":"http://%s/omakase/x2","created":false}`, r.Host)
		case "https://github.com/a/none":
			w.WriteHeader(404)
			fmt.Fprint(w, `{"ok":false,"error":"notFound","message":"omasushi.yaml was not found"}`)
		default:
			w.WriteHeader(500)
			fmt.Fprint(w, "<html>oops</html>")
		}
	}))
	defer srv.Close()

	res, err := registerOnWeb(srv.URL, "https://github.com/a/new")
	if err != nil || !res.Created || res.URL != srv.URL+"/omakase/x1" {
		t.Errorf("new: %+v, %v", res, err)
	}
	res, err = registerOnWeb(srv.URL, "https://github.com/a/old")
	if err != nil || res.Created || res.ID != "x2" {
		t.Errorf("old: %+v, %v", res, err)
	}
	if _, err = registerOnWeb(srv.URL, "https://github.com/a/none"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("none: %v", err)
	}
	if _, err = registerOnWeb(srv.URL, "https://github.com/a/boom"); err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("boom: %v", err)
	}
}
