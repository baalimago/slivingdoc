//go:build !windows

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/build"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// The release scripts are the publication half of the contract: the
// dependency baselines of architecture section 21, the strict SHA256SUMS
// grammar the npm launcher parses, and the immutable release-workflow
// reference. They are POSIX shell, so these tests drive the real scripts
// rather than reimplementing their rules.

// releaseTestVersion is injected into the smoke binary through the linker so
// --version proves the release build wiring, not a compiled-in default.
const releaseTestVersion = "0.0.0-release-test"

type registryManifest struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	Packages []struct {
		Identifier       string `json:"identifier"`
		Version          string `json:"version"`
		RegistryType     string `json:"registryType"`
		RuntimeHint      string `json:"runtimeHint"`
		RuntimeArguments []struct {
			Type  string `json:"type"`
			Value string `json:"value"`
		} `json:"runtimeArguments"`
		PackageArguments []struct {
			Type  string `json:"type"`
			Value string `json:"value"`
		} `json:"packageArguments"`
		EnvironmentVariables []struct {
			Name       string `json:"name"`
			IsRequired bool   `json:"isRequired"`
		} `json:"environmentVariables"`
		Transport struct {
			Type string `json:"type"`
		} `json:"transport"`
	} `json:"packages"`
}

type npmPackageManifest struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	MCPName string `json:"mcpName"`
}

// TestMCPRegistryManifest proves the source-controlled npm package and
// registry card describe one installable stdio server. The registry verifies
// mcpName from the published npm package, so this parity must survive every
// release bump.
func TestMCPRegistryManifest(t *testing.T) {
	t.Parallel()

	pkgData, err := os.ReadFile("npm/slivingdoc/package.json")
	if err != nil {
		t.Fatalf("read npm package manifest: %v", err)
	}
	var pkg npmPackageManifest
	if err := json.Unmarshal(pkgData, &pkg); err != nil {
		t.Fatalf("decode npm package manifest: %v", err)
	}

	registryData, err := os.ReadFile("server.json")
	if err != nil {
		t.Fatalf("read MCP Registry manifest: %v", err)
	}
	var registry registryManifest
	if err := json.Unmarshal(registryData, &registry); err != nil {
		t.Fatalf("decode MCP Registry manifest: %v", err)
	}

	if registry.Name != pkg.MCPName {
		t.Fatalf("registry name = %q, npm mcpName = %q", registry.Name, pkg.MCPName)
	}
	if registry.Version != pkg.Version {
		t.Fatalf("registry version = %q, npm version = %q", registry.Version, pkg.Version)
	}
	if len(registry.Packages) != 1 {
		t.Fatalf("registry packages = %d, want 1", len(registry.Packages))
	}

	entry := registry.Packages[0]
	if entry.RegistryType != "npm" || entry.Identifier != pkg.Name || entry.Version != pkg.Version {
		t.Fatalf("registry package = (%q, %q, %q), want npm package %q at %q", entry.RegistryType, entry.Identifier, entry.Version, pkg.Name, pkg.Version)
	}
	if entry.RuntimeHint != "npx" || entry.Transport.Type != "stdio" {
		t.Fatalf("registry runtime and transport = (%q, %q), want (npx, stdio)", entry.RuntimeHint, entry.Transport.Type)
	}
	if len(entry.RuntimeArguments) != 1 || entry.RuntimeArguments[0].Type != "positional" || entry.RuntimeArguments[0].Value != "-y" {
		t.Fatalf("registry runtime arguments = %+v, want one positional -y", entry.RuntimeArguments)
	}
	if len(entry.PackageArguments) != 1 || entry.PackageArguments[0].Type != "positional" || entry.PackageArguments[0].Value != "serve" {
		t.Fatalf("registry package arguments = %+v, want one positional serve", entry.PackageArguments)
	}

	hasBucket := false
	for _, variable := range entry.EnvironmentVariables {
		if variable.Name == "SLIVINGDOC_BUCKET" && variable.IsRequired {
			hasBucket = true
		}
	}
	if !hasBucket {
		t.Fatal("registry manifest does not require SLIVINGDOC_BUCKET")
	}
}

