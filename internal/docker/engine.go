// docker = wrapper autour du sdk docker (go) pour causer avec un engine
package docker

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"

	"github.com/mmarquet/native-api/internal/domain"
)

// mapping de port résolu (valeurs concrètes)
type PortBinding struct {
	Container int
	Host      int
	Protocol  string
}

// spec résolue d'un conteneur à créer
type ContainerSpec struct {
	Name        string
	Image       string
	Env         []string // "KEY=VALUE"
	Labels      map[string]string
	Ports       []PortBinding
	Binds       []string // "volume:target"
	NetworkName string   // réseau à rejoindre (vide = défaut)
	Aliases     []string // alias dns (ex. nom du service)
}

// client attaché à un engine donné
type Engine struct {
	cli *client.Client
}

// garde un client docker par serveur (cache)
type Manager struct {
	mu          sync.Mutex
	engines     map[string]*Engine
	defaultHost string
}

// new manager
func NewManager(defaultHost string) *Manager {
	return &Manager{engines: map[string]*Engine{}, defaultHost: defaultHost}
}

// get -> récup (ou crée) le client d'un serveur
func (m *Manager) Get(s domain.Server) (*Engine, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e, ok := m.engines[s.ID]; ok {
		return e, nil
	}
	host := s.Host
	if s.IsLocal && host == "" {
		host = m.defaultHost
	}
	e, err := newEngine(host)
	if err != nil {
		return nil, err
	}
	m.engines[s.ID] = e
	return e, nil
}

func newEngine(host string) (*Engine, error) {
	opts := []client.Opt{client.WithAPIVersionNegotiation()}
	if host != "" {
		opts = append(opts, client.WithHost(host))
	} else {
		opts = append(opts, client.FromEnv)
	}
	cli, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, err
	}
	return &Engine{cli: cli}, nil
}

// ping l'engine
func (e *Engine) Ping(ctx context.Context) error {
	_, err := e.cli.Ping(ctx)
	return err
}

// pull une image depuis une registry
func (e *Engine) PullImage(ctx context.Context, ref string) error {
	r, err := e.cli.ImagePull(ctx, ref, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("pull %s: %w", ref, err)
	}
	defer r.Close()
	_, err = io.Copy(io.Discard, r)
	return err
}

// build une image depuis un contexte local + tag
func (e *Engine) BuildImage(ctx context.Context, contextPath, dockerfile, tag string) error {
	if dockerfile == "" {
		dockerfile = "Dockerfile"
	}
	tarball, err := tarDir(contextPath)
	if err != nil {
		return fmt.Errorf("contexte de build: %w", err)
	}
	resp, err := e.cli.ImageBuild(ctx, tarball, types.ImageBuildOptions{
		Tags:       []string{tag},
		Dockerfile: dockerfile,
		Remove:     true,
	})
	if err != nil {
		return fmt.Errorf("build %s: %w", tag, err)
	}
	defer resp.Body.Close()
	_, err = io.Copy(io.Discard, resp.Body)
	return err
}

// crée le volume nommé (idempotent) + labels
func (e *Engine) EnsureVolume(ctx context.Context, name string, labels map[string]string) error {
	_, err := e.cli.VolumeCreate(ctx, volume.CreateOptions{Name: name, Labels: labels})
	return err
}

// crée le réseau bridge (idempotent) + labels
func (e *Engine) EnsureNetwork(ctx context.Context, name string, labels map[string]string) error {
	f := filters.NewArgs(filters.Arg("name", name))
	nets, err := e.cli.NetworkList(ctx, network.ListOptions{Filters: f})
	if err != nil {
		return err
	}
	for _, n := range nets {
		if n.Name == name {
			return nil // déjà là
		}
	}
	_, err = e.cli.NetworkCreate(ctx, name, network.CreateOptions{Driver: "bridge", Labels: labels})
	return err
}

