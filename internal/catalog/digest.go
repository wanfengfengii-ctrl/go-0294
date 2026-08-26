package catalog

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"curtainwall.example/assembly-gate/internal/domain"
)

// Digest computes a deterministic rule digest over the canonical JSON of the
// snapshot's frozen fields. The digest intentionally excludes the mutable
// RuleDigest and LockedGen so the same construction always yields the same
// summary; map keys are sorted before hashing so ordering cannot change it.
func Digest(s domain.DesignSnapshot) string {
	canon := struct {
		Project, FacadeZone, PlateNumber string
		Version                          int
		ThicknessUM, WidthUM, HeightUM   int64
		EdgeMarginUM                     int64
		EdgeScheme                       string
		FurnaceLot, FilmBatch            string
		FilmOpeningUM2                   int64
		Geometry                         domain.Polygon
		Thresholds                       map[string]int64
		Rack                             domain.RackPlan
		Inspection                       domain.InspectionPlan
		Programs                         []string
	}{
		Project: s.Project, FacadeZone: s.FacadeZone, PlateNumber: s.PlateNumber,
		Version: s.Version, ThicknessUM: s.ThicknessUM, WidthUM: s.WidthUM,
		HeightUM: s.HeightUM, EdgeMarginUM: s.EdgeMarginUM, EdgeScheme: s.EdgeScheme,
		FurnaceLot: s.FurnaceLot, FilmBatch: s.FilmBatch, FilmOpeningUM2: s.FilmOpeningUM2,
		Geometry:   s.Geometry,
		Thresholds: s.Thresholds, Rack: s.Rack, Inspection: s.Inspection,
		Programs: s.Programs,
	}
	// Canonicalize map keys and adjacency pairs for a stable byte stream.
	canon.Thresholds = sortedMap(canon.Thresholds)
	canon.Rack.Adjacency = sortedAdjacency(canon.Rack.Adjacency)
	canon.Inspection.Sampling = sortedStringMap(canon.Inspection.Sampling)
	raw, _ := json.Marshal(canon)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:16])
}

func sortedMap(m map[string]int64) map[string]int64 {
	if m == nil {
		return nil
	}
	out := make(map[string]int64, len(m))
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		out[k] = m[k]
	}
	return out
}

func sortedStringMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		out[k] = m[k]
	}
	return out
}

func sortedAdjacency(pairs []domain.AdjacencyPair) []domain.AdjacencyPair {
	if pairs == nil {
		return nil
	}
	out := make([]domain.AdjacencyPair, len(pairs))
	copy(out, pairs)
	sort.Slice(out, func(i, j int) bool {
		if out[i].A != out[j].A {
			return out[i].A < out[j].A
		}
		return out[i].B < out[j].B
	})
	return out
}
