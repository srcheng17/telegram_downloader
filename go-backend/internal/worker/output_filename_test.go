package worker

import (
	"testing"

	"github.com/ryancheng/telegram-downloader/go-backend/internal/downloader"
)

func TestBuildDownloadFilenameUsesMetadataAndTimestamp(t *testing.T) {
	fileName := buildDownloadFilename(
		downloader.TaskMetadata{
			Writer: "作者A",
			Series: "系列B",
			Title:  "漫画C",
		},
		1700000000,
	)

	if fileName != "作者A_系列B_漫画C_1700000000.cbz" {
		t.Fatalf("expected metadata based filename, got %q", fileName)
	}
}

func TestBuildDownloadFilenameOmitsEmptySeries(t *testing.T) {
	fileName := buildDownloadFilename(
		downloader.TaskMetadata{
			Writer: "作者A",
			Title:  "漫画C",
		},
		1700000001,
	)

	if fileName != "作者A_漫画C_1700000001.cbz" {
		t.Fatalf("expected filename without series, got %q", fileName)
	}
}

func TestBuildDownloadFilenameUsesPlaceholdersWhenRequiredFieldsMissing(t *testing.T) {
	fileName := buildDownloadFilename(downloader.TaskMetadata{}, 1700000002)
	if fileName != "未知作者_未命名漫画_1700000002.cbz" {
		t.Fatalf("expected placeholder filename, got %q", fileName)
	}
}

func TestBuildDownloadFilenameSanitizesInvalidCharacters(t *testing.T) {
	fileName := buildDownloadFilename(
		downloader.TaskMetadata{
			Writer: " 作<者>:A ",
			Series: "  系|列/B  ",
			Title:  " 漫*画?C ",
		},
		1700000003,
	)

	if fileName != "作 者 A_系 列 B_漫 画 C_1700000003.cbz" {
		t.Fatalf("expected sanitized filename, got %q", fileName)
	}
}
