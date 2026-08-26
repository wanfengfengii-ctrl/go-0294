package store

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"curtainwall.example/assembly-gate/internal/arbiter"
	"curtainwall.example/assembly-gate/internal/catalog"
	"curtainwall.example/assembly-gate/internal/domain"
	"curtainwall.example/assembly-gate/internal/evidence"
	"curtainwall.example/assembly-gate/internal/film"
	"curtainwall.example/assembly-gate/internal/instrument"
	"curtainwall.example/assembly-gate/internal/lease"
)

// idemRecord binds an idempotency operation id to the task it committed
// against, plus its committed request and response digests. The task binding
// prevents an operation id reused across two different plates from being
// mistaken for a replay of an already-completed stage on the current plate.
type idemRecord struct {
	TaskID        string
	RequestDigest string
	Response      string
}

// state is the in-memory aggregate graph shared by the SQLite and memory
// stores. It holds every domain aggregate and the single source of truth for
// business-flow orchestration.
type state struct {
	tasks   []*Task
	byID    map[string]*Task
	catalog *catalog.Catalog
	film    *film.Manager
	leases  *lease.Registry
	calls   map[string]*instrument.Call
	retests map[string]*arbiter.RetestSet
	idem    map[string]idemRecord
	seq     int64
	clock   int64 // highest logical time observed; drives lease expiry
	// internal task fields not serialized to the API payload.
	prefixes        map[string]domain.Prefix
	heatSamples     map[string][]evidence.SamplePoint
	autoSamples     map[string][]evidence.SamplePoint
	barriers        map[string]*arbiter.FinalBarrier
	retestClosed    map[string]bool
	destructivePass map[string]bool
	retestGen       map[string]int
}

func newState() *state {
	return &state{
		byID:            make(map[string]*Task),
		catalog:         catalog.New(),
		film:            film.NewManager(),
		leases:          lease.NewRegistry(),
		calls:           make(map[string]*instrument.Call),
		retests:         make(map[string]*arbiter.RetestSet),
		idem:            make(map[string]idemRecord),
		prefixes:        make(map[string]domain.Prefix),
		heatSamples:     make(map[string][]evidence.SamplePoint),
		autoSamples:     make(map[string][]evidence.SamplePoint),
		barriers:        make(map[string]*arbiter.FinalBarrier),
		retestClosed:    make(map[string]bool),
		destructivePass: make(map[string]bool),
		retestGen:       make(map[string]int),
	}
}

func (s *state) clone() *state {
	out := newState()
	out.tasks = append(out.tasks, s.tasks...)
	for k, v := range s.byID {
		out.byID[k] = v
	}
	out.catalog = s.catalog.Clone()
	out.film = s.film.Clone()
	out.leases = s.leases.Clone()
	for k, v := range s.calls {
		cp := *v
		out.calls[k] = &cp
	}
	for k, v := range s.retests {
		out.retests[k] = v
	}
	for k, v := range s.idem {
		out.idem[k] = v
	}
	out.seq = s.seq
	out.clock = s.clock
	for k, v := range s.prefixes {
		out.prefixes[k] = v
	}
	for k, v := range s.heatSamples {
		out.heatSamples[k] = append([]evidence.SamplePoint(nil), v...)
	}
	for k, v := range s.autoSamples {
		out.autoSamples[k] = append([]evidence.SamplePoint(nil), v...)
	}
	for k, v := range s.barriers {
		cp := *v
		out.barriers[k] = &cp
	}
	for k, v := range s.retestClosed {
		out.retestClosed[k] = v
	}
	for k, v := range s.destructivePass {
		out.destructivePass[k] = v
	}
	for k, v := range s.retestGen {
		out.retestGen[k] = v
	}
	return out
}

func (s *state) nextSeq() int64 {
	s.seq++
	return s.seq
}

// tick advances the logical clock to at least t and expires any lease whose
// end has passed.
func (s *state) tick(t int64) {
	if t > s.clock {
		s.clock = t
	}
	s.leases.Expire(s.clock)
}

func digestString(v string) string {
	sum := sha256.Sum256([]byte(v))
	return hex.EncodeToString(sum[:8])
}

