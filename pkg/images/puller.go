package images

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
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
	srvconfig "github.com/containerd/containerd/services/server/config"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

const defaultRegistry = "https://registry-1.docker.io"
const maxRetries = 6

var sock = "/var/run/containerd/containerd.sock"
var mirrorsEndpoint []string

func init() {
	mirrorsEndpoint = getMirrorsEndpoint()
	klog.Infof("mirrorsEndPoint: %v", mirrorsEndpoint)
}

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
	originNamed, err := refdocker.ParseDockerRef(ref.Name)
	if err != nil {
		return ref.Name, err
	}
	ref.Name = originNamed.String()
	config := newFetchConfig()
	// replaced image ref
	replacedRef := replacedImageRef(originNamed.String())

	ongoing := newJobs(replacedRef, originNamed.String())

	imageName, err := is.GetExistsImage(originNamed.String())
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
		downloadFunc := func() error {
			attempt := 1

			for {
				_, err = is.client.Fetch(pctx, replacedRef, remoteOpts...)

				if err == nil {
					break
				}

				select {
				case <-pctx.Done():
					return pctx.Err()
				default:
				}

				if attempt >= maxRetries {
					return fmt.Errorf("download failed after %d attempts: %v", attempt, err)
				}

				delay := attempt * 5
				ticker := time.NewTicker(time.Second)
				klog.Infof("attempt %d", attempt)
				attempt++
			selectLoop:
				for {
					klog.Infof("Retrying in %d seconds", delay)
					select {
					case <-ticker.C:
						delay--
						if delay == 0 {
							ticker.Stop()
							break selectLoop
						}
					case <-pctx.Done():
						ticker.Stop()
						return pctx.Err()
					}
				}
			}
			err = is.tag(pctx, replacedRef, originNamed.String())
			if err != nil {
				return err
			}
			return nil
		}

		err = downloadFunc()
	}

	stopProgress()
	if err != nil {
		klog.Infof("fetch image name=%s err=%v", ref, err)
		return "", err
	}

	<-progress
	return originNamed.String(), nil
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

func (is *imageService) tag(ctx context.Context, ref, targetRef string) error {
	ctx, done, err := is.client.WithLease(ctx)
	if err != nil {
		return err
	}
	defer done(ctx)
	imgService := is.client.ImageService()
	image, err := imgService.Get(ctx, ref)
	if err != nil {
		return err
	}
	image.Name = targetRef
	if _, err = imgService.Create(ctx, image); err != nil {
		if errdefs.IsAlreadyExists(err) {
			if err = imgService.Delete(ctx, targetRef); err != nil {
				return err
			}
			if _, err = imgService.Create(ctx, image); err != nil {
				return err
			}
		} else {
			return err
		}
	}
	return nil
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

func getMirrorsEndpoint() (ep []string) {
	config := &srvconfig.Config{}
	err := srvconfig.LoadConfig("/etc/containerd/config.toml", config)
	if err != nil {
		klog.Infof("load mirrors endpoint failed err=%v", err)
		return
	}
	plugins := config.Plugins["io.containerd.grpc.v1.cri"]
	r := plugins.GetPath([]string{"registry", "mirrors", "docker.io", "endpoint"})
	if r == nil {
		return
	}
	for _, e := range r.([]interface{}) {
		ep = append(ep, e.(string))
	}
	return ep
}

func replacedImageRef(oldImageRef string) string {
	if len(mirrorsEndpoint) == 0 {
		return oldImageRef
	}
	for _, e := range mirrorsEndpoint {
		if e != defaultRegistry {
			url, _ := url.Parse(e)
			if url.Scheme == "http" {
				continue
			}
			host := url.Host
			if !hasPort(url.Host) {
				host = net.JoinHostPort(url.Host, "443")
			}
			conn, err := net.DialTimeout("tcp", host, 2*time.Second)
			if err != nil {
				continue
			}
			if conn != nil {
				conn.Close()
			}
			parts := strings.Split(oldImageRef, "/")
			parts[0] = url.Host
			return strings.Join(parts, "/")
		}
	}
	return oldImageRef
}

func hasPort(s string) bool { return strings.LastIndex(s, ":") > strings.LastIndex(s, "]") }
