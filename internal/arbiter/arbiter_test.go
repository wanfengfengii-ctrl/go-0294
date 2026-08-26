package arbiter

import "testing"

func TestReviewsRequireTwoDistinctQualified(t *testing.T) {
	if Reviews([]Review{{Reviewer: "a", Qualified: true, Generation: 1}}, 1) {
		t.Fatal("single review must not satisfy the gate")
	}
	if Reviews([]Review{
		{Reviewer: "a", Qualified: true, Generation: 1},
		{Reviewer: "a", Qualified: true, Generation: 1},
	}, 1) {
		t.Fatal("same reviewer twice must not satisfy the gate")
	}
	if Reviews([]Review{
		{Reviewer: "a", Qualified: true, Generation: 1},
		{Reviewer: "b", Qualified: false, Generation: 1},
	}, 1) {
		t.Fatal("unqualified reviewer must not satisfy the gate")
	}
	if !Reviews([]Review{
		{Reviewer: "a", Qualified: true, Generation: 1},
		{Reviewer: "b", Qualified: true, Generation: 1},
	}, 1) {
		t.Fatal("two distinct qualified reviewers must satisfy the gate")
	}
}

func TestBarrierAdmitMintsCredential(t *testing.T) {
	b := &FinalBarrier{}
	if err := b.Decide(VerdictAdmit, "task-1", 3); err != nil {
		t.Fatal(err)
	}
	if !b.Decided || b.Verdict != VerdictAdmit || b.Credential == "" {
		t.Fatalf("admission must be decided with a credential: %+v", b)
	}
}

func TestBarrierSingleWrite(t *testing.T) {
	b := &FinalBarrier{}
	if err := b.Decide(VerdictIsolate, "task-1", 3); err != nil {
		t.Fatal(err)
	}
	if err := b.Decide(VerdictAdmit, "task-1", 3); err == nil {
		t.Fatal("second verdict must be rejected")
	}
	if b.Credential != "" {
		t.Fatal("isolate verdict must not mint a credential")
	}
}
