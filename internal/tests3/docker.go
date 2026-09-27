package tests3

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// dockerClient speaks the few Docker Engine API calls the suite needs over
// the daemon socket with net/http alone. The suite used testcontainers-go
// before; its package initializer scans every process on the host, and the
// integration scenarios re-execute the test binary as their helper process
// hundreds of times per run, so that scan was a large share of the suite's
// CPU (architecture/testing.md).
type dockerClient struct {
	// host is where published ports are reached: loopback for a socket,
	// the daemon's host for tcp.
	host string
	dial func(ctx context.Context) (net.Conn, error)
	http *http.Client
}

// dockerDaemon locates the daemon: DOCKER_HOST (unix:// or plain tcp://),
// else the first existing socket among the system one, the rootless one
// under XDG_RUNTIME_DIR, and Docker Desktop's under the home directory.
func dockerDaemon(env func(string) string) (*dockerClient, error) {
	hostEnv := env("DOCKER_HOST")
	if hostEnv == "" {
		for _, sock := range dockerSockets(env) {
			if info, err := os.Stat(sock); err == nil && info.Mode()&os.ModeSocket != 0 {
				return newDockerClient("unix", sock, "127.0.0.1"), nil
			}
		}
		return nil, errors.New("no Docker socket found; set DOCKER_HOST")
	}
	u, err := url.Parse(hostEnv)
	if err != nil {
		return nil, fmt.Errorf("DOCKER_HOST: %w", err)
	}
	switch u.Scheme {
	case "unix":
		return newDockerClient("unix", u.Path, "127.0.0.1"), nil
	case "tcp":
		if env("DOCKER_TLS_VERIFY") != "" {
			return nil, errors.New("DOCKER_HOST: a TLS daemon is not supported by the test suite")
		}
		return newDockerClient("tcp", u.Host, u.Hostname()), nil
	default:
		return nil, fmt.Errorf("DOCKER_HOST: scheme %q is not supported by the test suite", u.Scheme)
	}
}

func dockerSockets(env func(string) string) []string {
	socks := []string{"/var/run/docker.sock"}
	if dir := env("XDG_RUNTIME_DIR"); dir != "" {
		socks = append(socks, filepath.Join(dir, "docker.sock"))
	}
	if home := env("HOME"); home != "" {
		socks = append(socks, filepath.Join(home, ".docker", "run", "docker.sock"))
	}
	return socks
}

func newDockerClient(network, address, host string) *dockerClient {
	var d net.Dialer
	dial := func(ctx context.Context) (net.Conn, error) { return d.DialContext(ctx, network, address) }
	return &dockerClient{
		host: host,
		dial: dial,
		http: &http.Client{Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) { return dial(ctx) },
		}},
	}
}

// dockerError is a non-success answer of the daemon.
type dockerError struct {
	Status  int
	Message string
}

func (e *dockerError) Error() string {
	return fmt.Sprintf("docker: HTTP %d: %s", e.Status, e.Message)
}

