// Package docker runs machines as local Docker containers. It is the
// development provider: the same workspace image, without needing a cloud
// account.
package docker

import (
	"context"
	"fmt"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"

	"github.com/HasanMal1k/coder/internal/sandbox"
)

const (
	// homeDir is where the persistent volume is mounted inside the machine.
	homeDir = "/home/coder"

	labelInstanceID = "dev.coder.instance-id"
	labelOwnerID    = "dev.coder.owner-id"

	// stopTimeoutSeconds is how long a machine gets to shut down cleanly
	// before Docker kills it.
	stopTimeoutSeconds = 10
)

type Provisioner struct {
	cli *client.Client
}

// New connects to the Docker daemon configured in the environment
// (DOCKER_HOST, or the default local socket) and checks it is reachable.
func New(ctx context.Context) (*Provisioner, error) {
	cli, err := client.New(client.FromEnv)
	if err != nil {
		return nil, fmt.Errorf("create docker client: %w", err)
	}

	if _, err := cli.Ping(ctx, client.PingOptions{}); err != nil {
		cli.Close()
		return nil, fmt.Errorf("ping docker: %w", err)
	}

	return &Provisioner{
		cli: cli,
	}, nil
}

func (p *Provisioner) Close() error {
	return p.cli.Close()
}

func (p *Provisioner) Create(ctx context.Context, spec sandbox.Spec) (sandbox.Handle, error) {
	name := resourceName(spec.InstanceID)
	labels := map[string]string{
		labelInstanceID: spec.InstanceID,
		labelOwnerID:    spec.OwnerID,
	}

	vol, err := p.cli.VolumeCreate(ctx, client.VolumeCreateOptions{
		Name:   name,
		Labels: labels,
	})
	if err != nil {
		return sandbox.Handle{}, fmt.Errorf("create volume: %w", err)
	}

	netName := networkName(spec.OwnerID)
	if err := p.ensureNetwork(ctx, netName, spec.OwnerID); err != nil {
		p.cli.VolumeRemove(ctx, vol.Volume.Name, client.VolumeRemoveOptions{})
		return sandbox.Handle{}, err
	}

	env := make([]string, 0, len(spec.Env))
	for k, v := range spec.Env {
		env = append(env, k+"="+v)
	}

	init := true
	created, err := p.cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Name: name,
		Config: &container.Config{
			Image:    spec.Image,
			Hostname: spec.Hostname,
			Env:      env,
			Labels:   labels,
		},
		HostConfig: &container.HostConfig{
			Resources: container.Resources{
				NanoCPUs: int64(spec.Resources.CPUs) * 1_000_000_000,
				Memory:   int64(spec.Resources.MemoryMB) * 1024 * 1024,
			},
			Mounts: []mount.Mount{
				{
					Type:   mount.TypeVolume,
					Source: vol.Volume.Name,
					Target: homeDir,
				},
			},
			// Run an init process as PID 1 so orphaned processes left behind
			// by user shells are reaped.
			Init: &init,
			// Lets the guest daemon reach a control plane running on the
			// host, as it does in development.
			ExtraHosts: []string{"host.docker.internal:host-gateway"},
			// A machine that was running comes back if its main process dies
			// or the Docker daemon restarts, so it matches what Postgres says.
			// Docker never restarts a machine we stopped ourselves.
			RestartPolicy: container.RestartPolicy{Name: container.RestartPolicyUnlessStopped},
			NetworkMode:   container.NetworkMode(netName),
		},
		// On its owner's network a machine answers to its name, so one of
		// their machines can reach another as, say, http://api:8000.
		NetworkingConfig: &network.NetworkingConfig{
			EndpointsConfig: map[string]*network.EndpointSettings{
				netName: {Aliases: []string{spec.Hostname}},
			},
		},
	})
	if err != nil {
		// Don't leave a volume behind for a machine that was never created.
		// The create error is the one worth reporting.
		p.cli.VolumeRemove(ctx, vol.Volume.Name, client.VolumeRemoveOptions{})
		// The one thing Docker reports as missing when creating a container
		// is the image it was to be made from.
		if cerrdefs.IsNotFound(err) {
			return sandbox.Handle{}, fmt.Errorf("%w: %s", sandbox.ErrImageMissing, spec.Image)
		}
		return sandbox.Handle{}, fmt.Errorf("create container: %w", err)
	}

	return sandbox.Handle{
		MachineID: created.ID,
		VolumeID:  vol.Volume.Name,
	}, nil
}

