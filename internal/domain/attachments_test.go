package domain

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"GoTodo/internal/storage"
)

func setAttachmentSettings(t *testing.T, enabled bool, maxBytes int64) {
	t.Helper()
	pool, err := storage.OpenDatabase()
	if err != nil {
		t.Fatal(err)
	}
	defer storage.CloseDatabase(pool)
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO site_settings (id, attachments_enabled, attachment_max_bytes) VALUES (1, $1, $2)
		ON CONFLICT (id) DO UPDATE SET attachments_enabled = $1, attachment_max_bytes = $2`, enabled, maxBytes); err != nil {
		t.Fatal(err)
	}
}

func TestTaskAttachments(t *testing.T) {
	ctx := context.Background()
	t.Cleanup(func() { setAttachmentSettings(t, false, 0) })

	proj, err := CreateProject(ctx, 1, "Attachment Project", "")
	if err != nil {
		t.Fatal(err)
	}
	pid := proj.ID
	if err := upsertTestMember(t, pid, 2, testRoleEditor); err != nil {
		t.Fatal(err)
	}
	taskID := mustCreateTask(t, 1, CreateTaskInput{Title: "Has files", ProjectID: &pid})
	small := []byte("hello, attachment")

	setAttachmentSettings(t, false, 0)
	if _, err := UploadTaskAttachmentForUser(ctx, 2, taskID, "a.txt", small); !errors.Is(err, ErrValidation) {
		t.Fatalf("upload while disabled: %v", err)
	}

	setAttachmentSettings(t, true, 16<<10)
	if _, err := UploadTaskAttachmentForUser(ctx, 2, taskID, "big.bin", bytes.Repeat([]byte("x"), 20<<10)); !errors.Is(err, ErrValidation) {
		t.Fatalf("oversized upload: %v", err)
	}
	if _, err := UploadTaskAttachmentForUser(ctx, 3, taskID, "a.txt", small); !errors.Is(err, ErrNotFound) {
		t.Fatalf("non-member upload: %v", err)
	}

	mine, err := UploadTaskAttachmentForUser(ctx, 2, taskID, `..\..\notes".txt`, small)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if mine.FileName != "notes.txt" || mine.SizeBytes != int64(len(small)) || mine.ContentType == "" {
		t.Fatalf("stored attachment = %+v", mine)
	}
	got, data, err := GetTaskAttachmentForUser(ctx, 1, taskID, mine.ID)
	if err != nil || got.ID != mine.ID || !bytes.Equal(data, small) {
		t.Fatalf("download: %+v %q %v", got, data, err)
	}
	if _, _, err := GetTaskAttachmentForUser(ctx, 3, taskID, mine.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("non-member download: %v", err)
	}
	other := mustCreateTask(t, 1, CreateTaskInput{Title: "Other", ProjectID: &pid})
	if _, _, err := GetTaskAttachmentForUser(ctx, 1, other, mine.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("attachment read through another task: %v", err)
	}

	owners, err := UploadTaskAttachmentForUser(ctx, 1, taskID, "plan.txt", small)
	if err != nil {
		t.Fatal(err)
	}
	if err := DeleteTaskAttachmentForUser(ctx, 2, taskID, owners.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("editor deleting the owner's file: %v", err)
	}
	if err := DeleteTaskAttachmentForUser(ctx, 2, taskID, mine.ID); err != nil {
		t.Fatalf("editor deleting own file: %v", err)
	}
	if err := DeleteTaskAttachmentForUser(ctx, 1, taskID, owners.ID); err != nil {
		t.Fatalf("owner deleting: %v", err)
	}
	list, err := ListTaskAttachmentsForUser(ctx, 2, taskID)
	if err != nil || len(list) != 0 {
		t.Fatalf("remaining = %+v, %v", list, err)
	}
}

func TestCleanAttachmentName(t *testing.T) {
	cases := map[string]string{
		"report.pdf":         "report.pdf",
		"../../etc/passwd":   "passwd",
		`C:\Users\me\a.docx`: "a.docx",
		"  ":                 "file",
		"bad\x00\nname.txt":  "badname.txt",
	}
	for in, want := range cases {
		if got := cleanAttachmentName(in); got != want {
			t.Errorf("cleanAttachmentName(%q) = %q, want %q", in, got, want)
		}
	}
}
