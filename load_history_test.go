package opencodesdk

import (
	"context"
	"testing"
	"time"

	"github.com/coder/acp-go-sdk"
)

func userChunk(sessionID, text string) acp.SessionNotification {
	return acp.SessionNotification{
		SessionId: acp.SessionId(sessionID),
		Update: acp.SessionUpdate{
			UserMessageChunk: &acp.SessionUpdateUserMessageChunk{Content: acp.TextBlock(text)},
		},
	}
}

func agentChunk(sessionID, text string) acp.SessionNotification {
	return acp.SessionNotification{
		SessionId: acp.SessionId(sessionID),
		Update: acp.SessionUpdate{
			AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{Content: acp.TextBlock(text)},
		},
	}
}

func thoughtChunk(sessionID, text string) acp.SessionNotification {
	return acp.SessionNotification{
		SessionId: acp.SessionId(sessionID),
		Update: acp.SessionUpdate{
			AgentThoughtChunk: &acp.SessionUpdateAgentThoughtChunk{Content: acp.TextBlock(text)},
		},
	}
}

func TestMessagesFromNotifications_CoalescesAdjacentChunks(t *testing.T) {
	ns := []acp.SessionNotification{
		userChunk("s", "hello "),
		userChunk("s", "world"),
		agentChunk("s", "hi "),
		agentChunk("s", "there"),
		thoughtChunk("s", "planning..."),
		agentChunk("s", "response"),
	}

	msgs := messagesFromNotifications(ns)

	want := []HistoryMessage{
		{Role: "user", Text: "hello world"},
		{Role: "assistant", Text: "hi there"},
		{Role: "thought", Text: "planning..."},
		{Role: "assistant", Text: "response"},
	}

	if len(msgs) != len(want) {
		t.Fatalf("len(msgs) = %d, want %d: %+v", len(msgs), len(want), msgs)
	}

	for i := range want {
		if msgs[i] != want[i] {
			t.Fatalf("msgs[%d] = %+v, want %+v", i, msgs[i], want[i])
		}
	}
}

func TestMessagesFromNotifications_IgnoresNonTextUpdates(t *testing.T) {
	ns := []acp.SessionNotification{
		agentChunk("s", "keep"),
		{SessionId: "s", Update: acp.SessionUpdate{Plan: &acp.SessionUpdatePlan{}}},
		{SessionId: "s", Update: acp.SessionUpdate{UsageUpdate: &acp.SessionUsageUpdate{}}},
		agentChunk("s", "me"),
	}

	msgs := messagesFromNotifications(ns)

	if len(msgs) != 1 {
		t.Fatalf("len(msgs) = %d, want 1 (coalesced): %+v", len(msgs), msgs)
	}

	if msgs[0].Text != "keepme" {
		t.Fatalf("Text = %q, want keepme", msgs[0].Text)
	}
}

func TestDrainReplay_StopsAfterGrace(t *testing.T) {
	ch := make(chan acp.SessionNotification, 4)

	ch <- agentChunk("s", "one")

	ch <- agentChunk("s", "two")

	ns := drainReplay(context.Background(), ch, 50*time.Millisecond)

	if len(ns) != 2 {
		t.Fatalf("len(ns) = %d, want 2", len(ns))
	}
}

func TestDrainReplay_RespectsCtxDone(t *testing.T) {
	ch := make(chan acp.SessionNotification)
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	ns := drainReplay(ctx, ch, time.Hour)

	if len(ns) != 0 {
		t.Fatalf("len(ns) = %d, want 0", len(ns))
	}
}

func TestExtractMessageChunk(t *testing.T) {
	role, text := extractMessageChunk(userChunk("s", "x").Update)
	if role != "user" || text != "x" {
		t.Fatalf("user: got (%q, %q)", role, text)
	}

	role, text = extractMessageChunk(acp.SessionUpdate{Plan: &acp.SessionUpdatePlan{}})
	if role != "" || text != "" {
		t.Fatalf("plan: got (%q, %q)", role, text)
	}
}
