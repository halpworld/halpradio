package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/halpworld/halpradio/pkg/party"
	"github.com/halpworld/halpradio/pkg/player"
	"github.com/halpworld/halpradio/pkg/radio"
	"github.com/halpworld/halpradio/pkg/util"
)

func TestPartyModalKeyAndStateFlow(t *testing.T) {
	store := radio.NewStore()
	mockPlayer := player.NewMockPlayer(80, nil)
	cfg := util.DefaultConfig()
	cfg.PartyNickname = "tester"

	m := NewModel(store, mockPlayer, cfg)

	// 1. Press ctrl+p -> should open Party modal at Screen 0
	msgCtrlP := tea.KeyMsg{Type: tea.KeyCtrlP}
	newModel, _ := m.Update(msgCtrlP)
	m = newModel.(Model)

	if !m.ShowPartyModal {
		t.Errorf("Expected ShowPartyModal to be true after pressing Ctrl+p")
	}
	if m.PartyModalScreen != 0 {
		t.Errorf("Expected PartyModalScreen to be 0 (Menu), got %d", m.PartyModalScreen)
	}

	// 2. Press "1" to navigate to Create Room screen
	msg1 := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}}
	newModel, _ = m.Update(msg1)
	m = newModel.(Model)

	if m.PartyModalScreen != 1 {
		t.Errorf("Expected PartyModalScreen to be 1 (Create), got %d", m.PartyModalScreen)
	}

	// 3. Press Enter on Create Room screen -> creates PartySession
	msgEnter := tea.KeyMsg{Type: tea.KeyEnter}
	newModel, _ = m.Update(msgEnter)
	m = newModel.(Model)

	if m.ShowPartyModal {
		t.Errorf("Expected ShowPartyModal to be closed after room creation")
	}
	if m.PartySession == nil || !m.PartySession.IsActive() {
		t.Fatalf("Expected active PartySession after room creation")
	}
	defer m.PartySession.Close()

	if !m.PartySession.IsHost() {
		t.Errorf("Expected creator to be Host")
	}

	// 4. In Party Mode: Press "1" (🔥) to broadcast reaction
	newModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	m = newModel.(Model)

	activeReacts := m.PartySession.ActiveReactions()
	if len(activeReacts) != 1 || activeReacts[0].Emoji != "🔥" {
		t.Errorf("Expected 1 active 🔥 reaction, got %+v", activeReacts)
	}

	// 5. In Party Mode: Press "t" to open Chat Ping
	newModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	m = newModel.(Model)

	if !m.IsChatting {
		t.Errorf("Expected IsChatting to be true after pressing 't'")
	}

	// Type chat: "hey"
	for _, r := range "hey" {
		newModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = newModel.(Model)
	}
	if m.ChatInput != "hey" {
		t.Errorf("Expected ChatInput to be 'hey', got %q", m.ChatInput)
	}

	// Press enter to send chat
	newModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = newModel.(Model)

	if m.IsChatting {
		t.Errorf("Expected IsChatting to be false after pressing Enter")
	}
	recentChat := m.PartySession.RecentChat()
	if len(recentChat) != 1 || recentChat[0].Message != "hey" {
		t.Errorf("Expected recent chat message 'hey', got %+v", recentChat)
	}

	// 6. Incoming PartyPlaybackSyncMsg should tune the player
	testStation := radio.Station{
		ID:   "sync-test-id",
		Name: "Sync Test Station",
		URL:  "http://example.com/sync",
	}
	store.Bundled = append(store.Bundled, testStation)

	syncMsg := PartyPlaybackSyncMsg(party.SyncPayload{
		StationID:   testStation.ID,
		StationName: testStation.Name,
		StreamURL:   testStation.URL,
		Status:      "playing",
	})
	newModel, _ = m.Update(syncMsg)
	m = newModel.(Model)

	if m.PlayingID != testStation.ID {
		t.Errorf("Expected PlayingID to be %s after sync, got %s", testStation.ID, m.PlayingID)
	}

	// 7. Open Party Manager again with Ctrl+p -> should show Active Dashboard (Screen 3)
	newModel, _ = m.Update(msgCtrlP)
	m = newModel.(Model)

	if !m.ShowPartyModal || m.PartyModalScreen != 3 {
		t.Errorf("Expected ShowPartyModal=true and PartyModalScreen=3, got screen %d", m.PartyModalScreen)
	}

	// Press "d" to toggle DJ pass
	newModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m = newModel.(Model)
	if m.PartySession.DJPass() != party.DJPassOpenDemocracy {
		t.Errorf("Expected DJPass to toggle to Open Democracy, got %s", m.PartySession.DJPass())
	}

	// Press "l" to leave room
	newModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = newModel.(Model)

	if m.PartySession != nil {
		t.Errorf("Expected PartySession to be nil after leaving room")
	}
	if m.ShowPartyModal {
		t.Errorf("Expected modal to close after leaving room")
	}
}
