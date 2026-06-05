package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestConfigDoesNotExposeRetiredRedisStreamSettings(t *testing.T) {
	configType := reflect.TypeOf(Config{})
	retiredFields := []string{
		"Redis" + "URL",
		"Stream" + "Name",
		"V2" + "Stream" + "Name",
		"Consumer" + "Group",
	}

	for _, fieldName := range retiredFields {
		if _, ok := configType.FieldByName(fieldName); ok {
			t.Fatalf("Config must not expose retired runtime field %s", fieldName)
		}
	}
}

func TestStartLocalScriptUsesComposeMainline(t *testing.T) {
	script := readRepoFile(t, "scripts", "start_local.sh")

	forbidden := []string{
		"python3",
		"pip install",
		"requirements.txt",
		"FLASK_APP",
		"app.py",
		"flask run",
	}
	for _, token := range forbidden {
		if strings.Contains(script, token) {
			t.Fatalf("scripts/start_local.sh must not contain legacy Python runtime token %q", token)
		}
	}
	if !strings.Contains(script, "docker compose up -d --build") {
		t.Fatalf("scripts/start_local.sh must start the Docker Compose mainline")
	}
	if !strings.Contains(script, "INTERNAL_ENQUEUE_TOKEN") {
		t.Fatalf("scripts/start_local.sh must provide or require INTERNAL_ENQUEUE_TOKEN")
	}
}

func TestDockerfileDeclaresArchiveExtractorRuntimePackage(t *testing.T) {
	dockerfile := readRepoFile(t, "Dockerfile")

	if !strings.Contains(dockerfile, "libarchive-tools") {
		t.Fatalf("Dockerfile runtime image must install libarchive-tools so bsdtar is available for RAR/7Z uploads")
	}
}

func TestE2ERunnerDoesNotReintroduceRetiredRuntimeService(t *testing.T) {
	runner := readRepoFile(t, "tests", "e2e", "run-e2e.sh")

	forbidden := []string{
		"${temp_data_root}/redis",
		"\n  redis:\n",
		":/data",
	}
	for _, token := range forbidden {
		if strings.Contains(runner, token) {
			t.Fatalf("tests/e2e/run-e2e.sh must not declare retired runtime service or volume token %q", token)
		}
	}
}

func TestRuntimeDocsDescribeTaskCorePostgresMainline(t *testing.T) {
	targets := []string{
		readRepoFile(t, "README.md"),
		readRepoFile(t, "docs", "architecture", "current-system-overview.md"),
		readRepoFile(t, "docs", "architecture", "config-inventory.md"),
		readRepoFile(t, "docs", "architecture", "task-lifecycle-baseline.md"),
	}

	for _, content := range targets {
		forbidden := []string{
			"Redis Streams",
			"redis stream",
			"redis streams",
			"postgres/redis",
			"postgres + redis",
			"internal/queue/v2",
			"internal/queue/redisstream",
		}
		for _, token := range forbidden {
			if strings.Contains(content, token) {
				t.Fatalf("current runtime docs must not describe retired mainline token %q", token)
			}
		}
	}
}

func readRepoFile(t *testing.T, pathParts ...string) string {
	t.Helper()

	candidates := [][]string{
		append([]string{"..", ".."}, pathParts...),
		append([]string{"..", "..", ".."}, pathParts...),
		append([]string{".."}, pathParts...),
		pathParts,
	}
	for _, parts := range candidates {
		path := filepath.Join(parts...)
		content, err := os.ReadFile(path)
		if err == nil {
			return string(content)
		}
	}
	t.Fatalf("repo file not found: %s", filepath.Join(pathParts...))
	return ""
}
