package docker

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/docker/docker/api/types"
	containertypes "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
)

// Client wraps the Docker SDK client.
type Client struct {
	cli *client.Client
}

// NewClient creates a new Docker client.
func NewClient(opts ...ClientOption) (*Client, error) {
	o := &clientOptions{}
	for _, opt := range opts {
		opt(o)
	}

	var clientOpts []client.Opt
	clientOpts = append(clientOpts, client.FromEnv)

	if o.apiVersion != "" {
		clientOpts = append(clientOpts, client.WithVersion(o.apiVersion))
	}
	if o.host != "" {
		clientOpts = append(clientOpts, client.WithHost(o.host))
	}

	cli, err := client.NewClientWithOpts(clientOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %w", err)
	}

	return &Client{cli: cli}, nil
}

type clientOptions struct {
	apiVersion string
	host       string
}

// ClientOption configures the Docker client.
type ClientOption func(*clientOptions)

// WithAPIVersion sets the Docker API version.
func WithAPIVersion(v string) ClientOption {
	return func(o *clientOptions) { o.apiVersion = v }
}

// WithHost sets the Docker host.
func WithHost(h string) ClientOption {
	return func(o *clientOptions) { o.host = h }
}

// Close closes the Docker client.
func (c *Client) Close() error {
	return c.cli.Close()
}

// ListContainers returns all containers.
func (c *Client) ListContainers(ctx context.Context) ([]types.Container, error) {
	return c.cli.ContainerList(ctx, containertypes.ListOptions{All: true})
}

// ContainerLogs returns the logs of a container.
func (c *Client) ContainerLogs(ctx context.Context, containerID string, tail string) (io.ReadCloser, error) {
	return c.cli.ContainerLogs(ctx, containerID, containertypes.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Tail:       tail,
	})
}

// ListServices returns all Docker Swarm services.
func (c *Client) ListServices(ctx context.Context) ([]swarm.Service, error) {
	return c.cli.ServiceList(ctx, types.ServiceListOptions{})
}

// GetService returns a single Docker Swarm service.
func (c *Client) GetService(ctx context.Context, serviceID string) (swarm.Service, error) {
	svc, _, err := c.cli.ServiceInspectWithRaw(ctx, serviceID, types.ServiceInspectOptions{})
	return svc, err
}

// RemoveService removes a Docker Swarm service by name.
func (c *Client) RemoveService(ctx context.Context, serviceName string) error {
	return c.cli.ServiceRemove(ctx, serviceName)
}

// ScaleService scales a Docker Swarm service.
func (c *Client) ScaleService(ctx context.Context, serviceName string, replicas uint64) error {
	svc, _, err := c.cli.ServiceInspectWithRaw(ctx, serviceName, types.ServiceInspectOptions{})
	if err != nil {
		return err
	}

	if svc.Spec.Mode.Replicated == nil {
		return fmt.Errorf("service %s is not in replicated mode", serviceName)
	}

	svc.Spec.Mode.Replicated.Replicas = &replicas
	_, err = c.cli.ServiceUpdate(ctx, svc.ID, svc.Version, svc.Spec, types.ServiceUpdateOptions{})
	return err
}

// PruneSystem performs a Docker system prune.
func (c *Client) PruneSystem(ctx context.Context) error {
	_, err := c.cli.ContainersPrune(ctx, filters.Args{})
	if err != nil {
		return err
	}
	_, err = c.cli.ImagesPrune(ctx, filters.NewArgs(filters.Arg("dangling", "true")))
	if err != nil {
		return err
	}
	_, err = c.cli.BuildCachePrune(ctx, types.BuildCachePruneOptions{All: true})
	return err
}

// GetServiceLogs returns the logs of a Docker Swarm service.
func (c *Client) GetServiceLogs(ctx context.Context, serviceID string, tail string) (io.ReadCloser, error) {
	return c.cli.ServiceLogs(ctx, serviceID, containertypes.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Tail:       tail,
	})
}

// NetworkExists checks if a Docker network exists.
func (c *Client) NetworkExists(ctx context.Context, networkName string) (bool, error) {
	networks, err := c.cli.NetworkList(ctx, network.ListOptions{
		Filters: filters.NewArgs(filters.Arg("name", networkName)),
	})
	if err != nil {
		return false, err
	}
	for _, n := range networks {
		if n.Name == networkName {
			return true, nil
		}
	}
	return false, nil
}

// CreateNetwork creates a Docker network.
func (c *Client) CreateNetwork(ctx context.Context, name string, driver string) error {
	_, err := c.cli.NetworkCreate(ctx, name, network.CreateOptions{
		Driver:     driver,
		Attachable: true,
	})
	return err
}

// RemoveVolume removes a Docker volume.
func (c *Client) RemoveVolume(ctx context.Context, volumeName string, force bool) error {
	return c.cli.VolumeRemove(ctx, volumeName, force)
}

// GetContainerByName finds a container by name.
func (c *Client) GetContainerByName(ctx context.Context, name string) (*types.Container, error) {
	containers, err := c.cli.ContainerList(ctx, containertypes.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("name", name)),
	})
	if err != nil {
		return nil, err
	}
	for _, ctr := range containers {
		for _, n := range ctr.Names {
			if strings.TrimPrefix(n, "/") == name {
				return &ctr, nil
			}
		}
	}
	return nil, nil
}
