package updater

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/swingstate/oarfish/backend/internal/audit"
)

// The follow-ups of G6.10 step c (ADR 0010 A17): F1 the installer view for
// every installer case, F2 the discard after a refused activate, F3 the last
// results of a rebuilt state, F4 the hand-over of the helper as progress.

// stagingHelper is the fake helper with the one piece of helper state that F2
// is about: a staged release makes the next stage answer busy until it is
// activated or discarded (ADR 0010 U5, stage check a).
type stagingHelper struct {
	*fakeHelper
	smu    sync.Mutex
	staged string
}

func (s *stagingHelper) Stage(ctx context.Context, v, cid string) (StageInfo, *Error) {
	s.smu.Lock()
	busy := s.staged != ""
	s.smu.Unlock()
	if busy {
		return StageInfo{}, fail(CodeBusy)
	}
	si, e := s.fakeHelper.Stage(ctx, v, cid)
	if e == nil {
		s.smu.Lock()
		s.staged = v
		s.smu.Unlock()
	}
	return si, e
}

func (s *stagingHelper) Activate(ctx context.Context, v, cid string, confirmed bool) (string, *Error) {
	id, e := s.fakeHelper.Activate(ctx, v, cid, confirmed)
	if e == nil {
		s.smu.Lock()
		s.staged = ""
		s.smu.Unlock()
	}
	return id, e
}

func (s *stagingHelper) Discard(ctx context.Context, v string) *Error {
	e := s.fakeHelper.Discard(ctx, v)
	s.smu.Lock()
	if e == nil && s.staged == v {
		s.staged = ""
	}
	s.smu.Unlock()
	return e
}

// F2: every refusal of activate that leaves the release staged (U5, U6: the
// confirmation rule, a confused flag, both cooldown markers, the damaged
// known-good set, the pre-check, a storage error, an internal error of the
// helper) is followed by one discard of that version, so the next apply
// stages at once instead of answering busy. The refusal stays the job's error
// and its finishing entry; the cooldown is the helper's and still answers.
func TestActivateRefusalDiscardsTheStagedRelease(t *testing.T) {
	for _, code := range []Code{CodeConfirmationRequired, CodeBadRequest, CodeCooldown, CodeComponentsDamaged,
		CodeCurrentUnhealthy, CodeIOError, CodeInternal} {
		t.Run(string(code), func(t *testing.T) {
			e, _ := ready(t, relOpts{})
			sh := &stagingHelper{fakeHelper: e.helper}
			e.u.cfg.Helper = sh
			e.helper.activateErr = fail(code)
			if err := e.apply(v9); err != nil {
				t.Fatal(err)
			}
			if je := e.jobErr(); je == nil || je.Code != code {
				t.Fatalf("job error %v", je)
			}
			if got := e.helper.nonStatus(); got != "stage "+v9+",activate "+v9+",discard "+v9 {
				t.Fatalf("calls %q", got)
			}
			// A new attempt stages at once. The helper's cooldown still answers.
			if err := e.apply(v9); err != nil {
				t.Fatal(err)
			}
			if je := e.jobErr(); je == nil || je.Code != code {
				t.Fatalf("second attempt: %v", je)
			}
			if e.helper.count("stage") != 2 {
				t.Fatalf("the second stage did not run: %v", e.helper.nonStatus())
			}
			// runApply writes the finishing entry just after the job is done.
			var fin []string
			for i := 0; i < 1000 && len(fin) < 2; i++ {
				time.Sleep(time.Millisecond)
				fin = finishing(e)
			}
			if len(fin) != 2 || fin[1] != string(audit.UpdateResult(string(code)))+" 0123456789abcdef -" {
				t.Fatalf("finishing entries %v", fin)
			}
		})
	}
}

// F2: a failure of the activate call itself does not say whether the helper
// took the request, so no discard is sent; the staged release expires (U5).
func TestActivateCallFailureSendsNoDiscard(t *testing.T) {
	for _, code := range []Code{CodeUnavailable, CodeTimeout} {
		t.Run(string(code), func(t *testing.T) {
			e, _ := ready(t, relOpts{})
			e.helper.activateErr = fail(code)
			if err := e.apply(v9); err != nil {
				t.Fatal(err)
			}
			if e.helper.count("discard") != 0 {
				t.Fatalf("calls %q", e.helper.nonStatus())
			}
		})
	}
}