// TestMCPRegistryPublishWorkflow keeps the automated Registry publication
// behind the npm publication it verifies. The Registry accepts a GitHub OIDC
// identity for io.github.baalimago/*, so this job needs no stored credential.
func TestMCPRegistryPublishWorkflow(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(".github/workflows/release.yml")
	if err != nil {
		t.Fatalf("read release workflow: %v", err)
	}
	workflow := string(data)

	publishNPM := strings.Index(workflow, "  publish-npm:")
	publishMCP := strings.Index(workflow, "  publish-mcp:")
	if publishNPM < 0 || publishMCP < 0 || publishMCP < publishNPM {
		t.Fatal("release workflow does not define publish-mcp after publish-npm")
	}
	for _, want := range []string{
		"needs: [publish-npm]",
		"if: github.ref_type == 'tag'",
		"id-token: write",
		"mcp-publisher validate server.json",
		"mcp-publisher login github-oidc",
		"mcp-publisher publish server.json",
	} {
		if !strings.Contains(workflow[publishMCP:], want) {
			t.Errorf("publish-mcp job does not contain %q", want)
		}
	}
}

// slivingdocEnv is every variable that configures the server. A spawned
// process must not inherit these from whoever runs the suite: a developer
// who exports SLIVINGDOC_BUCKET to drive the CLI would otherwise hand the
// released binary a bucket and turn the startup-refusal assertions into
// silent passes or confusing failures. internal/integrationtest sanitizes
// the same set for the same reason.
var slivingdocEnv = map[string]bool{
	"AWS_ACCESS_KEY_ID": true, "AWS_SECRET_ACCESS_KEY": true, "AWS_SESSION_TOKEN": true,
	"AWS_PROFILE": true, "AWS_DEFAULT_REGION": true, "AWS_REGION": true,
	"AWS_ENDPOINT_URL_S3": true, "AWS_ENDPOINT_URL": true, "AWS_CA_BUNDLE": true,
	"AWS_SHARED_CREDENTIALS_FILE": true, "AWS_CONFIG_FILE": true,
	"SLIVINGDOC_BUCKET": true, "SLIVINGDOC_PREFIX": true,
	"SLIVINGDOC_WORKSPACE_ROOT": true, "SLIVINGDOC_PRIVATE_ROOT": true,
	"SLIVINGDOC_PATH_STYLE": true, "SLIVINGDOC_SHARED_PACK_CACHE": true,
	"SLIVINGDOC_COMMIT_RETRIES":   true,
	"SLIVINGDOC_CHECKPOINT_PACKS": true, "SLIVINGDOC_RETAINED_CHECKPOINTS": true,
	"NO_COLOR": true, "LOG_LEVEL": true,
}

