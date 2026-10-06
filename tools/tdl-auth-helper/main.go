// The bridge deliberately lives in its own module: pin both official tdl modules
// without adding their Telegram dependency graph to the API and worker binaries.
package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/auth/qrlogin"
	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"github.com/iyear/tdl/core/storage"
	"github.com/iyear/tdl/core/util/tutil"
	"github.com/iyear/tdl/pkg/key"
	"github.com/iyear/tdl/pkg/kv"
	"github.com/iyear/tdl/pkg/tclient"
	"github.com/skip2/go-qrcode"
)

const version = "tdl-auth-helper/1 tdl/v0.20.4 core/v0.20.4 gotd/v0.140.0"
const maximumBytes int64 = 500 << 20

var namespacePattern = regexp.MustCompile(`^candidate_[a-f0-9]{32}$`)

type command struct {
	Mode        string     `json:"mode"`
	StoragePath string     `json:"storage_path"`
	Namespace   string     `json:"namespace"`
	Proxy       string     `json:"proxy,omitempty"`
	MessageURL  string     `json:"message_url,omitempty"`
	Password    string     `json:"password,omitempty"`
	TDLPath     string     `json:"tdl_path,omitempty"`
	OutputDir   string     `json:"output_dir,omitempty"`
	MaxBytes    int64      `json:"max_bytes,omitempty"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}
type event struct {
	Type      string     `json:"type"`
	Code      string     `json:"code,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	QR        string     `json:"qr,omitempty"`
	Identity  string     `json:"identity,omitempty"`
	Size      int64      `json:"size,omitempty"`
	Extension string     `json:"extension,omitempty"`
}

func readCommand(scanner *bufio.Scanner) (command, error) {
	var c command
	if !scanner.Scan() {
		return c, io.EOF
	}
	d := json.NewDecoder(bytes.NewReader(scanner.Bytes()))
	d.DisallowUnknownFields()
	if err := d.Decode(&c); err != nil {
		return command{}, errors.New("invalid_input")
	}
	if d.Decode(new(any)) != io.EOF {
		return command{}, errors.New("invalid_input")
	}
	return c, nil
}
func emit(e event) error { return json.NewEncoder(os.Stdout).Encode(e) }
func readPasswordCommand(ctx context.Context, scanner *bufio.Scanner) (command, error) {
	type result struct {
		input command
		err   error
	}
	ready := make(chan result, 1)
	go func() { input, err := readCommand(scanner); ready <- result{input, err} }()
	select {
	case value := <-ready:
		return value.input, value.err
	case <-ctx.Done():
		return command{}, ctx.Err()
	}
}
func isPasswordInvalid(err error) bool {
	return errors.Is(err, auth.ErrPasswordInvalid) || tgerr.Is(err, "PASSWORD_HASH_INVALID")
}
func errorCode(err error) string {
	if err == nil {
		return "authorization_unconfirmed"
	}
	if isPasswordInvalid(err) || tgerr.Is(err, "PASSWORD_EMPTY") {
		return "password_invalid"
	}
	if _, ok := tgerr.AsFloodWait(err); ok {
		return "rate_limited"
	}
	if tgerr.Is(err, "AUTH_KEY_UNREGISTERED", "SESSION_REVOKED", "SESSION_EXPIRED", "USER_DEACTIVATED") {
		return "auth_required"
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "expired"
	}
	switch err.Error() {
	case "auth_required", "no_attachment", "unsupported_media", "ambiguous_media", "source_too_large", "source_size_unknown", "invalid_input":
		return err.Error()
	}
	return "network_error"
}
func identity(id int64) string {
	sum := sha256.Sum256([]byte("telegram-user:" + strconv.FormatInt(id, 10)))
	return hex.EncodeToString(sum[:])
}

