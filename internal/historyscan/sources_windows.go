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
	roots := []string{cleanConfiguredPath(os.Getenv("FARHOME"), "")}
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
		cleanConfiguredPath(os.Getenv("FARPROFILE"), ""),
		cleanConfiguredPath(os.Getenv("FARLOCALPROFILE"), ""),
	}
	appendJoinedPath(&profiles, appData, "Far Manager")
	appendJoinedPath(&profiles, localAppData, "Far Manager")
	appendJoinedPath(&profiles, appData, "Far Manager", "Profile")
	appendJoinedPath(&profiles, localAppData, "Far Manager", "Profile")
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
			} else {
				userProfile = cleanConfiguredPath(expandWindowsEnvironment(userProfile, overrides), root)
			}
			localProfile := iniValue(entries, "general", "userlocalprofiledir")
			if localProfile == "" {
				localProfile = userProfile
			} else {
				localProfile = cleanConfiguredPath(expandWindowsEnvironment(localProfile, overrides), root)
			}
			profiles = append(profiles, userProfile, localProfile)
		case 2:
			appendJoinedPath(&profiles, appData, "Far Manager")
		default:
			appendJoinedPath(&profiles, appData, "Far Manager")
			appendJoinedPath(&profiles, localAppData, "Far Manager")
		}

		if directory := iniValue(entries, "general", "globalusermenudir"); directory != "" {
			directory = expandWindowsEnvironment(directory, overrides)
			globalMenuDirs = append(globalMenuDirs, cleanConfiguredPath(directory, root))
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

	userCommands := []string{}
	for _, directory := range totalCommanderDirectories() {
		userCommands = append(userCommands, filepath.Join(directory, "usercmd.ini"))
	}
	appData := os.Getenv("APPDATA")
	appendJoinedPath(&userCommands, appData, "GHISLER", "usercmd.ini")
	appendJoinedPath(&userCommands, appData, "usercmd.ini")
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
	directories := totalCommanderDirectories()
	paths := []string{cleanConfiguredPath(os.Getenv("COMMANDER_INI"), "")}
	appendJoinedPath(&paths, appData, "GHISLER", "wincmd.ini")
	appendJoinedPath(&paths, appData, "wincmd.ini")
	appendJoinedPath(&paths, windowsDir, "wincmd.ini")
	for _, directory := range directories {
		paths = append(paths, filepath.Join(directory, "wincmd.ini"))
	}

	for _, configured := range totalCommanderRegistryValues("IniFileName") {
		configured = expandWindowsEnvironment(configured, nil)
		if filepath.IsAbs(strings.Trim(configured, `"`)) {
			paths = append(paths, cleanConfiguredPath(configured, ""))
			continue
		}
		for _, directory := range directories {
			paths = append(paths, cleanConfiguredPath(configured, directory))
		}
	}
	return uniqueExistingFiles(paths)
}

func totalCommanderDirectories() []string {
	directories := []string{cleanConfiguredPath(os.Getenv("COMMANDER_PATH"), "")}
	for _, configured := range totalCommanderRegistryValues("InstallDir") {
		directories = append(directories, cleanConfiguredPath(expandWindowsEnvironment(configured, nil), ""))
	}
	for _, variable := range []string{"ProgramFiles", "ProgramFiles(x86)"} {
		base := os.Getenv(variable)
		if base == "" {
			continue
		}
		for _, directory := range []string{"totalcmd", "totalcmd64", "Total Commander"} {
			directories = append(directories, filepath.Join(base, directory))
		}
	}
	return uniqueExistingDirectories(directories)
}

func cleanConfiguredPath(value, relativeTo string) string {
	value = strings.TrimSpace(strings.Trim(value, `"`))
	if value == "" {
		return ""
	}
	value = expandWindowsEnvironment(value, nil)
	if !filepath.IsAbs(value) && relativeTo != "" {
		value = filepath.Join(relativeTo, value)
	}
	return filepath.Clean(value)
}

func appendJoinedPath(paths *[]string, base string, elements ...string) {
	if strings.TrimSpace(base) == "" {
		return
	}
	*paths = append(*paths, filepath.Join(append([]string{base}, elements...)...))
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
