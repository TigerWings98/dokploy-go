// Input: schema.Application, schema.Registry, schema.Mount, schema.Port
// Output: Docker CLI 命令字符串（docker service create/update 的完整参数）
// Role: Docker Swarm 服务配置生成器，将数据库中的 JSONB 配置转换为 Docker CLI flags，与 TS 版 mechanizeDockerContainer 完全对齐
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package service

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/dokploy/dokploy/internal/db/schema"
)

// getImageName 获取应用的完整镜像名称，包含 registry 前缀
// 与 TS 版 getImageName 完全一致
func getImageName(app *schema.Application) string {
	imageName := fmt.Sprintf("%s:latest", app.AppName)

	if app.SourceType == schema.SourceTypeDocker {
		if app.DockerImage != nil && *app.DockerImage != "" {
			return *app.DockerImage
		}
		return "ERROR-NO-IMAGE-PROVIDED"
	}

	if app.Registry != nil {
		return getRegistryTag(app.Registry, imageName)
	}
	if app.BuildRegistry != nil {
		return getRegistryTag(app.BuildRegistry, imageName)
	}

	return imageName
}

// getRegistryTag 构建 registry 标签
// 与 TS 版 getRegistryTag 完全一致
func getRegistryTag(registry *schema.Registry, imageName string) string {
	prefix := registry.Username
	if registry.ImagePrefix != nil && *registry.ImagePrefix != "" {
		prefix = *registry.ImagePrefix
	}

	repoName := extractRepositoryName(imageName)

	if registry.RegistryURL != "" {
		return fmt.Sprintf("%s/%s/%s", registry.RegistryURL, prefix, repoName)
	}
	return fmt.Sprintf("%s/%s", prefix, repoName)
}

func extractRepositoryName(imageName string) string {
	idx := strings.LastIndex(imageName, "/")
	if idx == -1 {
		return imageName
	}
	return imageName[idx+1:]
}

// getAuthConfig 获取 registry 认证信息
// 与 TS 版 getAuthConfig 完全一致：sourceType=docker 时用直接凭证，否则 registry > buildRegistry
func getAuthConfig(app *schema.Application) (username, password, serverAddr string, hasAuth bool) {
	if app.SourceType == schema.SourceTypeDocker {
		if app.Username != nil && app.Password != nil && *app.Username != "" && *app.Password != "" {
			return *app.Username, *app.Password, safeStr(app.RegistryURL), true
		}
	} else if app.Registry != nil {
		return app.Registry.Username, app.Registry.Password, app.Registry.RegistryURL, true
	} else if app.BuildRegistry != nil {
		return app.BuildRegistry.Username, app.BuildRegistry.Password, app.BuildRegistry.RegistryURL, true
	}
	return "", "", "", false
}

// buildRegistryLoginCmd 构建 docker login 命令
func buildRegistryLoginCmd(app *schema.Application) string {
	username, password, serverAddr, hasAuth := getAuthConfig(app)
	if !hasAuth {
		return ""
	}
	if serverAddr != "" {
		return fmt.Sprintf("docker login -u '%s' -p '%s' %s",
			shellEscapeSingleQuote(username), shellEscapeSingleQuote(password), serverAddr)
	}
	return fmt.Sprintf("docker login -u '%s' -p '%s'",
		shellEscapeSingleQuote(username), shellEscapeSingleQuote(password))
}

