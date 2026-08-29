package main

import (
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// webURL is where omasushi-web lives. Overridable at build time
// (-ldflags "-X main.webURL=https://…"), by $OMASUSHI_WEB_URL, or --web.
var webURL = "http://localhost:3000"

// publishCmd puts an omakase on the omasushi-web conveyor belt.
//
// omasushi-web only accepts registrations from a signed-in browser (GitHub /
// GitLab OAuth, no API tokens), so the CLI does the local half — find the
// repository URL, make sure omasushi.yaml is there and pushed — and hands the
// browser a prefilled /new?url=… page for the user to confirm.
func publishCmd(cfg *Config, file string, args []string) error {
	fs := flag.NewFlagSet("publish", flag.ExitOnError)
	web := fs.String("web", envOr("OMASUSHI_WEB_URL", webURL), "omasushi-web base URL")
	printOnly := fs.Bool("print", false, "print the registration URL instead of opening a browser")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, `usage: omasushi publish [--web URL] [--print] [<name>|<owner/repo>|<url>|<path>]

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

	reg, err := registrationURL(*web, repo)
	if err != nil {
		return err
	}
	fmt.Printf("omakase:  %s\n", repo)
	fmt.Printf("register: %s\n", reg)
	if *printOnly {
		return nil
	}
	if err := openBrowser(reg); err != nil {
		return fmt.Errorf("could not open a browser (%v); open the URL above yourself", err)
	}
	fmt.Println("opened in your browser — sign in and press the button to put it on the belt")
	return nil
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
// source for git ones, the checkout's origin for local ones.
func publishOmakase(r Omakase) (repo, dir string, err error) {
	if !r.Local {
		_, target, _, err := resolveSource(r.Source)
		if err != nil {
			return "", "", err
		}
		repo, err = canonicalRepoURL(target)
		return repo, r.Dir, err
	}
	repo, err = originOf(r.Dir)
	return repo, r.Dir, err
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

func registrationURL(base, repo string) (string, error) {
	u, err := url.Parse(strings.TrimRight(base, "/"))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("bad omasushi-web URL %q (set --web or $OMASUSHI_WEB_URL)", base)
	}
	u.Path = strings.TrimSuffix(u.Path, "/") + "/new"
	u.RawQuery = url.Values{"url": {repo}}.Encode()
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
