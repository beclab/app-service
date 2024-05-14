package userspace

import (
	"context"
	"crypto"
	"encoding/hex"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"time"

	"bytetrade.io/web3os/app-service/pkg/client/clientset"
	"bytetrade.io/web3os/app-service/pkg/constants"
	"bytetrade.io/web3os/app-service/pkg/helm"
	"bytetrade.io/web3os/app-service/pkg/users/userspace"
	"bytetrade.io/web3os/app-service/pkg/users/userspace/templates"
	"bytetrade.io/web3os/app-service/pkg/utils"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
)

type Creator struct {
	Deleter
	rand *rand.Rand
}

const (
	USER_SPACE_ROLE = "admin"
	MAX_RAND_INT    = 1000000
)

var (
	REQUIRE_PERMISSION_APPS = []string{
		// "files",
		"portfolio",
		"vault",
		"desktop",
		"message",
		"rss",
		"search",
	}
)

func init() {
	envRPA := os.Getenv("REQUIRE_PERMISSION_APPS")
	if envRPA != "" {
		apps := strings.Split(envRPA, ",")
		REQUIRE_PERMISSION_APPS = apps
	}
}

func NewCreator(client *clientset.ClientSet, config *rest.Config, user string) *Creator {
	return &Creator{
		Deleter: Deleter{
			clientSet: client,
			k8sConfig: config,
			user:      user,
		},
		rand: rand.New(rand.NewSource(time.Now().Unix())),
	}
}

func (c *Creator) CreateUserApps(ctx context.Context) (int32, int32, error) {
	var clear bool
	var launcherReleaseName string
	userspace, userspaceRoleBinding, err := c.createNamespace(ctx)

	if err != nil {
		return 0, 0, err
	}
	clear = true

	defer func() {
		if clear {
			klog.Infof("Start clear process err=%v", err)
			if userspace != "" {
				// clear context should not be canceled
				clearCtx := context.Background()
				if launcherReleaseName != "" {
					klog.Warningf("Clear launcher failed launcherReleaseName=%s", launcherReleaseName)

					err1 := c.clearLauncher(clearCtx, launcherReleaseName, userspace)
					if err1 != nil {
						klog.Warningf("Clear launcher failed err=%v", err1)
					}
				}

				klog.Warningf("Clear userspace failed userspace=%s", userspace)

				err1 := c.deleteNamespace(clearCtx, userspace, userspaceRoleBinding)
				if err1 != nil {
					klog.Warningf("Failed to delete namespace err=%v", err1)
				}
			}
		}

	}()

	actionCfg, settings, err := helm.InitConfig(c.k8sConfig, userspace)
	if err != nil {
		return 0, 0, err
	}
	c.helmCfg.ActionCfg = actionCfg
	c.helmCfg.Settings = settings

	launcherReleaseName, err = c.installLauncher(ctx, userspace)
	if err != nil {
		return 0, 0, err
	}

	var bfl *corev1.Pod
	if bfl, err = c.checkLauncher(ctx, userspace, checkLauncherRunning); err != nil {
		return 0, 0, err
	}

	err = c.installSysApps(ctx, bfl)
	if err != nil {
		return 0, 0, err
	}

	desktopPort, wizardPort, err := c.checkDesktopRunning(ctx, userspace)
	if err == nil {
		clear = false
	}
	return desktopPort, wizardPort, err
}

