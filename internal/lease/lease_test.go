package lease

import "testing"

func TestLeaseOverlapConflict(t *testing.T) {
	r := NewRegistry()
	if err := r.Acquire(Lease{ResourceKey: "rack-F1", Holder: "op1", Start: 0, End: 100}); err != nil {
		t.Fatal(err)
	}
	err := r.Acquire(Lease{ResourceKey: "rack-F1", Holder: "op2", Start: 50, End: 150})
	if err == nil {
		t.Fatal("overlapping lease must be rejected")
	}
}

func TestLeaseNonOverlapAllowed(t *testing.T) {
	r := NewRegistry()
	if err := r.Acquire(Lease{ResourceKey: "rack-F1", Holder: "op1", Start: 0, End: 100}); err != nil {
		t.Fatal(err)
	}
	if err := r.Acquire(Lease{ResourceKey: "rack-F1", Holder: "op2", Start: 100, End: 200}); err != nil {
		t.Fatalf("adjacent non-overlapping lease rejected: %v", err)
	}
}

func TestLeaseExpiryAndCheckWrite(t *testing.T) {
	r := NewRegistry()
	if err := r.Acquire(Lease{ResourceKey: "furnace-1", Holder: "op1", Start: 10, End: 100}); err != nil {
		t.Fatal(err)
	}
	if err := r.CheckWrite("furnace-1", "op1", 50); err != nil {
		t.Fatalf("in-window write rejected: %v", err)
	}
	r.Expire(100)
	if err := r.CheckWrite("furnace-1", "op1", 100); err == nil {
		t.Fatal("expired lease must not allow writes")
	}
}

func TestLeaseInvalidIntervalRejected(t *testing.T) {
	r := NewRegistry()
	if err := r.Acquire(Lease{ResourceKey: "rack", Holder: "op1", Start: 100, End: 50}); err == nil {
		t.Fatal("negative-length interval must be rejected")
	}
}
