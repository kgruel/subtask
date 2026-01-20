package harness

import (
	"os/exec"
	"time"
)

// configureCmdCancellation applies conservative defaults that avoid hangs when a command's
// context is canceled but descendant processes keep stdio pipes open (common with shell wrappers).
func configureCmdCancellation(cmd *exec.Cmd) {
	// Keep this short: on ctx cancel we want to stop promptly; on success it only matters
	// if pipes stay open unexpectedly (better an error than a hang).
	if cmd.WaitDelay == 0 {
		cmd.WaitDelay = 2 * time.Second
	}
}
