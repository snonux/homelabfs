package daemoncli

import "errors"

// mountLifetime releases source ownership only after successful normal unmount
// and adapter drain. On uncertainty the live admin closure keeps local resources
// reachable for in-process Run callers as well as ordinary process execution.
type mountLifetime struct {
	local    *localService
	admin    *adminService
	retained bool
}

func (l *mountLifetime) unmount(normal func() error) error {
	if err := normal(); err != nil {
		l.local.service.BlockAdmission("normal-unmount-failed")
		return err
	}
	l.retained = false
	return nil
}
func (l *mountLifetime) close() error {
	if l.retained {
		return nil
	}
	var err error
	if l.admin != nil {
		err = l.admin.close()
	}
	return errors.Join(err, l.local.close())
}
