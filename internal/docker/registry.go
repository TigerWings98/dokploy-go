package docker

import (
	"fmt"
	"strings"

	"github.com/dokploy/dokploy/internal/db/schema"
)

// RegistryTag builds the full registry tag for an image.
func RegistryTag(registry *schema.Registry, imageName string) string {
	repoName := extractRepositoryName(imageName)
	prefix := registry.Username
	if registry.ImagePrefix != nil && *registry.ImagePrefix != "" {
		prefix = *registry.ImagePrefix
	}

	if registry.RegistryURL != "" {
		return fmt.Sprintf("%s/%s/%s", registry.RegistryURL, prefix, repoName)
	}
	return fmt.Sprintf("%s/%s", prefix, repoName)
}

// RegistryPushCommands generates shell commands for logging into a registry,
// tagging an image, and pushing it. Used for multi-server deployments.
func RegistryPushCommands(registry *schema.Registry, imageName string) string {
	tag := RegistryTag(registry, imageName)
	if tag == "" {
		return ""
	}

	return fmt.Sprintf(`echo "📦 [Enabled Registry] Uploading image to '%s' | '%s'"
echo "%s" | docker login %s -u '%s' --password-stdin || {
	echo "❌ Registry Login Failed"
	exit 1
}
echo "✅ Registry Login Success"
docker tag %s %s || {
	echo "❌ Error tagging image"
	exit 1
}
echo "✅ Image Tagged"
docker push %s || {
	echo "❌ Error pushing image"
	exit 1
}
echo "✅ Image Pushed"`,
		registry.RegistryType, tag,
		registry.Password, registry.RegistryURL, registry.Username,
		imageName, tag,
		tag,
	)
}

// BuildRegistryUploadScript generates the full upload script for an application
// that has registry, build registry, or rollback registry configured.
func BuildRegistryUploadScript(app *schema.Application) string {
	if app == nil {
		return ""
	}

	imageName := app.AppName + ":latest"
	if app.SourceType == schema.SourceTypeDocker && app.DockerImage != nil {
		imageName = *app.DockerImage
	}

	var commands []string

	if app.Registry != nil {
		cmd := RegistryPushCommands(app.Registry, imageName)
		if cmd != "" {
			commands = append(commands, `echo "📦 [Enabled Registry Swarm]"`, cmd)
		}
	}

	if app.BuildRegistry != nil {
		cmd := RegistryPushCommands(app.BuildRegistry, imageName)
		if cmd != "" {
			commands = append(commands,
				`echo "🔑 [Enabled Build Registry]"`,
				cmd,
				`echo "⚠️ INFO: After the build is finished, wait a few seconds for the server to download the image."`,
			)
		}
	}

	return strings.Join(commands, "\n")
}

// extractRepositoryName extracts the last path segment from an image name.
// "nginx" -> "nginx", "myuser/myrepo:tag" -> "myrepo:tag", "docker.io/myuser/myrepo" -> "myrepo"
func extractRepositoryName(imageName string) string {
	idx := strings.LastIndex(imageName, "/")
	if idx == -1 {
		return imageName
	}
	return imageName[idx+1:]
}