// sanitizedEnv is the ambient environment without any slivingdoc
// configuration. Everything else is preserved, because the same spawner
// runs the Go toolchain and the dependency-check scripts, which need PATH,
// HOME, and the Go cache variables.
func sanitizedEnv() []string {
	env := os.Environ()
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if name, _, ok := strings.Cut(kv, "="); ok && slivingdocEnv[name] {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// startAndWait runs name with args and collects its streams and exit code.
//
// os/exec is absent from the server by contract: the seam scan in
// internal/git2 rejects it module-wide (scripts/ excepted) so the server
// can never shell out to the Git executable
// (TestNoGitExecutableOrGit2goImport). Processes therefore start through
// os.StartProcess, as they do everywhere else here.
func startAndWait(name string, args ...string) (stdout, stderr string, code int, err error) {
	devNull, err := os.OpenFile(os.DevNull, os.O_RDONLY, 0)
	if err != nil {
		return "", "", -1, err
	}
	defer devNull.Close()

	outR, outW, err := os.Pipe()
	if err != nil {
		return "", "", -1, err
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		outR.Close()
		outW.Close()
		return "", "", -1, err
	}

	proc, startErr := os.StartProcess(name, append([]string{name}, args...), &os.ProcAttr{
		Env:   sanitizedEnv(),
		Files: []*os.File{devNull, outW, errW},
	})
	// The parent must drop its write ends or the readers never see EOF.
	outW.Close()
	errW.Close()
	if startErr != nil {
		outR.Close()
		errR.Close()
		return "", "", -1, fmt.Errorf("start %s: %w", name, startErr)
	}

	// Both pipes drain concurrently: a child that fills one while the parent
	// blocks on the other deadlocks.
	var wg sync.WaitGroup
	var out, errOut []byte
	wg.Add(2)
	go func() { defer wg.Done(); out, _ = io.ReadAll(outR) }()
	go func() { defer wg.Done(); errOut, _ = io.ReadAll(errR) }()

	state, waitErr := proc.Wait()
	wg.Wait()
	outR.Close()
	errR.Close()
	if waitErr != nil {
		return string(out), string(errOut), -1, fmt.Errorf("wait %s: %w", name, waitErr)
	}
	return string(out), string(errOut), state.ExitCode(), nil
}

// runScript runs a repository script and returns its streams and exit code.
func runScript(t *testing.T, name string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	stdout, stderr, code, err := startAndWait(filepath.Join("scripts", name), args...)
	if err != nil {
		t.Fatalf("run %s: %v", name, err)
	}
	return stdout, stderr, code
}

// TestReleaseDependencyBaselines proves each platform checker accepts
// exactly the architecture section 21 baseline and rejects anything the
// pinned build must link statically or bundle — above all libgit2.
func TestReleaseDependencyBaselines(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		name   string
		script string
		deps   []string
		accept bool
	}{
		{"linux static (no dynamic deps)", "check-deps-linux.sh", nil, true},
		{"linux libc alone", "check-deps-linux.sh", []string{"libc.so.6"}, true},
		{
			"linux complete baseline", "check-deps-linux.sh",
			[]string{
				"linux-vdso.so.1", "libc.so.6", "ld-linux-x86-64.so.2",
				"libpthread.so.0", "libdl.so.2", "librt.so.1", "libm.so.6",
			},
			true,
		},
		{"linux rejects libgit2", "check-deps-linux.sh", []string{"libc.so.6", "libgit2.so.1"}, false},
		{"linux rejects zlib", "check-deps-linux.sh", []string{"libc.so.6", "libz.so.1"}, false},
		{"linux rejects pcre2", "check-deps-linux.sh", []string{"libc.so.6", "libpcre2-8.so.0"}, false},

		{"macos usr lib", "check-deps-macos.sh", []string{"/usr/lib/libSystem.B.dylib"}, true},
		{
			"macos complete baseline", "check-deps-macos.sh",
			[]string{
				"/usr/lib/libSystem.B.dylib",
				"/System/Library/Frameworks/CoreFoundation.framework/Versions/A/CoreFoundation",
				"/System/Library/Frameworks/Security.framework/Versions/A/Security",
			},
			true,
		},
		{
			"macos rejects local libgit2", "check-deps-macos.sh",
			[]string{"/usr/lib/libSystem.B.dylib", "/usr/local/lib/libgit2.1.dylib"},
			false,
		},
		{"macos rejects rpath libgit2", "check-deps-macos.sh", []string{"@rpath/libgit2.dylib"}, false},
		{"macos rejects homebrew zlib", "check-deps-macos.sh", []string{"/opt/homebrew/lib/libz.1.dylib"}, false},

		{"windows system dll", "check-deps-windows.sh", []string{"KERNEL32.dll"}, true},
		{
			"windows complete baseline", "check-deps-windows.sh",
			[]string{"KERNEL32.dll", "msvcrt.dll", "ucrtbase.dll", "WS2_32.dll", "bcrypt.dll", "CRYPT32.dll", "ADVAPI32.dll"},
			true,
		},
		{
			"windows ucrt api-set forwarders", "check-deps-windows.sh",
			[]string{"KERNEL32.dll", "api-ms-win-crt-stdio-l1-1-0.dll", "api-ms-win-crt-heap-l1-1-0.dll", "API-MS-WIN-CRT-MATH-L1-1-0.DLL"},
			true,
		},
		{"windows rejects non-crt api-set", "check-deps-windows.sh", []string{"KERNEL32.dll", "api-ms-win-core-synch-l1-1-0.dll"}, false},
		{"windows rejects git2", "check-deps-windows.sh", []string{"KERNEL32.dll", "git2.dll"}, false},
		{"windows rejects libgit2", "check-deps-windows.sh", []string{"KERNEL32.dll", "libgit2.dll"}, false},
		{"windows rejects mingw runtime", "check-deps-windows.sh", []string{"KERNEL32.dll", "libgcc_s_seh-1.dll"}, false},
		{"windows rejects third party", "check-deps-windows.sh", []string{"KERNEL32.dll", "libssl-3.dll"}, false},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			_, stderr, code := runScript(t, row.script, append([]string{"--check"}, row.deps...)...)
			if row.accept {
				if code != 0 {
					t.Fatalf("%s rejected the baseline %v: exit %d; stderr: %s", row.script, row.deps, code, stderr)
				}
				return
			}
			if code == 0 {
				t.Fatalf("%s accepted %v, want rejection", row.script, row.deps)
			}
			// The diagnostic must name the offending dependency, or a release
			// failure is unactionable.
			if !strings.Contains(stderr, "unexpected dynamic dependencies") {
				t.Fatalf("%s stderr = %q, want the unexpected-dependency diagnostic", row.script, stderr)
			}
		})
	}
}