// F2: a refusal of stage leaves nothing staged (the helper removes the staging
// tree itself), so nothing is discarded.
func TestStageRefusalSendsNoDiscard(t *testing.T) {
	e, _ := ready(t, relOpts{})
	e.helper.stage = func(context.Context, string, string) (StageInfo, *Error) { return StageInfo{}, fail(CodeNoSpace) }
	if err := e.apply(v9); err != nil {
		t.Fatal(err)
	}
	if got := e.helper.nonStatus(); got != "stage "+v9 {
		t.Fatalf("calls %q", got)
	}
}

func lastResultAnswer(result, extra string) string {
	return `{"v":1,"ok":true,"protocol_min":1,"protocol_max":1,"helper_version":"protocol-1","installed_version":"0.1.0-alpha.6",` +
		`"previous_version":"","free_bytes":1,"cooldown_version":"","cooldown_remaining_s":0` + extra +
		`,"last_result":` + result + `}` + "\n"
}

// F3: the last results that a rebuilt or deferred state can hold are accepted,
// and the rule stays closed: only aborted with state_rebuilt may lack the
// target version, and then it has no versions at all.
func TestRebuiltStateResultsAreAccepted(t *testing.T) {
	ctx := context.Background()
	const id = `"transaction_id":"00112233445566ff"`
	accepted := map[string]string{
		// U7: state.json missing or unreadable, nothing else found.
		"rebuild": `{` + id + `,"to":"","from":"","outcome":"aborted","reason_code":"state_rebuilt","correlation_id":"","finished_at":"2026-11-11T10:00:00Z","acknowledged":false}`,
		// P11: an unproven set was active and went back to known-good.
		"P11": `{` + id + `,"to":"0.1.0-alpha.9","from":"0.1.0-alpha.6","outcome":"components_rolled_back","reason_code":"state_unreadable","correlation_id":"","finished_at":"2026-11-11T10:00:00Z","acknowledged":false}`,
		// P11 when current could not be read either.
		"P11 no current": `{` + id + `,"to":"0.1.0-alpha.9","from":"","outcome":"components_rolled_back","reason_code":"state_unreadable","correlation_id":"","finished_at":"2026-11-11T10:00:00Z","acknowledged":false}`,
		// E5: the known-good set cannot be read; the failed rollback is kept.
		"E5": `{` + id + `,"to":"0.1.0-alpha.9","from":"0.1.0-alpha.6","outcome":"rollback_failed","reason_code":"known_good_damaged","correlation_id":"","finished_at":"2026-11-11T10:00:00Z","acknowledged":false}`,
		// A finished time that a clock without a battery may make odd is dropped, not refused.
		"odd time": `{` + id + `,"to":"","from":"","outcome":"aborted","reason_code":"state_rebuilt","correlation_id":"","finished_at":"","acknowledged":true}`,
	}
	for name, res := range accepted {
		for _, phase := range []string{"idle", "rollback_failed", "c_rolling_back"} {
			// The phase on disk: P18 reports it while the start-up recovery waits for the lock.
			st, e := helperWith(fixed(lastResultAnswer(res, `,"phase":"`+phase+`","staged_version":"","needs_attention":true`))).Status(ctx)
			if e != nil || st.LastResult == nil || st.Phase != phase || !st.NeedsAttention {
				t.Errorf("%s in %s: %v %+v", name, phase, e, st)
			}
		}
	}
	refused := map[string]string{
		"no target, other reason":     `{` + id + `,"to":"","from":"","outcome":"aborted","reason_code":"interrupted","correlation_id":"","finished_at":"","acknowledged":false}`,
		"no target, other outcome":    `{` + id + `,"to":"","from":"","outcome":"success","reason_code":"state_rebuilt","correlation_id":"","finished_at":"","acknowledged":false}`,
		"rebuild with a from":         `{` + id + `,"to":"","from":"0.1.0-alpha.6","outcome":"aborted","reason_code":"state_rebuilt","correlation_id":"","finished_at":"","acknowledged":false}`,
		"target not a version":        `{` + id + `,"to":"../x","from":"","outcome":"aborted","reason_code":"state_rebuilt","correlation_id":"","finished_at":"","acknowledged":false}`,
		"unknown outcome":             `{` + id + `,"to":"0.1.0-alpha.9","from":"","outcome":"rebuilt","reason_code":"state_rebuilt","correlation_id":"","finished_at":"","acknowledged":false}`,
		"bad transaction id":          `{"transaction_id":"x","to":"","from":"","outcome":"aborted","reason_code":"state_rebuilt","correlation_id":"","finished_at":"","acknowledged":false}`,
		"reason outside the pattern":  `{` + id + `,"to":"0.1.0-alpha.9","from":"","outcome":"rollback_failed","reason_code":"Known Good","correlation_id":"","finished_at":"","acknowledged":false}`,
		"correlation outside pattern": `{` + id + `,"to":"","from":"","outcome":"aborted","reason_code":"state_rebuilt","correlation_id":"zz","finished_at":"","acknowledged":false}`,
	}
	for name, res := range refused {
		if _, e := helperWith(fixed(lastResultAnswer(res, `,"phase":"idle","staged_version":"","needs_attention":true`))).Status(ctx); e == nil || e.Code != CodeInternal {
			t.Errorf("%s: %v", name, e)
		}
	}
}

