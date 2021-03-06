package admin

import (
	"../../fact"
	"github.com/bwmarrin/discordgo"
)

// StopServer saves and stops the server.
func StopServer(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {

	fact.SetRelaunchThrottle(0)
	fact.SetAutoStart(false)
	if fact.IsFactRunning() {

		fact.CMS(m.ChannelID, "Stopping Factorio, and disabling auto-launch.")
		fact.QuitFactorio()
	} else {
		fact.CMS(m.ChannelID, "Factorio isn't running, disabling auto-launch")
	}

}
