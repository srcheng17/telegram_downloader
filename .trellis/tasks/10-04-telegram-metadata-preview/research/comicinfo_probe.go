// Run explicitly with go run; this research probe does not create a task or CBZ.
package main

import (
	"encoding/json"
	"encoding/xml"
	"flag"
	"fmt"
	"os"

	"github.com/ryancheng/telegram-downloader/internal/app/tasks"
	"github.com/ryancheng/telegram-downloader/internal/downloader"
)

func optional(fields map[string]string, key string) *string {
	value, ok := fields[key]
	if !ok {
		return nil
	}
	return &value
}

func text(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func run(input, output string) error {
	if input == "" || output == "" {
		return fmt.Errorf("input and output paths are required")
	}
	data, err := os.ReadFile(input)
	if err != nil {
		return fmt.Errorf("cannot read preview input")
	}
	var preview struct {
		Outcome    string `json:"outcome"`
		Candidates []struct {
			Metadata map[string]string `json:"metadata"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(data, &preview); err != nil {
		return fmt.Errorf("invalid preview JSON")
	}
	if preview.Outcome != "ready" || len(preview.Candidates) != 1 {
		return fmt.Errorf("expected one unambiguous preview candidate")
	}
	fields := preview.Candidates[0].Metadata
	normalized := tasks.NormalizeMetadata(tasks.MetadataInput{
		Author: optional(fields, "author"), ComicName: optional(fields, "comic_name"),
		SeriesName: optional(fields, "series_name"), SeriesNumber: optional(fields, "series_number"),
		Summary: optional(fields, "summary"), TagsRaw: optional(fields, "tags"), GenresRaw: optional(fields, "genres"),
	})
	metadata := downloader.TaskMetadata{
		Writer: text(normalized.Author), Title: text(normalized.ComicName),
		Series: text(normalized.SeriesName), Number: text(normalized.SeriesNumber),
		Summary: text(normalized.Summary), Tags: text(normalized.TagsNormalized), Genre: text(normalized.GenresNormalized),
	}
	encoded, err := downloader.WriteComicInfoXML(metadata)
	if err != nil {
		return fmt.Errorf("ComicInfo generation failed")
	}
	var decoded downloader.TaskMetadata
	if err := xml.Unmarshal(encoded, &decoded); err != nil || decoded != metadata {
		return fmt.Errorf("ComicInfo field roundtrip failed")
	}
	file, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("cannot create private output; use a new path")
	}
	_, writeErr := file.Write(encoded)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		return fmt.Errorf("cannot write private output")
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{
		"xml_roundtrip_valid": true,
		"writer_present":      metadata.Writer != "", "title_present": metadata.Title != "",
		"summary_present": metadata.Summary != "", "tags_present": metadata.Tags != "",
		"series_present": metadata.Series != "", "number_present": metadata.Number != "", "genre_present": metadata.Genre != "",
	})
}

func main() {
	input := flag.String("input", "", "private preview JSON path")
	output := flag.String("output", "", "new private ComicInfo XML path")
	flag.Parse()
	if err := run(*input, *output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
