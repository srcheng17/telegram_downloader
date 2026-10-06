package komgaedit

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ryancheng/telegram-downloader/internal/app/tasks"
	"github.com/ryancheng/telegram-downloader/internal/config"
	"github.com/ryancheng/telegram-downloader/internal/credentials"
	"github.com/ryancheng/telegram-downloader/internal/infra/komga"
)

// This test owns its entire Komga instance and only synthetic media. It never
// accepts an external server URL or production credentials. Run through
// scripts/check_komga_delivery.sh; ordinary unit test runs skip Docker startup.
func TestDeliveryRealKomga1281(t *testing.T) {
	if os.Getenv("KOMGA_DELIVERY_INTEGRATION") != "1" {
		t.Skip("run scripts/check_komga_delivery.sh for isolated Komga acceptance")
	}
	docker, err := exec.LookPath("docker")
	if err != nil {
		t.Fatal("Docker is required for isolated Komga acceptance")
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	booksRoot := filepath.Join(root, "books")
	configRoot := filepath.Join(root, "config")
	for _, dir := range []string{booksRoot, configRoot} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	name := "media-komga-acceptance-" + integrationRandom(t)
	runDocker := func(args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, docker, args...).CombinedOutput()
		if err != nil {
			t.Fatalf("isolated Docker operation failed: %v", err)
		}
		return strings.TrimSpace(string(out))
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := exec.CommandContext(ctx, docker, "rm", "-f", name).Run(); err != nil {
			t.Errorf("could not remove isolated container %s: %v", name, err)
		}
	})
	runDocker("run", "-d", "--name", name, "--pull=never",
		"--label", "media-workspace-test=komga-delivery", "--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()),
		"-e", "JAVA_TOOL_OPTIONS=-Xmx512m", "-p", "127.0.0.1::25600",
		"--mount", "type=bind,src="+booksRoot+",dst=/books",
		"--mount", "type=bind,src="+configRoot+",dst=/config", "gotson/komga:1.28.1")
	endpoint := runDocker("port", name, "25600/tcp")
	if !strings.HasPrefix(endpoint, "127.0.0.1:") || strings.ContainsAny(endpoint, "\r\n") {
		t.Fatal("isolated Komga did not bind exactly one loopback port")
	}
	base := "http://" + endpoint
	admin := &integrationKomgaHTTP{t: t, base: base, password: integrationRandom(t), client: &http.Client{Timeout: 5 * time.Second}}
	deadline := time.Now().Add(120 * time.Second)
	for {
		resp, err := admin.client.Get(base + "/api/v1/claim")
		if err == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("isolated Komga did not become ready in 120 seconds")
		}
		time.Sleep(500 * time.Millisecond)
	}
	admin.request(http.MethodPost, "/api/v1/claim", nil, http.StatusOK, nil)
	var key struct {
		Key string `json:"key"`
	}
	admin.request(http.MethodPost, "/api/v2/users/me/api-keys", map[string]string{"comment": "synthetic delivery acceptance"}, http.StatusOK, &key)
	if key.Key == "" {
		t.Fatal("isolated API key was not created")
	}
	var library komga.Library
	admin.request(http.MethodPost, "/api/v1/libraries", map[string]any{
		"name": "Synthetic delivery", "root": "/books", "importComicInfoBook": true,
		"importBarcodeIsbn": false, "scanOnStartup": false, "scanInterval": "DISABLED",
	}, http.StatusOK, &library)
	if library.ID == "" || library.Root != "/books" || !library.ImportComicInfoBook {
		t.Fatal("isolated library configuration does not match mapping")
	}
	vault, err := credentials.NewVault(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	connection := NewConnectionService(&memoryConnectionRepo{}, vault, true)
	zero := int64(0)
	if _, err := connection.Save(context.Background(), ConnectionUpdate{ExpectedVersion: &zero, BaseURL: &base, Credential: CredentialChange{Action: "replace", Value: key.Key}}); err != nil {
		t.Fatal(err)
	}
	catalog := NewCatalogService(connection, []config.KomgaLibraryMapping{{LibraryID: library.ID, KomgaRoot: "/books", LocalRoot: booksRoot}}, nil)
	service := NewDeliveryService(catalog, booksRoot)
	client, err := komga.NewClient(base, credentials.NewSecret(key.Key))
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "synthetic-source.cbz")
	writeIntegrationCBZ(t, source, "Synthetic reviewed summary")
	var result tasks.KomgaDeliveryResult
	deadline = time.Now().Add(60 * time.Second)
	for {
		result, err = service.Deliver(context.Background(), source, "synthetic-delivery.cbz", "")
		if err != nil {
			t.Fatalf("real delivery failed: %v", err)
		}
		if result.Indexed == "verified" {
			break
		}
		if result.Indexed != "pending" || !result.Copied || time.Now().After(deadline) {
			t.Fatalf("real readback did not verify: status=%s indexed=%s reason=%s copied=%v", result.Status, result.Indexed, result.Reason, result.Copied)
		}
		time.Sleep(time.Second)
	}
	book, err := client.Book(context.Background(), result.BookID)
	if err != nil || result.LibraryID != library.ID || book.ID == "" || book.Metadata.Title != "合成验收作品" || book.Metadata.Summary != "Synthetic reviewed summary" || book.Media.PagesCount != 1 || len(book.Metadata.Authors) != 2 {
		t.Fatalf("real Komga metadata readback mismatch: err=%v", err)
	}
	target := filepath.Join(booksRoot, "tankobon", "synthetic-delivery.cbz")
	before, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	beforeHash := integrationFileHash(t, target)
	result, err = NewDeliveryService(catalog, booksRoot).Deliver(context.Background(), source, "synthetic-delivery.cbz", "")
	after, statErr := os.Stat(target)
	if err != nil || statErr != nil || result.Indexed != "verified" || result.BookID != book.ID || !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) || beforeHash != integrationFileHash(t, target) {
		t.Fatal("verified retry changed the destination or catalog identity")
	}
	conflict := filepath.Join(root, "conflicting-source.cbz")
	writeIntegrationCBZ(t, conflict, "Another synthetic summary")
	if _, err := service.Deliver(context.Background(), conflict, "synthetic-delivery.cbz", ""); !errors.Is(err, tasks.ErrKomgaTargetConflict) || beforeHash != integrationFileHash(t, target) {
		t.Fatalf("same-name different-content destination was not preserved: %v", err)
	}
	// A real catalog record in READY can still contain stale, locked metadata.
	// A scan/analyze acceptance must not turn this into a verified result.
	admin.request(http.MethodPatch, "/api/v1/books/"+book.ID+"/metadata", map[string]any{"summary": "Stale catalog summary", "summaryLock": true}, http.StatusNoContent, nil)
	result, err = service.Deliver(context.Background(), source, "synthetic-delivery.cbz", "")
	stale, readErr := client.Book(context.Background(), book.ID)
	if err != nil || readErr != nil || result.Indexed != "pending" || result.BookID != "" || !result.Copied || stale.Media.Status != "READY" || stale.Metadata.Summary != "Stale catalog summary" || beforeHash != integrationFileHash(t, target) {
		t.Fatalf("READY plus stale metadata falsely verified: indexed=%s reason=%s err=%v read=%v", result.Indexed, result.Reason, err, readErr)
	}
	admin.request(http.MethodPatch, "/api/v1/books/"+book.ID+"/metadata", map[string]any{"summary": "Synthetic reviewed summary", "summaryLock": false}, http.StatusNoContent, nil)
	result, err = service.Deliver(context.Background(), source, "synthetic-delivery.cbz", "")
	after, statErr = os.Stat(target)
	if err != nil || statErr != nil || result.Indexed != "verified" || result.BookID != book.ID || !os.SameFile(before, after) {
		t.Fatal("pending delivery did not resume without recopying")
	}
	page, err := client.Books(context.Background(), komga.ListBooksOptions{LibraryID: library.ID, Size: 100})
	if err != nil || page.TotalElements != 1 {
		t.Fatalf("retry or conflict produced duplicate catalog books: %v", err)
	}
	t.Log("Komga 1.28.1: exact book and ComicInfo readback verified; unchanged inode/hash on retry; conflicting content preserved; READY plus stale metadata remains pending; same book resumes verified; catalog count=1")
}

