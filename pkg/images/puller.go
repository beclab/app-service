package images

import (
	"context"
	"os"
	"time"

	"github.com/containerd/containerd"
	"github.com/containerd/containerd/cmd/ctr/commands"
	"github.com/containerd/containerd/cmd/ctr/commands/content"
	"github.com/containerd/containerd/images"
	"github.com/containerd/containerd/platforms"
	refdocker "github.com/containerd/containerd/reference/docker"
	"github.com/containerd/containerd/remotes/docker"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"k8s.io/klog/v2"
)

var sock = "/var/run/containerd/containerd.sock"

type PullOptions struct {
	AppName   string
	OwnerName string
}

type ImageService interface {
	PullImage(ctx context.Context, ref string, opts PullOptions) (string, error)
	Progress(ctx context.Context, ref string, opts PullOptions) (string, error)
}

type imageService struct {
	client *containerd.Client
}

func NewClientOrDie(ctx context.Context) (*imageService, context.Context, context.CancelFunc) {
	_, err := os.Stat(sock)
	if err != nil {
		panic(err)
	}
	client, err := containerd.New(sock, containerd.WithDefaultNamespace("k8s.io"),
		containerd.WithTimeout(10*time.Second))
	if err != nil {
		panic(err)
	}
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(ctx)
	return &imageService{
		client: client,
	}, ctx, cancel
}

func (is *imageService) PullImage(ctx context.Context, ref string, opts PullOptions) (string, error) {
	named, err := refdocker.ParseDockerRef(ref)
	if err != nil {
		return ref, err
	}
	ref = named.String()
	config := newFetchConfig()
	ongoing := newJobs(ref)

	klog.Infof("start to pull image name=%s", ref)

	pctx, stopProgress := context.WithCancel(ctx)

	progress := make(chan struct{})
	activeSeen := make(map[string]struct{})
	go func(map[string]struct{}) {
		showProgress(pctx, ongoing, is.client.ContentStore(), activeSeen, opts)
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
	_, err = is.client.Fetch(pctx, ref, remoteOpts...)
	stopProgress()
	if err != nil {
		klog.Infof("fetch image name=%s err=%v", ref, err)
		return "", err
	}

	<-progress
	return ref, nil
}

func (is *imageService) Progress(ctx context.Context, ref string, opts PullOptions) (string, error) {
	return "", nil
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
