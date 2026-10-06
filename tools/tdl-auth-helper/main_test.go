package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

func TestMain(m *testing.M) {
	if filepath.Base(os.Args[0]) == "bounded-writer" {
		var dir string
		for i, arg := range os.Args {
			if arg == "--dir" {
				dir = os.Args[i+1]
			}
		}
		file, err := os.Create(filepath.Join(dir, "source.tmp"))
		if err != nil {
			os.Exit(10)
		}
		// Same *os.File.WriteAt system call used by the pinned official downloader.
		if _, err = file.WriteAt([]byte{1}, 4095); err != nil {
			os.Exit(11)
		}
		if _, err = file.WriteAt([]byte{1}, 4096); err == nil {
			os.Exit(12)
		}
		file.Close()
		os.Exit(0)
	}
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "bridge-probe":
			s := bufio.NewScanner(os.Stdin)
			c, err := readCommand(s)
			if err == nil {
				err = run(context.Background(), c, s)
			}
			if err != nil {
				os.Exit(13)
			}
			os.Exit(0)
		case "lock-sleeper":
			if syscall.Flock(3, syscall.LOCK_EX|syscall.LOCK_NB) != nil {
				os.Exit(14)
			}
			fmt.Println("ready")
			for {
				time.Sleep(time.Hour)
			}
		case "lock-launcher":
			f, err := os.OpenFile(os.Args[2], os.O_CREATE|os.O_RDWR, 0600)
			if err != nil || syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
				os.Exit(15)
			}
			exe, _ := os.Executable()
			child := exec.Command(exe, "lock-sleeper")
			child.ExtraFiles = []*os.File{f}
			out, _ := child.StdoutPipe()
			if child.Start() != nil {
				os.Exit(16)
			}
			s := bufio.NewScanner(out)
			if !s.Scan() {
				os.Exit(17)
			}
			fmt.Println(child.Process.Pid)
			for {
				time.Sleep(time.Hour)
			}
		}
	}
	os.Exit(m.Run())
}
func TestKernelBoundCoversWriteAt(t *testing.T) {
	root := t.TempDir()
	storage := filepath.Join(root, "storage")
	os.Mkdir(storage, 0700)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.Open(exe)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	writer := filepath.Join(root, "bounded-writer")
	target, err := os.OpenFile(writer, os.O_CREATE|os.O_WRONLY, 0700)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = io.Copy(target, source); err != nil {
		t.Fatal(err)
	}
	target.Close()
	lock, err := os.OpenFile(filepath.Join(root, "lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "bridge-probe")
	cmd.ExtraFiles = []*os.File{lock}
	data, _ := json.Marshal(command{Mode: "download", StoragePath: storage, Namespace: "candidate_0123456789abcdef0123456789abcdef", TDLPath: writer, OutputDir: root, MaxBytes: 4096})
	cmd.Stdin = strings.NewReader(string(data) + "\n")
	if err = cmd.Run(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(root, "source.tmp"))
	if err != nil || info.Size() != 4096 {
		t.Fatalf("hard limit not enforced: %v %v", info, err)
	}
}
func TestLockSurvivesLauncherSIGKILL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "account.lock")
	exe, _ := os.Executable()
	launcher := exec.Command(exe, "lock-launcher", path)
	out, _ := launcher.StdoutPipe()
	if err := launcher.Start(); err != nil {
		t.Fatal(err)
	}
	s := bufio.NewScanner(out)
	if !s.Scan() {
		t.Fatal("launcher did not start child")
	}
	pid, err := strconv.Atoi(s.Text())
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Kill(pid, syscall.SIGKILL)
	if err = launcher.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = launcher.Wait()
	other, err := os.OpenFile(path, os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if err = syscall.Flock(int(other.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
		t.Fatal("live child lost lock when launcher died")
	}
	if err = syscall.Kill(pid, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		err = syscall.Flock(int(other.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("dead child did not release lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
func TestInputFramingDoesNotEchoSecrets(t *testing.T) {
	for _, raw := range []string{`{"mode":"password","password":"secret","extra":1}`, `{"mode":"verify"} {}`, `not json`} {
		s := bufio.NewScanner(strings.NewReader(raw))
		_, err := readCommand(s)
		if err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatal("invalid private command accepted or echoed")
		}
	}
}

func TestPasswordInvalidErrorCode(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"sentinel", auth.ErrPasswordInvalid},
		{"wrapped_sentinel", fmt.Errorf("password: %w", auth.ErrPasswordInvalid)},
		{"rpc_hash_invalid", tgerr.New(400, "PASSWORD_HASH_INVALID")},
		{"wrapped_rpc_hash_invalid", fmt.Errorf("password: %w", tgerr.New(400, "PASSWORD_HASH_INVALID"))},
		{"rpc_empty", tgerr.New(400, "PASSWORD_EMPTY")},
		{"wrapped_rpc_empty", fmt.Errorf("password: %w", tgerr.New(400, "PASSWORD_EMPTY"))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := errorCode(tc.err); got != "password_invalid" {
				t.Fatalf("error code %q want password_invalid", got)
			}
		})
	}
}

func TestPasswordRetryRecognition(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"sentinel", auth.ErrPasswordInvalid, true},
		{"wrapped_sentinel", fmt.Errorf("password: %w", auth.ErrPasswordInvalid), true},
		{"rpc_hash_invalid", tgerr.New(400, "PASSWORD_HASH_INVALID"), true},
		{"wrapped_rpc_hash_invalid", fmt.Errorf("password: %w", tgerr.New(400, "PASSWORD_HASH_INVALID")), true},
		{"rpc_empty", tgerr.New(400, "PASSWORD_EMPTY"), false},
		{"wrapped_rpc_empty", fmt.Errorf("password: %w", tgerr.New(400, "PASSWORD_EMPTY")), false},
		{"successful", nil, false},
		{"cancelled", context.Canceled, false},
		{"network_error", errors.New("connection failed"), false},
		{"sentinel_text_only", errors.New(auth.ErrPasswordInvalid.Error()), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := isPasswordInvalid(tc.err); got != tc.want {
				t.Fatalf("retry recognition %t want %t", got, tc.want)
			}
		})
	}
}

