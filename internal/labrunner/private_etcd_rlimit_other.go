//go:build !freebsd

package labrunner

import "errors"

func setEtcdRlimit(_ int, _, _ uint64) error {
	return errors.New("private etcd resource enforcement requires a dedicated FreeBSD helper")
}

func checkEtcdPlatform(_ string) error {
	return errors.New("private etcd requires the independent native FreeBSD guest UFS disk")
}
