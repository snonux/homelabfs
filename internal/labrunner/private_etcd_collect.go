package labrunner

import (
	"context"
	"errors"
	"time"

	"github.com/snonux/homelabfs/internal/admin"
)

// EtcdCollection reports the implemented guest-local observations. Member
// quorum/auth health is checked by controller mTLS bootstrap, not inferred from
// a running process or substituted for HFS protocol/recovery evidence.
type EtcdCollection struct {
	Enrollment         EtcdEnrollment `json:"enrollment"`
	Observed           time.Time      `json:"observed"`
	DirectoryBytes     int64          `json:"directory_bytes"`
	DiskGuard          string         `json:"disk_guard"`
	Supervisor         *admin.Status  `json:"supervisor,omitempty"`
	SupervisorError    string         `json:"supervisor_error,omitempty"`
	Log                string         `json:"log"`
	LogError           string         `json:"log_error,omitempty"`
	MemberHealth       string         `json:"member_health"`
	RemainingResources []Resource     `json:"remaining_resources"`
}

// CollectPrivateEtcd reads only enrolled state, bounded logs and directory
// inventory. TLS contents are never read or included in collection evidence.
func CollectPrivateEtcd(ctx context.Context, p Plan, a Approval, guest string) (EtcdCollection, error) {
	if err := admitEtcdNative(p, a, guest, false); err != nil {
		return EtcdCollection{}, err
	}
	root := etcdRoot(p) + "/etcd"
	e, err := readEtcdEnrollment(p, guest, root)
	if err != nil {
		return EtcdCollection{}, err
	}
	size, err := boundedEtcdTree(root, p.PrivateEtcd.DiskBytes)
	if err != nil {
		return EtcdCollection{}, err
	}
	r := EtcdCollection{Enrollment: e, Observed: time.Now().UTC(), DirectoryBytes: size, DiskGuard: "250ms monitored aggregate; one-second scan bound; per-file rlimit; aggregate byte overshoot unestablished", MemberHealth: "not observed by guest collection; controller mTLS bootstrap is separate"}
	for _, resource := range p.Resources {
		if resource.Target == guest {
			r.RemainingResources = append(r.RemainingResources, resource)
		}
	}
	if e.Process != nil && !e.Stopped {
		response, statusErr := admin.Call(ctx, root+"/control/supervisor-etcd.sock", admin.Request{Version: 1, Operation: "status"})
		if statusErr == nil && (response.Status == nil || response.Status.NodeID != guest || len(response.Status.Volumes) != 1 || response.Status.Volumes[0].VolumeID != "private-etcd" || response.Status.Volumes[0].Generation != e.Process.Birth || response.Status.Volumes[0].Profile != "private-etcd-process" || response.Status.Volumes[0].NormalPublication) {
			statusErr = errors.New("private supervisor identity/profile mismatch")
		}
		if statusErr != nil {
			r.SupervisorError = statusErr.Error()
		} else {
			r.Supervisor = response.Status
		}
	} else {
		r.SupervisorError = "not running"
	}
	log, logErr := readOwnedBounded(root+"/control/etcd.daemon.log", p.PrivateEtcd.MaxLogBytes)
	if logErr != nil {
		r.LogError = logErr.Error()
	} else {
		r.Log = string(log)
	}
	return r, nil
}
