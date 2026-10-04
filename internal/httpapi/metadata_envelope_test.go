package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDownloadJSONRejectsDuplicateEnvelopeAndNestedDocumentKeys(t *testing.T) {
	cases := []string{
		`{"url":"https://telegra.ph/one","url":"https://telegra.ph/two"}`,
		`{"url":"https://telegra.ph/one","metadata_document":{"schema_version":1,"definitions_version":"standard-v1","revision":0,"revision":1,"fields":{},"definition_snapshot":{}}}`,
		`{"url":"https://telegra.ph/one","metadata_document":{"schema_version":1,"definitions_version":"standard-v1","revision":0,"fields":{"title":{"state":"value","value":"one","value":"two","revision":0,"manual_locked":false,"provenance":[]}},"definition_snapshot":{}}}`,
		`null`,
	}
	for _, body := range cases {
		t.Run(body, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/download", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			if _, _, _, err := extractDownloadRequest(req); err == nil {
				t.Fatal("ambiguous JSON accepted")
			}
		})
	}
}

func TestUploadJSONRejectsDuplicateKeysBeforeCallingTaskService(t *testing.T) {
	for _, body := range []string{`{"file_name":"one.zip","file_name":"two.zip"}`, `{"file_name":"one.zip","file_size":1,"file_size":2}`, `{"file_name":"one.zip","metadata_document":{"schema_version":1,"definitions_version":"standard-v1","revision":0,"revision":1,"fields":{},"definition_snapshot":{}}}`} {
		svc := &fakeTaskCoreHTTPService{}
		router := newTaskCoreTestRouter(t, svc)
		req := httptest.NewRequest(http.MethodPost, "/api/tasks/upload/init", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status=%d want=400 body=%s", rec.Code, rec.Body.String())
		}
	}
}
