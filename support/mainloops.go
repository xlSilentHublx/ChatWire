package support

import (
	"bufio"
	"fmt"
	"io"
	"math/rand"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"../config"
	"../constants"
	"../disc"
	"../fact"
	"../glob"
	"../logs"
	"../platform"
)

//*******************
//Main threads/loops
//*******************

func MainLoops() {

	go func() {

		//**************
		//Game watchdog
		//**************
		go func() {
			for {
				time.Sleep(constants.WatchdogInterval)

				if fact.IsFactRunning() == false && (fact.IsQueued() || fact.IsSetRebootBot() || fact.GetDoUpdateFactorio()) {
					if fact.GetDoUpdateFactorio() {
						fact.FactUpdate()
					}
					fact.DoExit()
				} else if fact.IsFactRunning() { //Currently running normally

					nores := 0
					if fact.GetPausedTicks() <= constants.PauseThresh {
						glob.NoResponseCountLock.Lock()
						glob.NoResponseCount = glob.NoResponseCount + 1
						nores = glob.NoResponseCount
						glob.NoResponseCountLock.Unlock()

						fact.WriteFact("/time")
					}

					if fact.IsFactorioBooted() {
						if nores >= 30 && nores%10 == 0 {
							fact.CMS(config.Config.FactorioChannelID, fmt.Sprintf("Game not responding for %d seconds.", nores))
						}
					}
					if nores == 120 {
						fact.DoShowLocks("")
						fact.SetNoResponseCount(0)
						fact.LogCMS(config.Config.FactorioChannelID, "Game unresponsive for 120 seconds... restarting it.")
						fact.SetFactRunning(false, true)
					}
				} else if fact.IsFactRunning() == false && fact.IsSetAutoStart() == true && fact.GetDoUpdateFactorio() == false { //Isn't running, but we should be
					//Dont relaunch if we are set to auto update

					command := config.Config.ZipScript
					out, errs := exec.Command(command, config.Config.ServerLetter).Output()
					if errs != nil {
						logs.Log(fmt.Sprintf("Unable to run soft-mod insert script. Details:\nout: %v\nerr: %v", out, errs))
					} else {
						logs.Log("Soft-mod inserted into save file.")
					}

					time.Sleep(time.Second)

					//Relaunch Throttle
					throt := fact.GetRelaunchThrottle()
					if throt > 0 {

						delay := throt * throt * 10

						if delay > 0 {
							logs.Log(fmt.Sprintf("Automatically rebooting Factroio in %d seconds.", delay))
							for i := 0; i < delay*10 && throt > 0; i++ {
								time.Sleep(100 * time.Millisecond)
							}
						}
					}
					fact.SetRelaunchThrottle(throt + 1)

					//Prevent us from distrupting updates
					glob.FactorioLaunchLock.Lock()

					var err error
					cmd := exec.Command(config.Config.Executable, config.Config.LaunchParameters...)
					platform.LinuxSetProcessGroup(cmd)
					//Used later on when binary is launched, redirects game stdout to file.
					logwriter := io.Writer(glob.GameLogDesc)

					cmd.Stdout = logwriter

					tpipe, errp := cmd.StdinPipe()

					if errp != nil {
						logs.Log(fmt.Sprintf("An error occurred when attempting to execute cmd.StdinPipe() Details: %s", errp))
						//close lock
						glob.FactorioLaunchLock.Unlock()
						fact.DoExit()
						return
					}

					if tpipe != nil && err == nil {
						glob.PipeLock.Lock()
						glob.Pipe = tpipe
						glob.PipeLock.Unlock()
					}

					//Pre-launch prep
					fact.SetFactRunning(true, false)
					fact.SetFactorioBooted(false)

					fact.SetGameTime(constants.Unknown)
					fact.SetSaveTimer()
					fact.SetNoResponseCount(0)

					err = cmd.Start()

					if err != nil {
						logs.Log(fmt.Sprintf("An error occurred when attempting to start the game. Details: %s", err))
						//close lock
						glob.FactorioLaunchLock.Unlock()
						fact.DoExit()
						return
					}
					logs.Log("Factorio booting...")

					//close lock
					glob.FactorioLaunchLock.Unlock()
				}
			}
		}()

		//*******************************
		//CMS Output from buffer, batched
		//*******************************
		go func() {
			for {

				if glob.DS != nil {

					//Check if buffer is active
					active := false
					glob.CMSBufferLock.Lock()
					if glob.CMSBuffer != nil {
						active = true
					}
					glob.CMSBufferLock.Unlock()

					//If buffer is active, sleep and wait for it to fill up
					if active {
						time.Sleep(constants.CMSRate)

						//Waited for buffer to fill up, grab and clear buffers
						glob.CMSBufferLock.Lock()
						lcopy := glob.CMSBuffer
						glob.CMSBuffer = nil
						glob.CMSBufferLock.Unlock()

						if lcopy != nil {

							var factmsg []string
							var aux []string
							var moder []string

							for _, msg := range lcopy {
								if msg.Channel == config.Config.FactorioChannelID {
									factmsg = append(factmsg, msg.Text)
								} else if msg.Channel == config.Config.AuxChannel {
									aux = append(aux, msg.Text)
								} else if msg.Channel == config.Config.ModerationChannel {
									moder = append(moder, msg.Text)
								} else {
									disc.SmartWriteDiscord(msg.Channel, msg.Text)
								}
							}

							//Send out buffer, split up if needed
							//Factorio
							buf := ""
							for _, line := range factmsg {
								oldlen := len(buf) + 1
								addlen := len(line)
								if oldlen+addlen >= 2000 {
									disc.SmartWriteDiscord(config.Config.FactorioChannelID, buf)
									buf = line
								} else {
									buf = buf + "\n" + line
								}
							}
							if buf != "" {
								disc.SmartWriteDiscord(config.Config.FactorioChannelID, buf)
							}

							//Aux
							buf = ""
							for _, line := range aux {
								oldlen := len(buf) + 1
								addlen := len(line)
								if oldlen+addlen >= 2000 {
									disc.SmartWriteDiscord(config.Config.AuxChannel, buf)
									buf = line
								} else {
									buf = buf + "\n" + line
								}
							}
							if buf != "" {
								disc.SmartWriteDiscord(config.Config.AuxChannel, buf)
							}

							//Moderation
							buf = ""
							for _, line := range moder {
								oldlen := len(buf) + 1
								addlen := len(line)
								if oldlen+addlen >= 2000 {
									disc.SmartWriteDiscord(config.Config.ModerationChannel, buf)
									buf = line
								} else {
									buf = buf + "\n" + line
								}
							}
							if buf != "" {
								disc.SmartWriteDiscord(config.Config.ModerationChannel, buf)
							}
						}

						//Don't send any more messages for a while (throttle)
						time.Sleep(constants.CMSRestTime)
					}

				}

				//Sleep for a moment before checking buffer again
				time.Sleep(constants.CMSPollRate)
			}
		}()

		//**************
		//Bot console
		//**************
		go func() {
			Console := bufio.NewReader(os.Stdin)
			for {
				time.Sleep(100 * time.Millisecond)
				line, _, err := Console.ReadLine()
				if err != nil {
					//logs.Log(fmt.Sprintf("%s: An error occurred when attempting to read the input to pass as input to the console Details: %s", time.Now(), err))
					//fact.SetFactRunning(false, true)
					continue
				} else {
					fact.WriteFact(string(line))
				}
			}
		}()

		//**********************
		//Check players online
		//**********************
		//Safety, in case player count gets off
		//Also helps detect servers crash/dead while paused
		go func() {
			for {
				time.Sleep(15 * time.Minute)

				if fact.IsFactRunning() {
					fact.WriteFact("/p o c")
				}
			}
		}()

		//**********************************
		//Delete expired registration codes
		//**********************************
		go func() {
			s1 := rand.NewSource(time.Now().UnixNano())
			r1 := rand.New(s1)

			for {
				time.Sleep(30 * time.Second)

				t := time.Now()

				glob.PasswordListLock.Lock()
				for i := 0; i <= glob.PasswordMax && i <= constants.MaxPasswords; i++ {
					if glob.PasswordList[i] != "" && (t.Unix()-glob.PasswordTime[i]) > 300 {
						logs.Log("Invalidating old unused access code for user: " + disc.GetNameFromID(glob.PasswordID[i], false))
						glob.PasswordList[i] = ""
						glob.PasswordID[i] = ""
						glob.PasswordTime[i] = 0
					}
				}
				glob.PasswordListLock.Unlock()
				fuzz := r1.Intn(constants.SecondInMicro)
				time.Sleep(time.Duration(fuzz) * time.Microsecond)
			}
		}()

		//**********************************
		//Pause on connect
		//**********************************
		go func() {
			for {

				time.Sleep(5 * time.Second)
				tn := time.Now()

				if strings.ToLower(config.Config.PauseOnConnect) == "yes" ||
					strings.ToLower(config.Config.PauseOnConnect) == "true" {

					glob.ConnectPauseLock.Lock()

					if glob.ConnectPauseTimer > 0 {
						if tn.Unix()-glob.ConnectPauseTimer >= 120 {
							glob.ConnectPauseTimer = 0
							glob.ConnectPauseCount = 0

							buf := "Catch-up taking over two minutes, returning to normal speed."
							fact.CMS(config.Config.FactorioChannelID, buf)
							fact.WriteFact("/chat (SYSTEM) " + buf)

							if config.Config.DefaultGSpeed != "" {
								fact.WriteFact("/gspeed " + config.Config.DefaultGSpeed)
							} else {
								fact.WriteFact("/gspeed 1.0")
							}
						}
					}

					glob.ConnectPauseLock.Unlock()

				}
			}
		}()

		//****************************************
		//Load & Save player database, for safety
		//****************************************
		go func() {
			s1 := rand.NewSource(time.Now().UnixNano())
			r1 := rand.New(s1)
			for {
				time.Sleep(60 * time.Minute)

				logs.LogWithoutEcho("Database safety read/write.")
				fact.LoadPlayers()
				fact.WritePlayers()

				fuzz := r1.Intn(5 * constants.MinuteInMicro)
				time.Sleep(time.Duration(fuzz) * time.Microsecond)
			}
		}()

		//*******************************
		//Save database, if marked dirty
		//*******************************
		go func() {
			s1 := rand.NewSource(time.Now().UnixNano())
			r1 := rand.New(s1)
			for {
				time.Sleep(30 * time.Second)

				glob.PlayerListDirtyLock.Lock()

				if glob.PlayerListDirty {
					glob.PlayerListDirty = false
					//Prevent recursive lock
					go func() {
						logs.LogWithoutEcho("Database marked dirty, saving.")
						fact.WritePlayers()
					}()
				}
				glob.PlayerListDirtyLock.Unlock()

				fuzz := r1.Intn(constants.SecondInMicro)
				time.Sleep(time.Duration(fuzz) * time.Microsecond)
			}
		}()

		//********************************************
		//Save database, if last seen is marked dirty
		//********************************************
		go func() {
			s1 := rand.NewSource(time.Now().UnixNano())
			r1 := rand.New(s1)
			for {
				time.Sleep(15 * time.Minute)
				glob.PlayerListSeenDirtyLock.Lock()

				if glob.PlayerListSeenDirty {
					glob.PlayerListSeenDirty = false

					//Prevent recursive lock
					go func() {
						logs.LogWithoutEcho("Database last seen flagged, saving.")
						fact.WritePlayers()
					}()
				}
				glob.PlayerListSeenDirtyLock.Unlock()

				fuzz := r1.Intn(10 * constants.SecondInMicro)
				time.Sleep(time.Duration(fuzz) * time.Microsecond)

			}
		}()

		//***********************************
		//Database file modifcation watching
		//***********************************
		fact.WatchDatabaseFile()

		//Read database, if the file was modifed
		go func() {
			s1 := rand.NewSource(time.Now().UnixNano())
			r1 := rand.New(s1)
			updated := false

			for {

				time.Sleep(5 * time.Second)

				//Detect update
				glob.PlayerListUpdatedLock.Lock()
				if glob.PlayerListUpdated {
					updated = true
					glob.PlayerListUpdated = false
				}
				glob.PlayerListUpdatedLock.Unlock()

				if updated {
					updated = false

					logs.LogWithoutEcho("Database file modified, loading.")
					fact.LoadPlayers()
					time.Sleep(30 * time.Second)
				}

				fuzz := r1.Intn(constants.SecondInMicro)
				time.Sleep(time.Duration(fuzz) * time.Microsecond)

			}
		}()

		//**************************
		//Get Guild information
		//Needed for Discord roles
		//**************************
		go func() {
			for {
				time.Sleep(5 * time.Second)

				glob.GuildLock.Lock()

				//Get guild id, if we need it

				if glob.Guild == nil && glob.DS != nil {
					// Attempt to get the guild from the state,
					// If there is an error, fall back to the restapi.
					nguild, err := glob.DS.State.Guild(config.Config.GuildID)
					if err != nil {
						nguild, err = glob.DS.Guild(config.Config.GuildID)
						if err != nil {
							//logs.LogWithoutEcho("Failed to get guild data, will retry...")
							return
						}
					}

					if err != nil {
						logs.Log(fmt.Sprintf("Was unable to get guild data from GuildID: %s", err))

						glob.GuildLock.Unlock()
						continue
					}
					if nguild == nil || err != nil {
						glob.Guildname = constants.Unknown
						logs.LogWithoutEcho("Guild data came back nil.")
					} else {

						//Guild found, exit loop
						glob.Guild = nguild
						glob.Guildname = nguild.Name
						logs.LogWithoutEcho("Guild data linked.")

						glob.GuildLock.Unlock()
						break
					}
				}

				glob.GuildLock.Unlock()
			}
		}()

		//************************************
		//Reboot if queued, when server empty
		//************************************
		go func() {
			s1 := rand.NewSource(time.Now().UnixNano())
			r1 := rand.New(s1)

			for {
				time.Sleep(5 * time.Second)

				if (fact.IsQueued() || fact.GetDoUpdateFactorio()) && fact.GetNumPlayers() == 0 {
					if fact.IsFactRunning() {
						fact.LogCMS(config.Config.FactorioChannelID, "No players currently online, performing scheduled reboot.")
						fact.QuitFactorio()
						break //We don't need to loop anymore
					}
				}
				fuzz := r1.Intn(constants.SecondInMicro)
				time.Sleep(time.Duration(fuzz) * time.Microsecond)
			}
		}()

		//*******************
		//Check signal files
		//*******************
		clearOldSignals()
		go func() {
			s1 := rand.NewSource(time.Now().UnixNano())
			r1 := rand.New(s1)
			for {
				time.Sleep(1 * time.Second)

				// Look for signal files
				if _, err := os.Stat(".upgrade"); !os.IsNotExist(err) {
					fact.SetAutoStart(false)

					if err := os.Remove(".upgrade"); err != nil {
						logs.Log(".upgrade disappeared?")
					}
					if fact.IsFactRunning() {
						go func() {
							fact.LogCMS(config.Config.FactorioChannelID, "Updating Factorio!")

							if fact.IsFactRunning() && fact.GetNumPlayers() > 0 {
								for x := 0; x < 3; x++ {
									fact.WriteFact(fmt.Sprintf("/cchat %sFactorio is shutting down in 30 seconds, to upgrade to a new version![/color]", fact.RandomColor(false)))
								}
								time.Sleep(30 * time.Second)
							}
							fact.QuitFactorio()
						}()
					}
				} else if _, err := os.Stat(".queue"); !os.IsNotExist(err) {
					if err := os.Remove(".queue"); err != nil {
						logs.Log(".queue file disappeared?")
					}
					if fact.IsQueued() == false {
						fact.SetQueued(true)
						fact.LogCMS(config.Config.FactorioChannelID, "Reboot queued!")
					}
				} else if _, err := os.Stat(".reload"); !os.IsNotExist(err) {

					if err := os.Remove(".reload"); err != nil {
						logs.Log(".reload file disappeared?")
					}
					fact.LogCMS(config.Config.FactorioChannelID, "Rebooting!")
					fact.SetBotReboot(true)
					fact.QuitFactorio()
				} else if _, err := os.Stat(".start"); !os.IsNotExist(err) {

					if err := os.Remove(".start"); err != nil {
						logs.Log(".start file disappeared?")
					}
					if fact.IsFactRunning() == false {
						fact.SetAutoStart(true)
						fact.LogCMS(config.Config.FactorioChannelID, "Factorio is starting!")
					}
				} else if _, err := os.Stat(".restart"); !os.IsNotExist(err) {

					if err := os.Remove(".restart"); err != nil {
						logs.Log(".restart file disappeared?")
					}
					if fact.IsFactRunning() == false {
						fact.SetAutoStart(true)
						fact.LogCMS(config.Config.FactorioChannelID, "Factorio is starting!")
					} else {
						fact.SetAutoStart(true)
						fact.SetQueued(false)

						fact.LogCMS(config.Config.FactorioChannelID, "Factorio is restarting!")
						go func() {
							for x := 0; x < 3; x++ {
								fact.WriteFact(fmt.Sprintf("/cchat %sFactorio is rebooting in 30 seconds![/color]", fact.RandomColor(false)))
							}
							time.Sleep(30 * time.Second)
							fact.QuitFactorio()
						}()
					}

				} else if _, err := os.Stat(".qrestart"); !os.IsNotExist(err) {

					if err := os.Remove(".qrestart"); err != nil {
						logs.Log(".qrestart file disappeared?")
					}
					if fact.IsFactRunning() == false {
						fact.SetAutoStart(true)
						fact.LogCMS(config.Config.FactorioChannelID, "Factorio is starting!")
					} else {
						fact.SetAutoStart(true)
						fact.SetQueued(false)

						fact.LogCMS(config.Config.FactorioChannelID, "Factorio is quick restarting!")
						go func() {
							if fact.IsFactRunning() && fact.GetNumPlayers() > 0 {
								for x := 0; x < 3; x++ {
									fact.WriteFact(fmt.Sprintf("/cchat %sFactorio is rebooting in 5 seconds![/color]", fact.RandomColor(false)))
								}
								time.Sleep(5 * time.Second)
							}

							fact.QuitFactorio()
						}()
					}
				} else if _, err := os.Stat(".shutdown"); !os.IsNotExist(err) {
					if err := os.Remove(".shutdown"); err != nil {
						logs.Log(".shutdown disappeared?")
					}
					if fact.IsFactRunning() {
						fact.SetAutoStart(false)
						go func() {
							fact.LogCMS(config.Config.FactorioChannelID, "Factorio is shutting down for maintenance!")
							if fact.IsFactRunning() && fact.GetNumPlayers() > 0 {
								for x := 0; x < 3; x++ {
									fact.WriteFact(fmt.Sprintf("/cchat %sFactorio is shutting down in 30 seconds, for system maintenance![/color]", fact.RandomColor(false)))
								}
								time.Sleep(30 * time.Second)
							}
							fact.QuitFactorio()
						}()
					}
				}
				fuzz := r1.Intn(constants.SecondInMicro)
				time.Sleep(time.Duration(fuzz) * time.Microsecond)
			}

		}()

		//****************************
		// Check for factorio updates
		//****************************
		go func() {
			s1 := rand.NewSource(time.Now().UnixNano())
			r1 := rand.New(s1)
			fuzz := r1.Intn(constants.MinuteInMicro*5) + constants.MinuteInMicro*15
			time.Sleep(time.Duration(fuzz) * time.Microsecond)
			//Sleep 15-20 minutes before checking for updates

			for {
				fact.CheckFactUpdate(false)

				//Add 30 minutes of randomness to delay
				fuzz := r1.Intn(constants.MinuteInMicro * 30)
				time.Sleep(time.Duration(fuzz) * time.Microsecond)

				time.Sleep(1 * time.Hour)
			}
		}()

		//****************************
		// Refresh channel names
		//****************************
		go func() {

			for {
				fact.UpdateChannelName()

				glob.UpdateChannelLock.Lock()
				if glob.OldChanName != glob.NewChanName {
					glob.UpdateChannelLock.Unlock()

					fact.DoUpdateChannelName()
					time.Sleep(60 * time.Second)
				} else {
					glob.UpdateChannelLock.Unlock()

					time.Sleep(2 * time.Second)
				}
			}
		}()

		//****************************
		// Force refresh channel names
		//****************************
		go func() {

			for {
				time.Sleep(time.Hour)
				fact.UpdateChannelName()
				fact.DoUpdateChannelName()
			}
		}()

		//****************************
		// Capture man-minutes
		//****************************
		go func() {
			for {
				time.Sleep(time.Minute)
				if fact.IsFactorioBooted() {
					nump := fact.GetNumPlayers()

					glob.ManMinutesLock.Lock()
					if nump > 0 {
						glob.ManMinutes = (glob.ManMinutes + nump)
					}
					glob.ManMinutesLock.Unlock()
				}
			}
		}()
	}()

	//After starting loops, wait here for process signals
	sc := make(chan os.Signal, 1)

	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sc
	fact.SetAutoStart(false)
	fact.SetBotReboot(false)
	fact.SetQueued(false)
	fact.QuitFactorio()
	for x := 0; x < 60 && fact.IsFactRunning(); x++ {
		time.Sleep(time.Second)
	}
	fact.DoExit()
}

//Delete old signal files
func clearOldSignals() {
	if err := os.Remove(".start"); err == nil {
		logs.Log("old .start removed.")
	}
	if err := os.Remove(".restart"); err == nil {
		logs.Log("old .restart removed.")
	}
	if err := os.Remove(".qrestart"); err == nil {
		logs.Log("old .qrestart removed.")
	}
	if err := os.Remove(".shutdown"); err == nil {
		logs.Log("old .shutdown removed.")
	}
	if err := os.Remove(".upgrade"); err == nil {
		logs.Log("old .upgrade removed.")
	}
	if err := os.Remove(".queue"); err == nil {
		logs.Log("old .queue removed.")
	}
	if err := os.Remove(".reload"); err == nil {
		logs.Log("old .reload removed.")
	}
}
