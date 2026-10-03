package objectstore

import "testing"

func TestS3ObjectKeyAppliesPrefix(t *testing.T) {
	store := &S3Store{prefix: normalizeKey("/firecortex/prod/")}

	if got := store.objectKey("/user_1/2026/05/file.txt"); got != "firecortex/prod/user_1/2026/05/file.txt" {
		t.Fatalf("unexpected object key: %q", got)
	}
}

func TestS3ObjectKeyWithoutPrefix(t *testing.T) {
	store := &S3Store{}

	if got := store.objectKey("/user_1/2026/05/file.txt"); got != "user_1/2026/05/file.txt" {
		t.Fatalf("unexpected object key: %q", got)
	}
}