// LockDesign validates and locks a design snapshot, producing a new task.
func (s *state) LockDesign(snap domain.DesignSnapshot) (*Task, error) {
	digest, gen, err := s.catalog.Lock(snap)
	if err != nil {
		return nil, err
	}
	snap.RuleDigest = digest
	snap.LockedGen = gen
	id := digestString(snap.Project + "|" + snap.FacadeZone + "|" + snap.PlateNumber)
	task := &Task{
		ID:           id,
		Snapshot:     snap,
		Generation:   gen,
		Lineage:      domain.NewLineage(),
		Completed:    []string{},
		Measurements: []domain.Measurement{},
		Reviews:      []arbiter.Review{},
	}
	s.tasks = append(s.tasks, task)
	s.byID[id] = task
	s.prefixes[id] = 0
	s.barriers[id] = &arbiter.FinalBarrier{}
	s.retestClosed[id] = false
	s.destructivePass[id] = false
	// Open the film batch account if the snapshot locks one.
	if snap.FilmBatch != "" {
		if _, err := s.film.EnsureBatch(snap.FilmBatch, snap.FilmOpeningUM2); err != nil {
			return nil, err
		}
	}
	// The raw glass node is born at lock time and is never removed.
	_ = task.Lineage.AddNode(domain.MaterialNode{
		ID: rawNodeID(id), Kind: domain.KindRawGlass,
		FurnaceLot: snap.FurnaceLot, Generation: gen,
	})
	return task, nil
}

func rawNodeID(taskID string) string { return "raw:" + taskID }
func temperedNodeID(taskID string, gen int) string {
	return fmt.Sprintf("tempered:%s:%d", taskID, gen)
}
func laminatedNodeID(taskID string, gen int) string {
	return fmt.Sprintf("laminated:%s:%d", taskID, gen)
}

func (s *state) getTask(id string) (*Task, error) {
	t, ok := s.byID[id]
	if !ok {
		return nil, ErrNotFound
	}
	return t, nil
}

func (s *state) publicTask(t *Task) *Task {
	// Completed stages are derived from the internal prefix on every read.
	out := *t
	out.Completed = completedStages(s.prefixes[t.ID])
	return &out
}

func completedStages(p domain.Prefix) []string {
	out := []string{}
	for st := domain.StageEdgeConfirm; st <= domain.StageFinal; st++ {
		if p.Has(st) {
			out = append(out, st.String())
		}
	}
	return out
}

func parseFilmEntry(req *FilmEntryRequest) (film.Entry, error) {
	if req == nil {
		return film.Entry{}, nil
	}
	var kind film.EntryKind
	switch req.Kind {
	case "issue":
		kind = film.EntryIssue
	case "cut":
		kind = film.EntryCut
	case "recycle":
		kind = film.EntryRecycle
	case "sample":
		kind = film.EntrySample
	case "loss":
		kind = film.EntryLoss
	default:
		return film.Entry{}, domain.NewError(domain.CodeInsufficientArea, "unknown film entry kind", req.Kind)
	}
	return film.Entry{Kind: kind, Amount: req.AmountUM2}, nil
}

func parseStageFromString(s string) (domain.Stage, bool) {
	for st := domain.StageEdgeConfirm; st <= domain.StageFinal; st++ {
		if st.String() == s {
			return st, true
		}
	}
	return 0, false
}

func requestCanonical(req OperationRequest) string {
	return fmt.Sprintf("%s|%d|%s|%s|%d|%d", req.Stage, req.Generation, req.Operator,
		req.ResourceKey, req.LeaseStart, req.LeaseEnd)
}

// stageRequiresLease reports the stages that must hold a resource lease.
func stageRequiresLease(st domain.Stage) bool {
	switch st {
	case domain.StageHeatSoak, domain.StageLamination, domain.StageAutoclave:
		return true
	default:
		return false
	}
}

// stageRequiresFilm reports the stages that must move film in the same
// transaction.
func stageRequiresFilm(st domain.Stage) bool {
	return st == domain.StageLamination || st == domain.StagePrepPress
}