// F3, through the service: a rebuilt state is shown with its result and the
// attention flag instead of an internal error, and it is acknowledged; it has
// no correlation ID, so no finishing entry is written for it.
func TestRebuiltStateIsShownByTheService(t *testing.T) {
	e := newEnv(t, v6)
	e.helper.setStatus(HelperStatus{ProtocolMin: 1, ProtocolMax: 1, InstalledVersion: v6, Phase: "idle", NeedsAttention: true,
		LastResult: &HelperResult{TransactionID: "00112233445566ff", Outcome: "aborted", ReasonCode: "state_rebuilt",
			FinishedAt: "2026-11-11T10:00:00Z"}}, nil)
	st := e.u.State(context.Background())
	if !st.Helper.Present || !st.Helper.NeedsAttention || st.LastResult == nil || st.LastResult.To != "" || st.LastResult.ReasonCode != "state_rebuilt" {
		t.Fatalf("%+v %+v", st.Helper, st.LastResult)
	}
	if len(finishing(e)) != 0 || e.helper.count("ack 00112233445566ff") != 1 {
		t.Fatalf("finishing %v, calls %v", finishing(e), e.helper.calls)
	}
}

// F4: the bound of the hand-over, on both sides. The window is handOverLimit
// after the later of the acceptance of activate and the last successful status.
func TestHandOverToleranceBound(t *testing.T) {
	e := componentFlow(t)
	ctx := context.Background()
	over := func() HelperView {
		t.Helper()
		e.helper.setStatus(HelperStatus{}, fail(CodeUnavailable))
		hv := e.u.State(ctx).Helper
		if hv.Present || hv.Error == nil || hv.Error.Code != CodeUnavailable {
			t.Fatalf("%+v", hv)
		}
		return hv
	}
	if !over().HandingOver {
		t.Fatal("not handing over right after activate")
	}
	e.clk.Advance(handOverLimit)
	if !over().HandingOver {
		t.Fatal("not handing over at the bound")
	}
	e.clk.Advance(time.Second)
	if over().HandingOver {
		t.Fatal("still handing over after the bound")
	}
	// A successful status answer starts the window again.
	e.helper.setStatus(HelperStatus{ProtocolMin: 1, ProtocolMax: 1, InstalledVersion: v6, Phase: "c_switched"}, nil)
	if hv := e.u.State(ctx).Helper; !hv.Present || hv.HandingOver || hv.Phase != "c_switched" {
		t.Fatalf("%+v", hv)
	}
	e.clk.Advance(handOverLimit)
	if !over().HandingOver {
		t.Fatal("not handing over within the bound after a status answer")
	}
	e.clk.Advance(time.Second)
	if over().HandingOver {
		t.Fatal("still handing over after the bound after a status answer")
	}
	// A timed-out status call counts as well; an unusable answer does not.
	e.helper.setStatus(HelperStatus{ProtocolMin: 1, ProtocolMax: 1, InstalledVersion: v6, Phase: "c_switched"}, nil)
	e.u.State(ctx)
	e.helper.setStatus(HelperStatus{}, fail(CodeTimeout))
	if hv := e.u.State(ctx).Helper; !hv.HandingOver {
		t.Fatalf("timeout: %+v", hv)
	}
	e.helper.setStatus(HelperStatus{}, fail(CodeInternal))
	if hv := e.u.State(ctx).Helper; hv.HandingOver {
		t.Fatalf("internal: %+v", hv)
	}
}

