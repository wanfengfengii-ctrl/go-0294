package domain

import "testing"

func raw(id string) MaterialNode {
	return MaterialNode{ID: id, Kind: KindRawGlass, FurnaceLot: "LOT-1", Generation: 1}
}

func tempered(id string) MaterialNode {
	return MaterialNode{ID: id, Kind: KindTempered, FurnaceLot: "LOT-1", Generation: 1}
}

func laminated(id string) MaterialNode {
	return MaterialNode{ID: id, Kind: KindLaminated, FurnaceLot: "LOT-1", Generation: 1}
}

func TestLineageValidProgression(t *testing.T) {
	l := NewLineage()
	if err := l.AddNode(raw("r1")); err != nil {
		t.Fatal(err)
	}
	if err := l.AddNode(tempered("t1")); err != nil {
		t.Fatal(err)
	}
	if err := l.AddNode(laminated("l1")); err != nil {
		t.Fatal(err)
	}
	if err := l.AddEdge("r1", "t1"); err != nil {
		t.Fatalf("raw->tempered rejected: %v", err)
	}
	if err := l.AddEdge("t1", "l1"); err != nil {
		t.Fatalf("tempered->laminated rejected: %v", err)
	}
}

func TestLineageIllegalSkipRejected(t *testing.T) {
	l := NewLineage()
	_ = l.AddNode(raw("r1"))
	_ = l.AddNode(laminated("l1"))
	if err := l.AddEdge("r1", "l1"); err == nil {
		t.Fatal("raw->laminated must be rejected")
	}
}

func TestLineageDuplicateEdgeIdempotent(t *testing.T) {
	l := NewLineage()
	_ = l.AddNode(raw("r1"))
	_ = l.AddNode(tempered("t1"))
	if err := l.AddEdge("r1", "t1"); err != nil {
		t.Fatal(err)
	}
	if err := l.AddEdge("r1", "t1"); err != nil {
		t.Fatalf("duplicate edge must be idempotent: %v", err)
	}
	if len(l.Edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(l.Edges))
	}
}

func TestLineageDuplicateNodeRejected(t *testing.T) {
	l := NewLineage()
	if err := l.AddNode(raw("r1")); err != nil {
		t.Fatal(err)
	}
	if err := l.AddNode(raw("r1")); err == nil {
		t.Fatal("duplicate identity must be rejected")
	}
}

func TestStageOrdering(t *testing.T) {
	var p Prefix
	if _, err := p.Complete(StageTemper); err == nil {
		t.Fatal("temper before edge confirm must fail")
	}
	p, err := p.Complete(StageEdgeConfirm)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Has(StageEdgeConfirm) {
		t.Fatal("edge confirm not recorded")
	}
	if _, err := p.Complete(StageEdgeConfirm); err == nil {
		t.Fatal("duplicate stage must fail")
	}
	if _, err := p.Complete(StageTemper); err != nil {
		t.Fatalf("temper after edge confirm must pass: %v", err)
	}
}
