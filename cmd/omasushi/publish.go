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

// submitRepo is the GitHub repository whose issue form takes omakase
// submissions (Omarchy-plugin style). Overridable at build time
// (-ldflags "-X main.submitRepo=…") or by $OMASUSHI_SUBMIT_REPO.
var submitRepo = "polidog/omasushi"

// publishCmd puts an omakase on the omasushi-web conveyor belt.
//
// There is no direct registration API any more: submissions go through a
// GitHub issue on the omasushi repository, where a workflow validates
// omasushi.yaml and puts the plate on the belt. The CLI does the local
// half — find the repository URL, make sure omasushi.yaml is there and
// pushed — then opens the submit issue form prefilled.
func publishCmd(machine *Machine, file string, args []string) error {
	fs := flag.NewFlagSet("publish", flag.ExitOnError)
	submit := fs.String("submit-repo", envOr("OMASUSHI_SUBMIT_REPO", submitRepo), "GitHub owner/repo whose issue form takes submissions")
	dry := fs.Bool("dry-run", false, "resolve and print the submission URL, open nothing")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, `usage: omasushi publish [--dry-run] [<name>|<owner/repo>|<url>|<path>]

With no argument, publishes the omakase in the working directory (./omasushi.yaml),
else the single omakase in use. A configured omakase's name, a GitHub owner/repo,
a github.com / gitlab.com URL, or a local checkout also work.

Publishing opens a prefilled submission issue on github.com/`+*submit+` in your
browser. A workflow there validates omasushi.yaml and comments the plate's URL
on the issue once it is on the belt.`)
		fs.PrintDefaults()
	}
	fs.Parse(args)
	if fs.NArg() > 1 {
		fs.Usage()
		os.Exit(2)
	}

	repo, dir, err := publishTarget(machine, file, fs.Arg(0))
	if err != nil {
		return err
	}
	if dir != "" {
		for _, w := range publishWarnings(dir) {
			fmt.Fprintln(os.Stderr, "warning:", w)
		}
	}
	fmt.Printf("omakase: %s\n", repo)

	issue, err := submitIssueURL(*submit, repo)
	if err != nil {
		return err
	}
	fmt.Printf("submit:  %s\n", issue)
	if *dry {
		return nil
	}
	if err := openBrowser(issue); err != nil {
		return fmt.Errorf("could not open a browser (%v); open the URL above yourself", err)
	}
	fmt.Println("press Submit there; the workflow comments the plate's URL on the issue")
	return nil
}

// submitIssueURL builds the prefilled "Submit an omakase" issue form URL.
// Query keys matching the form's field ids (repo) prefill them.
func submitIssueURL(submitRepo, repo string) (string, error) {
	parts := strings.Split(submitRepo, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", fmt.Errorf("bad submit repo %q (want owner/repo; set --submit-repo or $OMASUSHI_SUBMIT_REPO)", submitRepo)
	}
	q := url.Values{
		"template": {"submit-omakase.yml"},
		"title":    {"Submit: " + strings.TrimPrefix(strings.TrimPrefix(repo, "https://"), "www.")},
		"repo":     {repo},
	}
	return "https://github.com/" + submitRepo + "/issues/new?" + q.Encode(), nil
}

// publishTarget resolves what to publish into a canonical repository URL and,
// when it comes from a local checkout, that checkout's directory. With no
// argument it is the checkout you are standing in, else this machine's recipe
// — the one layer meant to be shared.
func publishTarget(machine *Machine, file, arg string) (repo, dir string, err error) {
	switch {
	case arg == "" && file != "":
		dir = filepath.Dir(file)
	case arg == "":
		if _, statErr := os.Stat(ManifestFile); statErr == nil {
			dir = "." // standing in a checkout: publish the one you are in
			break
		}
		if r := machine.recipeRepo(); r != "" {
			return publishDir(r)
		}
		return "", "", fmt.Errorf("nothing to publish: no %s here, and no recipe set (`omasushi recipe <path>` names the omakase this machine publishes)", ManifestFile)
	default:
		omakases, err := activeOmakases(machine, "")
		if err != nil {
			return "", "", err
		}
		for _, r := range omakases {
			if r.Name == arg {
				return publishOmakase(r)
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

	return publishDir(dir)
}

// publishDir is the local half of publishing: the checkout's origin, plus the
// checks that it is an omakase at all. This machine's own manifest is not one
// — it is the layer that never leaves the machine.
func publishDir(dir string) (repo, abs string, err error) {
	abs, err = filepath.Abs(dir)
	if err != nil {
		return "", "", err
	}
	if abs == machineDir() {
		return "", "", fmt.Errorf("%s is this machine's own omakase, not a recipe — publish the one under recipe:", tildify(abs))
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
