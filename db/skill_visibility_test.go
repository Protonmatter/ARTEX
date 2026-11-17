package db

import (
	"database/sql"
	"errors"
	"slices"
	"testing"
)

// skillRowState reports whether an (agent, skill) row exists and whether it is
// enabled. seedBuiltinSkillVisibility is insert-if-absent, so never seeded and
// explicitly turned off must be different states.
func skillRowState(t *testing.T, d *DB, agentID int64, skill string) (exists, enabled bool) {
	t.Helper()
	err := d.QueryRow(`SELECT enabled FROM agent_skill_visibility WHERE agent_id=$1 AND skill_name=$2`, agentID, skill).Scan(&enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return false, false
	}
	if err != nil {
		t.Fatalf("read skill visibility row (%d, %s): %v", agentID, skill, err)
	}
	return true, enabled
}

// TestSkillVisibilityOffIsNotResurrectedBySeed pins the seed policy: a toggle-off
// survives the next startup. The seeded map is api-recon to auto, pentest, and
// worker. Those agents are restored on the way out.
func TestSkillVisibilityOffIsNotResurrectedBySeed(t *testing.T) {
	const skill = "api-recon"
	d, err := Open(testDSN(t))
	if err != nil {
		t.Skipf("postgres unavailable (%v), skipping", err)
	}

	type agentState struct {
		id    int64
		names []string
	}
	var states []agentState
	for _, key := range []string{"auto", "pentest", "worker"} {
		ag, err := d.GetAgentByKey(key)
		if err != nil || ag == nil {
			t.Fatalf("agent %q missing: %v", key, err)
		}
		orig, err := d.AgentSkillNames(ag.ID)
		if err != nil {
			t.Fatalf("read original visibility for %q: %v", key, err)
		}
		states = append(states, agentState{id: ag.ID, names: orig})
	}
	t.Cleanup(func() {
		for _, st := range states {
			if err := d.SetAgentSkillVisibility(st.id, st.names); err != nil {
				t.Errorf("restore skill visibility for agent %d: %v", st.id, err)
			}
		}
		d.Close()
	})

	worker, err := d.GetAgentByKey("worker")
	if err != nil || worker == nil {
		t.Fatalf("worker agent missing: %v", err)
	}

	if err := d.ToggleSkillVisibility(worker.ID, skill, true); err != nil {
		t.Fatal(err)
	}
	if err := d.seedBuiltinSkillVisibility(); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if names, _ := d.AgentSkillNames(worker.ID); !slices.Contains(names, skill) {
		t.Fatalf("baseline: %s should be visible to worker after seeding, got %v", skill, names)
	}

	if err := d.ToggleSkillVisibility(worker.ID, skill, false); err != nil {
		t.Fatal(err)
	}
	if names, _ := d.AgentSkillNames(worker.ID); slices.Contains(names, skill) {
		t.Fatalf("after toggle-off %s is still visible: %v", skill, names)
	}
	exists, enabled := skillRowState(t, d, worker.ID, skill)
	if !exists {
		t.Fatalf("toggle-off deleted the %s row; an explicit off must be recorded", skill)
	}
	if enabled {
		t.Fatalf("toggle-off left %s enabled=true", skill)
	}

	if err := d.seedBuiltinSkillVisibility(); err != nil {
		t.Fatalf("seed after toggle-off: %v", err)
	}
	if names, _ := d.AgentSkillNames(worker.ID); slices.Contains(names, skill) {
		t.Fatalf("seed resurrected %s for worker after an explicit toggle-off: %v", skill, names)
	}

	if err := d.ToggleSkillVisibility(worker.ID, skill, true); err != nil {
		t.Fatal(err)
	}
	if err := d.SetAgentSkillVisibility(worker.ID, nil); err != nil {
		t.Fatal(err)
	}
	if err := d.seedBuiltinSkillVisibility(); err != nil {
		t.Fatalf("seed after clear: %v", err)
	}
	if names, _ := d.AgentSkillNames(worker.ID); len(names) != 0 {
		t.Fatalf("seed resurrected skills after SetAgentSkillVisibility(nil): %v", names)
	}

	if err := d.ToggleSkillVisibility(worker.ID, skill, true); err != nil {
		t.Fatal(err)
	}
	if err := d.DeleteSkillVisibility(skill); err != nil {
		t.Fatal(err)
	}
	if exists, _ := skillRowState(t, d, worker.ID, skill); exists {
		t.Fatalf("DeleteSkillVisibility left a %s row behind", skill)
	}
	if err := d.seedBuiltinSkillVisibility(); err != nil {
		t.Fatalf("seed after skill delete: %v", err)
	}
	if names, _ := d.AgentSkillNames(worker.ID); !slices.Contains(names, skill) {
		t.Fatalf("a deleted-then-seeded %s should be visible again, got %v", skill, names)
	}
}
