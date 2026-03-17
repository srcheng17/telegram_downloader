package tasks

import "testing"

func TestStatusCatalogIncludesCancelableAndDownloadableFlags(t *testing.T) {
	catalog := Catalog()
	if !catalog["QUEUED"].CanCancel {
		t.Fatalf("queued should be cancelable")
	}
	if !catalog["SUCCESS"].CanDownload {
		t.Fatalf("success should be downloadable")
	}
}