func TestExpiredHelperStopsBeforeOpeningSession(t *testing.T) {
	expires := time.Now().Add(-time.Second)
	err := run(context.Background(), command{Mode: "login", Namespace: "candidate_0123456789abcdef0123456789abcdef", StoragePath: filepath.Join(t.TempDir(), "storage"), ExpiresAt: &expires}, bufio.NewScanner(strings.NewReader("")))
	if err != context.DeadlineExceeded {
		t.Fatalf("expired helper error %v", err)
	}
}
func TestPasswordInputCannotOutliveHelperDeadline(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := readPasswordCommand(ctx, bufio.NewScanner(reader))
	if err != context.DeadlineExceeded {
		t.Fatalf("password read ignored lifetime: %v", err)
	}
}

func TestInspectAcceptsOnlyOneBoundedArchive(t *testing.T) {
	doc := func(name string, size int64) *tg.Document {
		return &tg.Document{Size: size, Attributes: []tg.DocumentAttributeClass{&tg.DocumentAttributeFilename{FileName: name}}}
	}
	for _, tc := range []struct {
		name  string
		media tg.MessageMediaClass
		want  string
	}{
		{"valid", &tg.MessageMediaDocument{Document: doc("source.ZIP", 4096)}, ""},
		{"no_document", &tg.MessageMediaPhoto{}, "no_attachment"},
		{"multiple", &tg.MessageMediaDocument{Document: doc("source.zip", 4096), AltDocuments: []tg.DocumentClass{doc("other.zip", 4096)}}, "ambiguous_media"},
		{"not_archive", &tg.MessageMediaDocument{Document: doc("photo.jpg", 8)}, "unsupported_media"},
		{"unknown_size", &tg.MessageMediaDocument{Document: doc("source.zip", 0)}, "source_size_unknown"},
		{"oversized", &tg.MessageMediaDocument{Document: doc("source.zip", 4097)}, "source_too_large"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := inspectAttachment(tc.media, 4096)
			if tc.want == "" {
				if err != nil || result.Size != 4096 || result.Extension != ".zip" {
					t.Fatal("archive inspect failed", err)
				}
			} else if err == nil || errorCode(err) != tc.want {
				t.Fatalf("inspect error %v want %s", err, tc.want)
			}
		})
	}
}