// Advance pushes one manufacturing stage forward, atomically committing any
// film entry and resource lease together with the stage evidence.
func (s *state) Advance(taskID string, req OperationRequest) (*Task, error) {
	t, err := s.getTask(taskID)
	if err != nil {
		return nil, err
	}
	if err := s.catalog.ValidateDigest(t.Snapshot.Project, t.Snapshot.FacadeZone,
		t.Snapshot.PlateNumber, req.RuleDigest); err != nil {
		return nil, err
	}
	if req.Generation != t.Generation {
		return nil, domain.NewError(domain.CodeRetestGenerationConflict,
			"generation mismatch", fmt.Sprintf("%d", req.Generation))
	}
	if req.OperationID != "" {
		if rec, ok := s.idem[req.OperationID]; ok {
			// An operation id is globally unique. A replay must target the
			// exact task it was committed against: reusing the same id on a
			// different plate (even with identical content) is a conflict,
			// not an already-completed stage on this plate.
			if rec.TaskID != t.ID || rec.RequestDigest != digestString(requestCanonical(req)) {
				return nil, domain.NewError(domain.CodeIdempotencyConflict,
					"operation id reused across tasks or with different content").WithOperation(req.OperationID)
			}
			return s.publicTask(t), nil
		}
	}
	s.tick(req.LogicalTime)
	stage, ok := parseStageFromString(req.Stage)
	if !ok {
		return nil, domain.NewError(domain.CodeStageOutOfOrder, "unknown stage", req.Stage)
	}
	if stage >= domain.StageRetest {
		return nil, domain.NewError(domain.CodeStageOutOfOrder,
			"stage is managed by a dedicated endpoint", req.Stage)
	}

	p := s.prefixes[t.ID]
	newPrefix, err := p.Complete(stage)
	if err != nil {
		return nil, err
	}

	filmEntry, err := parseFilmEntry(req.FilmEntry)
	if err != nil {
		return nil, err
	}
	if stageRequiresFilm(stage) {
		if req.FilmEntry == nil {
			return nil, domain.NewError(domain.CodeInsufficientArea,
				"stage requires a film entry", stage.String())
		}
		if t.Snapshot.FilmBatch == "" {
			return nil, domain.NewError(domain.CodeInsufficientArea, "no film batch locked")
		}
		if err := s.film.Apply(t.Snapshot.FilmBatch, filmEntry); err != nil {
			return nil, err
		}
	}
	if stageRequiresLease(stage) {
		if req.ResourceKey == "" {
			return nil, domain.NewError(domain.CodeLeaseConflict,
				"stage requires a resource lease", stage.String())
		}
		if err := s.leases.Acquire(lease.Lease{
			ResourceKey: req.ResourceKey,
			Holder:      t.ID,
			Start:       req.LeaseStart,
			End:         req.LeaseEnd,
		}); err != nil {
			return nil, err
		}
	}

	s.prefixes[t.ID] = newPrefix
	s.growLineage(t, stage)
	if req.OperationID != "" {
		s.idem[req.OperationID] = idemRecord{TaskID: t.ID, RequestDigest: digestString(requestCanonical(req))}
	}
	return s.publicTask(t), nil
}

// growLineage appends the material node for a stage that creates a new
// material identity (tempered sheet at temper, laminated assembly at
// lamination), wiring the append-only raw -> tempered -> laminated edges.
func (s *state) growLineage(t *Task, stage domain.Stage) {
	switch stage {
	case domain.StageTemper:
		_ = t.Lineage.AddNode(domain.MaterialNode{
			ID: temperedNodeID(t.ID, t.Generation), Kind: domain.KindTempered,
			FurnaceLot: t.Snapshot.FurnaceLot, Generation: t.Generation,
		})
		_ = t.Lineage.AddEdge(rawNodeID(t.ID), temperedNodeID(t.ID, t.Generation))
	case domain.StageLamination:
		_ = t.Lineage.AddNode(domain.MaterialNode{
			ID: laminatedNodeID(t.ID, t.Generation), Kind: domain.KindLaminated,
			FurnaceLot: t.Snapshot.FurnaceLot, Generation: t.Generation,
		})
		_ = t.Lineage.AddEdge(temperedNodeID(t.ID, t.Generation), laminatedNodeID(t.ID, t.Generation))
	}
}

