package support

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"../config"
	"../constants"
	"../disc"
	"../fact"
	"../glob"
	"../logs"

	embed "github.com/Clinet/discordgo-embed"
	"github.com/hpcloud/tail"
)

// Chat pipes in-game chat to Discord, and handles log events
func Chat() {

	go func() {
		for {

			t, err := tail.TailFile(glob.GameLogName, tail.Config{Follow: true})
			if err != nil {
				logs.LogWithoutEcho(fmt.Sprintf("An error occurred when attempting to tail logfile %s Details: %s", glob.GameLogName, err))
				fact.DoExit()
			}

			//*****************
			//TAIL LOOP
			//*****************
			for line := range t.Lines {

				//Strip stuff we don't want
				lineText := StripControlAndSubSpecial(line.Text)

				linelen := len(lineText)
				//Ignore blanks
				if lineText == "" || linelen <= 2 {
					continue
				}

				//Server is alive
				fact.SetFactRunning(true, false)

				if linelen > 4096 {
					//Message too long
					logs.Log("Line from factorio was too long.")
					continue
				}

				//***************************************
				//Pre-process lines for quick use
				//This could be optimized,
				//but would be at cost of repeated code
				//***************************************

				//Timecode removal
				trimmed := strings.TrimLeft(lineText, " ")
				words := strings.Split(trimmed, " ")
				numwords := len(words)
				NoTC := constants.Unknown
				NoDS := constants.Unknown

				if numwords > 1 {
					NoTC = strings.Join(words[1:], " ")
				}
				if numwords > 2 {
					NoDS = strings.Join(words[2:], " ")
				}

				//Seperate args -- for use with script output
				linelist := strings.Split(lineText, " ")
				linelistlen := len(linelist)

				//Seperate args, notc -- for use with factorio subsystem output
				notclist := strings.Split(NoTC, " ")
				notclistlen := len(notclist)

				//Seperate args, nods -- for use with normal factorio log output
				nodslist := strings.Split(NoDS, " ")
				nodslistlen := len(nodslist)

				//Lowercase converted
				lowerline := strings.ToLower(lineText)
				lowerlist := strings.Split(lowerline, " ")
				lowerlistlen := len(lowerlist)

				//Decrement every time we see activity, if we see time not progressing, add two
				glob.PausedTicksLock.Lock()
				if glob.PausedTicks > 0 {
					glob.PausedTicks--
				}
				glob.PausedTicksLock.Unlock()

				//********************************
				//FILTERED AREA
				//NO CMD, ESCAPED OR CONSOLE CHAT
				//*********************************
				if !strings.HasPrefix(lineText, "[CMD]") && !strings.HasPrefix(lineText, "~") && !strings.HasPrefix(lineText, "<server>") {

					//*****************
					//NO CHAT AREA
					//*****************
					if !strings.HasPrefix(NoDS, "[CHAT]") {

						//*****************
						//GET FACTORIO TIME
						//*****************
						if strings.Contains(lowerline, " second") || strings.Contains(lowerline, " minute") || strings.Contains(lowerline, " hour") || strings.Contains(lowerline, " day") {

							day := 0
							hour := 0
							minute := 0
							second := 0

							//TODO
							//We should check, that at least one starts on 2nd word
							if lowerlistlen > 1 {

								for x := 0; x < lowerlistlen; x++ {
									if strings.Contains(lowerlist[x], "day") {
										day, _ = strconv.Atoi(lowerlist[x-1])
									} else if strings.Contains(lowerlist[x], "hour") {
										hour, _ = strconv.Atoi(lowerlist[x-1])
									} else if strings.Contains(lowerlist[x], "minute") {
										minute, _ = strconv.Atoi(lowerlist[x-1])
									} else if strings.Contains(lowerlist[x], "second") {
										second, _ = strconv.Atoi(lowerlist[x-1])
									}
								}
								newtime := fmt.Sprintf("%.2d-%.2d-%.2d-%.2d", day, hour, minute, second)

								//Pause detection
								glob.GametimeLock.Lock()
								glob.PausedTicksLock.Lock()

								if glob.LastGametime == glob.Gametime {
									if glob.PausedTicks <= constants.PauseThresh {
										glob.PausedTicks = glob.PausedTicks + 2
									}
								} else {
									glob.PausedTicks = 0
								}
								glob.LastGametime = glob.Gametime
								glob.GametimeString = lowerline
								glob.Gametime = newtime

								glob.PausedTicksLock.Unlock()
								glob.GametimeLock.Unlock()
							}
							//This might block stuff by accident
							//continue
						}

						//*****************
						//COMMAND REPORTING
						//*****************
						if strings.HasPrefix(lineText, "[CMD]") {
							logs.Log(lineText)
							continue
						}

						//*****************
						//USER REPORT
						//*****************
						if strings.HasPrefix(lineText, "[REPORT]") {
							if linelistlen >= 3 {
								buf := fmt.Sprintf("**USER REPORT:**\nServer: %v, User: %v: Report:\n %v",
									config.Config.ChannelName, linelist[1], strings.Join(linelist[2:], " "))
								fact.CMS(config.Config.ModerationChannel, buf)
								logs.Log(lineText)
							}
							continue
						}

						//*****************
						//ACCESS
						//*****************
						if strings.HasPrefix(lineText, "[ACCESS]") {
							if linelistlen == 4 {
								//Format:
								//print("[ACCESS] " .. ptype .. " " .. player.name .. " " .. param.parameter)

								ptype := linelist[1]
								pname := linelist[2]
								code := linelist[3]

								//Filter just in case, and so accidental spaces won't ruin passcodes
								code = strings.ReplaceAll(code, ":", "")
								code = strings.ReplaceAll(code, ",", "")
								code = strings.ReplaceAll(code, " ", "")
								code = strings.ReplaceAll(code, "\n", "")
								code = strings.ReplaceAll(code, "\r", "")

								pname = strings.ReplaceAll(pname, ":", "")
								pname = strings.ReplaceAll(pname, ",", "")
								pname = strings.ReplaceAll(pname, " ", "")
								pname = strings.ReplaceAll(pname, "\n", "")
								pname = strings.ReplaceAll(pname, "\r", "")

								codegood := true
								codefound := false
								plevel := 0

								glob.PasswordListLock.Lock()
								for i := 0; i <= glob.PasswordMax && i <= constants.MaxPasswords; i++ {
									if glob.PasswordList[i] == code {
										codefound = true
										//Delete password from list
										glob.PasswordList[i] = ""
										pid := glob.PasswordID[i]
										glob.PasswordID[i] = ""
										glob.PasswordTime[i] = 0

										newrole := ""
										if ptype == "normal" {
											newrole = config.Config.MembersRole
											plevel = 0
										} else if ptype == "trusted" {
											newrole = config.Config.MembersRole
											plevel = 1
										} else if ptype == "regular" {
											newrole = config.Config.RegularsRole
											plevel = 2
										} else if ptype == "admin" {
											newrole = config.Config.AdminsRole
											plevel = 255
										}

										discid := disc.GetDiscordIDFromFactorioName(pname)
										factname := disc.GetFactorioNameFromDiscordID(pid)

										if discid == pid && factname == pname {
											fact.WriteFact(fmt.Sprintf("/cwhisper %s This Factorio account, and Discord account are already connected! Setting role, if needed.", pname))
											codegood = true
											//Do not break, process
										} else if discid != "" {
											logs.Log(fmt.Sprintf("Factorio user '%s' tried to connect a Discord user, that is already connected to a different Factorio user.", pname))
											fact.WriteFact(fmt.Sprintf("/cwhisper %s that discord user is already connected to a different Factorio user.", pname))
											codegood = false
											continue
										} else if factname != "" {
											logs.Log(fmt.Sprintf("Factorio user '%s' tried to connect their Factorio user, that is already connected to a different Discord user.", pname))
											fact.WriteFact(fmt.Sprintf("/cwhisper %s This Factorio user is already connected to a different discord user.", pname))
											codegood = false
											continue
										}

										if codegood == true {
											fact.PlayerSetID(pname, pid, plevel)

											guild := fact.GetGuild()
											if guild != nil {
												errrole, regrole := disc.RoleExists(guild, newrole)

												if !errrole {
													fact.LogCMS(config.Config.FactorioChannelID, fmt.Sprintf("Sorry, there is an error. I couldn't find the Discord role '%s'.", newrole))
													fact.WriteFact(fmt.Sprintf("/cwhisper %s Sorry, there was an internal error, I coudn't find the Discord role '%s' Let the moderators know!", newrole, pname))
													continue
												}

												erradd := disc.SmartRoleAdd(config.Config.GuildID, pid, regrole.ID)

												if erradd != nil || glob.DS == nil {
													fact.CMS(config.Config.FactorioChannelID, fmt.Sprintf("Sorry, there is an error. I couldn't assign the Discord role '%s'.", newrole))
													fact.WriteFact(fmt.Sprintf("/cwhisper %s Sorry, there was an error, coundn't assign role '%s' Let the moderators know!", newrole, pname))
													continue
												}
												fact.WriteFact(fmt.Sprintf("/cwhisper %s Registration complete!", pname))
												fact.LogCMS(config.Config.FactorioChannelID, pname+": Registration complete!")
												continue
											} else {
												logs.Log("No guild info.")
												fact.CMS(config.Config.FactorioChannelID, "Sorry, I couldn't find the guild info!")
												continue
											}
										}
										continue
									}
								} //End of loop
								glob.PasswordListLock.Unlock()
								if codefound == false {
									logs.Log(fmt.Sprintf("Factorio user '%s', tried to use an invalid or expired code.", pname))
									fact.WriteFact(fmt.Sprintf("/cwhisper %s Sorry, that code is invalid or expired. Make sure you are entering the code on the correct Factorio server!", pname))
									continue
								}
							} else {
								logs.Log("Internal error, [ACCESS] had wrong argument count.")
								continue
							}
							continue
						}

						//***********************
						//CAPTURE ONLINE PLAYERS
						//***********************
						if strings.HasPrefix(lineText, "Online players") {

							if linelistlen > 2 {
								poc := strings.Join(linelist[2:], " ")
								poc = strings.ReplaceAll(poc, "(", "")
								poc = strings.ReplaceAll(poc, ")", "")
								poc = strings.ReplaceAll(poc, ":", "")
								poc = strings.ReplaceAll(poc, " ", "")

								nump, _ := strconv.Atoi(poc)
								fact.SetNumPlayers(nump)

								glob.RecordPlayersLock.Lock()
								if nump > glob.RecordPlayers {
									glob.RecordPlayers = nump

									//New thread, avoid deadlock
									go func() {
										fact.WriteRecord()
									}()

									buf := fmt.Sprintf("**New record!** Players online: %v", glob.RecordPlayers)
									fact.CMS(config.Config.FactorioChannelID, buf)
									//write to factorio as well
									buf = strings.ReplaceAll(buf, "*", "") //Remove bold
									fact.WriteFact("/cchat " + buf)

								}
								glob.RecordPlayersLock.Unlock()

								fact.UpdateChannelName()
							}
							continue
						}
						//*****************
						//JOIN AREA
						//*****************
						if strings.HasPrefix(NoDS, "[JOIN]") {
							fact.WriteFact("/p o c")

							if nodslistlen > 1 {
								pname := StripControlAndSubSpecial(nodslist[1])
								glob.NumLoginsLock.Lock()
								glob.NumLogins = glob.NumLogins + 1
								glob.NumLoginsLock.Unlock()

								if fact.PlayerLevelGet(pname) == 0 {
									skip := false

									//Don't block, make new thread
									if skip == false {
										go func(pname string) {

											time.Sleep(5 * time.Second)
											fact.WriteFact(fmt.Sprintf("/cwhisper %s %s[@ChatWire][/color] %sWelcome, use ` or ~ to chat, or to use commands. /help shows all commands.[/color]", pname, fact.RandomColor(false), fact.RandomColor(false)))

											time.Sleep(5 * time.Second)
											fact.WriteFact(fmt.Sprintf("/cwhisper %s %s[@ChatWire][/color] %sYou are currently a new player, some actions will not be available for you at first.[/color]", pname, fact.RandomColor(false), fact.RandomColor(false)))

											time.Sleep(10 * time.Second)
											fact.WriteFact(fmt.Sprintf("/cwhisper %s %s[@ChatWire][/color] %sFor more information, check out our Discord, copy-paste link is at the top-left of your screen.[/color]", pname, fact.RandomColor(false), fact.RandomColor(false)))

											time.Sleep(10 * time.Second)
											fact.WriteFact(fmt.Sprintf("/cwhisper %s %s[@ChatWire][/color] %sYou can report issues or abuse with /report <your message here>[/color]", pname, fact.RandomColor(false), fact.RandomColor(false)))

										}(pname)
									}
								}
								plevelname := fact.AutoPromote(pname)

								//Remove discord markdown
								regf := regexp.MustCompile(`\*+`)
								regg := regexp.MustCompile(`\~+`)
								regh := regexp.MustCompile(`\_+`)
								for regf.MatchString(pname) || regg.MatchString(pname) || regh.MatchString(pname) {
									//Filter discord tags
									pname = regf.ReplaceAllString(pname, "")
									pname = regg.ReplaceAllString(pname, "")
									pname = regh.ReplaceAllString(pname, "")
								}

								buf := fmt.Sprintf("`%-11s` **%s joined**%s", fact.GetGameTime(), pname, plevelname)
								fact.CMS(config.Config.FactorioChannelID, buf)
								//fact.UpdateChannelName()
							}
							continue
						}
						//*****************
						//LEAVE
						//*****************
						if strings.HasPrefix(NoDS, "[LEAVE]") {
							fact.WriteFact("/p o c")

							if nodslistlen > 1 {
								pname := nodslist[1]

								go func() {

									//Don't bother with this on whitelist servers
									if glob.WhitelistMode == false {
										// Don't save if we saved recently
										t := time.Now()
										if t.Sub(fact.GetSaveTimer()).Seconds() > constants.SaveThresh {

											fact.SaveFactorio()
											fact.SetSaveTimer()
										}
									}

								}()

								go func(factname string) {
									fact.UpdateSeen(factname)
								}(pname)

								//Remove discord markdown
								regf := regexp.MustCompile(`\*+`)
								regg := regexp.MustCompile(`\~+`)
								regh := regexp.MustCompile(`\_+`)
								for regf.MatchString(pname) || regg.MatchString(pname) || regh.MatchString(pname) {
									//Filter discord tags
									pname = regf.ReplaceAllString(pname, "")
									pname = regg.ReplaceAllString(pname, "")
									pname = regh.ReplaceAllString(pname, "")
								}
								fact.CMS(config.Config.FactorioChannelID, fmt.Sprintf("`%-11s` *%s left*", fact.GetGameTime(), pname))
								//fact.UpdateChannelName()
							}
							continue
						}
						//*****************
						//MAP END
						//*****************
						if strings.HasPrefix(lineText, "[END]MAPEND") {
							if fact.IsFactRunning() {
								go func() {
									msg := "Server will shutdown in 5 minutes."
									fact.WriteFact("/cchat " + msg)
									fact.CMS(config.Config.FactorioChannelID, msg)
									time.Sleep(5 * time.Minute)

									msg = "Server shutting down."
									fact.WriteFact("/cchat " + msg)
									fact.CMS(config.Config.FactorioChannelID, msg)
									time.Sleep(10 * time.Second)

									if fact.IsFactRunning() {
										fact.CMS(config.Config.FactorioChannelID, "Stopping Factorio, and disabling auto-launch.")
										fact.SetRelaunchThrottle(0)
										fact.SetAutoStart(false)
										fact.QuitFactorio()
									}
								}()
							}
						}

						//*****************
						//MSG AREA
						//*****************
						if strings.HasPrefix(lineText, "[MSG]") {

							if linelistlen > 1 {
								trustname := linelist[1]

								if strings.Contains(lineText, " is now a member!") {
									fact.PlayerLevelSet(trustname, 1)
									continue
								} else if strings.Contains(lineText, " is now a regular!") {
									fact.PlayerLevelSet(trustname, 2)
									continue
								} else if strings.Contains(lineText, " moved to Admins group.") {
									fact.PlayerLevelSet(trustname, 255)
									continue
								} else if strings.Contains(lineText, " to the map!") && strings.Contains(lineText, "Welcome ") {
									btrustname := linelist[2]
									fact.WriteFact(fmt.Sprintf("/pcolor %s %s", btrustname, fact.RandomColor(true)))
									fact.AutoPromote(btrustname)
								} else if strings.Contains(lineText, " has nil permissions.") {
									fact.AutoPromote(trustname)
									continue
								}

								fact.CMS(config.Config.FactorioChannelID, fmt.Sprintf("`%-11s` %s", fact.GetGameTime(), strings.Join(linelist[1:], " ")))
							}
							continue
						}
						//*****************
						//BAN
						//*****************
						if strings.HasPrefix(NoDS, "[BAN]") {

							if nodslistlen > 1 {
								trustname := nodslist[1]

								if strings.Contains(NoDS, "was banned by") {
									fact.PlayerLevelSet(trustname, -1)
									go func() {
										fact.WritePlayers()
									}()
								}

								fact.LogCMS(config.Config.FactorioChannelID, fmt.Sprintf("`%-11s` %s", fact.GetGameTime(), strings.Join(nodslist[1:], " ")))
							}
							continue
						}
						//*****************
						//(ONLINE)
						//*****************
						//if strings.Contains(lineText, "(online)") {

						//Upgrade or replace this...
						//fact.CMS(config.Config.FactorioChannelID, lineText)
						//continue
						//}

						//*****************
						//Pause on catch-up
						//*****************
						if strings.ToLower(config.Config.PauseOnConnect) == "yes" ||
							strings.ToLower(config.Config.PauseOnConnect) == "true" {

							tn := time.Now()

							if strings.HasPrefix(NoTC, "Info ServerMultiplayerManager") {

								if strings.Contains(lineText, "removing peer") {
									fact.WriteFact("/p o c")
									//Fix for players leaving with no leave message
								} else if strings.Contains(lineText, "oldState(ConnectedLoadingMap) newState(TryingToCatchUp)") {
									if config.Config.SlowGSpeed == "" {
										fact.WriteFact("/gspeed 0.166666666667")
									} else {
										fact.WriteFact("/gspeed " + config.Config.SlowGSpeed)
									}

									glob.ConnectPauseLock.Lock()
									glob.ConnectPauseTimer = tn.Unix()
									glob.ConnectPauseCount++
									glob.ConnectPauseLock.Unlock()

								} else if strings.Contains(lineText, "oldState(WaitingForCommandToStartSendingTickClosures) newState(InGame)") {

									glob.ConnectPauseLock.Lock()

									glob.ConnectPauseCount--
									if glob.ConnectPauseCount <= 0 {
										glob.ConnectPauseCount = 0
										glob.ConnectPauseTimer = 0

										if config.Config.DefaultGSpeed != "" {
											fact.WriteFact("/gspeed " + config.Config.DefaultGSpeed)
										} else {
											fact.WriteFact("/gspeed 1.0")
										}
									}

									glob.ConnectPauseLock.Unlock()
								}

							}
						}

						//*****************
						//MAP LOAD
						//*****************
						if strings.HasPrefix(NoTC, "Loading map") {

							//Strip file path
							if notclistlen > 3 {
								fullpath := notclist[2]
								size := notclist[3]
								sizei, _ := strconv.Atoi(size)
								fullpath = strings.Replace(fullpath, ":", "", -1)

								regaa := regexp.MustCompile(`\/.*?\/saves\/`)
								filename := regaa.ReplaceAllString(fullpath, "")

								glob.GameMapLock.Lock()
								glob.GameMapName = filename
								glob.GameMapPath = fullpath
								glob.GameMapLock.Unlock()

								fsize := 0.0
								if sizei > 0 {
									fsize = (float64(sizei) / 1024.0 / 1024.0)
								}

								buf := fmt.Sprintf("Loading map %s (%.2fmb)...", filename, fsize)
								logs.Log(buf)
							} else { //Just in case
								logs.Log("Loading map...")
							}
							continue
						}
						//******************
						//RESET MOD MESSAGE
						//******************
						if strings.HasPrefix(NoTC, "Loading mod core") {
							glob.ModLoadLock.Lock()
							glob.ModLoadMessage = nil
							glob.ModLoadString = constants.Unknown
							glob.ModLoadLock.Unlock()
							continue
						}
						//*****************
						//LOADING MOD
						//*****************
						if strings.HasPrefix(NoTC, "Loading mod") && strings.Contains(NoTC, "(data.lua)") &&
							!strings.Contains(NoTC, "settings") && !strings.Contains(NoTC, "base") && !strings.Contains(NoTC, "core") {

							if notclistlen > 4 && glob.DS != nil {

								glob.ModLoadLock.Lock()

								if glob.ModLoadMessage == nil {
									modmess, cerr := glob.DS.ChannelMessageSend(config.Config.AuxChannel, "Loading mods...")
									if cerr != nil {
										logs.Log(fmt.Sprintf("An error occurred when attempting to send mod load message. Details: %s", cerr))
										glob.ModLoadMessage = nil
										glob.ModLoadString = constants.Unknown

									} else {
										glob.ModLoadMessage = modmess

										if glob.ModLoadString == constants.Unknown {
											glob.ModLoadString = strings.Join(notclist[2:4], "-")
										}
										//_, err := glob.DS.ChannelMessageEdit(config.Config.AuxChannel, glob.ModLoadMessage.ID, "Loading mods: "+glob.ModLoadString)

										//if err != nil {
										//	logs.Log(fmt.Sprintf("An error occurred when attempting to edit mod load message. Details: %s", err))
										//}
									}
								} else {

									glob.ModLoadString = glob.ModLoadString + ", " + strings.Join(notclist[2:4], "-")
									//_, err := glob.DS.ChannelMessageEdit(config.Config.AuxChannel, glob.ModLoadMessage.ID, "Loading mods: "+glob.ModLoadString)
									//if err != nil {
									//	logs.Log(fmt.Sprintf("An error occurred when attempting to edit mod load message. Details: %s", err))
									//}
								}

								glob.ModLoadLock.Unlock()
							}
							continue
						}

						//*****************
						//GOODBYE
						//*****************
						if strings.HasPrefix(NoTC, "Goodbye") {
							logs.Log("Factorio is now offline.")
							fact.SetFactorioBooted(false)
							fact.SetFactRunning(false, false)
							continue
						}

						//*****************
						//READY MESSAGE
						//*****************
						// 5.164 Info RemoteCommandProcessor.cpp:131: Starting RCON interface at IP ADDR:({0.0.0.0:9100})
						if strings.HasPrefix(NoTC, "Info RemoteCommandProcessor") && strings.Contains(NoTC, "Starting RCON interface") {
							fact.SetFactorioBooted(true)
							fact.LogCMS(config.Config.FactorioChannelID, "Factorio "+glob.FactorioVersion+" is now online.")
							fact.WriteFact("/p o c")

							fact.WriteFact("/cname " + strings.ToUpper(config.Config.ChannelName))

							//Send whitelist
							if glob.WhitelistMode {
								fact.SetNoResponseCount(0)
								glob.PlayerListLock.RLock()
								var pcount = 0
								for i := 0; i <= glob.PlayerListMax; i++ {
									if glob.PlayerList[i].Name != "" && glob.PlayerList[i].Level > 1 {
										pcount++
										fact.WhitelistPlayer(glob.PlayerList[i].Name, glob.PlayerList[i].Level)
									}
									fact.SetNoResponseCount(0)
								}
								glob.PlayerListLock.RUnlock()
								fact.SetNoResponseCount(0)
								if pcount > 0 {
									buf := fmt.Sprintf("Whitelist of %d players sent.", pcount)
									fact.LogCMS(config.Config.FactorioChannelID, buf)
								}
							}
							continue
						}

						//*********************
						//GET FACTORIO VERSION
						//*********************
						if strings.HasPrefix(NoTC, "Loading mod base") {
							if notclistlen > 3 {
								glob.FactorioVersion = notclist[3]
							}
							continue
						}

						//**********************
						//CAPTURE SAVE MESSAGES
						//**********************
						if strings.HasPrefix(NoTC, "Info AppManagerStates") && strings.Contains(NoTC, "Saving finished") {
							fact.SetSaveTimer()
							continue
						}
						//**************************
						//CAPTURE MAP NAME, ON EXIT
						//**************************
						if strings.HasPrefix(NoTC, "Info MainLoop") && strings.Contains(NoTC, "Saving map as") {

							//Strip file path
							if notclistlen > 5 {
								fullpath := notclist[5]
								regaa := regexp.MustCompile(`\/.*?\/saves\/`)
								filename := regaa.ReplaceAllString(fullpath, "")
								filename = strings.Replace(filename, ":", "", -1)

								glob.GameMapLock.Lock()
								glob.GameMapName = filename
								glob.GameMapPath = fullpath
								glob.GameMapLock.Unlock()

								logs.Log("Map saved as: " + filename)
							}
							continue
						}
						//*****************
						//CAPTURE DESYNC
						//*****************
						if strings.HasPrefix(NoTC, "Info") {
							if strings.Contains(NoTC, "DesyncedWaitingForMap") {
								logs.Log("desync: " + NoTC)
								continue
							}
						}
						//*****************
						//CAPTURE CRASHES
						//*****************
						if strings.HasPrefix(NoTC, "Error") && fact.IsFactRunning() {
							fact.CMS(config.Config.AuxChannel, "error: "+NoTC)
							//Lock error
							if strings.Contains(NoTC, "Couldn't acquire exclusive lock") {
								fact.CMS(config.Config.FactorioChannelID, "Factorio is already running.")
								fact.SetAutoStart(false)
								fact.SetFactorioBooted(false)
								fact.SetFactRunning(false, true)
								continue
							}
							//Mod Errors
							if strings.Contains(NoTC, "caused a non-recoverable error.") {
								fact.CMS(config.Config.FactorioChannelID, "Factorio crashed.")
								fact.SetFactorioBooted(false)
								fact.SetFactRunning(false, true)
								continue
							}
							//Stack traces
							if strings.Contains(NoTC, "Hosting multiplayer game failed") {
								fact.CMS(config.Config.FactorioChannelID, "Factorio was unable to launch.")
								fact.SetAutoStart(false)
								fact.SetFactorioBooted(false)
								fact.SetFactRunning(false, true)
								continue
							}
							//Stack traces
							if strings.Contains(NoTC, "Unexpected error occurred.") {
								fact.CMS(config.Config.FactorioChannelID, "Factorio crashed.")
								fact.SetFactorioBooted(false)
								fact.SetFactRunning(false, true)
								continue
							}
							//Multiplayer manger
							if strings.Contains(NoTC, "ServerMultiplayerManager") {
								if strings.Contains(NoTC, "MultiplayerManager failed:") &&
									strings.Contains(NoTC, "info.json not found (No such file or directory)") {
									fact.CMS(config.Config.FactorioChannelID, "Unable to load save-game.")
									fact.SetAutoStart(false)
									fact.SetFactorioBooted(false)
									fact.SetFactRunning(false, true)
									continue
								}
							}
							continue
						}

					}
					//***********************
					//FACTORIO CHAT MESSAGES
					//***********************
					if strings.HasPrefix(NoDS, "[CHAT]") || strings.HasPrefix(NoDS, "[SHOUT]") {

						if nodslistlen > 1 {
							nodslist[1] = strings.Replace(nodslist[1], ":", "", -1)
							pname := nodslist[1]

							if pname != "<server>" {

								cmess := strings.Join(nodslist[2:], " ")
								cmess = StripControlAndSubSpecial(cmess)
								//cmess = unidecode.Unidecode(cmess)

								//Remove factorio tags
								rega := regexp.MustCompile(`\[/[^][]+\]`) //remove close tags [/color]

								regc := regexp.MustCompile(`\[color=(.*?)\]`) //remove [color=*]
								regd := regexp.MustCompile(`\[font=(.*?)\]`)  //remove [font=*]

								rege := regexp.MustCompile(`\[(.*?)=(.*?)\]`) //Sub others

								regf := regexp.MustCompile(`\*+`) //Remove discord markdown
								regg := regexp.MustCompile(`\~+`)
								regh := regexp.MustCompile(`\_+`)

								cmess = strings.Replace(cmess, "\n", " ", -1)
								cmess = strings.Replace(cmess, "\r", " ", -1)
								cmess = strings.Replace(cmess, "\t", " ", -1)

								for regc.MatchString(cmess) || regd.MatchString(cmess) {
									//Remove colors/fonts
									cmess = regc.ReplaceAllString(cmess, "")
									cmess = regd.ReplaceAllString(cmess, "")
								}
								for rege.MatchString(cmess) {
									//Sub
									cmess = rege.ReplaceAllString(cmess, " [${1}: ${2}] ")
								}
								for rega.MatchString(cmess) {
									//Filter close tags
									cmess = rega.ReplaceAllString(cmess, "")
								}

								for regf.MatchString(cmess) || regg.MatchString(cmess) || regh.MatchString(cmess) {
									//Filter discord tags
									cmess = regf.ReplaceAllString(cmess, "")
									cmess = regg.ReplaceAllString(cmess, "")
									cmess = regh.ReplaceAllString(cmess, "")
								}

								if len(cmess) > 500 {
									cmess = fmt.Sprintf("%s**(message cut, too long!)**", TruncateString(cmess, 500))
								}

								//Yeah, on different thread please.
								go func(ptemp string) {
									fact.UpdateSeen(ptemp)
								}(pname)

								did := disc.GetDiscordIDFromFactorioName(pname)
								dname := disc.GetNameFromID(did, false)
								avatar := disc.GetDiscordAvatarFromId(did, 64)
								factname := StripControlAndSubSpecial(pname)
								factname = TruncateString(factname, 25)

								fbuf := ""
								//Filter Factorio names
								factname = regf.ReplaceAllString(factname, "")
								factname = regg.ReplaceAllString(factname, "")
								factname = regh.ReplaceAllString(factname, "")
								if dname != "" {
									fbuf = fmt.Sprintf("`%-11s` **%s**: %s", fact.GetGameTime(), factname, cmess)
								} else {
									fbuf = fmt.Sprintf("`%-11s` %s: %s", fact.GetGameTime(), factname, cmess)
								}

								//Remove all but letters
								filter, _ := regexp.Compile("[^a-zA-Z]+")

								//Name to lowercase
								dnamelower := strings.ToLower(dname)
								fnamelower := strings.ToLower(pname)

								//Reduce to letters only
								dnamereduced := filter.ReplaceAllString(dnamelower, "")
								fnamereduced := filter.ReplaceAllString(fnamelower, "")

								//If we find discord name, and discord name and factorio name don't contain the same name
								if dname != "" && !strings.Contains(dnamereduced, fnamereduced) && !strings.Contains(fnamereduced, dnamereduced) {
									//Slap data into embed format.
									myembed := embed.NewEmbed().
										SetAuthor("@"+dname, avatar).
										SetDescription(fbuf).
										MessageEmbed

									//Send it off!
									err := disc.SmartWriteDiscordEmbed(config.Config.FactorioChannelID, myembed)
									if err != nil {
										//On failure, send normal message
										logs.Log("Failed to send chat embed.")
									} else {
										//Stop if succeeds
										continue
									}
								}
								fact.CMS(config.Config.FactorioChannelID, fbuf)
							}
							continue
						}
						continue
					}
					//*****************
					//END CHAT
					//*****************
				}
				//*****************
				//END FILTERED
				//*****************

				//*****************
				//"/online"
				//*****************
				if strings.HasPrefix(lineText, "~") {
					if strings.Contains(lineText, "Activity: ") && strings.Contains(lineText, "Online: ") &&
						(strings.Contains(lineText, ", (Members)") ||
							strings.Contains(lineText, ", (Regulars)") ||
							strings.Contains(lineText, ", (NEW)") ||
							strings.Contains(lineText, ", (Admins)")) {

						//Upgrade or replace this...
						fact.CMS(config.Config.FactorioChannelID, lineText)
						continue
					}
				}

			}
			//*****************
			//END TAIL LOOP
			//*****************
		}
	}()
}
