package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// Protocol and state compatibility during the transition (ADR 0010 U15 rule 6,
// A15 D5): status is answered for every positive v, and the state file of this
// step and of the code before it are read by each other's decoder.

// status is the one request that is answered for every v, with the helper's own
// v and the accepted range; every other op keeps the range check.
func TestStatusIsAnsweredForEveryVersion(t *testing.T) {
	v := newTenv(t)
	for _, n := range []string{"1", "2", "3", "99", "2147483647"} {
		r := wantOK(t, v.request([]byte(`{"v":`+n+`,"op":"status"}`+"\n")))
		if r.V != protocolMax || r.ProtocolMin != protocolMin || r.ProtocolMax != protocolMax || r.Phase != phaseIdle {
			t.Fatalf("v %s: %+v", n, r)
		}
	}
	for _, n := range []string{"0", "-1", "1.5", `"1"`, "null", "1e999", "99999999999999999999"} {
		wantErrCode(t, v.request([]byte(`{"v":`+n+`,"op":"status"}`+"\n")), codeBadRequest)
	}
	// The other members of a status request stay strict for every v.
	wantErrCode(t, v.request([]byte(`{"v":2,"op":"status","version":"1.2.3"}`+"\n")), codeBadRequest)
	// Every other op is refused outside the range, before it is looked at.
	for _, line := range []string{
		`{"v":2,"op":"stage","version":"1.1.0","correlation_id":"0123456789abcdef"}`,
		`{"v":2,"op":"activate","version":"1.1.0","correlation_id":"0123456789abcdef","components_confirmed":true}`,
		`{"v":2,"op":"discard","version":"1.1.0"}`,
		`{"v":2,"op":"ack","transaction_id":"0123456789abcdef"}`,
		`{"v":0,"op":"discard","version":"1.1.0"}`,
	} {
		wantErrCode(t, v.request([]byte(line+"\n")), codeBadRequest)
	}
	if _, ok := v.readState(); ok {
		t.Fatal("a refused request wrote the state")
	}
}

// The member lists that the decoder of the code before this step accepted
// (state.go at the merge of the pull request of step a). They are a copy on
// purpose: the test must not follow the code.
var (
	oldTopMembers  = []string{"schema_version", "helper_version", "previous_version", "needs_attention", "transaction", "cooldown", "last_result"}
	oldTxnMembers  = []string{"id", "phase", "to", "from", "from_binary_sha256", "correlation_id", "staged_at_uptime_s", "components_from", "components_to", "known_good_sha256", "socket_restart_pending"}
	oldCoolMembers = []string{"refuse_version", "refuse_until_uptime_s", "refuse_set_at_uptime_s", "last_activate_uptime_s", "last_activate_set_at_uptime_s"}
	oldLastMembers = []string{"transaction_id", "to", "from", "outcome", "reason_code", "correlation_id", "finished_at", "acknowledged"}
)

// oldDecodeState is the decoder of the code before this step: strict for the
// members it knows (the same validators, which this step did not change),
// unknown members ignored at every level, and a member that differs from a
// known name only in letter case refused.
func oldDecodeState(data []byte) (*state, error) {
	top, err := object(data, oldTopMembers...)
	if err != nil {
		return nil, err
	}
	var schema *int64
	if json.Unmarshal(top["schema_version"], &schema) != nil || schema == nil || *schema != 1 {
		return nil, errStateInvalid
	}
	s := &state{Txn: txn{Phase: phaseIdle}}
	errs := []error{str(top, "helper_version", okHelperV, &s.HelperVersion), str(top, "previous_version", okVersion, &s.Previous),
		flag(top, "needs_attention", &s.NeedsAttention)}
	if raw, ok := top["transaction"]; ok {
		m, err := object(raw, oldTxnMembers...)
		if err != nil {
			return nil, err
		}
		t := &s.Txn
		errs = append(errs, str(m, "id", okID, &t.ID), str(m, "phase", okPhase, &t.Phase), str(m, "to", okVersion, &t.To), str(m, "from", okVersion, &t.From),
			str(m, "from_binary_sha256", okSHA, &t.FromBinarySHA256), str(m, "correlation_id", okID, &t.CorrelationID), num(m, "staged_at_uptime_s", &t.StagedAtUptimeS),
			str(m, "components_from", okVersion, &t.ComponentsFrom), str(m, "components_to", okVersion, &t.ComponentsTo),
			str(m, "known_good_sha256", okSHA, &t.KnownGoodSHA256), flag(m, "socket_restart_pending", &t.SocketRestartPending))
	}
	if raw, ok := top["cooldown"]; ok {
		m, err := object(raw, oldCoolMembers...)
		if err != nil {
			return nil, err
		}
		c := &s.Cool
		errs = append(errs, str(m, "refuse_version", okVersion, &c.RefuseVersion), num(m, "refuse_until_uptime_s", &c.RefuseUntilUptimeS),
			num(m, "refuse_set_at_uptime_s", &c.RefuseSetAtUptimeS), num(m, "last_activate_uptime_s", &c.LastActivateUptimeS),
			num(m, "last_activate_set_at_uptime_s", &c.LastActivateSetAtUptimeS))
	}
	if raw, ok := top["last_result"]; ok && strings.TrimSpace(string(raw)) != "null" {
		m, err := object(raw, oldLastMembers...)
		if err != nil {
			return nil, err
		}
		l := &lastResult{}
		errs = append(errs, str(m, "transaction_id", okID, &l.TransactionID), str(m, "to", okVersion, &l.To), str(m, "from", okVersion, &l.From),
			str(m, "outcome", okOutcome, &l.Outcome), str(m, "reason_code", okReason, &l.ReasonCode), str(m, "correlation_id", okID, &l.CorrelationID),
			str(m, "finished_at", okFinished, &l.FinishedAt), flag(m, "acknowledged", &l.Acknowledged))
		s.Last = l
	}
	for _, e := range errs {
		if e != nil {
			return nil, e
		}
	}
	return s, nil
}

