package tasks

type StatusMeta struct {
	Code        string `json:"code"`
	Label       string `json:"label"`
	CanCancel   bool   `json:"can_cancel"`
	CanDownload bool   `json:"can_download"`
}

type MetadataInput struct {
	Author     *string
	SeriesName *string
	ComicName  *string
	Summary    *string
	TagsRaw    *string
	GenresRaw  *string
}

type Metadata struct {
	Author           *string
	SeriesName       *string
	ComicName        *string
	Summary          *string
	TagsRaw          *string
	TagsNormalized   *string
	GenresRaw        *string
	GenresNormalized *string
}