// SubmitSamples validates and records a heat-soak or autoclave sample batch,
// returning the derived coverage matrix or autoclave metrics.
func (s *state) SubmitSamples(taskID string, req SampleRequest) (SamplesResult, error) {
	t, err := s.getTask(taskID)
	if err != nil {
		return SamplesResult{}, err
	}
	if err := s.catalog.ValidateDigest(t.Snapshot.Project, t.Snapshot.FacadeZone,
		t.Snapshot.PlateNumber, req.RuleDigest); err != nil {
		return SamplesResult{}, err
	}
	if req.Generation != t.Generation {
		return SamplesResult{}, domain.NewError(domain.CodeRetestGenerationConflict,
			"generation mismatch", fmt.Sprintf("%d", req.Generation))
	}
	s.tick(maxSampleTime(req.Samples))
	switch req.Stage {
	case "heat_soak":
		orders, err := evidence.SegmentOrders(req.Samples, false)
		if err != nil {
			return SamplesResult{}, err
		}
		if err := evidence.ValidateContinuousPrefix(orders); err != nil {
			return SamplesResult{}, err
		}
		matrix, err := evidence.BuildCoverage(t.Snapshot.Rack, req.Samples)
		if err != nil {
			return SamplesResult{}, err
		}
		s.heatSamples[t.ID] = append(s.heatSamples[t.ID], req.Samples...)
		return SamplesResult{Coverage: matrix, FullyCovered: matrix.FullyCovered()}, nil
	case "autoclave":
		result, err := evidence.ComputeAutoclave(req.Samples)
		if err != nil {
			return SamplesResult{}, err
		}
		s.autoSamples[t.ID] = append(s.autoSamples[t.ID], req.Samples...)
		return SamplesResult{Autoclave: result}, nil
	default:
		return SamplesResult{}, domain.NewError(domain.CodeSampleGap, "unknown sample stage", req.Stage)
	}
}

// instrumentStage maps a device to the stage it advances on a qualified
// reading.
func instrumentStage(d instrument.Device) (domain.Stage, bool) {
	switch d {
	case instrument.DeviceStressMeter:
		return domain.StageStress, true
	case instrument.DeviceOptical:
		return domain.StageOpticalScan, true
	case instrument.DeviceDestructive:
		return domain.StageDestructive, true
	default:
		return 0, false
	}
}

// SubmitInstrumentCall runs a scripted device call. A failure only records a
// pending retry; a success records the measurement and advances the mapped
// stage.
func (s *state) SubmitInstrumentCall(taskID string, req InstrumentRequest, adapter instrument.Adapter) (InstrumentResult, error) {
	t, err := s.getTask(taskID)
	if err != nil {
		return InstrumentResult{}, err
	}
	if err := s.catalog.ValidateDigest(t.Snapshot.Project, t.Snapshot.FacadeZone,
		t.Snapshot.PlateNumber, req.RuleDigest); err != nil {
		return InstrumentResult{}, err
	}
	if req.Generation != t.Generation {
		return InstrumentResult{}, domain.NewError(domain.CodeRetestGenerationConflict,
			"generation mismatch", fmt.Sprintf("%d", req.Generation))
	}
	s.tick(req.LogicalTime)
	stage, ok := instrumentStage(req.Device)
	if !ok {
		return InstrumentResult{}, domain.NewError(domain.CodeDeviceFailure,
			"device does not advance a stage", string(req.Device))
	}
	return s.runDevice(t, stage, req, adapter, 0)
}

// runDevice executes a device call and applies the qualified reading or
// records a deterministic retry.
func (s *state) runDevice(t *Task, stage domain.Stage, req InstrumentRequest, adapter instrument.Adapter, attempt int) (InstrumentResult, error) {
	result := adapter.Run(req.Device, req.Payload)
	if result.Outcome != instrument.OutcomeOK || result.Reading == nil || !result.Reading.WellFormed {
		call := &instrument.Call{
			ID:          digestString(fmt.Sprintf("%s|%d|%d", t.ID, req.LogicalTime, s.nextSeq())),
			TaskID:      t.ID,
			Device:      req.Device,
			Payload:     req.Payload,
			Attempt:     attempt,
			LogicalTime: req.LogicalTime,
			NextTime:    instrument.NextRetryTime(attempt, req.LogicalTime),
			Status:      "pending",
		}
		if instrument.Exhausted(attempt) {
			call.Status = "exhausted"
		}
		s.calls[call.ID] = call
		return InstrumentResult{Call: call}, nil
	}

	// Qualified reading: record the measurement and advance the mapped stage.
	reading := result.Reading
	t.Measurements = append(t.Measurements, *reading)
	if req.Device == instrument.DeviceDestructive {
		if reading.Value != 0 {
			s.destructivePass[t.ID] = false
			return InstrumentResult{Call: &instrument.Call{Status: "done"}, Task: s.publicTask(t)}, nil
		}
		s.destructivePass[t.ID] = true
	}
	p := s.prefixes[t.ID]
	newPrefix, err := p.Complete(stage)
	if err != nil {
		// A repeated qualified reading is idempotent; do not double-advance.
		if newPrefix == p {
			return InstrumentResult{Call: &instrument.Call{Status: "done", Reading: reading}, Task: s.publicTask(t)}, nil
		}
		return InstrumentResult{}, err
	}
	s.prefixes[t.ID] = newPrefix
	return InstrumentResult{
		Call: &instrument.Call{Status: "done", Reading: reading},
		Task: s.publicTask(t),
	}, nil
}

