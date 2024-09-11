package kubesphere

import (
	"context"
	"fmt"

	v1alpha1client "bytetrade.io/web3os/app-service/pkg/client/clientset/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/constants"

	"github.com/dgrijalva/jwt-go"
	"gopkg.in/yaml.v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
)

var (
	annotationGroup              = "bytetrade.io"
	userAnnotationZoneKey        = fmt.Sprintf("%s/zone", annotationGroup)
	userAnnotationOwnerRole      = fmt.Sprintf("%s/owner-role", annotationGroup)
	userAnnotationCPULimitKey    = "bytetrade.io/user-cpu-limit"
	userAnnotationMemoryLimitKey = "bytetrade.io/user-memory-limit"
)

type Options struct {
	JwtSecret string `yaml:"jwtSecret"`
}

type Config struct {
	AuthenticationOptions *Options `yaml:"authentication,omitempty"`
}

type Type string

type Claims struct {
	jwt.StandardClaims
	// Private Claim Names
	// TokenType defined the type of the token
	TokenType Type `json:"token_type,omitempty"`
	// Username user identity, deprecated field
	Username string `json:"username,omitempty"`
	// Extra contains the additional information
	Extra map[string][]string `json:"extra,omitempty"`

	// Used for issuing authorization code
	// Scopes can be used to request that specific sets of information be made available as Claim Values.
	Scopes []string `json:"scopes,omitempty"`

	// The following is well-known ID Token fields

	// End-User's full name in displayable form including all name parts,
	// possibly including titles and suffixes, ordered according to the End-User's locale and preferences.
	Name string `json:"name,omitempty"`
	// String value used to associate a Client session with an ID Token, and to mitigate replay attacks.
	// The value is passed through unmodified from the Authentication Request to the ID Token.
	Nonce string `json:"nonce,omitempty"`
	// End-User's preferred e-mail address.
	Email string `json:"email,omitempty"`
	// End-User's locale, represented as a BCP47 [RFC5646] language tag.
	Locale string `json:"locale,omitempty"`
	// Shorthand name by which the End-User wishes to be referred to at the RP,
	PreferredUsername string `json:"preferred_username,omitempty"`
}

func getKubeSphereConfig(ctx context.Context, kubeConfig *rest.Config) (*Config, error) {
	kubeClientInService, err := v1alpha1client.NewKubeClient("", kubeConfig)
	if err != nil {
		return nil, err
	}

	ksConfig, err := kubeClientInService.Kubernetes().
		CoreV1().ConfigMaps(constants.KubeSphereNamespace).
		Get(ctx, constants.KubeSphereConfigName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	c := &Config{}
	value, ok := ksConfig.Data[constants.KubeSphereConfigMapDataKey]
	if !ok {
		return nil, fmt.Errorf("failed to get configmap kubesphere.yaml value")
	}

	if err := yaml.Unmarshal([]byte(value), c); err != nil {
		return nil, fmt.Errorf("failed to unmarshal value from configmap. err: %s", err)
	}
	return c, nil
}

// ValidateToken validates a token by performing an authentication check.
func ValidateToken(ctx context.Context, kubeConfig *rest.Config, tokenString string) (string, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}

		// get jwt secret from kubesphere's config
		config, err := getKubeSphereConfig(ctx, kubeConfig)
		if err != nil {
			return nil, err
		}

		if config.AuthenticationOptions == nil || config.AuthenticationOptions.JwtSecret == "" {
			return nil, fmt.Errorf("jwt secret not found")
		}
		return []byte(config.AuthenticationOptions.JwtSecret), nil
	})

	if err != nil {
		return "", err
	}

	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims.Username, nil
	}
	return "", fmt.Errorf("invalid token, or claims not match")
}

// GetUserZone returns user zone, an error if there is any.
func GetUserZone(ctx context.Context, kubeConfig *rest.Config, username string) (string, error) {
	return GetUserAnnotation(ctx, kubeConfig, username, userAnnotationZoneKey)
}

// GetUserRole returns user role, an error if there is any.
func GetUserRole(ctx context.Context, kubeConfig *rest.Config, username string) (string, error) {
	return GetUserAnnotation(ctx, kubeConfig, username, userAnnotationOwnerRole)
}

// GetUserAnnotation returns user annotation, an error if there is any.
func GetUserAnnotation(ctx context.Context, kubeConfig *rest.Config, username, annotation string) (string, error) {
	gvr := schema.GroupVersionResource{
		Group:    "iam.kubesphere.io",
		Version:  "v1alpha2",
		Resource: "users",
	}
	client, err := dynamic.NewForConfig(kubeConfig)
	if err != nil {
		return "", err
	}
	data, err := client.Resource(gvr).Get(ctx, username, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("Failed to get user=%s err=%v", username, err)
		return "", err
	}

	a, ok := data.GetAnnotations()[annotation]
	if !ok {
		return "", fmt.Errorf("user annotation %s not found", annotation)
	}

	return a, nil
}

// GetUserCPULimit returns user cpu limit value, an error if there is any.
func GetUserCPULimit(ctx context.Context, kubeConfig *rest.Config, username string) (string, error) {
	return GetUserAnnotation(ctx, kubeConfig, username, userAnnotationCPULimitKey)
}

// GetUserMemoryLimit returns user memory limit value, an error if there is any.
func GetUserMemoryLimit(ctx context.Context, kubeConfig *rest.Config, username string) (string, error) {
	return GetUserAnnotation(ctx, kubeConfig, username, userAnnotationMemoryLimitKey)
}

// GetAdminUsername returns admin username, an error if there is any.
func GetAdminUsername(ctx context.Context, kubeConfig *rest.Config) (string, error) {
	gvr := schema.GroupVersionResource{
		Group:    "iam.kubesphere.io",
		Version:  "v1alpha2",
		Resource: "users",
	}
	client, err := dynamic.NewForConfig(kubeConfig)
	if err != nil {
		return "", err
	}
	data, err := client.Resource(gvr).List(ctx, metav1.ListOptions{})
	if err != nil {
		klog.Errorf("Failed to get user list err=%v", err)
		return "", err
	}

	var admin string
	for _, u := range data.Items {
		if u.Object == nil {
			continue
		}
		annotations := u.GetAnnotations()
		if annotations["bytetrade.io/owner-role"] == "platform-admin" {
			admin = u.GetName()
			break
		}
	}

	return admin, nil
}
