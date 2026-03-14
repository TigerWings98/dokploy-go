// Input: docker-compose.yml 文件内容, appName, suffix 配置
// Output: TransformComposeFile (添加前缀/后缀到 service name + 注入 dokploy-network)
// Role: Compose 文件转换器，为 service 名添加唯一前缀防止命名冲突，注入 overlay 网络
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package compose

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Spec represents a docker-compose file structure.
type Spec struct {
	Version  string                    `yaml:"version,omitempty"`
	Name     string                    `yaml:"name,omitempty"`
	Services map[string]map[string]any `yaml:"services,omitempty"`
	Networks map[string]any            `yaml:"networks,omitempty"`
	Volumes  map[string]any            `yaml:"volumes,omitempty"`
	Configs  map[string]any            `yaml:"configs,omitempty"`
	Secrets  map[string]any            `yaml:"secrets,omitempty"`
}

// GenerateSuffix generates a random 8-char hex suffix.
func GenerateSuffix() string {
	b := make([]byte, 4)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// AddSuffixToAll applies suffix to all properties in a compose file.
// Returns the modified YAML bytes.
func AddSuffixToAll(data []byte, suffix string) ([]byte, error) {
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse compose file: %w", err)
	}

	addSuffixToServices(raw, suffix)
	addSuffixToVolumes(raw, suffix)
	addSuffixToNetworks(raw, suffix)
	addSuffixToConfigs(raw, suffix)
	addSuffixToSecrets(raw, suffix)

	return yaml.Marshal(raw)
}

// --- Service name transformation ---

func addSuffixToServices(raw map[string]any, suffix string) {
	services, ok := raw["services"].(map[string]any)
	if !ok {
		return
	}

	// Build old->new name mapping
	nameMap := make(map[string]string)
	for name := range services {
		nameMap[name] = name + "-" + suffix
	}

	// Rename services and update internal references
	newServices := make(map[string]any)
	for oldName, svc := range services {
		svcMap, ok := svc.(map[string]any)
		if !ok {
			newServices[nameMap[oldName]] = svc
			continue
		}

		// container_name
		if cn, ok := svcMap["container_name"].(string); ok {
			svcMap["container_name"] = cn + "-" + suffix
		}

		// depends_on
		if deps, ok := svcMap["depends_on"]; ok {
			svcMap["depends_on"] = renameDepsRefs(deps, nameMap)
		}

		// links
		if links, ok := svcMap["links"].([]any); ok {
			for i, l := range links {
				if ls, ok := l.(string); ok {
					if newName, exists := nameMap[ls]; exists {
						links[i] = newName
					}
				}
			}
		}

		// volumes_from
		if vf, ok := svcMap["volumes_from"].([]any); ok {
			for i, v := range vf {
				if vs, ok := v.(string); ok {
					if newName, exists := nameMap[vs]; exists {
						vf[i] = newName
					}
				}
			}
		}

		newServices[nameMap[oldName]] = svcMap
	}

	raw["services"] = newServices
}

func renameDepsRefs(deps any, nameMap map[string]string) any {
	switch d := deps.(type) {
	case []any:
		for i, v := range d {
			if vs, ok := v.(string); ok {
				if newName, exists := nameMap[vs]; exists {
					d[i] = newName
				}
			}
		}
		return d
	case map[string]any:
		newDeps := make(map[string]any)
		for k, v := range d {
			newKey := k
			if nn, exists := nameMap[k]; exists {
				newKey = nn
			}
			newDeps[newKey] = v
		}
		return newDeps
	}
	return deps
}

// --- Volume transformation ---

func addSuffixToVolumes(raw map[string]any, suffix string) {
	// Root volumes
	volumes, ok := raw["volumes"].(map[string]any)
	if ok {
		newVolumes := make(map[string]any)
		for name, v := range volumes {
			newVolumes[name+"-"+suffix] = v
		}
		raw["volumes"] = newVolumes
	}

	// Service volume references
	services, ok := raw["services"].(map[string]any)
	if !ok {
		return
	}
	// Collect root volume names (original, before suffix)
	rootNames := make(map[string]bool)
	if volumes != nil {
		for name := range volumes {
			// volumes was already renamed, strip suffix to get originals
			orig := strings.TrimSuffix(name, "-"+suffix)
			rootNames[orig] = true
		}
	}

	for _, svc := range services {
		svcMap, ok := svc.(map[string]any)
		if !ok {
			continue
		}
		vols, ok := svcMap["volumes"].([]any)
		if !ok {
			continue
		}
		for i, v := range vols {
			switch vol := v.(type) {
			case string:
				vols[i] = suffixVolumeString(vol, suffix)
			case map[string]any:
				if source, ok := vol["source"].(string); ok {
					if vol["type"] == "volume" || isNamedVolume(source) {
						vol["source"] = source + "-" + suffix
					}
				}
			}
		}
	}
}

