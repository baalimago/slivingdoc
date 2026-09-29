package notebook

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/baalimago/slivingdoc/internal/storage/fake"
)

func TestStatusListsLocalChanges(t *testing.T) {
	nb, w, _ := newNotebook(t, nbConfig{store: fake.New(""), ids: &testIDSource{}})
	writeLocal(t, w, map[string]string{"a.md": "one\ntwo\n", "gone.md": "x\n", "same.md": "s\n"})
	pullOK(t, nb)
	commitOK(t, nb, "first")

	writeLocal(t, w, map[string]string{"a.md": "one\nthree\nfour\n", "new.md": "n\n", "empty.md": ""})
	removeLocal(t, w, "gone.md")

	st, err := nb.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() = %v", err)
	}
	want := []Change{
		{Path: "a.md", Kind: ChangeModified, Insertions: 2, Deletions: 1},
		{Path: "empty.md", Kind: ChangeAdded},
		{Path: "gone.md", Kind: ChangeDeleted, Deletions: 1},
		{Path: "new.md", Kind: ChangeAdded, Insertions: 1},
	}
	if st.Generation != 1 || !st.Pulled || st.RecoveryRequired || !reflect.DeepEqual(st.Changes, want) {
		t.Fatalf("Status() = %+v, want generation 1 pulled with changes %+v", st, want)
	}
}

func TestStatusRejectsInvalidVisibleContent(t *testing.T) {
	nb, w, _ := newNotebook(t, nbConfig{store: fake.New(""), ids: &testIDSource{}})
	pullOK(t, nb)
	writeLocal(t, w, map[string]string{"bin.dat": "a\x00b"})
	_, err := nb.Status(context.Background())
	var ne *Error
	if !errors.As(err, &ne) || ne.Reason != ReasonInvalidContent {
		t.Fatalf("Status() = %v, want INVALID_CONTENT", err)
	}
}

func TestLogListsPublicationsNewestFirst(t *testing.T) {
	nb, w, _ := newNotebook(t, nbConfig{store: fake.New(""), ids: &testIDSource{}})
	h, err := nb.Log(context.Background(), 5)
	if err != nil || len(h.Entries) != 0 || h.More {
		t.Fatalf("Log() before any publication = %+v, %v, want empty", h, err)
	}
	pullOK(t, nb)
	for _, m := range []string{"first", "second", "third"} {
		writeLocal(t, w, map[string]string{"f.md": m})
		commitOK(t, nb, m)
	}

	h, err = nb.Log(context.Background(), 5)
	if err != nil {
		t.Fatalf("Log() = %v", err)
	}
	want := []LogEntry{{"third"}, {"second"}, {"first"}}
	if !reflect.DeepEqual(h.Entries, want) || h.More {
		t.Fatalf("Log(5) = %+v, want %v and no more", h, want)
	}
	h, err = nb.Log(context.Background(), 2)
	if err != nil || len(h.Entries) != 2 || !h.More {
		t.Fatalf("Log(2) = %+v, %v, want two entries and more", h, err)
	}
	if _, err := nb.Log(context.Background(), 0); err == nil {
		t.Fatal("Log(0) = nil, want a refusal")
	}
}
