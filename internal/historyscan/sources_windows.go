//go:build windows

package historyscan

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"tunnelctl/internal/config"
)

func scanPlatformSources(existing config.Config, used map[string]bool, dedup map[string]*Candidate) error {
	scanFarSources(existing, used, dedup)
	scanTotalCommanderSources(existing, used, dedup)
	return nil
}

func scanFarSources(existing config.Config, used map[string]bool, dedup map[string]*Candidate) {
	roots := farRoots()
	profiles, globalMenuDirs := farProfiles(roots)

	for _, profile := range profiles {
		database := filepath.Join(profile, "history.db")
		if records, err := readFarHistoryDB(database); err == nil {
			for index, record := range records {
				addCommand(record.Command, database, index+1, farRecordTime(record.Time), existing, used, dedup)
			}
		}
	}

	menus := []string{}
	if current, err := os.Getwd(); err == nil {
		menus = append(menus, filepath.Join(current, "FarMenu.ini"))
	}
	for _, profile := range profiles {
		menus = append(menus, filepath.Join(profile, "FarMenu.ini"))
		menus = append(menus, globINIFiles(filepath.Join(profile, "Menus"))...)
	}
	for _, root := range roots {
		menus = append(menus, filepath.Join(root, "FarMenu.ini"))
		menus = append(menus, globINIFiles(filepath.Join(root, "Menus"))...)
	}
	for _, directory := range globalMenuDirs {
		menus = append(menus, filepath.Join(directory, "FarMenu.ini"))
	}
	for _, menu := range uniquePaths(menus) {
		_ = scanFarMenu(menu, existing, used, dedup)
	}
}

func farRoots() []string {
	roots := []string{os.Getenv("FARHOME")}
	if executable, err := exec.LookPath("Far.exe"); err == nil {
		if absolute, absErr := filepath.Abs(executable); absErr == nil {
			roots = append(roots, filepath.Dir(absolute))
		}
	}
	for _, variable := range []string{"ProgramFiles", "ProgramFiles(x86)", "LOCALAPPDATA"} {
		base := os.Getenv(variable)
		if base == "" {
			continue
		}
		roots = append(roots,
			filepath.Join(base, "Far Manager"),
			filepath.Join(base, "FarManager"),
		)
	}
	return uniqueExistingDirectories(roots)
}

func farProfiles(roots []string) ([]string, []string) {
	appData := os.Getenv("APPDATA")
	localAppData := os.Getenv("LOCALAPPDATA")
	profiles := []string{
		os.Getenv("FARPROFILE"),
		os.Getenv("FARLOCALPROFILE"),
		filepath.Join(appData, "Far Manager"),
		filepath.Join(localAppData, "Far Manager"),
		filepath.Join(appData, "Far Manager", "Profile"),
		filepath.Join(localAppData, "Far Manager", "Profile"),
	}
	globalMenuDirs := []string{}

	for _, root := range roots {
		profiles = append(profiles, filepath.Join(root, "Profile"))
		globalMenuDirs = append(globalMenuDirs, root)

		entries, err := readINI(filepath.Join(root, "Far.exe.ini"))
		if err != nil {
			continue
		}
		overrides := map[string]string{"FARHOME": root}
		mode := 1
		if value := iniValue(entries, "general", "usesystemprofiles"); value != "" {
			if parsed, parseErr := strconv.Atoi(strings.TrimSpace(value)); parseErr == nil {
				mode = parsed
			}
		}
		switch mode {
		case 0:
			userProfile := iniValue(entries, "general", "userprofiledir")
			if userProfile == "" {
				userProfile = filepath.Join(root, "Profile")
			}
			userProfile = expandWindowsEnvironment(userProfile, overrides)
			localProfile := iniValue(entries, "general", "userlocalprofiledir")
			if localProfile == "" {
				localProfile = userProfile
			}
			localProfile = expandWindowsEnvironment(localProfile, overrides)
			profiles = append(profiles, userProfile, localProfile)
		case 2:
			profiles = append(profiles, filepath.Join(appData, "Far Manager"))
		default:
			profiles = append(profiles,
				filepath.Join(appData, "Far Manager"),
				filepath.Join(localAppData, "Far Manager"),
			)
		}

		if directory := iniValue(entries, "general", "globalusermenudir"); directory != "" {
			globalMenuDirs = append(globalMenuDirs, expandWindowsEnvironment(directory, overrides))
		}
	}
	return uniqueExistingDirectories(profiles), uniqueExistingDirectories(globalMenuDirs)
}

func farRecordTime(ticks int64) time.Time {
	const windowsToUnixTicks = int64(116444736000000000)
	if ticks <= windowsToUnixTicks {
		return time.Time{}
	}
	return time.Unix(0, (ticks-windowsToUnixTicks)*100)
}

func scanTotalCommanderSources(existing config.Config, used map[string]bool, dedup map[string]*Candidate) {
	iniFiles := totalCommanderINIFiles()
	for _, path := range iniFiles {
		_ = scanTotalCommanderINI(path, existing, used, dedup)
	}

	userCommands := []string{
		filepath.Join(os.Getenv("COMMANDER_PATH"), "usercmd.ini"),
		filepath.Join(os.Getenv("APPDATA"), "GHISLER", "usercmd.ini"),
		filepath.Join(os.Getenv("APPDATA"), "usercmd.ini"),
	}
	for _, path := range iniFiles {
		userCommands = append(userCommands, filepath.Join(filepath.Dir(path), "usercmd.ini"))
	}
	for _, path := range uniquePaths(userCommands) {
		_ = scanTotalCommanderUserCmd(path, existing, used, dedup)
	}
}

func totalCommanderINIFiles() []string {
	appData := os.Getenv("APPDATA")
	windowsDir := os.Getenv("WINDIR")
	commanderPath := os.Getenv("COMMANDER_PATH")
	paths := []string{
		os.Getenv("COMMANDER_INI"),
		filepath.Join(appData, "GHISLER", "wincmd.ini"),
		filepath.Join(appData, "wincmd.ini"),
		filepath.Join(windowsDir, "wincmd.ini"),
		filepath.Join(commanderPath, "wincmd.ini"),
	}
	for _, variable := range []string{"ProgramFiles", "ProgramFiles(x86)"} {
		base := os.Getenv(variable)
		if base == "" {
			continue
		}
		for _, directory := range []string{"totalcmd", "totalcmd64", "Total Commander"} {
			paths = append(paths, filepath.Join(base, directory, "wincmd.ini"))
		}
	}
	return uniqueExistingFiles(paths)
}

func globINIFiles(directory string) []string {
	matches, _ := filepath.Glob(filepath.Join(directory, "*.ini"))
	sort.Strings(matches)
	return matches
}

func uniqueExistingDirectories(paths []string) []string {
	return filterExistingPaths(paths, true)
}

func uniqueExistingFiles(paths []string) []string {
	return filterExistingPaths(paths, false)
}

func filterExistingPaths(paths []string, wantDirectory bool) []string {
	filtered := []string{}
	for _, path := range uniquePaths(paths) {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() != wantDirectory {
			continue
		}
		filtered = append(filtered, path)
	}
	return filtered
}
