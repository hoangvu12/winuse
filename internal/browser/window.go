package browser

// Identify names the browser a Window's process image belongs to (chrome,
// edge, ...), or "" when it is none of them. Ticket 04 reads a Window's
// Page through the Browser Connection of that browser.
func (m *Manager) Identify(exePath string) string {
	return m.sys.Env.IdentifyImage(exePath)
}
