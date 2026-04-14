package services_test

import (
	"reflect"
	"testing"

	"ritual/internal/core/domain"
	"ritual/internal/core/services"
)

func TestMark_EmptyList(t *testing.T) {
	got := services.Mark(nil, domain.RetentionRules{KeepLast: 5}, services.ParseTimestampDir)
	if len(got) != 0 {
		t.Errorf("empty input: got %v, want empty", got)
	}
}

func TestMark_AllTiersZero_DeletesAll(t *testing.T) {
	keys := []string{
		"backups/20260414160000/",
		"backups/20260413160000/",
		"backups/20260412160000/",
	}
	got := services.Mark(keys, domain.RetentionRules{}, services.ParseTimestampDir)
	if len(got) != 3 {
		t.Errorf("got %d deletions, want 3. got=%v", len(got), got)
	}
}

func TestMark_UnparseableKeysDeleted(t *testing.T) {
	keys := []string{
		"backups/20260414160000/",
		"backups/garbage.txt",
		"backups/manual.tar",
	}
	got := services.Mark(keys, domain.RetentionRules{KeepLast: 5}, services.ParseTimestampDir)
	if !reflect.DeepEqual(got, []string{"backups/garbage.txt", "backups/manual.tar"}) {
		t.Errorf("got %v, want unparseables only", got)
	}
}