func suffixVolumeString(vol, suffix string) string {
	// Don't modify bind mounts, relative paths, or env vars
	if strings.HasPrefix(vol, "/") || strings.HasPrefix(vol, "./") ||
		strings.HasPrefix(vol, "../") || strings.HasPrefix(vol, "$") {
		return vol
	}

	// Named volume: "volname:/path" or "volname:/path:opts"
	parts := strings.SplitN(vol, ":", 2)
	if len(parts) < 2 {
		return vol
	}

	name := parts[0]
	rest := parts[1]

	// Handle nested paths: "volname/subdir:/path"
	slashIdx := strings.Index(name, "/")
	if slashIdx > 0 {
		baseName := name[:slashIdx]
		subPath := name[slashIdx:]
		return baseName + "-" + suffix + subPath + ":" + rest
	}

	return name + "-" + suffix + ":" + rest
}

func isNamedVolume(source string) bool {
	return !strings.HasPrefix(source, "/") &&
		!strings.HasPrefix(source, "./") &&
		!strings.HasPrefix(source, "../") &&
		!strings.HasPrefix(source, "$")
}

// --- Network transformation ---

func addSuffixToNetworks(raw map[string]any, suffix string) {
	// Root networks
	networks, ok := raw["networks"].(map[string]any)
	if ok {
		newNetworks := make(map[string]any)
		for name, v := range networks {
			if name == "dokploy-network" {
				newNetworks[name] = v // Reserved, don't rename
			} else {
				newNetworks[name+"-"+suffix] = v
			}
		}
		raw["networks"] = newNetworks
	}

	// Service network references
	services, ok := raw["services"].(map[string]any)
	if !ok {
		return
	}
	for _, svc := range services {
		svcMap, ok := svc.(map[string]any)
		if !ok {
			continue
		}
		nets, ok := svcMap["networks"]
		if !ok {
			continue
		}
		switch n := nets.(type) {
		case []any:
			for i, net := range n {
				if ns, ok := net.(string); ok && ns != "dokploy-network" {
					n[i] = ns + "-" + suffix
				}
			}
		case map[string]any:
			newNets := make(map[string]any)
			for name, v := range n {
				if name == "dokploy-network" {
					newNets[name] = v
				} else {
					newNets[name+"-"+suffix] = v
				}
			}
			svcMap["networks"] = newNets
		}
	}
}

// InjectDokployNetwork 注入 dokploy-network 到 compose 文件
// 与 TS 版 addDokployNetworkToRoot + addDokployNetworkToService 一致：
// 1. 将根 networks 中的 dokploy-network 设为 external: true
// 2. 为所有 service 添加 dokploy-network（如果尚未包含）
// 3. 同时保留 default 网络，确保 service 间通信不断
// isolatedDeployment=true 时不调用此函数（使用独立网络）
func InjectDokployNetwork(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty compose content")
	}
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse compose file: %w", err)
	}
	if raw == nil {
		return nil, fmt.Errorf("compose file is empty or invalid")
	}

	// 1. 根 networks：确保 dokploy-network 存在且为 external
	networks, ok := raw["networks"].(map[string]any)
	if !ok {
		networks = make(map[string]any)
	}
	networks["dokploy-network"] = map[string]any{
		"external": true,
	}
	raw["networks"] = networks

	// 2. 每个 service：确保包含 dokploy-network 和 default
	// 跳过设置了 network_mode 的 service（network_mode 与 networks 在 Docker Compose 中互斥）
	services, ok := raw["services"].(map[string]any)
	if ok {
		for _, svc := range services {
			svcMap, ok := svc.(map[string]any)
			if !ok {
				continue
			}
			if _, hasNetworkMode := svcMap["network_mode"]; hasNetworkMode {
				continue
			}
			svcMap["networks"] = addDokployNetworkToService(svcMap["networks"])
		}
	}

	return yaml.Marshal(raw)
}

// addDokployNetworkToService 为 service 的 networks 添加 dokploy-network 和 default
// 与 TS 版 addDokployNetworkToService 完全一致
func addDokployNetworkToService(nets any) any {
	const network = "dokploy-network"
	const defaultNet = "default"

	if nets == nil {
		return []any{network, defaultNet}
	}

	switch n := nets.(type) {
	case []any:
		hasDokploy := false
		hasDefault := false
		for _, v := range n {
			if s, ok := v.(string); ok {
				if s == network {
					hasDokploy = true
				}
				if s == defaultNet {
					hasDefault = true
				}
			}
		}
		if !hasDokploy {
			n = append(n, network)
		}
		if !hasDefault {
			n = append(n, defaultNet)
		}
		return n
	case map[string]any:
		if _, ok := n[network]; !ok {
			n[network] = map[string]any{}
		}
		if _, ok := n[defaultNet]; !ok {
			n[defaultNet] = map[string]any{}
		}
		return n
	}

	return []any{network, defaultNet}
}

