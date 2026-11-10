package ui

// modalKind names the overlay that is drawn and receives keys.
type modalKind int

const (
	modalNone modalKind = iota
	modalApproval
	modalWhichKey
	modalPRExport
	modalArt
	modalThemePicker
	modalAdd
	modalTimer
	modalEQ
	modalParty
	modalPlugin
	modalDJ
)

// activeModal is the single priority order for overlays. View draws the
// overlay it returns and Update sends keys to the same one, so a key can
// never reach a prompt that is not on screen (FR-A-1). The plugin
// permission prompt is on top of everything.
func (m Model) activeModal() modalKind {
	switch {
	case m.ShowPermissionApproval:
		return modalApproval
	case m.ShowWhichKey:
		return modalWhichKey
	case m.ShowPRExport:
		return modalPRExport
	case m.ShowArtModal:
		return modalArt
	case m.ShowThemePicker:
		return modalThemePicker
	case m.ShowAddModal:
		return modalAdd
	case m.ShowTimerModal:
		return modalTimer
	case m.ShowEQModal:
		return modalEQ
	case m.ShowPartyModal:
		return modalParty
	case m.ShowPluginModal:
		return modalPlugin
	case m.ShowDJModal:
		return modalDJ
	}
	return modalNone
}

// busyWithOtherInput reports whether the user is in a modal other than the
// plugin manager, or typing into a text input. An install that finishes
// then does not open its permission prompt.
func (m Model) busyWithOtherInput() bool {
	switch m.activeModal() {
	case modalNone, modalPlugin:
	default:
		return true
	}
	return m.IsSearching || m.IsChatting || m.ThemeIsSearching
}