// oldStateFile is a state file as the code before this step wrote it (its
// encoder, every member set).
const oldStateFile = `{
  "schema_version": 1,
  "helper_version": "protocol-1",
  "previous_version": "0.9.0",
  "needs_attention": true,
  "transaction": {
    "id": "00000000000000aa",
    "phase": "staged",
    "to": "1.1.0",
    "from": "1.0.0",
    "from_binary_sha256": "abababababababababababababababababababababababababababababababab",
    "correlation_id": "0123456789abcdef",
    "staged_at_uptime_s": 1234,
    "components_from": "",
    "components_to": "",
    "known_good_sha256": "cdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcd",
    "socket_restart_pending": true
  },
  "cooldown": {
    "refuse_version": "1.1.0",
    "refuse_until_uptime_s": 2000,
    "refuse_set_at_uptime_s": 1100,
    "last_activate_uptime_s": 1100,
    "last_activate_set_at_uptime_s": 1100
  },
  "last_result": {
    "transaction_id": "00000000000000bb",
    "to": "1.0.0",
    "from": "0.9.0",
    "outcome": "rolled_back",
    "reason_code": "health_timeout",
    "correlation_id": "fedcba9876543210",
    "finished_at": "2023-11-14T22:13:20Z",
    "acknowledged": false
  }
}
`