// InjectIsolatedNetwork 为隔离部署注入独立网络（以 appName 命名）
// 与 TS 版 addAppNameToPreventCollision 一致：
// 1. addAppNameToRootNetwork + addAppNameToServiceNetworks：注入 appName 网络
// 2. 如果 isolateVolumes=true：addSuffixToAllVolumes，给 volume 加 appName 后缀防冲突
func InjectIsolatedNetwork(data []byte, appName string, isolateVolumes ...bool) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty compose content")
	}
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse compose file: %w", err)
	}
	if raw == nil {
		return nil, fmt.Errorf("compose file is empty or invalid")
	}

	// 根 networks：添加 appName 网络为 external
	networks, ok := raw["networks"].(map[string]any)
	if !ok {
		networks = make(map[string]any)
	}
	networks[appName] = map[string]any{
		"name":     appName,
		"external": true,
	}
	raw["networks"] = networks

	// 每个 service：添加 appName 网络
	// 跳过设置了 network_mode 的 service（network_mode 与 networks 在 Docker Compose 中互斥）
	services, ok := raw["services"].(map[string]any)
	if ok {
		for _, svc := range services {
			svcMap, ok := svc.(map[string]any)
			if !ok {
				continue
			}
			if _, hasNetworkMode := svcMap["network_mode"]; hasNetworkMode {
				continue
			}
			svcNets := svcMap["networks"]
			if svcNets == nil {
				svcMap["networks"] = []any{appName}
				continue
			}
			switch n := svcNets.(type) {
			case []any:
				found := false
				for _, v := range n {
					if s, ok := v.(string); ok && s == appName {
						found = true
						break
					}
				}
				if !found {
					svcMap["networks"] = append(n, appName)
				}
			case map[string]any:
				if _, ok := n[appName]; !ok {
					n[appName] = map[string]any{}
				}
			}
		}
	}

	// Volume 隔离：给所有 volume 加 appName 后缀，防止不同 compose 项目间 volume 冲突
	// 与 TS 版 addSuffixToAllVolumes 一致（仅当 isolatedDeploymentsVolume=true 时生效）
	if len(isolateVolumes) > 0 && isolateVolumes[0] {
		addSuffixToVolumes(raw, appName)
	}

	return yaml.Marshal(raw)
}

// --- Config transformation ---

func addSuffixToConfigs(raw map[string]any, suffix string) {
	// Root configs
	configs, ok := raw["configs"].(map[string]any)
	if ok {
		newConfigs := make(map[string]any)
		for name, v := range configs {
			newConfigs[name+"-"+suffix] = v
		}
		raw["configs"] = newConfigs
	}

	// Service config references
	services, ok := raw["services"].(map[string]any)
	if !ok {
		return
	}
	for _, svc := range services {
		svcMap, ok := svc.(map[string]any)
		if !ok {
			continue
		}
		cfgs, ok := svcMap["configs"].([]any)
		if !ok {
			continue
		}
		for i, c := range cfgs {
			switch cfg := c.(type) {
			case string:
				cfgs[i] = cfg + "-" + suffix
			case map[string]any:
				if source, ok := cfg["source"].(string); ok {
					cfg["source"] = source + "-" + suffix
				}
			}
		}
	}
}

// --- Secret transformation ---

func addSuffixToSecrets(raw map[string]any, suffix string) {
	// Root secrets
	secrets, ok := raw["secrets"].(map[string]any)
	if ok {
		newSecrets := make(map[string]any)
		for name, v := range secrets {
			newSecrets[name+"-"+suffix] = v
		}
		raw["secrets"] = newSecrets
	}

	// Service secret references
	services, ok := raw["services"].(map[string]any)
	if !ok {
		return
	}
	for _, svc := range services {
		svcMap, ok := svc.(map[string]any)
		if !ok {
			continue
		}
		secs, ok := svcMap["secrets"].([]any)
		if !ok {
			continue
		}
		for i, s := range secs {
			switch sec := s.(type) {
			case string:
				secs[i] = sec + "-" + suffix
			case map[string]any:
				if source, ok := sec["source"].(string); ok {
					sec["source"] = source + "-" + suffix
				}
			}
		}
	}
}