func (c *Creator) createNamespace(ctx context.Context) (string, string, error) {

	// ns and rolebinding creation moves to bfl

	// create namespace user-space-<user>
	userspace := templates.NewUserspace(c.user)
	// role binding
	userspaceRoleBinding := templates.NewUserspaceRoleBinding(c.user, userspace.Name, USER_SPACE_ROLE)

	// create ns user-system
	usersystem := templates.NewUserSystem(c.user)
	ns, err := c.clientSet.KubeClient.Kubernetes().
		CoreV1().Namespaces().
		Get(ctx, usersystem.Name, metav1.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return "", "", err
	}

	if ns == nil || ns.Name == "" {
		ns, err = c.clientSet.KubeClient.Kubernetes().
			CoreV1().Namespaces().
			Create(ctx, (*corev1.Namespace)(usersystem), metav1.CreateOptions{})

		if err != nil {
			return "", "", err
		}
	}

	usersystemRoleBinding := templates.NewUserspaceRoleBinding(c.user, usersystem.Name, USER_SPACE_ROLE)

	_, err = c.clientSet.KubeClient.Kubernetes().
		RbacV1().RoleBindings(ns.Name).Create(ctx, (*rbacv1.RoleBinding)(usersystemRoleBinding), metav1.CreateOptions{})

	if err != nil {
		c.clientSet.KubeClient.Kubernetes().
			CoreV1().Namespaces().Delete(ctx, usersystem.Name, metav1.DeleteOptions{})

		return "", "", err
	}

	return userspace.Name, userspaceRoleBinding.Name, nil
}

