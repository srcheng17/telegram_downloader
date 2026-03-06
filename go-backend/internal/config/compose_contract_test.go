package config

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

func TestComposeTopologyMatchesGoBackendOnly(t *testing.T) {
	composePath := findComposePath(t)
	content, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatalf("read compose file %q: %v", composePath, err)
	}

	services := extractComposeServices(t, string(content))
	composeText := string(content)
	expectedServices := map[string]struct{}{
		"go-api":    {},
		"go-worker": {},
		"postgres":  {},
		"redis":     {},
		"gateway":   {},
	}
	if !sameServiceSet(services, expectedServices) {
		t.Fatalf(
			"compose services mismatch: expected=%v actual=%v",
			sortedServiceNames(expectedServices),
			sortedServiceNames(services),
		)
	}

	goAPIBlock := extractComposeServiceBlock(t, composeText, "go-api")
	if !strings.Contains(goAPIBlock, "    healthcheck:\n") {
		t.Fatalf("go-api must define healthcheck in %q", composePath)
	}
	if !strings.Contains(goAPIBlock, "/healthz") {
		t.Fatalf("go-api must define a healthcheck probing /healthz in %q", composePath)
	}

	gatewayBlock := extractComposeServiceBlock(t, composeText, "gateway")
	gatewayDependsOnHealthyGoAPI := regexp.MustCompile(`(?ms)go-api:\n\s+condition:\s*service_healthy`)
	if !gatewayDependsOnHealthyGoAPI.MatchString(gatewayBlock) {
		t.Fatalf("gateway must depend on go-api with service_healthy condition in %q", composePath)
	}

	nginxPath := findNginxConfigPath(t)
	nginxContent, err := os.ReadFile(nginxPath)
	if err != nil {
		t.Fatalf("read nginx config %q: %v", nginxPath, err)
	}
	nginxText := string(nginxContent)
	if strings.Contains(nginxText, "telegraph_python_web") {
		t.Fatalf("nginx config must not reference python frontend upstream: %q", nginxPath)
	}
	locationRootToGo := regexp.MustCompile(`location\s*/\s*\{\s*proxy_pass\s+http://telegraph_go_api;`)
	if !locationRootToGo.MatchString(nginxText) {
		t.Fatalf("nginx config must route location / to telegraph_go_api: %q", nginxPath)
	}
	locationDownloadToGo := regexp.MustCompile(`location\s*=\s*/download\s*\{\s*proxy_pass\s+http://telegraph_go_api;`)
	if !locationDownloadToGo.MatchString(nginxText) {
		t.Fatalf("nginx config must route /download to telegraph_go_api: %q", nginxPath)
	}
	locationAPIToGo := regexp.MustCompile(`location\s+\^~\s*/api/\s*\{\s*proxy_pass\s+http://telegraph_go_api;`)
	if !locationAPIToGo.MatchString(nginxText) {
		t.Fatalf("nginx config must route /api/* to telegraph_go_api: %q", nginxPath)
	}
}

func findComposePath(t *testing.T) string {
	t.Helper()

	candidates := []string{
		filepath.Join("..", "..", "..", "docker-compose.yml"),
		filepath.Join("..", "docker-compose.yml"),
		"docker-compose.yml",
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	t.Fatalf("docker-compose.yml not found in candidates: %v", candidates)
	return ""
}

func findNginxConfigPath(t *testing.T) string {
	t.Helper()

	candidates := []string{
		filepath.Join("..", "deploy", "nginx", "canary-go-full.conf"),
		filepath.Join("..", "..", "deploy", "nginx", "canary-go-full.conf"),
		filepath.Join("..", "..", "..", "deploy", "nginx", "canary-go-full.conf"),
		filepath.Join("deploy", "nginx", "canary-go-full.conf"),
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	t.Fatalf("nginx config not found in candidates: %v", candidates)
	return ""
}

func extractComposeServices(t *testing.T, composeContent string) map[string]struct{} {
	t.Helper()

	lines := strings.Split(composeContent, "\n")
	services := make(map[string]struct{})
	serviceLine := regexp.MustCompile(`^  ([A-Za-z0-9_-]+):\s*$`)

	inServices := false
	for _, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)

		if !inServices {
			if trimmed == "services:" {
				inServices = true
			}
			continue
		}

		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if !strings.HasPrefix(line, "  ") {
			break
		}
		if strings.HasPrefix(line, "    ") {
			continue
		}

		matches := serviceLine.FindStringSubmatch(line)
		if len(matches) == 2 {
			services[matches[1]] = struct{}{}
		}
	}

	if len(services) == 0 {
		t.Fatalf("no services parsed from docker-compose.yml")
	}
	return services
}

func extractComposeServiceBlock(t *testing.T, composeContent, serviceName string) string {
	t.Helper()

	lines := strings.Split(composeContent, "\n")
	target := "  " + serviceName + ":"

	inServices := false
	inTarget := false
	block := make([]string, 0, 16)
	for _, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)

		if !inServices {
			if trimmed == "services:" {
				inServices = true
			}
			continue
		}

		if !inTarget {
			if line == target {
				inTarget = true
				block = append(block, line)
			}
			continue
		}

		if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    ") {
			break
		}
		block = append(block, line)
	}

	if len(block) == 0 {
		t.Fatalf("service %q not found in docker-compose.yml", serviceName)
	}
	return strings.Join(block, "\n") + "\n"
}

func sortedServiceNames(services map[string]struct{}) []string {
	names := make([]string, 0, len(services))
	for name := range services {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func sameServiceSet(left, right map[string]struct{}) bool {
	if len(left) != len(right) {
		return false
	}
	for name := range left {
		if _, ok := right[name]; !ok {
			return false
		}
	}
	return true
}
