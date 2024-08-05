package images

import (
	"context"
	"errors"
	"os"
	"time"

	appv1alpha1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"

	"github.com/containerd/containerd"
	"github.com/containerd/containerd/cmd/ctr/commands"
	"github.com/containerd/containerd/cmd/ctr/commands/content"
	"github.com/containerd/containerd/errdefs"
	"github.com/containerd/containerd/images"
	"github.com/containerd/containerd/platforms"
	refdocker "github.com/containerd/containerd/reference/docker"
	"github.com/containerd/containerd/remotes/docker"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

var sock = "/var/run/containerd/containerd.sock"

type PullOptions struct {
	AppName      string
	OwnerName    string
	AppNamespace string
}

type ImageService interface {
	PullImage(ctx context.Context, ref string, opts PullOptions) (string, error)
	Progress(ctx context.Context, ref string, opts PullOptions) (string, error)
}

type imageService struct {
	client *containerd.Client
}

func NewClient(ctx context.Context) (*imageService, context.Context, context.CancelFunc, error) {
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(ctx)
	_, err := os.Stat(sock)
	if err != nil {
		return nil, ctx, cancel, err
	}
	client, err := containerd.New(sock, containerd.WithDefaultNamespace("k8s.io"),
		containerd.WithTimeout(10*time.Second))
	if err != nil {
		return nil, ctx, cancel, err
	}
	return &imageService{
		client: client,
	}, ctx, cancel, nil
}

func (is *imageService) PullImage(ctx context.Context, ref appv1alpha1.Ref, opts PullOptions) (string, error) {
	named, err := refdocker.ParseDockerRef(ref.Name)
	if err != nil {
		return ref.Name, err
	}
	imageRef := named.String()
	ref.Name = imageRef
	config := newFetchConfig()
	ongoing := newJobs(imageRef)

	imageName, err := is.GetExistsImage(imageRef)
	if err != nil && errors.Is(err, errdefs.ErrNotFound) {
		klog.Infof("Failed to get image status err=%v", err)
	}
	present := imageName != ""

	pctx, stopProgress := context.WithCancel(ctx)

	progress := make(chan struct{})
	activeSeen := make(map[string]struct{})
	go func(map[string]struct{}) {
		showProgress(pctx, ongoing, is.client.ContentStore(), activeSeen, shouldPUllImage(ref, present), opts)
		close(progress)
	}(activeSeen)

	h := images.HandlerFunc(func(ctx context.Context, desc ocispec.Descriptor) ([]ocispec.Descriptor, error) {
		if desc.MediaType != images.MediaTypeDockerSchema1Manifest {
			ongoing.add(desc)
		}
		return nil, nil
	})

	labels := commands.LabelArgs(config.Labels)
	remoteOpts := []containerd.RemoteOpt{
		containerd.WithPullLabels(labels),
		containerd.WithResolver(config.Resolver),
		containerd.WithImageHandler(h),
		containerd.WithSchema1Conversion,
	}
	if config.AllMetadata {
		remoteOpts = append(remoteOpts, containerd.WithAllMetadata())
	}
	if config.PlatformMatcher != nil {
		remoteOpts = append(remoteOpts, containerd.WithPlatformMatcher(config.PlatformMatcher))
	} else {
		for _, platform := range config.Platforms {
			remoteOpts = append(remoteOpts, containerd.WithPlatform(platform))
		}
	}
	if shouldPUllImage(ref, present) {
		var maxRetries = 5
		for i := 0; i < maxRetries; i++ {
			_, err = is.client.Fetch(pctx, imageRef, remoteOpts...)
			if err == nil {
				break
			}
			time.Sleep(time.Second)
		}
	}

	stopProgress()
	if err != nil {
		klog.Infof("fetch image name=%s err=%v", ref, err)
		return "", err
	}

	<-progress
	return imageRef, nil
}

func (is *imageService) Progress(ctx context.Context, ref string, opts PullOptions) (string, error) {
	return "", nil
}

func (is *imageService) GetExistsImage(ref string) (string, error) {
	name, _ := refdocker.ParseDockerRef(ref)
	image, err := is.client.GetImage(context.TODO(), name.String())
	if err != nil {
		return "", err
	}
	return image.Name(), nil
}

func shouldPUllImage(ref appv1alpha1.Ref, imagePresent bool) bool {
	if ref.ImagePullPolicy == corev1.PullNever {
		return false
	}
	if ref.ImagePullPolicy == corev1.PullAlways ||
		(ref.ImagePullPolicy == corev1.PullIfNotPresent && (!imagePresent)) {
		return true
	}
	return false
}

func newFetchConfig() *content.FetchConfig {
	options := docker.ResolverOptions{}
	resolver := docker.NewResolver(options)
	config := &content.FetchConfig{
		Resolver:        resolver,
		PlatformMatcher: platforms.Default(),
	}
	return config
}