// prepareEnvironmentVariables 解析环境变量并替换模板变量
// 与 TS 版 prepareEnvironmentVariables 完全一致
// 支持 ${{project.KEY}}、${{environment.KEY}}、${{KEY}} 三种模板
func prepareEnvironmentVariables(serviceEnv, projectEnv, environmentEnv string) []string {
	projectVars := parseEnvString(projectEnv)
	envVars := parseEnvString(environmentEnv)
	serviceVars := parseEnvString(serviceEnv)

	projectRe := regexp.MustCompile(`\$\{\{project\.(.*?)\}\}`)
	envRe := regexp.MustCompile(`\$\{\{environment\.(.*?)\}\}`)
	selfRe := regexp.MustCompile(`\$\{\{(.*?)\}\}`)

	var result []string
	for k, v := range serviceVars {
		resolved := v

		// 替换 ${{project.KEY}}
		resolved = projectRe.ReplaceAllStringFunc(resolved, func(match string) string {
			sub := projectRe.FindStringSubmatch(match)
			if len(sub) > 1 {
				if val, ok := projectVars[sub[1]]; ok {
					return val
				}
			}
			return match
		})

		// 替换 ${{environment.KEY}}
		resolved = envRe.ReplaceAllStringFunc(resolved, func(match string) string {
			sub := envRe.FindStringSubmatch(match)
			if len(sub) > 1 {
				if val, ok := envVars[sub[1]]; ok {
					return val
				}
			}
			return match
		})

		// 替换 ${{KEY}} (服务自身变量引用)
		resolved = selfRe.ReplaceAllStringFunc(resolved, func(match string) string {
			sub := selfRe.FindStringSubmatch(match)
			if len(sub) > 1 {
				if val, ok := serviceVars[sub[1]]; ok {
					return val
				}
			}
			return match
		})

		result = append(result, fmt.Sprintf("%s=%s", k, resolved))
	}

	return result
}