// TestReleaseChecksumGrammar proves make-sha256sums.sh emits the exact
// grammar the npm launcher's strict parser accepts: sorted by asset name,
// lowercase 64-hex digest, two spaces, basename, and a trailing LF. The
// digests are recomputed here, so a wrong hash fails rather than a
// well-shaped one passing.
func TestReleaseChecksumGrammar(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	assets := map[string][]byte{"z-asset": []byte("zebra content"), "a-asset": []byte("alpha content")}
	for name, content := range assets {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	// Input order must not matter; the output is sorted by asset name.
	stdout, stderr, code := runScript(t, "make-sha256sums.sh",
		filepath.Join(dir, "z-asset"), filepath.Join(dir, "a-asset"))
	if code != 0 {
		t.Fatalf("make-sha256sums.sh = exit %d; stderr: %s", code, stderr)
	}

	var want strings.Builder
	for _, name := range []string{"a-asset", "z-asset"} {
		sum := sha256.Sum256(assets[name])
		fmt.Fprintf(&want, "%s  %s\n", hex.EncodeToString(sum[:]), name)
	}
	if stdout != want.String() {
		t.Fatalf("SHA256SUMS =\n%q\nwant\n%q", stdout, want.String())
	}

	if _, _, code := runScript(t, "make-sha256sums.sh"); code == 0 {
		t.Fatal("make-sha256sums.sh accepted an empty asset list")
	}
}

// TestReleaseWorkflowReference proves the reusable release pipeline may only
// be referenced by an immutable commit SHA: a branch, a moving tag, a short
// SHA, and the phase-8 placeholder are all refused.
func TestReleaseWorkflowReference(t *testing.T) {
	t.Parallel()
	const uses = "uses: baalimago/simple-go-pipeline/.github/workflows/release.yml@"
	for _, row := range []struct {
		name   string
		ref    string
		accept bool
	}{
		{"pinned commit sha", "0123456789abcdef0123456789abcdef01234567", true},
		{"phase-8 placeholder", "0000000000000000000000000000000000000000", false},
		{"moving tag", "v0.2.8", false},
		{"branch", "main", false},
		{"short sha", "01234567", false},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			file := filepath.Join(t.TempDir(), "release.yml")
			if err := os.WriteFile(file, []byte(uses+row.ref+"\n"), 0o644); err != nil {
				t.Fatalf("write fixture: %v", err)
			}
			_, stderr, code := runScript(t, "check-release-ref.sh", file)
			if row.accept && code != 0 {
				t.Fatalf("check-release-ref.sh rejected %q: exit %d; stderr: %s", row.ref, code, stderr)
			}
			if !row.accept && code == 0 {
				t.Fatalf("check-release-ref.sh accepted %q, want rejection", row.ref)
			}
		})
	}

	t.Run("missing file", func(t *testing.T) {
		t.Parallel()
		if _, _, code := runScript(t, "check-release-ref.sh", filepath.Join(t.TempDir(), "absent.yml")); code == 0 {
			t.Fatal("check-release-ref.sh accepted a missing workflow file")
		}
	})
}

// releaseBinaryDir holds the temporary directory TestMain removes; the build
// happens at most once per test process.
var releaseBinaryDir string

// releaseBinary builds the release-style executable once per test process,
// so -count=3 links libgit2 once rather than three times.
var releaseBinary = sync.OnceValues(func() (string, error) {
	dir, err := os.MkdirTemp("", "slivingdoc-release-")
	if err != nil {
		return "", err
	}
	releaseBinaryDir = dir
	bin := filepath.Join(dir, "slivingdoc")
	goTool := filepath.Join(build.Default.GOROOT, "bin", "go")
	ldflags := "-s -w -X github.com/baalimago/slivingdoc/internal/app.Version=" + releaseTestVersion
	_, stderr, code, err := startAndWait(goTool, "build", "-trimpath", "-ldflags", ldflags, "-o", bin, ".")
	if err != nil {
		return "", err
	}
	if code != 0 {
		return "", fmt.Errorf("go build: exit %d: %s", code, stderr)
	}
	return bin, nil
})