func (p *Provisioner) Start(ctx context.Context, h sandbox.Handle) error {
	if err := p.joinOwnerNetwork(ctx, h); err != nil {
		return err
	}

	// Docker treats starting a running container as a no-op.
	if _, err := p.cli.ContainerStart(ctx, h.MachineID, client.ContainerStartOptions{}); err != nil {
		if cerrdefs.IsNotFound(err) {
			return sandbox.ErrNotFound
		}
		return fmt.Errorf("start container: %w", err)
	}

	return nil
}

func (p *Provisioner) Stop(ctx context.Context, h sandbox.Handle) error {
	timeout := stopTimeoutSeconds

	// Docker treats stopping a stopped container as a no-op.
	if _, err := p.cli.ContainerStop(ctx, h.MachineID, client.ContainerStopOptions{Timeout: &timeout}); err != nil {
		if cerrdefs.IsNotFound(err) {
			return sandbox.ErrNotFound
		}
		return fmt.Errorf("stop container: %w", err)
	}

	return nil
}

func (p *Provisioner) Delete(ctx context.Context, h sandbox.Handle) error {
	var owner string

	if h.MachineID != "" {
		if res, err := p.cli.ContainerInspect(ctx, h.MachineID, client.ContainerInspectOptions{}); err == nil && res.Container.Config != nil {
			owner = res.Container.Config.Labels[labelOwnerID]
		}

		_, err := p.cli.ContainerRemove(ctx, h.MachineID, client.ContainerRemoveOptions{Force: true})
		if err != nil && !cerrdefs.IsNotFound(err) {
			return fmt.Errorf("remove container: %w", err)
		}
	}

	if h.VolumeID != "" {
		_, err := p.cli.VolumeRemove(ctx, h.VolumeID, client.VolumeRemoveOptions{})
		if err != nil && !cerrdefs.IsNotFound(err) {
			return fmt.Errorf("remove volume: %w", err)
		}
	}

	// If that was its owner's last machine, their network goes too. Docker
	// refuses to remove a network that still has machines on it, which is
	// exactly the check wanted, so the error is not one.
	if owner != "" {
		p.cli.NetworkRemove(ctx, networkName(owner), client.NetworkRemoveOptions{})
	}

	return nil
}

// ensureNetwork creates the owner's private network unless it exists.
func (p *Provisioner) ensureNetwork(ctx context.Context, name, ownerID string) error {
	_, err := p.cli.NetworkCreate(ctx, name, client.NetworkCreateOptions{
		Driver: "bridge",
		Labels: map[string]string{labelOwnerID: ownerID},
	})
	// Two machines created at once both try; the loser finds it there.
	if err != nil && !cerrdefs.IsConflict(err) {
		return fmt.Errorf("create network: %w", err)
	}

	return nil
}

// joinOwnerNetwork puts a machine on its owner's network if it isn't on it:
// one created before machines had a network, or whose network was removed.
func (p *Provisioner) joinOwnerNetwork(ctx context.Context, h sandbox.Handle) error {
	res, err := p.cli.ContainerInspect(ctx, h.MachineID, client.ContainerInspectOptions{})
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return sandbox.ErrNotFound
		}
		return fmt.Errorf("inspect container: %w", err)
	}

	info := res.Container
	if info.Config == nil {
		return nil
	}
	owner := info.Config.Labels[labelOwnerID]
	if owner == "" {
		return nil
	}

	netName := networkName(owner)
	if info.NetworkSettings != nil {
		if _, joined := info.NetworkSettings.Networks[netName]; joined {
			return nil
		}
	}

	if err := p.ensureNetwork(ctx, netName, owner); err != nil {
		return err
	}

	_, err = p.cli.NetworkConnect(ctx, netName, client.NetworkConnectOptions{
		Container:      h.MachineID,
		EndpointConfig: &network.EndpointSettings{Aliases: []string{info.Config.Hostname}},
	})
	if err != nil {
		return fmt.Errorf("connect to network: %w", err)
	}

	return nil
}

func (p *Provisioner) State(ctx context.Context, h sandbox.Handle) (sandbox.State, error) {
	res, err := p.cli.ContainerInspect(ctx, h.MachineID, client.ContainerInspectOptions{})
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return "", sandbox.ErrNotFound
		}
		return "", fmt.Errorf("inspect container: %w", err)
	}

	if res.Container.State == nil {
		return "", fmt.Errorf("inspect container: no state reported")
	}

	switch res.Container.State.Status {
	case container.StateCreated:
		return sandbox.StateCreated, nil
	case container.StateRunning, container.StatePaused, container.StateRestarting:
		return sandbox.StateRunning, nil
	default:
		return sandbox.StateStopped, nil
	}
}

// networkName is the name of the private network shared by one owner's
// machines.
func networkName(ownerID string) string {
	return "coder-user-" + ownerID
}

// resourceName is the name given to both the container and its volume.
func resourceName(instanceID string) string {
	return "coder-" + instanceID
}