// call sends one API request with an optional JSON body and decodes a JSON
// answer into out when out is not nil.
func (d *dockerClient) call(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("docker: encode %s %s: %w", method, path, err)
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://docker"+path, reader)
	if err != nil {
		return fmt.Errorf("docker: %s %s: %w", method, path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := d.http.Do(req)
	if err != nil {
		return fmt.Errorf("docker: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return answerError(resp)
	}
	if out == nil {
		_, err = io.Copy(io.Discard, resp.Body)
		return err
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("docker: decode %s %s: %w", method, path, err)
	}
	return nil
}

func answerError(resp *http.Response) error {
	var answer struct {
		Message string `json:"message"`
	}
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if json.Unmarshal(data, &answer) != nil || answer.Message == "" {
		answer.Message = strings.TrimSpace(string(data))
	}
	return &dockerError{Status: resp.StatusCode, Message: answer.Message}
}

func (d *dockerClient) ping(ctx context.Context) error {
	return d.call(ctx, http.MethodGet, "/_ping", nil, nil)
}

// ensureImage pulls the image unless the daemon already has it. A pull
// answers 200 and reports a failure inside its progress stream.
func (d *dockerClient) ensureImage(ctx context.Context, image string) error {
	err := d.call(ctx, http.MethodGet, "/images/"+image+"/json", nil, nil)
	var missing *dockerError
	if !errors.As(err, &missing) || missing.Status != http.StatusNotFound {
		return err
	}
	name, tag, _ := strings.Cut(image, ":")
	query := url.Values{"fromImage": {name}, "tag": {tag}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://docker/images/create?"+query.Encode(), nil)
	if err != nil {
		return fmt.Errorf("docker: pull %s: %w", image, err)
	}
	resp, err := d.http.Do(req)
	if err != nil {
		return fmt.Errorf("docker: pull %s: %w", image, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("docker: pull %s: %w", image, answerError(resp))
	}
	decoder := json.NewDecoder(resp.Body)
	for {
		var progress struct {
			Error string `json:"error"`
		}
		if err := decoder.Decode(&progress); errors.Is(err, io.EOF) {
			return nil
		} else if err != nil {
			return fmt.Errorf("docker: pull %s: %w", image, err)
		}
		if progress.Error != "" {
			return fmt.Errorf("docker: pull %s: %s", image, progress.Error)
		}
	}
}

// containerSpec is the part of the create request the suite uses.
type containerSpec struct {
	Image      string
	Entrypoint []string
	Cmd        []string
	Port       string // the container port to publish, as "8333/tcp"
	Labels     map[string]string
}

// container is one running container whose lifetime is bound to stdin:
// its command ends at end of input, and the daemon removes it once it
// exits. Holding the attach connection holds the container; closing it,
// or the owning process dying, ends it, so no reaper process is needed.
type container struct {
	d     *dockerClient
	id    string
	stdin net.Conn
}

// run creates, attaches to and starts a container, then returns it with
// the host port that publishes spec.Port.
func (d *dockerClient) run(ctx context.Context, spec containerSpec) (*container, string, error) {
	var created struct {
		ID string `json:"Id"`
	}
	create := map[string]any{
		"Image":        spec.Image,
		"Entrypoint":   spec.Entrypoint,
		"Cmd":          spec.Cmd,
		"Labels":       spec.Labels,
		"AttachStdin":  true,
		"OpenStdin":    true,
		"StdinOnce":    true,
		"ExposedPorts": map[string]any{spec.Port: map[string]any{}},
		"HostConfig": map[string]any{
			"AutoRemove":   true,
			"PortBindings": map[string]any{spec.Port: []map[string]string{{"HostIp": d.bindIP(), "HostPort": ""}}},
		},
	}
	if err := d.call(ctx, http.MethodPost, "/containers/create", create, &created); err != nil {
		return nil, "", fmt.Errorf("create container: %w", err)
	}
	c := &container{d: d, id: created.ID}
	stdin, err := d.attachStdin(ctx, created.ID)
	if err != nil {
		c.remove()
		return nil, "", fmt.Errorf("attach container: %w", err)
	}
	c.stdin = stdin
	if err := d.call(ctx, http.MethodPost, "/containers/"+created.ID+"/start", nil, nil); err != nil {
		c.stop()
		return nil, "", fmt.Errorf("start container: %w", err)
	}
	var inspected struct {
		NetworkSettings struct {
			Ports map[string][]struct {
				HostPort string `json:"HostPort"`
			} `json:"Ports"`
		} `json:"NetworkSettings"`
	}
	if err := d.call(ctx, http.MethodGet, "/containers/"+created.ID+"/json", nil, &inspected); err != nil {
		c.stop()
		return nil, "", fmt.Errorf("inspect container: %w", err)
	}
	bindings := inspected.NetworkSettings.Ports[spec.Port]
	if len(bindings) == 0 || bindings[0].HostPort == "" {
		c.stop()
		return nil, "", fmt.Errorf("container publishes no host port for %s", spec.Port)
	}
	return c, bindings[0].HostPort, nil
}

func (d *dockerClient) bindIP() string {
	if d.host == "127.0.0.1" {
		return "127.0.0.1"
	}
	return ""
}

// attachStdin opens the hijacked attach stream of the container's stdin and
// returns the raw connection that carries it.
func (d *dockerClient) attachStdin(ctx context.Context, id string) (net.Conn, error) {
	conn, err := d.dial(ctx)
	if err != nil {
		return nil, err
	}
	request := "POST /containers/" + id + "/attach?stream=1&stdin=1 HTTP/1.1\r\n" +
		"Host: docker\r\nConnection: Upgrade\r\nUpgrade: tcp\r\nContent-Length: 0\r\n\r\n"
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	if _, err := io.WriteString(conn, request); err != nil {
		conn.Close()
		return nil, err
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if resp.StatusCode != http.StatusSwitchingProtocols && resp.StatusCode != http.StatusOK {
		err := answerError(resp)
		conn.Close()
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})
	return conn, nil
}

// stop ends the container by closing its stdin; the daemon then removes
// it. A container that never got an attached stdin is removed directly.
func (c *container) stop() {
	if c.stdin == nil {
		c.remove()
		return
	}
	_ = c.stdin.Close()
}

func (c *container) remove() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = c.d.call(ctx, http.MethodDelete, "/containers/"+c.id+"?force=1", nil, nil)
}
