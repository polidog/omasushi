package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// webURL is where omasushi-web lives. Overridable at build time
// (-ldflags "-X main.webURL=https://…"), by $OMASUSHI_WEB_URL, or --web.
var webURL = "http://localhost:3000"

// publishCmd puts an omakase on the omasushi-web conveyor belt.
//
// The CLI does the local half — find the repository URL, make sure
// omasushi.yaml is there and pushed — then POSTs it to the web's JSON API
// (no account needed; the web fetches omasushi.yaml from the public repo).
// --browser opens the prefilled /new form instead of calling the API.
func publishCmd(cfg *Config, file string, args []string) error {
	fs := flag.NewFlagSet("publish", flag.ExitOnError)
	web := fs.String("web", envOr("OMASUSHI_WEB_URL", webURL), "omasushi-web base URL")
	open := fs.Bool("open", false, "open the plate's page in a browser once registered")
	browser := fs.Bool("browser", false, "don't call the API; open the registration form prefilled instead")
	dry := fs.Bool("dry-run", false, "resolve and print the repository URL, register nothing")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, `usage: omasushi publish [--web URL] [--open|--browser|--dry-run] [<name>|<owner/repo>|<url>|<path>]

With no argument, publishes the omakase in the working directory (./omasushi.yaml),
else the single omakase in use. A configured omakase's name, a GitHub owner/repo,
a github.com / gitlab.com URL, or a local checkout also work.`)
		fs.PrintDefaults()
	}
	fs.Parse(args)
	if fs.NArg() > 1 {
		fs.Usage()
		os.Exit(2)
	}

	repo, dir, err := publishTarget(cfg, file, fs.Arg(0))
	if err != nil {
		return err
	}
	if dir != "" {
		for _, w := range publishWarnings(dir) {
			fmt.Fprintln(os.Stderr, "warning:", w)
		}
	}
	fmt.Printf("omakase: %s\n", repo)

	if *browser || *dry {
		form, err := webPath(*web, "/new", url.Values{"url": {repo}})
		if err != nil {
			return err
		}
		fmt.Printf("form:    %s\n", form)
		if *dry {
			return nil
		}
		if err := openBrowser(form); err != nil {
			return fmt.Errorf("could not open a browser (%v); open the URL above yourself", err)
		}
		return nil
	}

	res, err := registerOnWeb(*web, repo)
	if err != nil {
		return err
	}
	if res.Created {
		fmt.Printf("on the belt: %s\n", res.URL)
	} else {
		fmt.Printf("already on the belt: %s\n", res.URL)
	}
	if *open {
		if err := openBrowser(res.URL); err != nil {
			fmt.Fprintln(os.Stderr, "warning: could not open a browser:", err)
		}
	}
	return nil
}

// registerResult is the JSON omasushi-web's POST /api/omakase returns.
type registerResult struct {
	OK      bool   `json:"ok"`
	ID      string `json:"id"`
	URL     string `json:"url"`
	Created bool   `json:"created"`
	Error   string `json:"error"`   // badInput|notFound|http|parse|empty|rateLimit
	Message string `json:"message"` // human readable, English
}

// registerOnWeb POSTs the repo to omasushi-web. The web fetches
// omasushi.yaml from the public repository itself, so nothing local is sent.
func registerOnWeb(base, repo string) (*registerResult, error) {
	endpoint, err := webPath(base, "/api/omakase", nil)
	if err != nil {
		return nil, err
	}
	body, _ := json.Marshal(map[string]string{"repo": repo})
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "omasushi/"+version)
	client := &http.Client{Timeout: 60 * time.Second} // the web fetches the yaml + stars in-request
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("omasushi-web at %s: %w", base, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var res registerResult
	if err := json.Unmarshal(raw, &res); err != nil || (!res.OK && res.Error == "") {
		return nil, fmt.Errorf("omasushi-web at %s answered HTTP %d without a JSON result (is that the right --web URL?)", base, resp.StatusCode)
	}
	if !res.OK {
		msg := res.Message
		if msg == "" {
			msg = res.Error
		}
		switch res.Error {
		case "rateLimit":
			msg += " (try again later)"
		case "notFound":
			msg += " — is the repo public, and is omasushi.yaml committed and pushed to main/master?"
		}
		return nil, fmt.Errorf("omasushi-web: %s", msg)
	}
	if res.URL == "" {
		res.URL = strings.TrimRight(base, "/") + "/omakase/" + res.ID
	}
	return &res, nil
}

