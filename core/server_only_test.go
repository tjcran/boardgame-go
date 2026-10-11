package core

import (
	"errors"
	"testing"
)

// TestFromClientRefusesServerOnly: a request marked FromClient may not
// make a ServerOnly move, judged on the move the reducer resolves for
// it. A stage move shadowing a ServerOnly top-level name is not
// ServerOnly, so it runs. In-process requests keep the server's
// authority, which is also how Replay re-applies a recorded move.
func TestFromClientRefusesServerOnly(t *testing.T) {
	game := &Game{
		Name:       "from-client-test",
		MinPlayers: 2,
		MaxPlayers: 2,
		Setup:      func(_ Ctx, _ any) G { return &targetState{} },
		Moves: map[string]any{
			"enter": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				mc.Events.SetActivePlayers(ActivePlayersConfig{CurrentPlayer: Stage("admin")})
				return cloneT(mc.G.(*targetState), "enter"), nil
			}),
			"shadowed": Move{ServerOnly: true, Move: recordMove("topShadowed")},
		},
		Turn: &TurnConfig{Stages: map[string]*StageConfig{
			"admin": {Moves: map[string]any{
				"stageAdmin": Move{ServerOnly: true, Move: recordMove("stageAdmin")},
				"shadowed":   recordMove("stageShadowed"),
			}},
		}},
	}
	state, player := enterStage(t, game, "enter")

	next, err := Apply(game, state, MoveRequest{PlayerID: player, Move: "stageAdmin", FromClient: true})
	if !errors.Is(err, ErrServerOnly) {
		t.Fatalf("client stageAdmin: err = %v, want ErrServerOnly", err)
	}
	assertUnchanged(t, state, next)

	if next, err = Apply(game, state, MoveRequest{PlayerID: player, Move: "shadowed", FromClient: true}); err != nil {
		t.Fatalf("client shadowed: %v", err)
	}
	if events := next.G.(*targetState).Events; events[len(events)-1] != "stageShadowed" {
		t.Fatalf("shadowed ran the wrong move: events = %v", events)
	}

	next, err = Apply(game, state, MoveRequest{PlayerID: player, Move: "stageAdmin"})
	if err != nil {
		t.Fatalf("in-process stageAdmin: %v", err)
	}
	replayed, err := Replay(game, next.Log, 2, nil)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if events := replayed.G.(*targetState).Events; events[len(events)-1] != "stageAdmin" {
		t.Fatalf("Replay did not re-apply stageAdmin: events = %v", events)
	}
}
