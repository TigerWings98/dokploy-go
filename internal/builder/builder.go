// Input: BuildType 枚举, 应用配置 (Dockerfile/buildArgs/publishDirectory 等)
// Output: GenerateBuildCommand - 6 种构建类型的 Docker/Nixpacks/Buildpacks 命令生成
// Role: 构建命令生成器，根据 BuildType 生成 nixpacks/dockerfile/heroku/paketo/railpack/static 构建命令
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package builder

import (
	"fmt"
	"strings"
)

// BuildOptions contains options for building an application.
type BuildOptions struct {
	AppName        string
	BuildType      string
	BuildPath      string
	Dockerfile     string
	DockerContext   string
	DockerBuildStage string
	BuildArgs      map[string]string
	BuildSecrets   map[string]string
	HerokuVersion  string
	RailpackVersion string
	PublishDir     string
	IsStaticSpa    bool
	CleanCache     bool
}

// GenerateBuildCommand generates the appropriate build command based on build type.
func GenerateBuildCommand(opts BuildOptions) (string, error) {
	switch opts.BuildType {
	case "nixpacks":
		return generateNixpacksCommand(opts), nil
	case "dockerfile":
		return generateDockerfileCommand(opts), nil
	case "heroku_buildpacks":
		return generateHerokuCommand(opts), nil
	case "paketo_buildpacks":
		return generatePaketoCommand(opts), nil
	case "railpack":
		return generateRailpackCommand(opts), nil
	case "static":
		return generateStaticCommand(opts), nil
	default:
		return "", fmt.Errorf("unsupported build type: %s", opts.BuildType)
	}
}

func generateNixpacksCommand(opts BuildOptions) string {
	args := []string{"nixpacks", "build", opts.BuildPath}
	args = append(args, "--name", opts.AppName)

	for k, v := range opts.BuildArgs {
		args = append(args, "--env", fmt.Sprintf("%s=%s", k, v))
	}

	return strings.Join(args, " ")
}

func generateDockerfileCommand(opts BuildOptions) string {
	dockerfile := opts.Dockerfile
	if dockerfile == "" {
		dockerfile = "Dockerfile"
	}

	args := []string{"docker", "build"}
	args = append(args, "-t", opts.AppName)
	args = append(args, "-f", fmt.Sprintf("%s/%s", opts.BuildPath, dockerfile))

	if opts.DockerBuildStage != "" {
		args = append(args, "--target", opts.DockerBuildStage)
	}

	for k, v := range opts.BuildArgs {
		args = append(args, "--build-arg", fmt.Sprintf("%s=%s", k, v))
	}

	for k, v := range opts.BuildSecrets {
		args = append(args, "--secret", fmt.Sprintf("id=%s,src=%s", k, v))
	}

	if opts.CleanCache {
		args = append(args, "--no-cache")
	}

	context := opts.BuildPath
	if opts.DockerContext != "" {
		context = opts.DockerContext
	}
	args = append(args, context)

	return strings.Join(args, " ")
}

func generateHerokuCommand(opts BuildOptions) string {
	version := opts.HerokuVersion
	if version == "" {
		version = "24"
	}

	args := []string{"pack", "build", opts.AppName}
	args = append(args, "--builder", fmt.Sprintf("heroku/builder:%s", version))
	args = append(args, "--path", opts.BuildPath)

	for k, v := range opts.BuildArgs {
		args = append(args, "--env", fmt.Sprintf("%s=%s", k, v))
	}

	if opts.CleanCache {
		args = append(args, "--clear-cache")
	}

	return strings.Join(args, " ")
}

func generatePaketoCommand(opts BuildOptions) string {
	args := []string{"pack", "build", opts.AppName}
	args = append(args, "--builder", "paketobuildpacks/builder-jammy-full")
	args = append(args, "--path", opts.BuildPath)

	for k, v := range opts.BuildArgs {
		args = append(args, "--env", fmt.Sprintf("%s=%s", k, v))
	}

	if opts.CleanCache {
		args = append(args, "--clear-cache")
	}

	return strings.Join(args, " ")
}

func generateRailpackCommand(opts BuildOptions) string {
	// Railpack uses docker buildx
	args := []string{"docker", "buildx", "build"}
	args = append(args, "-t", opts.AppName)
	args = append(args, "--load")
	args = append(args, opts.BuildPath)

	return strings.Join(args, " ")
}

func generateStaticCommand(opts BuildOptions) string {
	// Static builds use a simple nginx-based Dockerfile
	args := []string{"docker", "build"}
	args = append(args, "-t", opts.AppName)

	if opts.CleanCache {
		args = append(args, "--no-cache")
	}

	args = append(args, opts.BuildPath)

	return strings.Join(args, " ")
}