// F4: no hand-over without one: a release that changes oarfish.service alone,
// an activate that was refused, and no job at all.
func TestNoHandOverWithoutAComponentSet(t *testing.T) {
	ctx := context.Background()
	t.Run("oarfish.service alone", func(t *testing.T) {
		e, _ := ready(t, relOpts{})
		e.helper.stage = func(_ context.Context, v, _ string) (StageInfo, *Error) {
			return StageInfo{Version: v, ComponentsDiffer: true, Differs: []string{"oarfish.service"}}, nil
		}
		slot, _ := e.u.Reserve()
		if err := slot.Start(ApplyRequest{Version: v9, User: "admin", CorrelationID: "0123456789abcdef", ComponentsConfirmed: true}); err != nil {
			t.Fatal(err)
		}
		e.settle()
		e.helper.setStatus(HelperStatus{}, fail(CodeUnavailable))
		if hv := e.u.State(ctx).Helper; hv.HandingOver {
			t.Fatalf("%+v", hv)
		}
	})
	t.Run("activate refused", func(t *testing.T) {
		e, _ := ready(t, relOpts{})
		e.helper.stage = func(_ context.Context, v, _ string) (StageInfo, *Error) {
			return StageInfo{Version: v, ComponentsDiffer: true, Differs: []string{"oarfish-update-helper"}}, nil
		}
		e.helper.activateErr = fail(CodeCooldown)
		slot, _ := e.u.Reserve()
		if err := slot.Start(ApplyRequest{Version: v9, User: "admin", CorrelationID: "0123456789abcdef", ComponentsConfirmed: true}); err != nil {
			t.Fatal(err)
		}
		e.settle()
		e.helper.setStatus(HelperStatus{}, fail(CodeUnavailable))
		if hv := e.u.State(ctx).Helper; hv.HandingOver {
			t.Fatalf("%+v", hv)
		}
	})
	t.Run("no job", func(t *testing.T) {
		e := newEnv(t, v6)
		e.helper.setStatus(HelperStatus{}, fail(CodeUnavailable))
		if hv := e.u.State(ctx).Helper; hv.HandingOver {
			t.Fatalf("%+v", hv)
		}
	})
}

// F1: every refusal of stage for which the device needs the installer gets the
// installer view with its code; the instruction is the fixed text. Other
// refusals, and the same codes from activate, do not.
func TestInstallerViewForEachInstallerCase(t *testing.T) {
	ctx := context.Background()
	for _, code := range []Code{CodeInstallerRequired, CodeKeyChanged, CodeUnitRejected, CodeRequirementsNotMet} {
		t.Run(string(code), func(t *testing.T) {
			e, _ := ready(t, relOpts{})
			e.helper.stage = func(context.Context, string, string) (StageInfo, *Error) {
				err := fail(code)
				if code == CodeInstallerRequired {
					err.Differs = []string{"oarfish-update-helper"}
				}
				return StageInfo{}, err
			}
			e.apply(v9)
			in := e.u.State(ctx).Installer
			if in == nil || !in.Required || in.Code != code || in.Version != v9 || in.Instruction != InstallerInstruction {
				t.Fatalf("%+v", in)
			}
			if code == CodeInstallerRequired && strings.Join(in.Differs, ",") != "oarfish-update-helper" {
				t.Fatalf("%+v", in)
			}
			if code != CodeInstallerRequired && len(in.Differs) != 0 {
				t.Fatalf("%+v", in)
			}
		})
	}
	t.Run("other stage refusal", func(t *testing.T) {
		e, _ := ready(t, relOpts{})
		e.helper.stage = func(context.Context, string, string) (StageInfo, *Error) { return StageInfo{}, fail(CodeHelperTooOld) }
		e.apply(v9)
		if in := e.u.State(ctx).Installer; in != nil {
			t.Fatalf("%+v", in)
		}
	})
	t.Run("unit_rejected at activate", func(t *testing.T) {
		// P14: a staged unit that changed after stage; that is not a unit text of the release.
		e, _ := ready(t, relOpts{})
		e.helper.activateErr = fail(CodeUnitRejected)
		e.apply(v9)
		if in := e.u.State(ctx).Installer; in != nil {
			t.Fatalf("%+v", in)
		}
	})
}