// RunRetry re-runs a pending instrument call with its fixed backoff.
func (s *state) RunRetry(callID string, adapter instrument.Adapter) (InstrumentResult, error) {
	call, ok := s.calls[callID]
	if !ok {
		return InstrumentResult{}, ErrNotFound
	}
	if call.Status != "pending" {
		return InstrumentResult{Call: call}, nil
	}
	t, err := s.getTask(call.TaskID)
	if err != nil {
		return InstrumentResult{}, err
	}
	s.tick(call.NextTime)
	stage, ok := instrumentStage(call.Device)
	if !ok {
		return InstrumentResult{}, domain.NewError(domain.CodeDeviceFailure, "unknown device")
	}
	result := adapter.Run(call.Device, call.Payload)
	if result.Outcome == instrument.OutcomeOK && result.Reading != nil && result.Reading.WellFormed {
		call.Status = "done"
		call.Reading = result.Reading
		t.Measurements = append(t.Measurements, *result.Reading)
		if call.Device == instrument.DeviceDestructive {
			s.destructivePass[t.ID] = result.Reading.Value == 0
		}
		p := s.prefixes[t.ID]
		if newPrefix, err := p.Complete(stage); err == nil {
			s.prefixes[t.ID] = newPrefix
		}
		return InstrumentResult{Call: call, Task: s.publicTask(t)}, nil
	}
	call.Attempt++
	if instrument.Exhausted(call.Attempt) {
		call.Status = "exhausted"
	} else {
		call.NextTime = instrument.NextRetryTime(call.Attempt, call.LogicalTime)
	}
	return InstrumentResult{Call: call}, nil
}

// RegisterAnomaly generates the deterministic retest scope and triggers rework:
// it bumps the generation and resets the process prefix so the new generation
// must re-run every stage, while keeping all prior evidence queryable.
func (s *state) RegisterAnomaly(taskID string, req AnomalyRequest) (*arbiter.RetestSet, error) {
	t, err := s.getTask(taskID)
	if err != nil {
		return nil, err
	}
	if err := s.catalog.ValidateDigest(t.Snapshot.Project, t.Snapshot.FacadeZone,
		t.Snapshot.PlateNumber, req.RuleDigest); err != nil {
		return nil, err
	}
	if req.Generation != t.Generation {
		return nil, domain.NewError(domain.CodeRetestGenerationConflict,
			"generation mismatch", fmt.Sprintf("%d", req.Generation))
	}
	trigger := domain.RetestScopeKey{
		FacadeZone: t.Snapshot.FacadeZone,
		Plate:      t.Snapshot.PlateNumber,
		RawGlass:   t.Snapshot.FurnaceLot,
		FurnaceRun: t.Snapshot.Rack.FurnaceRun,
		RackPos:    req.RackPos,
		Generation: req.Generation,
	}
	builder := arbiter.NewScopeBuilder(t.Snapshot, trigger, req.Kind)
	for _, other := range s.tasks {
		if other.ID == t.ID {
			continue
		}
		key := domain.RetestScopeKey{
			FacadeZone: other.Snapshot.FacadeZone, Plate: other.Snapshot.PlateNumber,
			RawGlass: other.Snapshot.FurnaceLot, FurnaceRun: other.Snapshot.Rack.FurnaceRun,
			Generation: other.Generation,
		}
		if other.Snapshot.FurnaceLot == t.Snapshot.FurnaceLot && t.Snapshot.FurnaceLot != "" {
			builder.AddMember(key, "furnace_lot")
		}
		if other.Snapshot.FilmBatch == t.Snapshot.FilmBatch && t.Snapshot.FilmBatch != "" {
			builder.AddMember(key, "film_generation")
		}
	}
	set := builder.Build()
	set.SummaryDigest = digestString(fmt.Sprintf("%s|%s", set.Kind, retestMemberDigest(set.Members)))

	// Rework: a new generation supersedes the old one; late receipts for the
	// old generation are now rejected by the generation check on every mutation.
	s.retests[t.ID] = set
	s.retestGen[t.ID] = t.Generation
	t.Generation++
	s.prefixes[t.ID] = 0
	s.retestClosed[t.ID] = false
	s.destructivePass[t.ID] = false
	return set, nil
}

