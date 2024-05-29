package images

import (
	"context"
	"os"
	"strconv"
	"sync"
	"time"

	"bytetrade.io/web3os/app-service/pkg/utils"

	"github.com/containerd/containerd/content"
	"github.com/containerd/containerd/errdefs"
	refdocker "github.com/containerd/containerd/reference/docker"
	"github.com/containerd/containerd/remotes"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
)

// StatusInfo holds the status info for an upload or download
type StatusInfo struct {
	Ref       string
	Status    string
	Offset    int64
	Total     int64
	StartedAt time.Time
	UpdatedAt time.Time
}

func showProgress(ctx context.Context, ongoing *jobs, cs content.Store, opts PullOptions) {
	var (
		ticker   = time.NewTicker(1 * time.Second)
		start    = time.Now()
		statuses = map[string]StatusInfo{}
		done     bool
		ordered  []StatusInfo
	)
	defer ticker.Stop()

outer:
	for {
		select {
		case <-ticker.C:
			resolved := "resolved"
			if !ongoing.isResolved() {
				resolved = "resolving"
			}
			statuses[ongoing.name] = StatusInfo{
				Ref:    ongoing.name,
				Status: resolved,
			}

			keys := []string{ongoing.name}

			activeSeen := map[string]struct{}{}
			if !done {
				active, err := cs.ListStatuses(ctx, "")
				if err != nil {
					klog.ErrorS(err, "active check failed")
					continue
				}
				// update status of active entries!
				for _, a := range active {
					statuses[a.Ref] = StatusInfo{
						Ref:       a.Ref,
						Status:    "downloading",
						Offset:    a.Offset,
						Total:     a.Total,
						StartedAt: a.StartedAt,
						UpdatedAt: a.UpdatedAt,
					}
					activeSeen[a.Ref] = struct{}{}
				}
			}

			// now, update the items in jobs that are not in active
			for _, j := range ongoing.jobs() {
				key := remotes.MakeRefKey(ctx, j)
				keys = append(keys, key)
				if _, ok := activeSeen[key]; ok {
					continue
				}

				status, ok := statuses[key]
				if !done && (!ok || status.Status == "downloading") {
					info, err := cs.Info(ctx, j.Digest)
					if err != nil {
						if !errdefs.IsNotFound(err) {
							klog.Errorf("Failed to get content info err=%v", err)
							continue outer
						} else {
							statuses[key] = StatusInfo{
								Ref:    key,
								Status: "waiting",
							}
						}
					} else if info.CreatedAt.After(start) {
						statuses[key] = StatusInfo{
							Ref:       key,
							Status:    "done",
							Offset:    info.Size,
							Total:     info.Size,
							UpdatedAt: info.CreatedAt,
						}
					} else {
						statuses[key] = StatusInfo{
							Ref:    key,
							Status: "exists",
						}
					}
				} else if done {
					if ok {
						if status.Status != "done" && status.Status != "exists" {
							status.Status = "done"
							statuses[key] = status
						}
					} else {
						statuses[key] = StatusInfo{
							Ref:    key,
							Status: "done",
						}
					}
				}
			}

			ordered = []StatusInfo{}
			for _, key := range keys {
				ordered = append(ordered, statuses[key])
			}
			klog.Infof("downloading image %v", ongoing.name)
			err := updateProgress(ordered, ongoing.name, opts)
			if err != nil {
				klog.Infof("update progress failed err=%v", err)

			}
			//tw.Flush()

			if done {
				klog.Infof("progress is done")
				return
			}
		case <-ctx.Done():
			done = true // allow ui to update once more
		}
	}
}

func updateProgress(statuses []StatusInfo, imageName string, opts PullOptions) error {
	client, err := utils.GetClient()
	if err != nil {
		return err
	}
	var offset, size int64
	var progress float64
	klog.Infof("in updateProgress: %#v", statuses)
	count := 0
	for _, status := range statuses {
		switch status.Status {
		case "downloading", "uploading":
			size += status.Total
			offset += status.Offset
		case "resolving", "waiting", "resolved":
			//if all status is resolving waiting, or resolved use last progress
			count++
			progress = 0
		default:
			progress = 100
		}
	}
	if count == len(statuses) {

	}
	if size > 0 {
		progress = float64(offset) / float64(size) * float64(100)
	}
	klog.Infof("download image %s progress=%v", imageName, progress)

	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		im, err := client.AppV1alpha1().ImageManagers().Get(context.TODO(), utils.FmtAppMgrName(opts.AppName, opts.OwnerName), metav1.GetOptions{})
		if err != nil {
			klog.Infof("cannot found image manager err=%v", err)
			return err
		}

		now := metav1.Now()
		imCopy := im.DeepCopy()
		status := imCopy.Status
		status.StatusTime = &now
		status.UpdateTime = &now
		for i, c := range status.Conditions {
			named, _ := refdocker.ParseDockerRef(c.ImageRef)
			if c.NodeName == os.Getenv("NODE_NAME") && named.String() == imageName {
				status.Conditions[i].Progress = strconv.FormatFloat(progress, 'f', 2, 64)
			}
		}
		imCopy.Status = status

		_, err = client.AppV1alpha1().ImageManagers().UpdateStatus(context.TODO(), imCopy, metav1.UpdateOptions{})
		if err != nil {
			klog.Infof("update imagemanager name=%s status err=%v", imCopy.Name, err)
			return err
		}

		return nil
	})
	if err != nil {
		klog.Infof("update status in showprogress error=%v", err)
		return err
	}
	return nil
}

// jobs provides a way of identifying the download keys for a particular task
// encountering during the pull walk.
//
// This is very minimal and will probably be replaced with something more
// featured.
type jobs struct {
	name     string
	added    map[digest.Digest]struct{}
	descs    []ocispec.Descriptor
	mu       sync.Mutex
	resolved bool
}

func newJobs(name string) *jobs {
	return &jobs{
		name:  name,
		added: map[digest.Digest]struct{}{},
	}
}

func (j *jobs) add(desc ocispec.Descriptor) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.resolved = true

	if _, ok := j.added[desc.Digest]; ok {
		return
	}
	j.descs = append(j.descs, desc)
	j.added[desc.Digest] = struct{}{}
}

func (j *jobs) jobs() []ocispec.Descriptor {
	j.mu.Lock()
	defer j.mu.Unlock()

	var descs []ocispec.Descriptor
	return append(descs, j.descs...)
}

func (j *jobs) isResolved() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.resolved
}
