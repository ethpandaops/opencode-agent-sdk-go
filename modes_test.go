package opencodesdk

import (
	"context"
	"testing"
	"time"

	"github.com/coder/acp-go-sdk"
)

func TestModeConstants_MatchOpencodeValues(t *testing.T) {
	const (
		wantPlan  = "plan"
		wantBuild = "build"
	)

	if ModePlan != wantPlan {
		t.Errorf("ModePlan = %q, want %q", ModePlan, wantPlan)
	}

	if ModeBuild != wantBuild {
		t.Errorf("ModeBuild = %q, want %q", ModeBuild, wantBuild)
	}
}

func TestWithInitialMode_IsAliasForWithAgent(t *testing.T) {
	o := apply([]Option{WithInitialMode(ModePlan)})
	if o.agent != ModePlan {
		t.Errorf("WithInitialMode(ModePlan): options.agent = %q, want %q", o.agent, ModePlan)
	}

	o = apply([]Option{WithAgent("custom-mode")})
	if o.agent != "custom-mode" {
		t.Errorf("WithAgent(custom-mode): options.agent = %q", o.agent)
	}
}

func TestSession_AvailableModes_DerivedFromInitialModeState(t *testing.T) {
	c := newTestClient()
	modes := &acp.SessionModeState{
		CurrentModeId: "build",
		AvailableModes: []acp.SessionMode{
			{Id: ModeBuild, Name: "Build"},
			{Id: ModePlan, Name: "Plan"},
		},
	}

	s := newSession(c, "ses_modes_1", nil, modes, nil, nil, 8)

	got := s.AvailableModes()
	if len(got) != 2 {
		t.Fatalf("AvailableModes len = %d, want 2", len(got))
	}

	if got[0].Id != ModeBuild || got[1].Id != ModePlan {
		t.Fatalf("unexpected modes: %+v", got)
	}
}

func TestSession_AvailableModes_NilWhenAbsent(t *testing.T) {
	c := newTestClient()
	s := newSession(c, "ses_modes_2", nil, nil, nil, nil, 8)

	if got := s.AvailableModes(); got != nil {
		t.Fatalf("AvailableModes with nil SessionModeState should be nil, got %+v", got)
	}
}

// TestWithInitialMode_AppliedAtSessionStart verifies the option flows
// through session/set_config_option during NewSession.
func TestWithInitialMode_AppliedAtSessionStart(t *testing.T) {
	var seen acp.SetSessionConfigOptionRequest

	agent := &setConfigCapturingAgent{
		onSet: func(req acp.SetSessionConfigOptionRequest) {
			seen = req
		},
	}

	c, cleanup := startPipeClient(t, agent)
	defer cleanup()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	_, err := c.NewSession(ctx, WithInitialMode(ModePlan))
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	if seen.ValueId == nil {
		t.Fatalf("agent never observed set_config_option")
	}

	if string(seen.ValueId.ConfigId) != "mode" {
		t.Errorf("ConfigId = %q, want %q", seen.ValueId.ConfigId, "mode")
	}

	if string(seen.ValueId.Value) != ModePlan {
		t.Errorf("Value = %q, want %q", seen.ValueId.Value, ModePlan)
	}
}

// setConfigCapturingAgent captures the last session/set_config_option
// request. It embeds fakeAgent so other RPCs still work.
type setConfigCapturingAgent struct {
	fakeAgent
	onSet func(acp.SetSessionConfigOptionRequest)
}

func (a *setConfigCapturingAgent) SetSessionConfigOption(_ context.Context, req acp.SetSessionConfigOptionRequest) (acp.SetSessionConfigOptionResponse, error) {
	if a.onSet != nil {
		a.onSet(req)
	}

	return acp.SetSessionConfigOptionResponse{}, nil
}