func retestMemberDigest(members []domain.RetestMember) string {
	var acc string
	for _, m := range members {
		acc += m.Key.String() + "|" + m.Reason + ";"
	}
	return acc
}

// AddReview records one independent reviewer attestation.
func (s *state) AddReview(taskID string, req ReviewRequest) error {
	t, err := s.getTask(taskID)
	if err != nil {
		return err
	}
	if req.Generation != t.Generation {
		return domain.NewError(domain.CodeRetestGenerationConflict,
			"generation mismatch", fmt.Sprintf("%d", req.Generation))
	}
	for _, r := range t.Reviews {
		if r.Reviewer == req.Reviewer && r.Generation == req.Generation {
			return domain.NewError(domain.CodeFinalExists, "reviewer already reviewed this generation")
		}
	}
	t.Reviews = append(t.Reviews, arbiter.Review{
		Reviewer: req.Reviewer, Qualified: req.Qualified, Generation: req.Generation,
	})
	return nil
}

// SubmitVerdict arbitrates the terminal verdict after the closure and
// dual-review gates, committing through the single-write barrier.
func (s *state) SubmitVerdict(taskID string, req VerdictRequest) (*arbiter.VerdictResult, error) {
	t, err := s.getTask(taskID)
	if err != nil {
		return nil, err
	}
	if req.Generation != t.Generation {
		return nil, domain.NewError(domain.CodeRetestGenerationConflict,
			"generation mismatch", fmt.Sprintf("%d", req.Generation))
	}
	var verdict arbiter.Verdict
	switch req.Verdict {
	case "admit":
		verdict = arbiter.VerdictAdmit
	case "isolate":
		verdict = arbiter.VerdictIsolate
	case "cancel":
		verdict = arbiter.VerdictCancel
	default:
		return nil, domain.NewError(domain.CodeFinalExists, "unknown verdict", req.Verdict)
	}

	if err := arbiter.CheckClosure(s.closureRequirements(t)); err != nil {
		return nil, err
	}
	if err := s.barriers[t.ID].Decide(verdict, t.ID, t.Generation); err != nil {
		return nil, err
	}
	s.prefixes[t.ID], _ = s.prefixes[t.ID].Complete(domain.StageFinal)
	return &arbiter.VerdictResult{
		Verdict:    verdict,
		Credential: s.barriers[t.ID].Credential,
	}, nil
}

func (s *state) closureRequirements(t *Task) arbiter.ClosureRequirements {
	return arbiter.ClosureRequirements{
		LineageComplete: lineageComplete(t.Lineage),
		FilmConserved:   s.film.Reconcile() == nil,
		AllStagesClosed: allManufacturingStagesClosed(s.prefixes[t.ID]),
		MetricsPass:     s.metricsPass(t),
		DestructivePass: s.destructivePass[t.ID],
		RetestComplete:  s.retestComplete(t),
		Reviews:         t.Reviews,
		Generation:      t.Generation,
	}
}

func lineageComplete(l *domain.Lineage) bool {
	if l == nil {
		return false
	}
	hasRaw, hasTempered, hasLaminated := false, false, false
	for _, n := range l.Nodes {
		switch n.Kind {
		case domain.KindRawGlass:
			hasRaw = true
		case domain.KindTempered:
			hasTempered = true
		case domain.KindLaminated:
			hasLaminated = true
		}
	}
	return hasRaw && hasTempered && hasLaminated
}