// buildSwarmFlags 生成所有 Swarm 配置的 CLI flags
// isUpdate 控制集合类型 flag 的前缀（create 用 --env，update 用 --env-add）
// 与 TS 版 generateConfigContainer + mechanizeDockerContainer 的 CreateServiceOptions 完全对齐
func buildSwarmFlags(app *schema.Application, envVars []string, isUpdate bool) string {
	var b strings.Builder

	// flag 前缀：create 模式 vs update 模式
	envFlag := "--env"
	mountFlag := "--mount"
	constraintFlag := "--constraint"
	labelFlag := "--container-label"
	publishFlag := "--publish"
	networkFlag := "--network"
	if isUpdate {
		envFlag = "--env-add"
		mountFlag = "--mount-add"
		constraintFlag = "--constraint-add"
		labelFlag = "--container-label-add"
		publishFlag = "--publish-add"
		networkFlag = "--network-add"
	}

	// ===== 网络 =====
	if app.NetworkSwarm != nil && len(app.NetworkSwarm.Data) > 0 {
		for _, net := range app.NetworkSwarm.Data {
			if net.Target != nil && *net.Target != "" {
				fmt.Fprintf(&b, " %s %s", networkFlag, *net.Target)
			}
		}
	} else if !isUpdate {
		// create 模式默认加 dokploy-network
		b.WriteString(" --network dokploy-network")
	}

	// ===== 环境变量 =====
	for _, env := range envVars {
		fmt.Fprintf(&b, " %s '%s'", envFlag, shellEscapeSingleQuote(env))
	}

	// ===== 挂载（volume/bind/file 三种类型） =====
	applicationsPath := "/etc/dokploy/applications"
	for _, mount := range app.Mounts {
		switch mount.Type {
		case schema.MountTypeVolume:
			if mount.VolumeName != nil {
				fmt.Fprintf(&b, " %s type=volume,source=%s,target=%s", mountFlag, *mount.VolumeName, mount.MountPath)
			}
		case schema.MountTypeBind:
			if mount.HostPath != nil {
				fmt.Fprintf(&b, " %s type=bind,source=%s,target=%s", mountFlag, *mount.HostPath, mount.MountPath)
			}
		case schema.MountTypeFile:
			if mount.FilePath != nil {
				sourcePath := filepath.Join(applicationsPath, app.AppName, "files", *mount.FilePath)
				fmt.Fprintf(&b, " %s type=bind,source=%s,target=%s", mountFlag, sourcePath, mount.MountPath)
			}
		}
	}

	// ===== 资源限制与预留 =====
	if app.MemoryLimit != nil && *app.MemoryLimit != "" {
		fmt.Fprintf(&b, " --limit-memory %s", *app.MemoryLimit)
	}
	if app.CPULimit != nil && *app.CPULimit != "" {
		fmt.Fprintf(&b, " --limit-cpu %s", *app.CPULimit)
	}
	if app.MemoryReservation != nil && *app.MemoryReservation != "" {
		fmt.Fprintf(&b, " --reserve-memory %s", *app.MemoryReservation)
	}
	if app.CPUReservation != nil && *app.CPUReservation != "" {
		fmt.Fprintf(&b, " --reserve-cpu %s", *app.CPUReservation)
	}

	// ===== 服务模式（replicated / global） =====
	// update 模式不能改 mode，只能改 replicas
	if app.ModeSwarm != nil {
		mode := app.ModeSwarm.Data
		if mode.Global != nil && !isUpdate {
			b.WriteString(" --mode global")
		} else if mode.Replicated != nil && mode.Replicated.Replicas != nil {
			fmt.Fprintf(&b, " --replicas %d", *mode.Replicated.Replicas)
		} else if mode.Global == nil {
			fmt.Fprintf(&b, " --replicas %d", app.Replicas)
		}
	} else if !isUpdate {
		fmt.Fprintf(&b, " --replicas %d", app.Replicas)
	}

	// ===== 健康检查 =====
	if app.HealthCheckSwarm != nil {
		hc := app.HealthCheckSwarm.Data
		if len(hc.Test) > 0 {
			if hc.Test[0] == "NONE" {
				b.WriteString(" --no-healthcheck")
			} else {
				var healthCmd string
				if hc.Test[0] == "CMD-SHELL" && len(hc.Test) > 1 {
					healthCmd = hc.Test[1]
				} else if hc.Test[0] == "CMD" && len(hc.Test) > 1 {
					healthCmd = strings.Join(hc.Test[1:], " ")
				} else {
					healthCmd = strings.Join(hc.Test, " ")
				}
				fmt.Fprintf(&b, " --health-cmd '%s'", shellEscapeSingleQuote(healthCmd))
			}
		}
		if hc.Interval != nil {
			fmt.Fprintf(&b, " --health-interval %s", nsToDuration(*hc.Interval))
		}
		if hc.Timeout != nil {
			fmt.Fprintf(&b, " --health-timeout %s", nsToDuration(*hc.Timeout))
		}
		if hc.StartPeriod != nil {
			fmt.Fprintf(&b, " --health-start-period %s", nsToDuration(*hc.StartPeriod))
		}
		if hc.Retries != nil {
			fmt.Fprintf(&b, " --health-retries %d", *hc.Retries)
		}
	}

	// ===== 重启策略 =====
	if app.RestartPolicySwarm != nil {
		rp := app.RestartPolicySwarm.Data
		if rp.Condition != nil && *rp.Condition != "" {
			fmt.Fprintf(&b, " --restart-condition %s", *rp.Condition)
		}
		if rp.Delay != nil {
			fmt.Fprintf(&b, " --restart-delay %s", nsToDuration(*rp.Delay))
		}
		if rp.MaxAttempts != nil {
			fmt.Fprintf(&b, " --restart-max-attempts %d", *rp.MaxAttempts)
		}
		if rp.Window != nil {
			fmt.Fprintf(&b, " --restart-window %s", nsToDuration(*rp.Window))
		}
	}

	// ===== 放置约束 =====
	// 与 TS 版一致：有 placementSwarm 用自定义约束，否则有挂载时默认约束到 manager 节点
	if app.PlacementSwarm != nil {
		ps := app.PlacementSwarm.Data
		for _, c := range ps.Constraints {
			fmt.Fprintf(&b, " %s '%s'", constraintFlag, shellEscapeSingleQuote(c))
		}
		for _, pref := range ps.Preferences {
			if pref.Spread.SpreadDescriptor != "" {
				fmt.Fprintf(&b, " --placement-pref 'spread=%s'", pref.Spread.SpreadDescriptor)
			}
		}
	} else if len(app.Mounts) > 0 && !isUpdate {
		fmt.Fprintf(&b, " %s 'node.role==manager'", constraintFlag)
	}

	// ===== 更新配置 =====
	// 与 TS 版一致：有 updateConfigSwarm 用自定义配置，否则默认 parallelism=1, order=start-first
	if app.UpdateConfigSwarm != nil {
		uc := app.UpdateConfigSwarm.Data
		fmt.Fprintf(&b, " --update-parallelism %d", uc.Parallelism)
		if uc.Delay != nil {
			fmt.Fprintf(&b, " --update-delay %s", nsToDuration(*uc.Delay))
		}
		if uc.FailureAction != nil && *uc.FailureAction != "" {
			fmt.Fprintf(&b, " --update-failure-action %s", *uc.FailureAction)
		}
		if uc.Monitor != nil {
			fmt.Fprintf(&b, " --update-monitor %s", nsToDuration(*uc.Monitor))
		}
		if uc.MaxFailureRatio != nil {
			fmt.Fprintf(&b, " --update-max-failure-ratio %g", *uc.MaxFailureRatio)
		}
		if uc.Order != "" {
			fmt.Fprintf(&b, " --update-order %s", uc.Order)
		}
	} else {
		b.WriteString(" --update-parallelism 1 --update-order start-first")
	}

	// ===== 回滚配置 =====
	if app.RollbackConfigSwarm != nil {
		rc := app.RollbackConfigSwarm.Data
		fmt.Fprintf(&b, " --rollback-parallelism %d", rc.Parallelism)
		if rc.Delay != nil {
			fmt.Fprintf(&b, " --rollback-delay %s", nsToDuration(*rc.Delay))
		}
		if rc.FailureAction != nil && *rc.FailureAction != "" {
			fmt.Fprintf(&b, " --rollback-failure-action %s", *rc.FailureAction)
		}
		if rc.Monitor != nil {
			fmt.Fprintf(&b, " --rollback-monitor %s", nsToDuration(*rc.Monitor))
		}
		if rc.MaxFailureRatio != nil {
			fmt.Fprintf(&b, " --rollback-max-failure-ratio %g", *rc.MaxFailureRatio)
		}
		if rc.Order != "" {
			fmt.Fprintf(&b, " --rollback-order %s", rc.Order)
		}
	}

	// ===== 容器标签 =====
	if app.LabelsSwarm != nil {
		for k, v := range app.LabelsSwarm.Data {
			fmt.Fprintf(&b, " %s '%s=%s'", labelFlag, k, shellEscapeSingleQuote(v))
		}
	}

	// ===== 停止宽限期 =====
	if app.StopGracePeriodSwarm != nil && *app.StopGracePeriodSwarm > 0 {
		fmt.Fprintf(&b, " --stop-grace-period %s", nsToDuration(*app.StopGracePeriodSwarm))
	}

	// ===== Ulimits =====
	if app.UlimitsSwarm != nil {
		for _, ul := range app.UlimitsSwarm.Data {
			fmt.Fprintf(&b, " --ulimit %s=%d:%d", ul.Name, ul.Soft, ul.Hard)
		}
	}

	// ===== 端口发布 =====
	// 与 TS 版一致：优先使用 EndpointSpecSwarm，否则使用 Ports 关系
	if app.EndpointSpecSwarm != nil {
		es := app.EndpointSpecSwarm.Data
		if es.Mode != nil && *es.Mode != "" && !isUpdate {
			fmt.Fprintf(&b, " --endpoint-mode %s", *es.Mode)
		}
		for _, port := range es.Ports {
			publishMode := "host"
			if port.PublishMode != nil && *port.PublishMode != "" {
				publishMode = *port.PublishMode
			}
			protocol := "tcp"
			if port.Protocol != nil && *port.Protocol != "" {
				protocol = *port.Protocol
			}
			targetPort := 0
			if port.TargetPort != nil {
				targetPort = *port.TargetPort
			}
			publishedPort := 0
			if port.PublishedPort != nil {
				publishedPort = *port.PublishedPort
			}
			fmt.Fprintf(&b, " %s mode=%s,target=%d,published=%d,protocol=%s",
				publishFlag, publishMode, targetPort, publishedPort, protocol)
		}
	} else if len(app.Ports) > 0 {
		for _, port := range app.Ports {
			fmt.Fprintf(&b, " %s published=%d,target=%d,protocol=%s",
				publishFlag, port.PublishedPort, port.TargetPort, string(port.Protocol))
		}
	}

	// ===== Registry 认证 =====
	if _, _, _, hasAuth := getAuthConfig(app); hasAuth {
		b.WriteString(" --with-registry-auth")
	}

	return b.String()
}