func (c *Creator) installSysApps(ctx context.Context, bflPod *corev1.Pod) error {
	vals := make(map[string]interface{})

	ip := utils.GetMyExternalIPAddr()
	port, err := c.findBflAPIPort(ctx, bflPod.Namespace)
	if err != nil {
		return err
	}

	bflDocURL := fmt.Sprintf("//%s:%d/bfl/apidocs.json", ip, port)

	vals["bfl"] = map[string]string{
		"username": c.user,
		"nodeName": bflPod.Spec.NodeName,
		"url":      bflDocURL,
	}

	vals["global"] = map[string]interface{}{
		"bfl": map[string]string{
			"username": c.user,
		},
	}

	pvcData, err := c.findPVC(ctx, bflPod.Namespace)
	if err != nil {
		return err
	}

	pv, err := c.clientSet.KubeClient.Kubernetes().CoreV1().PersistentVolumes().Get(ctx, pvcData.userspacePv, metav1.GetOptions{})
	if err != nil {
		return err
	}

	// pvc to mount with filebrowser
	pvPath := pv.Spec.HostPath.Path
	vals["pvc"] = map[string]string{
		"userspace": pvcData.userspacePvc,
	}

	vals["userspace"] = map[string]string{
		"appCache": pvcData.appCacheHostPath,
		"userData": fmt.Sprintf("%s/Home", pvPath),
		"appData":  fmt.Sprintf("%s/Data", pvPath),
		"dbdata":   pvcData.dbdataHostPath,
	}

	// generate app key and secret of permission required apps
	osVals := make(map[string]interface{})
	for _, r := range REQUIRE_PERMISSION_APPS {
		k, s := c.genAppkeyAndSecret(r)
		osVals[r] = map[string]interface{}{
			"appKey":    k,
			"appSecret": s,
		}
	}
	vals["os"] = osVals

	clientSet, err := kubernetes.NewForConfig(c.k8sConfig)
	if err != nil {
		return err
	}
	redisSecret, err := clientSet.CoreV1().Secrets("kubesphere-system").Get(ctx, "redis-secret", metav1.GetOptions{})
	if err != nil {
		return err
	}
	vals["kubesphere"] = map[string]string{
		"redis_password": string(redisSecret.Data["auth"]),
	}

	gpuType, err := findGpuTypeFromNodes(ctx, clientSet)
	if err != nil {
		return err
	}
	vals["gpu"] = gpuType

	sysApps, err := userspace.GetAppsFromDirectory(constants.UserChartsPath + "/apps")
	if err != nil {
		return err
	}
	for _, appname := range sysApps {
		name := helm.ReleaseName(appname, c.user)
		err = helm.InstallCharts(ctx, c.helmCfg.ActionCfg, c.helmCfg.Settings,
			name, constants.UserChartsPath+"/apps/"+appname, "", bflPod.Namespace, vals)
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *Creator) installLauncher(ctx context.Context, userspace string) (string, error) {
	vals := make(map[string]interface{})

	k, s := c.genAppkeyAndSecret("bfl")
	vals["bfl"] = map[string]string{
		"username":  c.user,
		"appKey":    k,
		"appSecret": s,
	}

	name := helm.ReleaseName("launcher", c.user)
	err := helm.InstallCharts(ctx, c.helmCfg.ActionCfg, c.helmCfg.Settings, name, constants.UserChartsPath+"/launcher", "", userspace, vals)
	if err != nil {
		return "", err
	}

	return name, nil
}

func (c *Creator) findBflAPIPort(ctx context.Context, namespace string) (int32, error) {
	bflSvc, err := c.clientSet.KubeClient.Kubernetes().CoreV1().Services(namespace).Get(ctx, "bfl", metav1.GetOptions{})
	if err != nil {
		return 0, err
	}
	var port int32 = 0
	for _, p := range bflSvc.Spec.Ports {
		if p.Name == "api" {
			port = p.NodePort
			break
		}
	}

	return port, nil
}

func (c *Creator) checkDesktopRunning(ctx context.Context, userspace string) (int32, int32, error) {
	ticker := time.NewTicker(1 * time.Second)
	timeout := time.NewTimer(5 * time.Minute)

	defer func() {
		ticker.Stop()
		timeout.Stop()
	}()

	for {
		select {
		case <-ticker.C:
			pods, err := c.clientSet.KubeClient.Kubernetes().CoreV1().Pods(userspace).
				List(ctx, metav1.ListOptions{LabelSelector: "app=wizard"})
			if err != nil {
				return 0, 0, err
			}

			if len(pods.Items) == 0 {
				klog.Warningf("Failed to find user's wizard userspace=%s", userspace)
				continue
			}

			wizard := pods.Items[0] // a single bfl per user
			if wizard.Status.Phase == "Running" {
				// find desktop port
				// desktop, err := c.clientSet.KubeClient.Kubernetes().CoreV1().Services(userspace).Get(ctx, "edge-desktop", metav1.GetOptions{})
				// if err != nil {
				// 	return 0, 0, err
				// }

				// find wizard port
				var wizardPort int32
				wizard, err := c.clientSet.KubeClient.Kubernetes().CoreV1().Services(userspace).Get(ctx, "swagger-ui", metav1.GetOptions{})
				if err != nil {
					klog.Errorf("Failed to get wizard port err=%v", err)
				} else {
					wizardPort = wizard.Spec.Ports[0].NodePort
				}

				return 0, wizardPort, nil
			}
		case <-timeout.C:
			return 0, 0, fmt.Errorf("user's wizard checking timeout error. [%s]", userspace)
		}
	}
}

func (c *Creator) genAppkeyAndSecret(app string) (string, string) {
	key := fmt.Sprintf("bytetrade_%s_%d", app, c.rand.Intn(MAX_RAND_INT))
	secret := md5(fmt.Sprintf("%s|%d", key, time.Now().Unix()))

	return key, secret[:16]
}

func md5(str string) string {
	h := crypto.MD5.New()
	h.Write([]byte(str))
	return hex.EncodeToString(h.Sum(nil))
}

func findGpuTypeFromNodes(ctx context.Context, clientSet *kubernetes.Clientset) (string, error) {
	gpuType := "none"
	nodes, err := clientSet.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return gpuType, err
	}
	for _, n := range nodes.Items {
		if _, ok := n.Status.Capacity[constants.NvidiaGPU]; ok {
			if _, ok = n.Status.Capacity[constants.NvshareGPU]; ok {
				return "nvshare", nil

			}
			gpuType = "nvidia"
		}
		if _, ok := n.Status.Capacity[constants.VirtAiTechVGPU]; ok {
			return "virtaitech", nil
		}
	}
	return gpuType, nil
}
