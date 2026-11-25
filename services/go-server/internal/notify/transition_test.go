package notify

import (
	"testing"
	"time"
)

func TestStateTracker_CameraTransitions(t *testing.T) {
	st := NewStateTracker()
	t0 := time.Date(2026, 11, 25, 8, 0, 0, 0, time.UTC)

	// 1. Initial observation: CLEAR -> not escalation
	tr1 := st.RecordCamera("cam-01", "CLEAR", t0)
	if !tr1.Transitioned {
		t.Errorf("expected transition for first observation")
	}
	if tr1.IsEscalation {
		t.Errorf("expected CLEAR to not be an escalation")
	}
	if tr1.IsRecovery {
		t.Errorf("expected CLEAR to not be recovery when no previous hazard")
	}

	// 2. Transition to FLOODED_SHALLOW -> is escalation
	tr2 := st.RecordCamera("cam-01", "FLOODED_SHALLOW", t0.Add(5*time.Minute))
	if !tr2.Transitioned {
		t.Errorf("expected transition from CLEAR to FLOODED_SHALLOW")
	}
	if !tr2.IsEscalation {
		t.Errorf("expected CLEAR -> FLOODED_SHALLOW to be an escalation")
	}
	if tr2.IsRecovery {
		t.Errorf("expected not recovery")
	}

	// 3. Repeat FLOODED_SHALLOW -> not transitioned, not escalation
	tr3 := st.RecordCamera("cam-01", "FLOODED_SHALLOW", t0.Add(10*time.Minute))
	if tr3.Transitioned {
		t.Errorf("expected no transition for repeated severity")
	}
	if tr3.IsEscalation {
		t.Errorf("expected no escalation for repeated severity")
	}

	// 4. Escalation to FLOODED_DEEP -> is escalation
	tr4 := st.RecordCamera("cam-01", "FLOODED_DEEP", t0.Add(15*time.Minute))
	if !tr4.Transitioned || !tr4.IsEscalation {
		t.Errorf("expected SHALLOW -> DEEP to be an escalation")
	}

	// 5. Recovery to CLEAR -> is recovery
	tr5 := st.RecordCamera("cam-01", "CLEAR", t0.Add(45*time.Minute))
	if !tr5.Transitioned {
		t.Errorf("expected transition to CLEAR")
	}
	if !tr5.IsRecovery {
		t.Errorf("expected DEEP -> CLEAR to be a recovery")
	}

	// 6. Later re-flood -> should be an escalation again
	tr6 := st.RecordCamera("cam-01", "FLOODED_SHALLOW", t0.Add(90*time.Minute))
	if !tr6.Transitioned || !tr6.IsEscalation {
		t.Errorf("expected re-flood after recovery to be an escalation")
	}
}

func TestStateTracker_CorridorTransitions(t *testing.T) {
	st := NewStateTracker()
	t0 := time.Date(2026, 11, 25, 8, 0, 0, 0, time.UTC)

	// 1. Initial PASSABLE
	tr1 := st.RecordCorridor("nguyen_huu_canh", "PASSABLE", t0)
	if tr1.IsEscalation || tr1.IsRecovery {
		t.Errorf("expected PASSABLE to neither escalate nor recover")
	}

	// 2. Escalation to CAUTION
	tr2 := st.RecordCorridor("nguyen_huu_canh", "CAUTION", t0.Add(5*time.Minute))
	if !tr2.Transitioned || !tr2.IsEscalation {
		t.Errorf("expected PASSABLE -> CAUTION to escalate")
	}

	// 3. Escalation to IMPASSABLE
	tr3 := st.RecordCorridor("nguyen_huu_canh", "IMPASSABLE", t0.Add(10*time.Minute))
	if !tr3.Transitioned || !tr3.IsEscalation {
		t.Errorf("expected CAUTION -> IMPASSABLE to escalate")
	}

	// 4. Recovery to PASSABLE
	tr4 := st.RecordCorridor("nguyen_huu_canh", "PASSABLE", t0.Add(60*time.Minute))
	if !tr4.Transitioned || !tr4.IsRecovery {
		t.Errorf("expected IMPASSABLE -> PASSABLE to be recovery")
	}
}
