package film

import "testing"

func TestFilmConservationInvariant(t *testing.T) {
	l, err := NewLedger("FILM-1", 1000000)
	if err != nil {
		t.Fatal(err)
	}
	posts := []Entry{
		{Kind: EntryIssue, Amount: 300000},
		{Kind: EntryCut, Amount: 200000},
		{Kind: EntrySample, Amount: 50000},
		{Kind: EntryLoss, Amount: 10000},
		{Kind: EntryRecycle, Amount: 40000},
	}
	for _, p := range posts {
		if err := l.Account.Apply(p); err != nil {
			t.Fatalf("post %v failed: %v", p, err)
		}
		if !l.Balanced() {
			t.Fatalf("conservation broken after %v: opening=%d sum=%d", p, l.Opening, l.Account.Opening())
		}
	}
	if l.Opening != 1000000 {
		t.Fatalf("opening changed: %d", l.Opening)
	}
}

func TestFilmInsufficientAreaRejected(t *testing.T) {
	l, _ := NewLedger("FILM-2", 100)
	err := l.Account.Apply(Entry{Kind: EntryIssue, Amount: 101})
	if err == nil {
		t.Fatal("over-issue must be rejected")
	}
	if l.Account.Available != 100 {
		t.Fatalf("balance changed on rejection: %d", l.Account.Available)
	}
}

func TestFilmEntryAmountMustBePositive(t *testing.T) {
	l, _ := NewLedger("FILM-3", 100)
	if err := l.Account.Apply(Entry{Kind: EntryIssue, Amount: 0}); err == nil {
		t.Fatal("zero amount must be rejected")
	}
}
