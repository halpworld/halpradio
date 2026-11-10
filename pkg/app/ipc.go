package app

import (
	"errors"
	"fmt"
	"io"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/halpworld/halpradio/pkg/desktop"
	"github.com/halpworld/halpradio/pkg/party"
	"github.com/halpworld/halpradio/pkg/ui"
)

// dispatchPartyIPC turns a party action received over the local IPC socket
// into a UI message. A join carries only an invite (a room code, optionally
// with the host fingerprint): a peer address is never taken from the IPC
// payload (LOC-7), and anything party.ParseInvite refuses is refused.
func dispatchPartyIPC(send func(tea.Msg), action desktop.MediaAction, payload string) error {
	if send == nil {
		return fmt.Errorf("halpradio program not ready")
	}
	switch action {
	case desktop.ActionPartyCreate:
		name := strings.TrimSpace(payload)
		if name == "" {
			name = "team-focus"
		}
		send(ui.PartyCreateRoomMsg{RoomName: name})
	case desktop.ActionPartyJoin:
		code, pin, err := party.ParseInvite(payload)
		if err != nil {
			return err
		}
		send(ui.PartyJoinRoomMsg{RoomCode: party.FormatInvite(code, pin)})
	case desktop.ActionPartyLeave:
		send(ui.PartyLeaveRoomMsg{})
	case desktop.ActionPartyReact:
		send(ui.PartySendReactionMsg(payload))
	case desktop.ActionPartyChat:
		send(ui.PartySendChatMsg(payload))
	}
	return nil
}

// programSender returns a send function for a program that may not exist
// yet, or nil when it does not.
func programSender(program *tea.Program) func(tea.Msg) {
	if program == nil {
		return nil
	}
	return program.Send
}

// reportIPCStart tells the user when the IPC socket could not be opened. The
// app keeps running without IPC.
func reportIPCStart(out io.Writer, mgr *desktop.Manager) string {
	err := mgr.IPCError()
	if err == nil {
		return ""
	}
	var msg string
	if errors.Is(err, desktop.ErrIPCAlreadyRunning) {
		msg = "Another halpradio instance owns the remote-control socket; this one runs without IPC (halpradio remote/current/status talk to the other instance)."
	} else {
		msg = "Remote control (IPC) is off: " + safe(err.Error())
	}
	if out != nil {
		fmt.Fprintln(out, "Note: "+msg)
	}
	return msg
}