func TestStateRoundTripWithThePreviousDecoder(t *testing.T) {
	t.Run("the previous decoder ignores the new members", func(t *testing.T) {
		full := fullState()
		data := encodeState(full)
		// Every new member is in the file, so the test covers all of them.
		for _, m := range []string{"components_previous", "differs", "service_unit_sha256", "components_set_sha256", "components_helper_sha256",
			"components_key_id", "components_protocol_min", "components_protocol_max", "socket_text_changed", "components_updated",
			"rollback_reason", "known_good_id", "socket_restart_set_at_uptime_s"} {
			if !strings.Contains(string(data), `"`+m+`":`) {
				t.Fatalf("the file has no member %s", m)
			}
		}
		got, err := oldDecodeState(data)
		if err != nil {
			t.Fatalf("the previous decoder refuses a state of this step: %v\n%s", err, data)
		}
		// What it knows, it reads unchanged.
		want := *full
		want.ComponentsPrevious = ""
		want.Txn = txn{ID: full.Txn.ID, Phase: full.Txn.Phase, To: full.Txn.To, From: full.Txn.From, FromBinarySHA256: full.Txn.FromBinarySHA256,
			CorrelationID: full.Txn.CorrelationID, StagedAtUptimeS: full.Txn.StagedAtUptimeS, ComponentsFrom: full.Txn.ComponentsFrom,
			ComponentsTo: full.Txn.ComponentsTo, KnownGoodSHA256: full.Txn.KnownGoodSHA256, SocketRestartPending: full.Txn.SocketRestartPending}
		last := *full.Last
		last.ComponentsUpdated = false
		want.Last = &last
		if !reflect.DeepEqual(got, &want) {
			t.Fatalf("the previous decoder reads\n %+v %+v\nwant\n %+v %+v", got, got.Last, &want, want.Last)
		}
		// A state in each component phase and with each outcome is read as well:
		// the phase and outcome names were known before this step.
		for _, phase := range []string{phaseCSwitching, phaseCSwitched, phaseCVerified, phaseCCommitted, phaseCRollingBack} {
			s := fullState()
			s.Txn.Phase, s.Last.Outcome = phase, outcomeComponentsRolledBack
			if _, err := oldDecodeState(encodeState(s)); err != nil {
				t.Fatalf("phase %s: %v", phase, err)
			}
		}
	})
	t.Run("no new member differs from a previous one only in letter case", func(t *testing.T) {
		// The decoder refuses such a name (it would be matched by encoding/json).
		var doc map[string]json.RawMessage
		if err := json.Unmarshal(encodeState(fullState()), &doc); err != nil {
			t.Fatal(err)
		}
		check := func(raw json.RawMessage, old []string) {
			var m map[string]json.RawMessage
			if err := json.Unmarshal(raw, &m); err != nil {
				t.Fatal(err)
			}
			for k := range m {
				for _, o := range old {
					if k != o && strings.EqualFold(k, o) {
						t.Errorf("member %s collides with %s", k, o)
					}
				}
			}
		}
		check(encodeState(fullState()), oldTopMembers)
		check(doc["transaction"], oldTxnMembers)
		check(doc["cooldown"], oldCoolMembers)
		check(doc["last_result"], oldLastMembers)
	})
	t.Run("this decoder reads a state of the previous code", func(t *testing.T) {
		got, err := decodeState([]byte(oldStateFile))
		if err != nil {
			t.Fatal(err)
		}
		old, err := oldDecodeState([]byte(oldStateFile))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, old) {
			t.Fatalf("this decoder reads\n %+v\nthe previous one\n %+v", got, old)
		}
		// The new members have their zero values: nothing is staged as a
		// difference, no reference hash counts (it names no set), and the socket
		// flag has no time, so it stays reported until a commit sets it again.
		tx := got.Txn
		if tx.Differs != nil || tx.KnownGoodID != "" || tx.SocketRestartSetAtUptimeS != 0 || !tx.SocketRestartPending || got.ComponentsPrevious != "" || got.Last.ComponentsUpdated {
			t.Fatalf("%+v", tx)
		}
		// And what this step writes from it is a file the previous decoder reads.
		if _, err := oldDecodeState(encodeState(got)); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("the helper works on a state of the previous code", func(t *testing.T) {
		v := newTenv(t)
		v.installOarfish()
		v.fakeSystemd()
		v.makeRelease("1.1.0")
		if err := writeFile(v, oldStateFile); err != nil {
			t.Fatal(err)
		}
		v.uptime.Store(1300) // 66 s after it was staged
		sr := v.status()
		if sr.Phase != phaseStaged || sr.StagedVersion != "1.1.0" || len(sr.StagedDiffers) != 0 || !sr.SocketRestartPending {
			t.Fatalf("status %+v", sr)
		}
		wantOK(t, v.activate("1.1.0"))
		if v.current() != "1.1.0" {
			t.Fatal("a release staged by the previous code was not activated")
		}
	})
}

func writeFile(v *tenv, content string) error {
	st, err := decodeState([]byte(content))
	if err != nil {
		return err
	}
	st.Cool = cooldown{}
	v.writeState(st)
	return nil
}

// The strict rules of the new members.
func TestStateNewMembersAreStrict(t *testing.T) {
	base := string(encodeState(fullState()))
	for name, repl := range map[string][2]string{
		"differs with an unknown name":  {`"differs": [`, `"differs": ["/etc/passwd", `},
		"differs out of order":          {`"oarfish-update-helper",` + "\n      " + `"oarfish.service"`, `"oarfish.service",` + "\n      " + `"oarfish-update-helper"`},
		"differs repeated":              {`"oarfish-update-helper",` + "\n      " + `"oarfish.service"`, `"oarfish.service",` + "\n      " + `"oarfish.service"`},
		"differs not an array":          {`"differs": [`, `"differs": "x", "x": [`},
		"a hash that is not 64 digits":  {`"components_helper_sha256": "0c`, `"components_helper_sha256": "0C`},
		"a key id that is not 16 hex":   {`"components_key_id": "11f1c52777090bfc"`, `"components_key_id": "11f1c52777090bf"`},
		"a protocol that is negative":   {`"components_protocol_min": 1`, `"components_protocol_min": -1`},
		"a protocol that is a string":   {`"components_protocol_max": 2`, `"components_protocol_max": "2"`},
		"a protocol that is too large":  {`"components_protocol_max": 2`, `"components_protocol_max": 99999999`},
		"a set id that is a path":       {`"known_good_id": "0.9.0"`, `"known_good_id": "../0.9.0"`},
		"a previous set that is a path": {`"components_previous": "0.8.0"`, `"components_previous": "a/b"`},
		"a reason with a space":         {`"rollback_reason": "helper_mismatch"`, `"rollback_reason": "helper mismatch"`},
		"a flag that is a number":       {`"socket_text_changed": true`, `"socket_text_changed": 1`},
		"components_updated null":       {`"components_updated": true,` + "\n    " + `"rollback_reason"`, `"components_updated": null,` + "\n    " + `"rollback_reason"`},
	} {
		if !strings.Contains(base, repl[0]) {
			t.Fatalf("%s: the file has no %q\n%s", name, repl[0], base)
		}
		if _, err := decodeState([]byte(strings.Replace(base, repl[0], repl[1], 1))); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