type integrationKomgaHTTP struct {
	t        *testing.T
	base     string
	password string
	client   *http.Client
}

func (c *integrationKomgaHTTP) request(method, endpoint string, body any, status int, out any) {
	c.t.Helper()
	var payload []byte
	var err error
	if body != nil {
		payload, err = json.Marshal(body)
		if err != nil {
			c.t.Fatal(err)
		}
	}
	req, err := http.NewRequest(method, c.base+endpoint, bytes.NewReader(payload))
	if err != nil {
		c.t.Fatal("could not construct isolated Komga request")
	}
	if endpoint == "/api/v1/claim" {
		req.Header.Set("X-Komga-Email", "delivery@example.invalid")
		req.Header.Set("X-Komga-Password", c.password)
	} else {
		req.SetBasicAuth("delivery@example.invalid", c.password)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		c.t.Fatalf("isolated Komga request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != status {
		c.t.Fatalf("isolated Komga %s %s returned HTTP %d, expected %d", method, endpoint, resp.StatusCode, status)
	}
	if out != nil {
		if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out); err != nil {
			c.t.Fatal("isolated Komga returned invalid JSON")
		}
	}
}

func integrationRandom(t *testing.T) string {
	t.Helper()
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b[:])
}

func integrationFileHash(t *testing.T, file string) [32]byte {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(data)
}

func writeIntegrationCBZ(t *testing.T, name, summary string) {
	t.Helper()
	f, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	z := zip.NewWriter(f)
	page, err := z.Create("001.png")
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 80, 100))
	for y := 0; y < 100; y++ {
		for x := 0; x < 80; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x * 3), G: uint8(y * 2), B: 120, A: 255})
		}
	}
	if err := png.Encode(page, img); err != nil {
		t.Fatal(err)
	}
	xml, err := z.Create("ComicInfo.xml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintf(xml, `<?xml version="1.0" encoding="utf-8"?><ComicInfo><Title>合成验收作品</Title><Number>1</Number><Summary>%s</Summary><Writer>Sample Writer</Writer><Translator>Sample Translator</Translator><Tags>Synthetic, Acceptance</Tags><Year>2026</Year><Month>10</Month><Day>7</Day><PageCount>1</PageCount></ComicInfo>`, summary); err != nil {
		t.Fatal(err)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
}
