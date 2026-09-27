package tests3

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeDaemon answers the Docker Engine API calls the suite makes over a
// unix socket, records them, and keeps the hijacked stdin connections so a
// test can observe that stopping a container closes its stdin.
type fakeDaemon struct {
	sock string

	mu        sync.Mutex
	calls     []string
	created   map[string]any
	stdin     chan net.Conn
	hasImage  bool
	pullError string
	noPort    bool
	failStart bool
	attach    int // status of the attach answer
}

func startFakeDaemon(t *testing.T) *fakeDaemon {
	t.Helper()
	// A unix socket path must stay short: t.TempDir can exceed the limit.
	dir, err := os.MkdirTemp("", "dkr")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	f := &fakeDaemon{sock: filepath.Join(dir, "d.sock"), stdin: make(chan net.Conn, 4), hasImage: true, attach: http.StatusSwitchingProtocols}
	ln, err := net.Listen("unix", f.sock)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(f.serve)}
	go func() { _ = server.Serve(ln) }()
	t.Cleanup(func() { _ = server.Close() })
	return f
}

func (f *fakeDaemon) record(r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)
}

func (f *fakeDaemon) serve(w http.ResponseWriter, r *http.Request) {
	f.record(r)
	switch {
	case r.URL.Path == "/_ping":
		_, _ = io.WriteString(w, "OK")
	case strings.HasPrefix(r.URL.Path, "/images/") && strings.HasSuffix(r.URL.Path, "/json"):
		if !f.hasImage {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"message":"No such image"}`)
			return
		}
		_, _ = io.WriteString(w, `{}`)
	case r.URL.Path == "/images/create":
		if r.URL.Query().Get("fromImage") != "chrislusf/seaweedfs" || r.URL.Query().Get("tag") != "4.42" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = io.WriteString(w, `{"status":"Pulling"}`+"\n")
		if f.pullError != "" {
			_, _ = fmt.Fprintf(w, `{"error":%q}`+"\n", f.pullError)
		}
	case r.URL.Path == "/containers/create":
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.created = body
		f.mu.Unlock()
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"Id":"c1"}`)
	case r.URL.Path == "/containers/c1/attach":
		if f.attach != http.StatusSwitchingProtocols {
			w.WriteHeader(f.attach)
			_, _ = io.WriteString(w, `{"message":"cannot attach"}`)
			return
		}
		conn, buf, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		_, _ = buf.WriteString("HTTP/1.1 101 UPGRADED\r\nConnection: Upgrade\r\nUpgrade: tcp\r\n\r\n")
		_ = buf.Flush()
		f.stdin <- conn
	case r.URL.Path == "/containers/c1/start":
		if f.failStart {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"message":"port is already allocated"}`)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case r.URL.Path == "/containers/c1/json":
		if f.noPort {
			_, _ = io.WriteString(w, `{"NetworkSettings":{"Ports":{}}}`)
			return
		}
		_, _ = io.WriteString(w, `{"NetworkSettings":{"Ports":{"8333/tcp":[{"HostIp":"127.0.0.1","HostPort":"40123"}]}}}`)
	case r.Method == http.MethodDelete && r.URL.Path == "/containers/c1":
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, "page not found")
	}
}

func (f *fakeDaemon) called() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeDaemon) client(t *testing.T) *dockerClient {
	t.Helper()
	d, err := dockerDaemon(envOf(map[string]string{"DOCKER_HOST": "unix://" + f.sock}))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func envOf(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

var testSpec = containerSpec{
	Image: Image, Entrypoint: []string{"/bin/sh"}, Cmd: []string{"-c", "cat"},
	Port: "8333/tcp", Labels: map[string]string{"org.slivingdoc.tests3": "seaweedfs"},
}

// TestContainerLivesWhileItsStdinIsHeld proves the lifecycle that replaces
// a reaper: the container is created to end with its stdin and to be
// removed on exit, the stdin stream is attached before the start, and
// stopping closes that stream.
func TestContainerLivesWhileItsStdinIsHeld(t *testing.T) {
	f := startFakeDaemon(t)
	d := f.client(t)
	ctx := context.Background()
	if err := d.ping(ctx); err != nil {
		t.Fatalf("ping() = %v", err)
	}
	if err := d.ensureImage(ctx, Image); err != nil {
		t.Fatalf("ensureImage() of a present image = %v", err)
	}
	c, port, err := d.run(ctx, testSpec)
	if err != nil {
		t.Fatalf("run() = %v", err)
	}
	if port != "40123" {
		t.Fatalf("run() port = %q, want the published host port", port)
	}
	created := f.created
	host := created["HostConfig"].(map[string]any)
	if created["OpenStdin"] != true || created["StdinOnce"] != true || host["AutoRemove"] != true {
		t.Fatalf("create request = %v, want stdin held once and the container removed on exit", created)
	}
	binding := host["PortBindings"].(map[string]any)["8333/tcp"].([]any)[0].(map[string]any)
	if binding["HostIp"] != "127.0.0.1" {
		t.Fatalf("port binding = %v, want loopback only", binding)
	}
	calls := f.called()
	want := []string{"GET /_ping", "GET /images/chrislusf/seaweedfs:4.42/json", "POST /containers/create", "POST /containers/c1/attach", "POST /containers/c1/start", "GET /containers/c1/json"}
	if strings.Join(calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("calls = %q, want %q", calls, want)
	}
	server := <-f.stdin
	c.stop()
	_ = server.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := bufio.NewReader(server).ReadByte(); !errors.Is(err, io.EOF) {
		t.Fatalf("stdin after stop = %v, want end of input", err)
	}
}

func TestEnsureImagePullsAMissingImage(t *testing.T) {
	f := startFakeDaemon(t)
	f.hasImage = false
	if err := f.client(t).ensureImage(context.Background(), Image); err != nil {
		t.Fatalf("ensureImage() = %v", err)
	}
	if calls := f.called(); calls[len(calls)-1] != "POST /images/create" {
		t.Fatalf("calls = %q, want a pull", calls)
	}
	f.pullError = "toomanyrequests: rate limit"
	if err := f.client(t).ensureImage(context.Background(), Image); err == nil || !strings.Contains(err.Error(), "toomanyrequests") {
		t.Fatalf("ensureImage() with a failing pull = %v, want the stream's error", err)
	}
}

func TestRunFailures(t *testing.T) {
	for _, tt := range []struct {
		name  string
		setup func(*fakeDaemon)
		want  string
	}{
		{"attach refused", func(f *fakeDaemon) { f.attach = http.StatusConflict }, "attach container: docker: HTTP 409: cannot attach"},
		{"start refused", func(f *fakeDaemon) { f.failStart = true }, "start container: docker: HTTP 500: port is already allocated"},
		{"no port", func(f *fakeDaemon) { f.noPort = true }, "publishes no host port"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := startFakeDaemon(t)
			tt.setup(f)
			if _, _, err := f.client(t).run(context.Background(), testSpec); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("run() = %v, want %q", err, tt.want)
			}
			if calls := f.called(); calls[len(calls)-1] != "DELETE /containers/c1" && tt.name == "attach refused" {
				t.Fatalf("calls = %q, want the unattached container removed", calls)
			}
		})
	}
}

func TestCallReportsTheDaemonsAnswer(t *testing.T) {
	f := startFakeDaemon(t)
	err := f.client(t).call(context.Background(), http.MethodGet, "/nowhere", nil, nil)
	var answer *dockerError
	if !errors.As(err, &answer) || answer.Status != http.StatusNotFound || answer.Message != "page not found" {
		t.Fatalf("call() = %v, want the plain-text 404", err)
	}
	unreachable := newDockerClient("unix", filepath.Join(t.TempDir(), "none.sock"), "127.0.0.1")
	if err := unreachable.ping(context.Background()); err == nil {
		t.Fatal("ping() of a missing socket = nil")
	}
}

func TestDockerDaemonLocation(t *testing.T) {
	sockDir, err := os.MkdirTemp("", "dkr")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(sockDir) })
	ln, err := net.Listen("unix", filepath.Join(sockDir, "docker.sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })

	if d, err := dockerDaemon(envOf(map[string]string{"XDG_RUNTIME_DIR": sockDir, "HOME": t.TempDir()})); err != nil || d.host != "127.0.0.1" {
		// The system socket may exist on the host; either way a socket is found.
		t.Fatalf("dockerDaemon() with a rootless socket = %v, %v", d, err)
	}
	if d, err := dockerDaemon(envOf(map[string]string{"DOCKER_HOST": "tcp://docker.example:2375"})); err != nil || d.host != "docker.example" || d.bindIP() != "" {
		t.Fatalf("dockerDaemon(tcp) = %+v, %v; want the daemon's host", d, err)
	}
	for _, tt := range []struct {
		env  map[string]string
		want string
	}{
		{map[string]string{"DOCKER_HOST": "tcp://docker.example:2376", "DOCKER_TLS_VERIFY": "1"}, "TLS"},
		{map[string]string{"DOCKER_HOST": "ssh://user@host"}, `scheme "ssh"`},
		{map[string]string{"DOCKER_HOST": "::"}, "DOCKER_HOST"},
	} {
		if _, err := dockerDaemon(envOf(tt.env)); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Fatalf("dockerDaemon(%v) = %v, want %q", tt.env, err, tt.want)
		}
	}
	if got := dockerSockets(envOf(nil)); len(got) != 1 || got[0] != "/var/run/docker.sock" {
		t.Fatalf("dockerSockets() without XDG_RUNTIME_DIR or HOME = %v", got)
	}
}
