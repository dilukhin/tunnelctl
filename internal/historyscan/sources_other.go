//go:build !windows

package historyscan

import "tunnelctl/internal/config"

func scanPlatformSources(config.Config, map[string]bool, map[string]*Candidate) error { return nil }