func allManufacturingStagesClosed(p domain.Prefix) bool {
	for st := domain.StageEdgeConfirm; st <= domain.StageDestructive; st++ {
		if !p.Has(st) {
			return false
		}
	}
	return true
}

func (s *state) metricsPass(t *Task) bool {
	for _, m := range t.Measurements {
		switch m.Kind {
		case domain.MeasureSurfaceStress:
			if limit, ok := t.Snapshot.Thresholds["surface_stress"]; ok && m.Value < limit {
				return false
			}
		case domain.MeasureBow:
			if limit, ok := t.Snapshot.Thresholds["bow"]; ok && m.Value > limit {
				return false
			}
		case domain.MeasureBubbleRate:
			if limit, ok := t.Snapshot.Thresholds["bubble_rate"]; ok && m.Value > limit {
				return false
			}
		}
	}
	return true
}

func (s *state) retestComplete(t *Task) bool {
	gen, ok := s.retestGen[t.ID]
	if !ok {
		return true // no anomaly ever registered
	}
	return t.Generation > gen
}

// GetLineage returns the append-only material lineage for a task.
func (s *state) GetLineage(taskID string) (*domain.Lineage, error) {
	t, err := s.getTask(taskID)
	if err != nil {
		return nil, err
	}
	return t.Lineage, nil
}

// GetCoverage returns the current heat-soak coverage matrix for a task.
func (s *state) GetCoverage(taskID string) (*evidence.CoverageMatrix, error) {
	t, err := s.getTask(taskID)
	if err != nil {
		return nil, err
	}
	return evidence.BuildCoverage(t.Snapshot.Rack, s.heatSamples[t.ID])
}

// GetFilmLedger returns the conserved film ledger for a batch.
func (s *state) GetFilmLedger(batch string) (*film.Ledger, error) {
	l := s.film.Ledger(batch)
	if l == nil {
		return nil, ErrNotFound
	}
	return l, nil
}

// GetRetests returns the stored retest scope for a task, or nil.
func (s *state) GetRetests(taskID string) (*arbiter.RetestSet, error) {
	if _, err := s.getTask(taskID); err != nil {
		return nil, err
	}
	set, ok := s.retests[taskID]
	if !ok {
		return nil, nil
	}
	return set, nil
}

// GetCredential returns the admission credential for a task, if admitted.
func (s *state) GetCredential(id string) (*Credential, error) {
	for _, t := range s.tasks {
		b := s.barriers[t.ID]
		if b != nil && b.Decided && b.Verdict == arbiter.VerdictAdmit && b.Credential != "" {
			credID := digestString(t.ID)
			if id == credID {
				return &Credential{
					ID: credID, TaskID: t.ID, Generation: t.Generation, Value: b.Credential,
				}, nil
			}
		}
	}
	return nil, ErrNotFound
}

// PendingRetries returns every pending instrument call in deterministic order.
func (s *state) PendingRetries() []*instrument.Call {
	out := []*instrument.Call{}
	for _, c := range s.calls {
		if c.Status == "pending" {
			out = append(out, c)
		}
	}
	sortCalls(out)
	return out
}

func sortCalls(calls []*instrument.Call) {
	// deterministic by id
	for i := 1; i < len(calls); i++ {
		for j := i; j > 0 && calls[j-1].ID > calls[j].ID; j-- {
			calls[j-1], calls[j] = calls[j], calls[j-1]
		}
	}
}

// ListTasks returns all tasks in stable insertion order with derived fields.
func (s *state) ListTasks() []*Task {
	out := make([]*Task, len(s.tasks))
	for i, t := range s.tasks {
		out[i] = s.publicTask(t)
	}
	return out
}

// Recover reconciles startup invariants: it verifies film conservation and
// closes every lease whose end has passed the recorded logical clock, without
// re-opening leases already persisted as expired.
func (s *state) Recover() error {
	if err := s.film.Reconcile(); err != nil {
		return err
	}
	s.leases.Expire(s.clock)
	return nil
}

func maxSampleTime(samples []evidence.SamplePoint) int64 {
	var max int64
	for _, s := range samples {
		if s.LogicalTime > max {
			max = s.LogicalTime
		}
	}
	return max
}
