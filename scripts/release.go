/*usr/local/go/bin/go run "$0" "$@"; exit; */
package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

const (
	pkgPath       = "npm/slivingdoc/package.json"
	releaseBranch = "master"
)

// semverRE is the exact semver.org pattern the release pipeline applies to
// release tags (RE2-compatible). The leading v is optional on input; the
// tag itself always carries it.
var semverRE = regexp.MustCompile(`^v?([0-9]|[1-9][0-9]*)\.([0-9]|[1-9][0-9]*)\.([0-9]|[1-9][0-9]*)(-(([0-9]|[1-9][0-9]*|[0-9]*[a-zA-Z-][0-9a-zA-Z-]*)(\.([0-9]|[1-9][0-9]*|[0-9]*[a-zA-Z-][0-9a-zA-Z-]*))*))?(\+[0-9a-zA-Z-]+(\.[0-9a-zA-Z-]+)*)?$`)

var versionLineRE = regexp.MustCompile(`^(\s*)"version"\s*:\s*"([^"]*)"(.*)$`)

type releaseTag struct {
	version string
	tag     string
	date    string
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "release:", err)
		os.Exit(1)
	}
}

func run() error {
	for _, marker := range []string{"go.mod", "Makefile", pkgPath} {
		if _, err := os.Stat(marker); err != nil {
			return fmt.Errorf("run from the repository root; %s is missing", marker)
		}
	}

	branch, err := gitOutput("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return err
	}
	if branch != releaseBranch {
		return fmt.Errorf("current branch is %q; releases target %s", branch, releaseBranch)
	}

	dirty, err := gitOutput("status", "--porcelain")
	if err != nil {
		return err
	}
	if dirty != "" {
		return fmt.Errorf("working tree is dirty; commit or stash changes first:\n%s", dirty)
	}

	current, err := pkgVersion()
	if err != nil {
		return err
	}

	tags, err := recentTags(5)
	if err != nil {
		return err
	}

	fmt.Printf("Current npm launcher version: %s\n\n", current)
	fmt.Println("Recent releases:")
	fmt.Printf(" %s %-2s  %-16s  %-17s  %s\n", " ", "#", "version", "tag", "date")
	for i, t := range tags {
		mark := " "
		if t.version == current {
			mark = "*"
		}
		fmt.Printf(" %s %-2d  %-16s  %-17s  %s\n", mark, i+1, t.version, t.tag, t.date)
	}
	fmt.Println("  * = current package version")
	fmt.Println()

	reader := bufio.NewReader(os.Stdin)

	var version string
	for {
		v, err := prompt(reader, "Enter the new release version: ")
		if err != nil {
			return err
		}
		version = strings.TrimPrefix(v, "v")
		if !semverRE.MatchString(version) {
			fmt.Printf("error: '%s' is not a valid semver version (v<major>.<minor>.<patch>[-prerelease]); try again\n", version)
			continue
		}
		if version == current {
			fmt.Printf("error: package.json is already at '%s'; try again\n", version)
			continue
		}
		tag := "v" + version
		exists, err := tagExists(tag)
		if err != nil {
			return err
		}
		if exists {
			fmt.Printf("error: tag '%s' already exists; try again\n", tag)
			continue
		}
		break
	}

	tag := "v" + version
	desc, err := prompt(reader, fmt.Sprintf("Tag description (default: Release %s): ", tag))
	if err != nil {
		return err
	}
	if desc == "" {
		desc = "Release " + tag
	}

	if err := bumpVersion(pkgPath, version); err != nil {
		return err
	}
	fmt.Printf("Bumped %s: %s -> %s\n", pkgPath, current, version)

	fmt.Println("Running npm launcher tests")
	cmd := exec.Command("npm", "test", "--prefix", "npm/slivingdoc")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("npm test failed: %w", err)
	}

	steps := [][]string{
		{"add", pkgPath},
		{"commit", "-m", "release: " + tag, "-m", desc},
		{"tag", "-a", tag, "-m", desc},
		{"push", "origin", releaseBranch},
		{"push", "origin", tag},
	}
	for _, args := range steps {
		fmt.Println("git", strings.Join(args, " "))
		if err := runGit(args...); err != nil {
			return err
		}
	}

	fmt.Println("release:", tag, "is released; the workflow run is at")
	fmt.Println("  https://github.com/baalimago/slivingdoc/actions")
	return nil
}

func gitOutput(args ...string) (string, error) {
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}

func runGit(args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

func pkgVersion() (string, error) {
	data, err := os.ReadFile(pkgPath)
	if err != nil {
		return "", err
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if m := versionLineRE.FindStringSubmatch(line); m != nil {
			return m[2], nil
		}
	}
	return "", fmt.Errorf("no version field in %s", pkgPath)
}

func bumpVersion(path, version string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	found := false
	for i, line := range lines {
		if m := versionLineRE.FindStringSubmatch(line); m != nil {
			lines[i] = m[1] + `"version": "` + version + `"` + m[3]
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("no version field in %s", path)
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), info.Mode().Perm())
}

func recentTags(n int) ([]releaseTag, error) {
	out, err := gitOutput("for-each-ref", "--sort=-creatordate", "--format=%(refname:short)%09%(creatordate:short)", "refs/tags/v*")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	var tags []releaseTag
	for line := range strings.SplitSeq(out, "\n") {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		name := parts[0]
		if !strings.HasPrefix(name, "v") {
			continue
		}
		tags = append(tags, releaseTag{version: name[1:], tag: name, date: parts[1]})
		if len(tags) >= n {
			break
		}
	}
	return tags, nil
}

func tagExists(tag string) (bool, error) {
	if _, err := exec.Command("git", "rev-parse", "--quiet", "--verify", "refs/tags/"+tag).Output(); err == nil {
		return true, nil
	}
	err := exec.Command("git", "ls-remote", "--exit-code", "--tags", "origin", "refs/tags/"+tag).Run()
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 2 {
		return false, nil
	}
	fmt.Fprintf(os.Stderr, "release: warning: cannot check origin for %s (%v); continuing\n", tag, err)
	return false, nil
}

func prompt(r *bufio.Reader, label string) (string, error) {
	fmt.Print(label)
	line, err := r.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	if err == io.EOF && line == "" {
		return "", errors.New("no input; aborting")
	}
	return strings.TrimSpace(line), nil
}
