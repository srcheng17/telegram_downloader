package config

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

func TestComposeTopologyMatchesSingleGoStack(t *testing.T) {
	composePath := findComposePath(t)
	content, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatalf("read compose file %q: %v", composePath, err)
	}

	services := extractComposeServices(t, string(content))
	required := []string{"go-api", "go-worker", "postgres", "redis", "gateway"}
	removed := []string{"web", "py-worker"}

	for _, name := range required {
		if _, ok := services[name]; !ok {
			t.Fatalf("expected required compose service %q, found services=%v", name, sortedServiceNames(services))
		}
	}
	for _, name := range removed {
		if _, ok := services[name]; ok {
			t.Fatalf("expected removed compose service %q to be absent, found services=%v", name, sortedServiceNames(services))
		}
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

func sortedServiceNames(services map[string]struct{}) []string {
	names := make([]string, 0, len(services))
	for name := range services {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
