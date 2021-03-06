package fact

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"../config"
	"../constants"
	"../glob"
	"../logs"
)

func CheckZip(filename string) bool {

	ctx, cancel := context.WithTimeout(context.Background(), constants.ZipIntegrityLimit)
	defer cancel()

	cmdargs := []string{"-t", filename}
	cmd := exec.CommandContext(ctx, config.Config.ZipBinary, cmdargs...)
	o, err := cmd.CombinedOutput()
	out := string(o)

	if ctx.Err() == context.DeadlineExceeded {
		logs.Log("Zip integrity check timed out.")
	}

	if err == nil {
		if strings.Contains(out, "No errors detected in compressed data of ") {
			logs.Log("Zipfile integrity good!")
			return true
		}
	}

	logs.Log("Zipfile integrity check failed!")
	return false
}

func CheckFactUpdate(logNoUpdate bool) {

	if config.Config.UpdaterPath != "" {

		glob.UpdateFactorioLock.Lock()
		defer glob.UpdateFactorioLock.Unlock()

		//Give up on check/download after a while
		ctx, cancel := context.WithTimeout(context.Background(), constants.FactorioUpdateCheckLimit)
		defer cancel()

		//Create cache directory
		os.MkdirAll(config.Config.UpdaterCache, os.ModePerm)

		cmdargs := []string{config.Config.UpdaterPath, "-O", config.Config.UpdaterCache, "-a", config.Config.Executable, "-d"}
		if strings.ToLower(config.Config.UpdateToExperimental) == "true" ||
			strings.ToLower(config.Config.UpdateToExperimental) == "yes" {
			cmdargs = append(cmdargs, "-x")
		}

		cmd := exec.CommandContext(ctx, config.Config.UpdaterShell, cmdargs...)
		o, err := cmd.CombinedOutput()
		out := string(o)

		if ctx.Err() == context.DeadlineExceeded {
			logs.Log("fact update check: download/check timed out... purging cache.")
			os.RemoveAll(config.Config.UpdaterCache)
			return
		}

		if err == nil {
			clines := strings.Split(out, "\n")
			for _, line := range clines {
				linelen := len(line)
				var newversion string
				var oldversion string

				if linelen > 0 {

					words := strings.Split(line, " ")
					numwords := len(words)

					if strings.HasPrefix(line, "No updates available") {
						if logNoUpdate == true {
							mess := "fact update check: Factorio is up-to-date."
							logs.Log(mess)
						}
						return
					} else if strings.HasPrefix(line, "Wrote ") {
						if linelen > 1 && strings.Contains(line, ".zip") {

							//Only trigger on a new patch file
							if line != glob.NewPatchName {
								glob.NewPatchName = line

								if numwords > 1 && CheckZip(words[1]) {
									mess := "Factorio update downloaded and verified, will update when no players are online."
									CMS(config.Config.FactorioChannelID, mess)
									WriteFact("/cchat [SYSTEM] " + mess)
									logs.Log(mess)

									SetDoUpdateFactorio(true)
								} else {
									os.RemoveAll(config.Config.UpdaterCache)
									//Purge patch name so we attempt check again
									glob.NewPatchName = constants.Unknown
									logs.Log("fact update check: Factorio update zip invalid... purging cache.")
								}
							}
							return
						}
					} else if strings.HasPrefix(line, "Dry run: would have fetched update from") {
						if numwords >= 9 {
							oldversion = words[7]
							newversion = words[9]

							messdisc := fmt.Sprintf("**Factorio update available:** '%v' to '%v'", oldversion, newversion)
							messfact := fmt.Sprintf("Factorio update available: '%v' to '%v'", oldversion, newversion)
							//Don't message, unless this is actually a unique new version
							if glob.NewVersion != newversion {
								glob.NewVersion = newversion

								CMS(config.Config.FactorioChannelID, messdisc)

								WriteFact("/cchat [SYSTEM] " + messfact)
								logs.Log(messfact)
							}
						}
					}
				}
			}
		} else {
			os.RemoveAll(config.Config.UpdaterCache)
			logs.Log("fact update dry: (error) Non-zero exit code... purging update cache.")
		}

		logs.Log(fmt.Sprintf("fact update dry: (error) update_fact.py:\n%v", out))
	}

}

func FactUpdate() {

	glob.UpdateFactorioLock.Lock()
	defer glob.UpdateFactorioLock.Unlock()

	//Give up on patching eventually
	ctx, cancel := context.WithTimeout(context.Background(), constants.FactorioUpdateCheckLimit)
	defer cancel()

	if IsFactRunning() == false {
		//Keep us from stepping on a factorio launch or update
		glob.FactorioLaunchLock.Lock()
		defer glob.FactorioLaunchLock.Unlock()

		cmdargs := []string{config.Config.UpdaterPath, "-O", config.Config.UpdaterCache, "-a", config.Config.Executable}
		if strings.ToLower(config.Config.UpdateToExperimental) == "true" ||
			strings.ToLower(config.Config.UpdateToExperimental) == "yes" {
			cmdargs = append(cmdargs, "-x")
		}

		cmd := exec.CommandContext(ctx, config.Config.UpdaterShell, cmdargs...)
		o, err := cmd.CombinedOutput()
		out := string(o)

		if ctx.Err() == context.DeadlineExceeded {
			logs.Log("fact update: (error) Factorio update patching timed out, deleting possible corrupted patch file.")

			os.RemoveAll(config.Config.UpdaterCache)
			return
		}

		if err == nil {
			clines := strings.Split(out, "\n")
			for _, line := range clines {
				linelen := len(line)

				if linelen > 0 {

					//words := strings.Split(line, " ")
					if strings.HasPrefix(line, "Update applied successfully!") {
						mess := "fact update: Factorio updated successfully!"
						logs.Log(mess)
						return
					}
				}
			}
		} else {
			os.RemoveAll(config.Config.UpdaterCache)
			logs.Log("fact update: (error) Non-zero exit code... purging update cache.")
			logs.Log(fmt.Sprintf("fact update: (error) update_fact.py:\n%v", out))
			return
		}

		logs.Log("fact update: (unknown error): " + out)
		return
	} else {

		logs.Log("fact update: (error) Factorio is currently running, unable to update.")
		return
	}
}
