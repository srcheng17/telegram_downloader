package main

import (
	"reflect"
	"testing"

	"github.com/ryancheng/telegram-downloader/go-backend/internal/config"
	"github.com/ryancheng/telegram-downloader/go-backend/internal/httpui"
)

func TestBuildUIConfigUsesRuntimeValues(t *testing.T) {
	input := config.Config{
		DownloadTimeout:  88,
		DownloadRetries:  0,
		ImageConcurrency: 6,
	}

	got := buildUIConfig(input)
	want := httpui.Config{
		Settings: httpui.Settings{
			TaskConcurrency:   2,
			ImageConcurrency:  6,
			Timeout:           88,
			Retries:           0,
			LogRetentionDays:  7,
			FileRetentionDays: 7,
		},
		Guardrails: httpui.Guardrails{
			AllowedDomains: []string{"telegra.ph", "graph.org"},
			MaxImages:      300,
			MaxImageBytes:  25 * 1024 * 1024,
			MaxTotalBytes:  500 * 1024 * 1024,
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected ui config:\n got: %#v\nwant: %#v", got, want)
	}
}
