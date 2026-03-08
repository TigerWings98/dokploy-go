// Input: Docker SDK (Swarm API)
// Output: SwarmManager (GetSwarmInfo/GetJoinTokens/ListNodes/RemoveNode/LeaveSwarm/UpdateNodeAvailability)
// Role: Docker Swarm 集群管理器，提供节点管理和集群状态查询操作
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package setup

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/swarm"
	"github.com/dokploy/dokploy/internal/docker"
)

func swarmInitRequest() swarm.InitRequest {
	return swarm.InitRequest{
		ListenAddr:    "0.0.0.0:2377",
		AdvertiseAddr: "127.0.0.1",
	}
}

// SwarmManager provides swarm management operations.
type SwarmManager struct {
	docker *docker.Client
}

// NewSwarmManager creates a new SwarmManager.
func NewSwarmManager(dockerClient *docker.Client) *SwarmManager {
	return &SwarmManager{docker: dockerClient}
}

// GetSwarmInfo returns current swarm status and node info.
func (m *SwarmManager) GetSwarmInfo(ctx context.Context) (*SwarmInfo, error) {
	info, err := m.docker.DockerClient().Info(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get docker info: %w", err)
	}

	result := &SwarmInfo{
		NodeState: string(info.Swarm.LocalNodeState),
		NodeID:    info.Swarm.NodeID,
		Nodes:     info.Swarm.Nodes,
		Managers:  info.Swarm.Managers,
	}

	if info.Swarm.LocalNodeState == swarm.LocalNodeStateActive {
		result.Active = true
	}

	return result, nil
}

// SwarmInfo holds swarm status information.
type SwarmInfo struct {
	Active    bool   `json:"active"`
	NodeState string `json:"nodeState"`
	NodeID    string `json:"nodeId"`
	Nodes     int    `json:"nodes"`
	Managers  int    `json:"managers"`
}

// GetJoinTokens returns the worker and manager join tokens.
func (m *SwarmManager) GetJoinTokens(ctx context.Context) (*JoinTokens, error) {
	sw, err := m.docker.DockerClient().SwarmInspect(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect swarm: %w", err)
	}

	return &JoinTokens{
		Worker:  sw.JoinTokens.Worker,
		Manager: sw.JoinTokens.Manager,
	}, nil
}

// JoinTokens holds swarm join tokens.
type JoinTokens struct {
	Worker  string `json:"worker"`
	Manager string `json:"manager"`
}

// ListNodes returns all swarm nodes.
func (m *SwarmManager) ListNodes(ctx context.Context) ([]swarm.Node, error) {
	return m.docker.DockerClient().NodeList(ctx, types.NodeListOptions{})
}

// RemoveNode removes a node from the swarm.
func (m *SwarmManager) RemoveNode(ctx context.Context, nodeID string, force bool) error {
	return m.docker.DockerClient().NodeRemove(ctx, nodeID, types.NodeRemoveOptions{Force: force})
}

// LeaveSwarm leaves the current swarm.
func (m *SwarmManager) LeaveSwarm(ctx context.Context, force bool) error {
	return m.docker.DockerClient().SwarmLeave(ctx, force)
}

// UpdateNodeAvailability updates a node's availability (active, pause, drain).
func (m *SwarmManager) UpdateNodeAvailability(ctx context.Context, nodeID string, availability swarm.NodeAvailability) error {
	node, _, err := m.docker.DockerClient().NodeInspectWithRaw(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("failed to inspect node: %w", err)
	}

	node.Spec.Availability = availability
	return m.docker.DockerClient().NodeUpdate(ctx, nodeID, node.Version, node.Spec)
}
