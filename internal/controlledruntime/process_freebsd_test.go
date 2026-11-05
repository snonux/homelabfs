//go:build freebsd && cgo

package controlledruntime

import (
	"os"
	"strconv"
	"testing"
)

func TestFreeBSDObserverReadOnly(t *testing.T) {
	identity, err := checkProcessObserver(observeProcess)
	if err != nil {
		t.Fatal(err)
	}
	after, err := checkProcessObserver(observeProcess)
	if err != nil || !sameProcessIdentity(identity, after) {
		t.Fatalf("unstable self observation: %v", err)
	}
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if identity.ExecutablePath != path || identity.TextVnode == 0 {
		t.Fatalf("self executable not observed: %#v", identity)
	}
	pids := []int{-1, 0, 1<<31 - 1}
	if strconv.IntSize == 64 {
		large := int64(1 << 32)
		pids = append(pids, int(large))
	}
	for _, pid := range pids {
		if _, err := observeProcess(pid); err == nil {
			t.Fatalf("invalid/absent pid %d authenticated", pid)
		}
	}
	for _, name := range []string{"pid", "uid", "gid", "groups", "incarnation", "path", "text-vnode"} {
		bad := identity
		switch name {
		case "pid":
			bad.PID++
		case "uid":
			bad.UID++
		case "gid":
			bad.GID++
		case "groups":
			bad.Groups = append(append([]uint32{}, identity.Groups...), ^uint32(0))
		case "incarnation":
			bad.Incarnation += ":stale"
		case "path":
			bad.ExecutablePath += ".stale"
		case "text-vnode":
			bad.TextVnode++
		}
		if sameProcessIdentity(identity, bad) {
			t.Fatalf("changed %s identity accepted", name)
		}
	}
	t.Logf("actual FreeBSD self observation: pid=%d uid=%d gid=%d groups=%v incarnation=%s executable=%s", identity.PID, identity.UID, identity.GID, identity.Groups, identity.Incarnation, path)
}