func run(ctx context.Context, c command, scanner *bufio.Scanner) (rerr error) {
	if !namespacePattern.MatchString(c.Namespace) || !filepath.IsAbs(c.StoragePath) {
		return errors.New("invalid_input")
	}
	if c.Mode != "download" {
		if c.ExpiresAt == nil || c.ExpiresAt.After(time.Now().Add(15*time.Minute)) {
			return errors.New("invalid_input")
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, *c.ExpiresAt)
		defer cancel()
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	// FD 3 is the same open-file description as the parent's account lock.
	// It remains open across exec, so a killed launcher cannot release a live child's lock.
	if err := syscall.Flock(3, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return errors.New("invalid_input")
	}
	syscall.Umask(0077)
	if c.Mode == "download" {
		return execDownload(c)
	}
	engine, err := kv.New(kv.DriverBolt, map[string]any{"path": c.StoragePath})
	if err != nil {
		return err
	}
	defer func() {
		if err := engine.Close(); rerr == nil {
			rerr = err
		}
	}()
	store, err := engine.Open(c.Namespace)
	if err != nil {
		return err
	}
	if c.Mode == "login" {
		if err := store.Set(ctx, key.App(), []byte(tclient.AppDesktop)); err != nil {
			return err
		}
	}
	dispatcher := tg.NewUpdateDispatcher()
	client, err := tclient.New(ctx, tclient.Options{KV: store, Proxy: c.Proxy, ReconnectTimeout: 5 * time.Minute, UpdateHandler: dispatcher}, c.Mode == "login")
	if err != nil {
		return err
	}
	confirmed := false
	err = client.Run(ctx, func(ctx context.Context) error {
		if c.Mode == "login" {
			_, err := client.QR().Auth(ctx, qrlogin.OnLoginToken(dispatcher), func(ctx context.Context, token qrlogin.Token) error {
				png, err := qrcode.Encode(token.URL(), qrcode.Medium, 256)
				if err != nil {
					return err
				}
				expires := token.Expires()
				return emit(event{Type: "qr", QR: "data:image/png;base64," + base64.StdEncoding.EncodeToString(png), ExpiresAt: &expires})
			})
			if tgerr.Is(err, "SESSION_PASSWORD_NEEDED") {
				for tries := 0; tries < 3; tries++ {
					if err := emit(event{Type: "password_required"}); err != nil {
						return err
					}
					input, e := readPasswordCommand(ctx, scanner)
					if e != nil {
						return e
					}
					if input.Mode != "password" || len(input.Password) < 1 || len(input.Password) > 1024 {
						return errors.New("invalid_input")
					}
					_, err = client.Auth().Password(ctx, input.Password)
					input.Password = ""
					if !isPasswordInvalid(err) {
						break
					}
					if e := emit(event{Type: "password_invalid", Code: "password_invalid"}); e != nil {
						return e
					}
				}
			}
			if err != nil {
				return err
			}
		} else if c.Mode != "verify" && c.Mode != "inspect" {
			return errors.New("invalid_input")
		}
		status, err := client.Auth().Status(ctx)
		if err != nil {
			return err
		}
		if !status.Authorized {
			return errors.New("auth_required")
		}
		self, err := client.Self(ctx)
		if err != nil {
			return err
		}
		if c.Mode == "inspect" {
			manager := peers.Options{Storage: storage.NewPeers(store)}.Build(client.API())
			peer, id, err := tutil.ParseMessageLink(ctx, manager, c.MessageURL)
			if err != nil {
				return err
			}
			message, err := tutil.GetSingleMessage(ctx, client.API(), peer.InputPeer(), id)
			if err != nil {
				return err
			}
			attachment, err := inspectAttachment(message.Media, c.MaxBytes)
			if err != nil {
				return err
			}
			attachment.Identity = identity(self.ID)
			if err := emit(attachment); err != nil {
				return err
			}
		} else {
			if err := emit(event{Type: "authorized", Identity: identity(self.ID)}); err != nil {
				return err
			}
		}
		confirmed = true
		return nil
	})
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err == nil && !confirmed {
		return errors.New("auth_required")
	}
	return err
}

func inspectAttachment(raw tg.MessageMediaClass, maxBytes int64) (event, error) {
	media, ok := raw.(*tg.MessageMediaDocument)
	if !ok {
		return event{}, errors.New("no_attachment")
	}
	if len(media.AltDocuments) != 0 {
		return event{}, errors.New("ambiguous_media")
	}
	document, ok := media.Document.(*tg.Document)
	if !ok {
		return event{}, errors.New("no_attachment")
	}
	filename := ""
	for _, attribute := range document.Attributes {
		if name, ok := attribute.(*tg.DocumentAttributeFilename); ok {
			if filename != "" {
				return event{}, errors.New("ambiguous_media")
			}
			filename = name.FileName
		}
	}
	extension := strings.ToLower(filepath.Ext(filename))
	if extension != ".zip" && extension != ".rar" && extension != ".7z" {
		return event{}, errors.New("unsupported_media")
	}
	if document.Size <= 0 {
		return event{}, errors.New("source_size_unknown")
	}
	if maxBytes < 1 || maxBytes > maximumBytes || document.Size > maxBytes {
		return event{}, errors.New("source_too_large")
	}
	return event{Type: "attachment", Size: document.Size, Extension: extension}, nil
}

func execDownload(c command) error {
	if !filepath.IsAbs(c.TDLPath) || !filepath.IsAbs(c.OutputDir) || c.MaxBytes < 1 || c.MaxBytes > maximumBytes {
		return errors.New("invalid_input")
	}
	// Official v0.20.4 creates one source.tmp for one URL without --group, writes
	// all chunks through *os.File.WriteAt, closes it, then renames it to source.
	// RLIMIT_FSIZE is enforced by the kernel on every write/ftruncate, not polling.
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &syscall.Rlimit{Cur: uint64(c.MaxBytes), Max: uint64(c.MaxBytes)}); err != nil {
		return err
	}
	args := []string{c.TDLPath, "--storage", "type=bolt,path=" + c.StoragePath, "--ns", c.Namespace, "--reconnect-timeout", "5m", "--limit", "1", "--threads", "1", "--pool", "1", "--disable-progress-ps", "download", "--url", c.MessageURL, "--dir", c.OutputDir, "--template", "source", "--restart"}
	// Proxy is passed only via the private input when needed by the bridge. The
	// download launcher rejects credential-bearing proxy URLs before this point.
	if c.Proxy != "" {
		return errors.New("invalid_input")
	}
	return syscall.Exec(c.TDLPath, args, []string{"HOME=" + filepath.Dir(c.StoragePath), "PATH=/usr/local/bin:/usr/bin:/bin", "TERM=dumb", "NO_COLOR=1"})
}

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Println(version)
		return
	}
	if len(os.Args) != 1 {
		_ = emit(event{Type: "error", Code: "invalid_input"})
		os.Exit(2)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), 32768)
	c, err := readCommand(scanner)
	if err == nil {
		err = run(ctx, c, scanner)
	}
	if err != nil {
		_ = emit(event{Type: "error", Code: errorCode(err)})
		os.Exit(1)
	}
}