// buildServiceCreateCmd 构建完整的 docker service create 命令
func buildServiceCreateCmd(app *schema.Application, imageName string, envVars []string) string {
	cmd := fmt.Sprintf("docker service create --name %s", app.AppName)
	cmd += buildSwarmFlags(app, envVars, false)

	// 命令覆盖（对应 TS 版 ContainerSpec.Command）
	if app.Command != nil && *app.Command != "" {
		cmd += fmt.Sprintf(" --entrypoint '%s'", shellEscapeSingleQuote(*app.Command))
	}

	// 镜像
	cmd += " " + imageName

	// Args（对应 TS 版 ContainerSpec.Args，作为 CMD 参数）
	if len(app.Args) > 0 {
		for _, arg := range app.Args {
			cmd += fmt.Sprintf(" '%s'", shellEscapeSingleQuote(arg))
		}
	}

	return cmd
}

// buildServiceUpdateCmd 构建 docker service update 命令
func buildServiceUpdateCmd(app *schema.Application, imageName string, envVars []string) string {
	cmd := fmt.Sprintf("docker service update --force --image %s", imageName)
	cmd += buildSwarmFlags(app, envVars, true)

	// 命令覆盖
	if app.Command != nil && *app.Command != "" {
		cmd += fmt.Sprintf(" --entrypoint '%s'", shellEscapeSingleQuote(*app.Command))
	}

	// 服务名称（update 的最后一个参数）
	cmd += " " + app.AppName

	return cmd
}

