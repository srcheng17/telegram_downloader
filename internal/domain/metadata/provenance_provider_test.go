package metadata

import "testing"

func TestProviderProvenanceBounds(t *testing.T) {
	valid := Provenance{Kind: "provider", SourceID: "bangumi", RecordID: "123", PublicURL: "https://bgm.tv/subject/123", RetrievedAt: "2026-10-05T00:00:00Z", SourceField: "infobox.ISBN", Attribution: "Bangumi CC BY-SA 3.0", LicenseURL: "https://bgm.tv/about/copyright"}
	if err := validateProvenance([]Provenance{valid}); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Provenance){func(p *Provenance) { p.RetrievedAt = "yesterday" }, func(p *Provenance) { p.LicenseURL = "javascript:bad" }, func(p *Provenance) { p.SourceField = "bad\x00field" }, func(p *Provenance) { p.Attribution = "bad\x00license" }} {
		bad := valid
		mutate(&bad)
		if validateProvenance([]Provenance{bad}) == nil {
			t.Fatal("invalid attribution accepted")
		}
	}
}
