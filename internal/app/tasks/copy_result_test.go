package tasks

import (
	"context"
	"errors"
	"testing"
)

type fakeCopyTaskStore struct {
	task   *CopyTaskRecord
	getErr error
}

func (f *fakeCopyTaskStore) GetTaskForCopy(_ context.Context, taskID string) (*CopyTaskRecord, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	if f.task == nil {
		return nil, nil
	}
	copy := *f.task
	copy.ID = taskID
	return &copy, nil
}

type fakeArtifactDescriber struct {
	fileName string
	err      error
}

func (f fakeArtifactDescriber) DescribeArtifact(_ string) (ArtifactDescriptor, error) {
	if f.err != nil {
		return ArtifactDescriptor{}, f.err
	}
	return ArtifactDescriptor{FileName: f.fileName}, nil
}

type fakeResultCopier struct {
	sourcePath string
	fileName   string
	seriesName string
	targetPath string
	err        error
}

func (f *fakeResultCopier) CopyFromPath(sourcePath, fileName, seriesName string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	f.sourcePath = sourcePath
	f.fileName = fileName
	f.seriesName = seriesName
	return f.targetPath, nil
}

func TestCopyResultServiceCopiesSuccessfulTaskIntoKomga(t *testing.T) {
	store := &fakeCopyTaskStore{
		task: &CopyTaskRecord{
			Status:        StatusSuccess,
			ResultZipPath: stringPtr("downloaded_images/demo.cbz"),
			SeriesName:    stringPtr("系列A"),
		},
	}
	copier := &fakeResultCopier{targetPath: "/komga/系列A/demo.cbz"}
	svc := NewCopyResultService(store, fakeArtifactDescriber{fileName: "demo.cbz"}, copier)

	result, err := svc.CopyToKomga(context.Background(), "task-copy")
	if err != nil {
		t.Fatalf("CopyToKomga returned error: %v", err)
	}
	if result.TaskID != "task-copy" || result.TargetPath != "/komga/系列A/demo.cbz" {
		t.Fatalf("unexpected copy result %#v", result)
	}
	if copier.sourcePath != "downloaded_images/demo.cbz" || copier.fileName != "demo.cbz" || copier.seriesName != "系列A" {
		t.Fatalf("unexpected copier state %#v", copier)
	}
}

func TestCopyResultServiceRejectsIncompleteTask(t *testing.T) {
	store := &fakeCopyTaskStore{
		task: &CopyTaskRecord{
			Status:        StatusRunning,
			ResultZipPath: stringPtr("downloaded_images/demo.cbz"),
		},
	}
	svc := NewCopyResultService(store, fakeArtifactDescriber{fileName: "demo.cbz"}, &fakeResultCopier{})

	_, err := svc.CopyToKomga(context.Background(), "task-running")
	if !errors.Is(err, ErrTaskResultNotReady) {
		t.Fatalf("expected ErrTaskResultNotReady, got %v", err)
	}
}

