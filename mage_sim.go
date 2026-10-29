//go:build mage

package main

import "github.com/magefile/mage/sh"

// SimHistory checks the external oracle and bounded seeded real local-service
// scenarios. HOMELABFS_SIM_SEED (default 1) and HOMELABFS_SIM_CASES (default 8,
// maximum 64) select replay. This opens no lab, VM, FUSE mount or remote service.
func SimHistory() error {
	return sh.RunWithV(map[string]string{"GOMAXPROCS": "2"}, "go", "test", "./internal/sim", "-run", "^(TestHistory.*|TestScenario.*|TestSeededLocalHistoryScenarios|TestLocalHistory.*)$", "-count=1", "-timeout=120s", "-v")
}