// publishTarget resolves what to publish into a canonical repository URL and,
// when it comes from a local checkout, that checkout's directory.
func publishTarget(cfg *Config, file, arg string) (repo, dir string, err error) {
	switch {
	case arg == "" && file != "":
		dir = filepath.Dir(file)
	case arg == "":
		if _, statErr := os.Stat(ManifestFile); statErr == nil {
			dir = "."
			break
		}
		omakases, err := activeOmakases(cfg, "")
		if err != nil {
			return "", "", err
		}
		r, err := pickOmakase(omakases, "")
		if err != nil {
			return "", "", fmt.Errorf("%w (or run publish inside an omakase checkout)", err)
		}
		return publishOmakase(*r)
	default:
		for _, ref := range cfg.Omakases {
			if ref.Name == arg {
				rs, err := LoadOmakases(&Config{Omakases: []OmakaseRef{ref}})
				if err != nil {
					return "", "", err
				}
				return publishOmakase(rs[0])
			}
		}
		_, target, local, err := resolveSource(arg)
		if err != nil {
			return "", "", err
		}
		if !local {
			repo, err := canonicalRepoURL(target)
			return repo, "", err
		}
		dir = target
	}

	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", "", err
	}
	if _, err := os.Stat(filepath.Join(abs, ManifestFile)); err != nil {
		return "", "", fmt.Errorf("%s has no %s", abs, ManifestFile)
	}
	if _, err := LoadManifest(filepath.Join(abs, ManifestFile)); err != nil {
		return "", "", err
	}
	repo, err = originOf(abs)
	return repo, abs, err
}

// publishOmakase picks the repo URL for an omakase in use: its recorded
// source for git ones, the checkout's origin for local ones. Parts publish
// their whole repository — the web lists the parts itself.
func publishOmakase(r Omakase) (repo, dir string, err error) {
	if !r.Local {
		_, target, _, err := resolveSource(r.Source)
		if err != nil {
			return "", "", err
		}
		repo, err = canonicalRepoURL(target)
		return repo, r.Repo, err
	}
	repo, err = originOf(r.Repo)
	return repo, r.Repo, err
}

func originOf(dir string) (string, error) {
	if !isGitRepo(dir) {
		return "", fmt.Errorf("%s is not a git repository; push it to GitHub or GitLab first", dir)
	}
	origin := run("git", "-C", dir, "remote", "get-url", "origin")
	if origin == "" {
		return "", fmt.Errorf("%s has no origin remote; push it to GitHub or GitLab first (gh repo create --public --push)", dir)
	}
	return canonicalRepoURL(origin)
}

var repoRe = regexp.MustCompile(`^(?i:(github\.com|gitlab\.com))/([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+)$`)

// canonicalRepoURL turns any github.com / gitlab.com remote (https, ssh,
// scp-style, with or without .git) into the https URL omasushi-web stores.
func canonicalRepoURL(remote string) (string, error) {
	s := strings.TrimSpace(remote)
	s = strings.TrimSuffix(s, "/")
	s = strings.TrimSuffix(s, ".git")
	for _, p := range []string{"https://", "http://", "ssh://", "git://"} {
		s = strings.TrimPrefix(s, p)
	}
	s = strings.TrimPrefix(s, "git@")
	s = strings.Replace(s, ":", "/", 1) // git@github.com:owner/repo
	s = strings.TrimPrefix(s, "www.")
	m := repoRe.FindStringSubmatch(s)
	if m == nil {
		return "", fmt.Errorf("omasushi-web only accepts public github.com / gitlab.com repositories, got %q", remote)
	}
	return "https://" + strings.ToLower(m[1]) + "/" + m[2] + "/" + m[3], nil
}

// publishWarnings lists things that would make the web's fetch of
// omasushi.yaml differ from what the user sees locally. None are fatal.
func publishWarnings(dir string) (out []string) {
	if !isGitRepo(dir) {
		return nil
	}
	if run("git", "-C", dir, "ls-files", "--error-unmatch", ManifestFile) == "" {
		out = append(out, ManifestFile+" is not committed; the web reads it from the repository")
	}
	if n := len(lines(run("git", "-C", dir, "status", "--porcelain"))); n > 0 {
		out = append(out, fmt.Sprintf("%d uncommitted change(s) in %s", n, dir))
	}
	if ab := strings.Fields(run("git", "-C", dir, "rev-list", "--left-right", "--count", "HEAD...@{u}")); len(ab) == 2 && ab[0] != "0" {
		out = append(out, fmt.Sprintf("%s commit(s) not pushed yet", ab[0]))
	}
	if branch := run("git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD"); branch != "" && branch != "main" && branch != "master" {
		out = append(out, fmt.Sprintf("on branch %q; the web reads HEAD, main or master", branch))
	}
	return out
}

// webPath joins a page or API path onto the omasushi-web base URL.
func webPath(base, path string, q url.Values) (string, error) {
	u, err := url.Parse(strings.TrimRight(base, "/"))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("bad omasushi-web URL %q (set --web or $OMASUSHI_WEB_URL)", base)
	}
	u.Path = strings.TrimSuffix(u.Path, "/") + path
	if q != nil {
		u.RawQuery = q.Encode()
	}
	return u.String(), nil
}

// openBrowser prefers Omarchy's launcher, then xdg-open, then $BROWSER.
func openBrowser(target string) error {
	if _, err := exec.LookPath("omarchy-launch-browser"); err == nil {
		return exec.Command("omarchy-launch-browser", target).Start()
	}
	if _, err := exec.LookPath("xdg-open"); err == nil {
		return exec.Command("xdg-open", target).Start()
	}
	if b := os.Getenv("BROWSER"); b != "" {
		return exec.Command(b, target).Start()
	}
	return fmt.Errorf("no omarchy-launch-browser, xdg-open or $BROWSER")
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