// TestReleaseBinary proves the release build wiring: the version is injected
// through the linker, and on Linux the resulting executable links only the
// architecture section 21 baseline, so libgit2 is genuinely static.
func TestReleaseBinary(t *testing.T) {
	t.Parallel()
	bin, err := releaseBinary()
	if err != nil {
		t.Fatalf("build release binary: %v", err)
	}

	stdout, stderr, code, err := startAndWait(bin, "version")
	if err != nil || code != 0 {
		t.Fatalf("%s version = exit %d (%v); stderr: %s", bin, code, err, stderr)
	}
	if want := "slivingdoc " + releaseTestVersion + "\n"; stdout != want {
		t.Fatalf("version = %q, want %q", stdout, want)
	}

	// readelf is a Linux-only inspection; the checker's rules themselves are
	// covered on every platform by TestReleaseDependencyBaselines.
	if runtime.GOOS != "linux" {
		return
	}
	t.Run("linux dependency baseline", func(t *testing.T) {
		stdout, stderr, code := runScript(t, "check-deps-linux.sh", bin)
		if code != 0 {
			t.Fatalf("check-deps-linux.sh %s = exit %d; stdout: %s stderr: %s", bin, code, stdout, stderr)
		}
	})
}

// TestReleaseBinaryCommandSurface proves the released executable's command
// line, which is what an MCP host and the npm launcher actually invoke. It
// runs the real binary, so it covers main.go and the router together —
// neither is reachable from an in-process test.
func TestReleaseBinaryCommandSurface(t *testing.T) {
	t.Parallel()
	bin, err := releaseBinary()
	if err != nil {
		t.Fatalf("build release binary: %v", err)
	}

	for _, row := range []struct {
		name     string
		args     []string
		wantCode int
		// wantStdout are substrings the command must print; a refusal must
		// still name both commands so the caller can correct the line.
		wantStdout []string
	}{
		{name: "version shortcut", args: []string{"v"}, wantStdout: []string{"slivingdoc " + releaseTestVersion}},
		{name: "serve help", args: []string{"serve", "-h"}, wantStdout: []string{"--bucket", "--retained-checkpoints"}},
		{name: "serve shortcut help", args: []string{"s", "-h"}, wantStdout: []string{"--bucket"}},
		{name: "no command", wantCode: 1, wantStdout: []string{"serve", "version"}},
		{name: "unknown command", args: []string{"frobnicate"}, wantCode: 1, wantStdout: []string{"serve", "version"}},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			stdout, stderr, code, err := startAndWait(bin, row.args...)
			if err != nil {
				t.Fatalf("run %v: %v", row.args, err)
			}
			if code != row.wantCode {
				t.Fatalf("%v = exit %d, want %d; stdout %q stderr %q", row.args, code, row.wantCode, stdout, stderr)
			}
			for _, want := range row.wantStdout {
				if !strings.Contains(stdout, want) {
					t.Fatalf("%v stdout = %q, want it to contain %q", row.args, stdout, want)
				}
			}
		})
	}

	// The serve command without a bucket is a refusal, not a server that
	// waits on stdin: a hanging process here is indistinguishable from
	// a working one until the suite times out.
	t.Run("serve without a bucket refuses", func(t *testing.T) {
		t.Parallel()
		stdout, stderr, code, err := startAndWait(bin, "serve", "--workspace-root="+t.TempDir())
		if err != nil {
			t.Fatalf("run serve: %v", err)
		}
		if code == 0 {
			t.Fatalf("serve without --bucket = exit 0, want a refusal; stdout %q", stdout)
		}
		if !strings.Contains(stderr, "bucket is required") {
			t.Fatalf("serve stderr = %q, want the required-bucket diagnostic", stderr)
		}
	})
}

func TestMain(m *testing.M) {
	code := m.Run()
	if releaseBinaryDir != "" {
		_ = os.RemoveAll(releaseBinaryDir)
	}
	os.Exit(code)
}