// vire les réseaux qui matchent les labels
func (e *Engine) RemoveNetworksByLabels(ctx context.Context, labels map[string]string) error {
	f := filters.NewArgs()
	for k, v := range labels {
		f.Add("label", k+"="+v)
	}
	nets, err := e.cli.NetworkList(ctx, network.ListOptions{Filters: f})
	if err != nil {
		return err
	}
	for _, n := range nets {
		_ = e.cli.NetworkRemove(ctx, n.ID)
	}
	return nil
}

// crée un conteneur depuis la spec, renvoie l'id docker
func (e *Engine) CreateContainer(ctx context.Context, spec ContainerSpec) (string, error) {
	exposed := nat.PortSet{}
	bindings := nat.PortMap{}
	for _, p := range spec.Ports {
		proto := p.Protocol
		if proto == "" {
			proto = "tcp"
		}
		port, err := nat.NewPort(proto, strconv.Itoa(p.Container))
		if err != nil {
			return "", err
		}
		exposed[port] = struct{}{}
		if p.Host > 0 {
			bindings[port] = []nat.PortBinding{{HostIP: "0.0.0.0", HostPort: strconv.Itoa(p.Host)}}
		}
	}
	var netCfg *network.NetworkingConfig
	if spec.NetworkName != "" {
		netCfg = &network.NetworkingConfig{
			EndpointsConfig: map[string]*network.EndpointSettings{
				spec.NetworkName: {Aliases: spec.Aliases},
			},
		}
	}
	resp, err := e.cli.ContainerCreate(ctx,
		&container.Config{
			Image:        spec.Image,
			Env:          spec.Env,
			Labels:       spec.Labels,
			ExposedPorts: exposed,
		},
		&container.HostConfig{
			PortBindings: bindings,
			Binds:        spec.Binds,
		}, netCfg, nil, spec.Name)
	if err != nil {
		return "", fmt.Errorf("création conteneur %s: %w", spec.Name, err)
	}
	return resp.ID, nil
}

// start
func (e *Engine) StartContainer(ctx context.Context, id string) error {
	return e.cli.ContainerStart(ctx, id, container.StartOptions{})
}

// stop
func (e *Engine) StopContainer(ctx context.Context, id string) error {
	return e.cli.ContainerStop(ctx, id, container.StopOptions{})
}

// remove (force)
func (e *Engine) RemoveContainer(ctx context.Context, id string) error {
	return e.cli.ContainerRemove(ctx, id, container.RemoveOptions{Force: true})
}

// liste les conteneurs qui matchent tous les labels
func (e *Engine) ListByLabels(ctx context.Context, labels map[string]string) ([]types.Container, error) {
	f := filters.NewArgs()
	for k, v := range labels {
		f.Add("label", k+"="+v)
	}
	return e.cli.ContainerList(ctx, container.ListOptions{All: true, Filters: f})
}

// dernières lignes de log d'un conteneur
func (e *Engine) Logs(ctx context.Context, id string, tail string) (string, error) {
	r, err := e.cli.ContainerLogs(ctx, id, container.LogsOptions{
		ShowStdout: true, ShowStderr: true, Tail: tail,
	})
	if err != nil {
		return "", err
	}
	defer r.Close()
	var out bytes.Buffer
	if _, err := stdcopy.StdCopy(&out, &out, r); err != nil {
		return out.String(), err
	}
	return out.String(), nil
}

// tar d'un répertoire = contexte de build
func tarDir(dir string) (io.Reader, error) {
	buf := new(bytes.Buffer)
	tw := tar.NewWriter(buf)
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(rel)
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(tw, f)
		return err
	})
	if err != nil {
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return buf, nil
}

// réduit l'état docker brut à un statut lisible
func NormalizeState(state string) string {
	switch strings.ToLower(state) {
	case "running":
		return "running"
	case "restarting", "created", "paused":
		return state
	default:
		return "stopped"
	}
}