// buildFileMountsSetupCmd 构建文件挂载准备命令（在部署前将文件内容写入磁盘）
// 与 TS 版 generateFileMounts 配合，先将文件写入 {APPLICATIONS_PATH}/{appName}/files/ 目录
func buildFileMountsSetupCmd(appName string, mounts []schema.Mount) string {
	var cmds []string
	applicationsPath := "/etc/dokploy/applications"

	for _, mount := range mounts {
		if mount.Type == schema.MountTypeFile && mount.FilePath != nil && mount.Content != nil {
			dir := filepath.Join(applicationsPath, appName, "files")
			filePath := filepath.Join(dir, *mount.FilePath)
			// 使用 base64 编码安全传输文件内容
			cmds = append(cmds,
				fmt.Sprintf("mkdir -p '%s'", dir),
				fmt.Sprintf("echo '%s' | base64 -d > '%s'",
					base64Encode(*mount.Content), filePath),
			)
		}
	}

	if len(cmds) == 0 {
		return ""
	}
	return strings.Join(cmds, " && ")
}

// nsToDuration 将纳秒转换为 Docker CLI 兼容的时间字符串
func nsToDuration(ns int64) string {
	d := time.Duration(ns)
	return d.String()
}

// shellEscapeSingleQuote 转义单引号，用于 shell 命令中的值
func shellEscapeSingleQuote(s string) string {
	return strings.ReplaceAll(s, "'", "'\\''")
}
