package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	appmetadata "github.com/ryancheng/telegram-downloader/internal/app/metadata"
	apptaskcore "github.com/ryancheng/telegram-downloader/internal/app/taskcore"
	"github.com/ryancheng/telegram-downloader/internal/config"
	metadata "github.com/ryancheng/telegram-downloader/internal/domain/metadata"
	"github.com/ryancheng/telegram-downloader/internal/httpapi"
	"github.com/ryancheng/telegram-downloader/internal/store/postgres"
	"github.com/ryancheng/telegram-downloader/internal/store/postgres/migrations"
	taskstore "github.com/ryancheng/telegram-downloader/internal/store/postgres/taskcore"
)

// Each server test has its own schema, so package-parallel integration tests
// cannot truncate its tasks or change its current metadata definition version.
func workspaceTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_SERVER_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("TEST_DATABASE_URL")
	}
	if dsn == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("TEST_DATABASE_URL required")
		}
		t.Skip("TEST_DATABASE_URL not configured")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	// Install this shared extension outside the disposable schema.
	if _, err := admin.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA public`); err != nil {
		t.Fatal(err)
	}
	schema := "workspace_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(context.Background(), "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
			t.Errorf("cleanup schema: %v", err)
		}
	})
	poolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := migrations.Run(ctx, pool); err != nil {
		t.Fatal(err)
	}
	return pool
}

const workspacePassword = "workspace-test-password-only"
const workspaceOrigin = "https://workspace.example"

func workspaceTestConfig(t *testing.T) config.Config {
	t.Helper()
	t.Setenv("ADMIN_BOOTSTRAP_PASSWORD", workspacePassword)
	t.Setenv("ADMIN_BOOTSTRAP_PASSWORD_FILE", "")
	t.Setenv("SOURCE_SETTINGS_MASTER_KEY", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{17}, 32)))
	t.Setenv("SOURCE_SETTINGS_MASTER_KEY_FILE", "")
	static := t.TempDir()
	if err := os.Mkdir(filepath.Join(static, "dist"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(static, "dist", "login-test.js"), []byte("// synthetic login asset"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(static, "dist", "assets"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(static, "dist", "assets", "shared-test.js"), []byte("// synthetic shared chunk"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GO_UI_STATIC_DIR", static)
	t.Setenv("TEMP_PATH", t.TempDir())
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	home, err = filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatal(err)
	}
	backupParent, err := os.MkdirTemp(home, "komga-edit-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(backupParent); err != nil {
			t.Errorf("remove private backup test directory: %v", err)
		}
	})
	return config.Config{PublicOrigin: workspaceOrigin, DownloadTimeout: 30, DownloadRetries: 1, ImageConcurrency: 1,
		KomgaEditBackupRoot: filepath.Join(backupParent, "komga-edit-backups")}
}

type workspaceClient struct {
	t      *testing.T
	router http.Handler
	cookie *http.Cookie
	csrf   string
}

func (c *workspaceClient) request(method, path string, body any) *httptest.ResponseRecorder {
	c.t.Helper()
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			c.t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, workspaceOrigin+path, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", workspaceOrigin)
	if c.cookie != nil {
		req.AddCookie(c.cookie)
	}
	if c.csrf != "" {
		req.Header.Set("X-CSRF-Token", c.csrf)
	}
	rec := httptest.NewRecorder()
	c.router.ServeHTTP(rec, req)
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == httpapi.AdminCookieName {
			c.cookie = cookie
		}
	}
	return rec
}
func decodeWorkspaceResponse[T any](t *testing.T, rec *httptest.ResponseRecorder, status int) T {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status=%d want=%d response=%s", rec.Code, status, rec.Body.String())
	}
	var value T
	if err := json.Unmarshal(rec.Body.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	return value
}
func (c *workspaceClient) login() {
	c.t.Helper()
	session := decodeWorkspaceResponse[struct {
		Authenticated bool   `json:"authenticated"`
		CSRF          string `json:"csrf_token"`
	}](c.t, c.request(http.MethodGet, "/api/auth/session", nil), 200)
	if session.Authenticated || session.CSRF == "" || c.cookie == nil {
		c.t.Fatal("preauth session missing")
	}
	c.csrf = session.CSRF
	old := c.cookie.Value
	loggedIn := decodeWorkspaceResponse[struct {
		Authenticated bool   `json:"authenticated"`
		CSRF          string `json:"csrf_token"`
	}](c.t, c.request(http.MethodPost, "/api/auth/login", map[string]string{"password": workspacePassword}), 200)
	if !loggedIn.Authenticated || loggedIn.CSRF == "" || c.cookie.Value == old || !c.cookie.HttpOnly || !c.cookie.Secure {
		c.t.Fatal("authenticated session not rotated/protected")
	}
	c.csrf = loggedIn.CSRF
}

func TestWorkspaceAuthMetadataTaskHistoryRoundTrip(t *testing.T) {
	pool := workspaceTestPool(t)
	ctx := context.Background()
	cfg := workspaceTestConfig(t)
	router, err := buildWorkspaceRouter(ctx, pool, cfg)
	if err != nil {
		t.Fatal(err)
	}
	client := &workspaceClient{t: t, router: router}
	for _, path := range []string{"/api/client-contract", "/api/metadata/schema", "/api/metadata-history", "/api/tasks", "/api/settings/sources", "/api/settings/metadata-fields"} {
		if rec := client.request(http.MethodGet, path, nil); rec.Code != 401 {
			t.Fatalf("anonymous %s status=%d", path, rec.Code)
		}
	}
	for _, path := range []string{"/healthz", "/readyz", "/static/dist/login-test.js", "/static/dist/assets/shared-test.js"} {
		if rec := client.request(http.MethodGet, path, nil); rec.Code != 200 {
			t.Fatalf("public %s status=%d", path, rec.Code)
		}
	}
	client.login()
	contract := decodeWorkspaceResponse[struct {
		ProtocolVersion int    `json:"protocol_version"`
		Client          string `json:"client"`
	}](t, client.request(http.MethodGet, "/api/client-contract", nil), 200)
	if contract.ProtocolVersion != httpapi.ClientContractVersion || contract.Client != "mediactl" {
		t.Fatalf("unexpected client contract: %+v", contract)
	}
	reg := decodeWorkspaceResponse[metadata.Registry](t, client.request(http.MethodGet, "/api/metadata/schema", nil), 200)
	custom := metadata.FieldDefinition{Key: "custom.user.reviewed", Label: "已核对", Type: "boolean", Editable: true, Enabled: true, ExportStatus: "internal_only", Extractable: []string{"legacy", "rule"}}
	input := appmetadata.UpdateFieldsInput{ExpectedDefinitionsVersion: reg.DefinitionsVersion, Definitions: []metadata.FieldDefinition{custom}}
	savedCSRF := client.csrf
	client.csrf = "wrong"
	if rec := client.request(http.MethodPut, "/api/settings/metadata-fields", input); rec.Code != 403 {
		t.Fatalf("missing CSRF accepted: %d", rec.Code)
	}
	client.csrf = savedCSRF
	saved := decodeWorkspaceResponse[appmetadata.FieldsResult](t, client.request(http.MethodPut, "/api/settings/metadata-fields", input), 200)
	if saved.DefinitionsVersion == reg.DefinitionsVersion || saved.Limits.CustomFields != 64 {
		t.Fatal("custom version/limits missing")
	}
	if rec := client.request(http.MethodPut, "/api/settings/metadata-fields", input); rec.Code != 409 {
		t.Fatalf("stale settings accepted: %d", rec.Code)
	}
	reg = decodeWorkspaceResponse[metadata.Registry](t, client.request(http.MethodGet, "/api/metadata/schema", nil), 200)
	patch := appmetadata.PatchInput{Document: metadata.EmptyDocument(reg), Operations: []metadata.Operation{
		{Op: "set", Key: "title", Value: json.RawMessage(`"集成测试作品"`)}, {Op: "set", Key: "aliases", Value: json.RawMessage(`["别名 with spaces"]`)},
		{Op: "set", Key: "creators.writer", Value: json.RawMessage(`["作者甲"]`)}, {Op: "set", Key: "series", Value: json.RawMessage(`"测试系列"`)},
		{Op: "set", Key: "number", Value: json.RawMessage(`"2.5 特别篇"`)}, {Op: "set", Key: "count", Value: json.RawMessage(`12`)}, {Op: "set", Key: "volume", Value: json.RawMessage(`2024`)},
		{Op: "set", Key: "publisher", Value: json.RawMessage(`"示例出版社"`)}, {Op: "set", Key: "language", Value: json.RawMessage(`"zh"`)},
		{Op: "set", Key: "publication_date", Value: json.RawMessage(`{"year":2024,"month":2}`)}, {Op: "clear", Key: "summary"}, {Op: "set", Key: custom.Key, Value: json.RawMessage(`false`)},
		{Op: "set", Key: "tags", Value: json.RawMessage(`["Slice of Life","科幻"]`)},
	}}
	result := decodeWorkspaceResponse[appmetadata.Result](t, client.request(http.MethodPost, "/api/metadata/patch", patch), 200)
	if len(result.Document.Fields) != 13 || !result.Document.Fields["summary"].ManualLocked {
		t.Fatal("extended patch lost fields or clear lock")
	}
	_ = decodeWorkspaceResponse[appmetadata.Result](t, client.request(http.MethodPost, "/api/metadata/validate", map[string]any{"document": result.Document}), 200)
	submitted := map[string]any{"url": "https://telegra.ph/workspace-integration", "metadata_document": result.Document, "comic_name": "集成测试作品"}
	created := decodeWorkspaceResponse[struct {
		TaskID string `json:"task_id"`
		Task   struct {
			Title  string `json:"comic_name"`
			Author string `json:"author"`
			Tags   string `json:"tags_raw"`
		} `json:"task"`
	}](t, client.request(http.MethodPost, "/download", submitted), 202)
	if created.TaskID == "" {
		t.Fatal("task missing")
	}
	if created.Task.Title != "集成测试作品" || created.Task.Author != "作者甲" || created.Task.Tags != "Slice of Life,科幻" {
		t.Fatal("creation response did not project authoritative document")
	}
	var storedDoc, historyDoc []byte
	if err := pool.QueryRow(ctx, `SELECT i.metadata_document,h.metadata_document FROM task_core_inputs i JOIN metadata_history h ON h.task_id=i.task_id::text WHERE i.task_id=$1`, created.TaskID).Scan(&storedDoc, &historyDoc); err != nil {
		t.Fatal(err)
	}
	for _, raw := range [][]byte{storedDoc, historyDoc} {
		stored, err := metadata.Decode(raw, reg)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(stored, result.Document) {
			t.Fatal("task/history transaction changed document")
		}
	}
	history := decodeWorkspaceResponse[[]postgres.MetadataHistoryEntry](t, client.request(http.MethodGet, "/api/metadata-history", nil), 200)
	if len(history) != 1 || history[0].MetadataDocument == nil || !reflect.DeepEqual(history[0].MetadataDocument, &result.Document) || history[0].Summary != nil {
		t.Fatal("history snapshot/clear changed")
	}
	// Mixed input must not create a second fact source or a partial task/history row.
	submitted["comic_name"] = "冲突标题"
	if rec := client.request(http.MethodPost, "/download", submitted); rec.Code != 400 {
		t.Fatalf("mixed metadata conflict accepted: %d", rec.Code)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM task_core_tasks`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("invalid request persisted task: count=%d err=%v", count, err)
	}
	custom.Enabled = false
	custom.Label = "已停用的核对字段"
	_ = decodeWorkspaceResponse[appmetadata.FieldsResult](t, client.request(http.MethodPut, "/api/settings/metadata-fields", appmetadata.UpdateFieldsInput{ExpectedDefinitionsVersion: reg.DefinitionsVersion, Definitions: []metadata.FieldDefinition{custom}}), 200)
	_ = decodeWorkspaceResponse[appmetadata.Result](t, client.request(http.MethodPost, "/api/metadata/validate", map[string]any{"document": result.Document}), 200)
	workerStore := taskstore.NewStore(pool)
	firstClaim, err := workerStore.ClaimNext(ctx, "integration-worker", time.Minute)
	if err != nil || firstClaim == nil {
		t.Fatalf("first claim failed: %v", err)
	}
	if rec := client.request(http.MethodPost, "/api/tasks/"+created.TaskID+"/cancel", nil); rec.Code != 200 {
		t.Fatal("cancel failed", rec.Code)
	}
	if err := workerStore.AcknowledgeCancel(ctx, created.TaskID, "integration-worker", firstClaim.Attempt, firstClaim.Generation); err != nil {
		t.Fatal(err)
	}
	if rec := client.request(http.MethodPost, "/api/tasks/"+created.TaskID+"/retry", nil); rec.Code != 200 {
		t.Fatal("retry failed", rec.Code)
	}
	claimed, err := workerStore.ClaimNext(ctx, "integration-worker", time.Minute)
	if err != nil || claimed == nil {
		t.Fatalf("claim failed: %v", err)
	}
	view, err := workerStore.GetTask(ctx, created.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	canonical, _, err := metadata.Validate(*view.Input.MetadataDocument, reg)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(canonical, result.Document) || view.Input.Metadata["author"] != "作者甲" || view.Input.Metadata["series_number"] != "2.5 特别篇" || claimed.Generation <= firstClaim.Generation {
		t.Fatal("worker/retry read lost immutable snapshot or legacy projection")
	}
	pageCandidate := metadata.MetadataCandidate{
		CandidateID: "artifact-pages", RequestID: "artifact-pages", Origin: "archive",
		SchemaVersion: reg.SchemaVersion, DefinitionsVersion: reg.DefinitionsVersion,
		BaseDocumentRevision: result.Document.Revision, FieldRevisions: map[string]uint64{"page_count": 0},
		Fields: map[string]metadata.CandidateField{"page_count": {State: "value", Value: json.RawMessage(`2`), Provenance: []metadata.Provenance{{Kind: "archive", SourceID: "artifact"}}}},
	}
	effective, _, err := metadata.ApplyCandidate(result.Document, reg, result.Document.Revision, pageCandidate, []string{"page_count"}, false, metadata.MergeContext{Now: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	completion := apptaskcore.CompleteInput{TaskID: created.TaskID, WorkerID: "integration-worker", Attempt: firstClaim.Attempt, Generation: firstClaim.Generation, ArtifactPath: filepath.Join(t.TempDir(), "synthetic.cbz"), ArtifactName: "synthetic.cbz", ArtifactSize: 0, EffectiveMetadataDocument: &effective}
	if err := workerStore.Complete(ctx, completion); !errors.Is(err, apptaskcore.ErrConflict) {
		t.Fatalf("stale effective snapshot published: %v", err)
	}
	completion.Attempt, completion.Generation = claimed.Attempt, claimed.Generation
	if err := workerStore.Complete(ctx, completion); err != nil {
		t.Fatal(err)
	}
	history = decodeWorkspaceResponse[[]postgres.MetadataHistoryEntry](t, client.request(http.MethodGet, "/api/metadata-history", nil), 200)
	if len(history) != 1 || history[0].EffectiveMetadataDocument == nil {
		t.Fatal("successful generation effective history missing")
	}
	actualEffective, _, err := metadata.Validate(*history[0].EffectiveMetadataDocument, reg)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actualEffective, effective) || !reflect.DeepEqual(history[0].MetadataDocument, &result.Document) {
		t.Fatal("effective result replaced submitted history")
	}
	completion.Generation = firstClaim.Generation
	if err := workerStore.Complete(ctx, completion); !errors.Is(err, apptaskcore.ErrConflict) {
		t.Fatalf("late completion overwrote successful snapshot: %v", err)
	}
	if rec := client.request(http.MethodPost, "/api/auth/logout", nil); rec.Code != 200 {
		t.Fatal("logout failed")
	}
	if rec := client.request(http.MethodGet, "/api/metadata/schema", nil); rec.Code != 401 {
		t.Fatal("logged out session still authorized")
	}
}

func TestWorkspaceLegacyRowsAndUploadDocument(t *testing.T) {
	pool := workspaceTestPool(t)
	ctx := context.Background()
	router, err := buildWorkspaceRouter(ctx, pool, workspaceTestConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	client := &workspaceClient{t: t, router: router}
	client.login()
	legacy := decodeWorkspaceResponse[struct {
		TaskID string `json:"task_id"`
	}](t, client.request(http.MethodPost, "/download", map[string]any{"url": "https://telegra.ph/legacy-integration", "author": "Ada Example，作者乙", "comic_name": "旧协议作品", "tags": "科幻 # 日常"}), 202)
	// Simulate a pre-015 row without rewriting the preserved compatibility values.
	if _, err := pool.Exec(ctx, `UPDATE task_core_inputs SET metadata_document=NULL WHERE task_id=$1`, legacy.TaskID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE metadata_history SET metadata_document=NULL WHERE task_id=$1`, legacy.TaskID); err != nil {
		t.Fatal(err)
	}
	history := decodeWorkspaceResponse[[]postgres.MetadataHistoryEntry](t, client.request(http.MethodGet, "/api/metadata-history", nil), 200)
	if len(history) != 1 || history[0].MetadataDocument == nil || history[0].MetadataDocument.DefinitionsVersion != metadata.StandardDefinitionsVersion || *history[0].Author != "Ada Example,作者乙" {
		t.Fatal("legacy history not upgraded deterministically")
	}
	row, err := taskstore.NewStore(pool).GetTask(ctx, legacy.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(row.Input.MetadataDocument, history[0].MetadataDocument) {
		t.Fatal("legacy task/history upgrade diverged")
	}
	uploaded := decodeWorkspaceResponse[struct {
		TaskID string `json:"task_id"`
	}](t, client.request(http.MethodPost, "/api/tasks/upload/init", map[string]any{"file_name": "synthetic.zip", "file_size": 23, "metadata_document": history[0].MetadataDocument}), 202)
	view, err := taskstore.NewStore(pool).GetTask(ctx, uploaded.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	canonical, _, err := metadata.Validate(*view.Input.MetadataDocument, metadata.StandardRegistry())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(canonical, *history[0].MetadataDocument) {
		t.Fatal("upload dropped metadata document")
	}
}

func TestWorkspaceInitializationFailsClosedWithoutSecrets(t *testing.T) {
	pool := workspaceTestPool(t)
	cfg := workspaceTestConfig(t)
	t.Setenv("ADMIN_BOOTSTRAP_PASSWORD", "")
	if handler, err := buildWorkspaceRouter(context.Background(), pool, cfg); err == nil || handler != nil {
		t.Fatal("workspace opened without administrator initialization")
	}
	t.Setenv("ADMIN_BOOTSTRAP_PASSWORD", workspacePassword)
	t.Setenv("SOURCE_SETTINGS_MASTER_KEY", "")
	if handler, err := buildWorkspaceRouter(context.Background(), pool, cfg); err == nil || handler != nil {
		t.Fatal("workspace opened without credential encryption key")
	}
}
