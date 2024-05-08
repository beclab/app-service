package sidecar

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"bytetrade.io/web3os/app-service/pkg/appinstaller"
	"bytetrade.io/web3os/app-service/pkg/constants"
	"bytetrade.io/web3os/app-service/pkg/utils"

	envoy_bootstrap "github.com/envoyproxy/go-control-plane/envoy/config/bootstrap/v3"
	clusterv3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	envoy_core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpointv3 "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	envoy_listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	routev3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	envoy_authz "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/ext_authz/v3"
	envoy_lua "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/lua/v3"
	envoy_router "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"
	originaldst "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/listener/original_dst/v3"
	http_connection_manager "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	matcherv3 "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	"github.com/golang/protobuf/ptypes/any"
	"github.com/golang/protobuf/ptypes/duration"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/yaml"
)

// GetEnvoySidecarContainerSpec returns the container specification for the envoy sidecar.
func GetEnvoySidecarContainerSpec(pod *corev1.Pod, namespace string) corev1.Container {
	clusterID := fmt.Sprintf("%s.%s", pod.Spec.ServiceAccountName, namespace)

	return corev1.Container{
		Name:            constants.EnvoyContainerName,
		Image:           constants.EnvoyImageVersion,
		ImagePullPolicy: corev1.PullIfNotPresent,
		SecurityContext: &corev1.SecurityContext{
			AllowPrivilegeEscalation: pointer.BoolPtr(false),
			RunAsUser: func() *int64 {
				uid := constants.EnvoyUID
				return &uid
			}(),
		},
		Ports: getEnvoyContainerPorts(),
		VolumeMounts: []corev1.VolumeMount{{
			Name:      constants.SidecarConfigMapVolumeName,
			ReadOnly:  true,
			MountPath: constants.EnvoyConfigFilePath + "/" + constants.EnvoyConfigFileName,
			SubPath:   constants.EnvoyConfigFileName,
		}},
		Command: []string{"envoy"},
		Args: []string{
			"--log-level", constants.DefaultEnvoyLogLevel,
			"-c", constants.EnvoyConfigFilePath + "/" + constants.EnvoyConfigFileName,
			"--service-cluster", clusterID,
		},
		Env: []corev1.EnvVar{
			{
				Name: "POD_UID",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{
						FieldPath: "metadata.uid",
					},
				},
			},
			{
				Name: "POD_NAME",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{
						FieldPath: "metadata.name",
					},
				},
			},
			{
				Name: "POD_NAMESPACE",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{
						FieldPath: "metadata.namespace",
					},
				},
			},
			{
				Name: "POD_IP",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{
						FieldPath: "status.podIP",
					},
				},
			},
			{
				Name: "SERVICE_ACCOUNT",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{
						FieldPath: "spec.serviceAccountName",
					},
				},
			},
		},
	}
}

func getEnvoyContainerPorts() []corev1.ContainerPort {
	containerPorts := []corev1.ContainerPort{
		{
			Name:          constants.EnvoyAdminPortName,
			ContainerPort: constants.EnvoyAdminPort,
		},
		{
			Name:          constants.EnvoyInboundListenerPortName,
			ContainerPort: constants.EnvoyInboundListenerPort,
		},
	}

	livenessPort := corev1.ContainerPort{
		// Name must be no more than 15 characters
		Name:          "liveness-port",
		ContainerPort: constants.EnvoyLivenessProbePort,
	}
	containerPorts = append(containerPorts, livenessPort)

	return containerPorts
}

func getEnvoyConfig(username string, injectPolicy, injectWs, injectUpload bool, appDomains []string, pod *corev1.Pod) string {
	setCookieInlineCode, err := genEnvoySetCookieScript(appDomains)
	if err != nil {
		klog.Errorf("Failed to get setCookieInlineCode err=%v", err)
	}
	ec := New(username, setCookieInlineCode, getHTTProbePath(pod))
	if injectPolicy {
		ec.WithPolicy()
	}
	if injectWs {
		ec.WithWebSocket()
	}
	if injectUpload {
		ec.WithUpload()
	}
	m, err := utils.ToJSONMap(ec.bs)
	if err != nil {
		klog.Errorf("Failed to convert proto.Message to map err=%v", err)
	}
	mBytes, err := json.Marshal(utils.SnakeCaseMarshaller{Value: m})
	if err != nil {
		klog.Errorf("Failed to make SnakeCaseMarshaller err=%v", err)
	}
	bootstrap, err := yaml.JSONToYAML(mBytes)
	if err != nil {
		klog.Errorf("Failed to convert yaml to json err=%v", err)
	}
	return string(bootstrap)
}

// GetInitContainerSpec returns init container spec.
func GetInitContainerSpec() corev1.Container {
	iptablesInitCommand := generateIptablesCommands()
	enablePrivilegedInitContainer := true

	return corev1.Container{
		Name:            constants.SidecarInitContainerName,
		Image:           "openservicemesh/init:v1.2.3", // TODO:
		ImagePullPolicy: corev1.PullIfNotPresent,
		SecurityContext: &corev1.SecurityContext{
			Privileged: &enablePrivilegedInitContainer,
			Capabilities: &corev1.Capabilities{
				Add: []corev1.Capability{
					"NET_ADMIN",
				},
			},
			RunAsNonRoot: pointer.BoolPtr(false),
			// User ID 0 corresponds to root
			RunAsUser: pointer.Int64Ptr(0),
		},
		Command: []string{"/bin/sh"},
		Args: []string{
			"-c",
			iptablesInitCommand,
		},
		Env: []corev1.EnvVar{
			{
				Name: "POD_IP",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{
						APIVersion: "v1",
						FieldPath:  "status.podIP",
					},
				},
			},
		},
	}
}

func generateIptablesCommands() string {
	cmd := fmt.Sprintf(`iptables-restore --noflush <<EOF
# sidecar interception rules
*nat
:PROXY_IN_REDIRECT - [0:0]
:PROXY_INBOUND - [0:0]
-A PROXY_IN_REDIRECT -p tcp -j REDIRECT --to-port %d
-A PREROUTING -p tcp -j PROXY_INBOUND
-A PROXY_INBOUND -p tcp --dport %d -j RETURN
-A PROXY_INBOUND -p tcp -j PROXY_IN_REDIRECT
COMMIT
EOF
`,
		constants.EnvoyInboundListenerPort,
		constants.EnvoyAdminPort,
	)

	return cmd
}

// GetWebSocketSideCarContainerSpec returns the container specification for the WebSocket sidecar.
func GetWebSocketSideCarContainerSpec(wsConfig *appinstaller.WsConfig) corev1.Container {
	return corev1.Container{
		Name:            constants.WsContainerName,
		Image:           os.Getenv(constants.WsContainerImage),
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{"/ws-gateway"},
		Env: []corev1.EnvVar{
			{
				Name:  "WS_PORT",
				Value: strconv.Itoa(wsConfig.Port),
			},
			{
				Name:  "WS_URL",
				Value: wsConfig.URL,
			},
		},
	}
}

// GetUploadSideCarContainerSpec returns the container specification for the upload sidecar.
func GetUploadSideCarContainerSpec(pod *corev1.Pod, upload *appinstaller.Upload) *corev1.Container {
	dest := filepath.Clean(upload.Dest)
	volumeName := ""
	for _, c := range pod.Spec.Containers {
		for _, v := range c.VolumeMounts {
			if filepath.Clean(v.MountPath) == dest {
				volumeName = v.Name
				break
			}
		}
	}
	if volumeName == "" {
		return nil
	}
	fileType := strings.Join(upload.FileType, ",")
	container := corev1.Container{
		Name:            constants.UploadContainerName,
		Image:           os.Getenv(constants.UploadContainerImage),
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{"/upload"},
		Env: []corev1.EnvVar{
			{
				Name:  "UPLOAD_FILE_TYPE",
				Value: fileType,
			},
			{
				Name:  "UPLOAD_DEST",
				Value: upload.Dest,
			},
			{
				Name:  "UPLOAD_LIMITED_SIZE",
				Value: strconv.Itoa(upload.LimitedSize),
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      volumeName,
				MountPath: upload.Dest,
			},
		},
	}
	return &container
}

type envoyConfig struct {
	bs       *envoy_bootstrap.Bootstrap
	username string
}

var httpFilters []*http_connection_manager.HttpFilter
var httpM *http_connection_manager.HttpConnectionManager
var routeConfig *routev3.RouteConfiguration

// New build a new envoy config.
func New(username string, inlineCode []byte, probesPath []string) *envoyConfig {
	httpFilters = []*http_connection_manager.HttpFilter{
		{
			Name: "envoy.filters.http.router",
			ConfigType: &http_connection_manager.HttpFilter_TypedConfig{
				TypedConfig: utils.MessageToAny(&envoy_router.Router{}),
			},
		},
	}
	if len(inlineCode) != 0 {
		httpFilters = append([]*http_connection_manager.HttpFilter{
			{
				Name: "envoy.filters.http.lua",
				ConfigType: &http_connection_manager.HttpFilter_TypedConfig{
					TypedConfig: utils.MessageToAny(&envoy_lua.Lua{
						InlineCode: string(inlineCode),
					}),
				},
			},
		}, httpFilters...)
	}
	routeConfig = &routev3.RouteConfiguration{
		Name: "local_route",
		VirtualHosts: []*routev3.VirtualHost{
			{
				Name:    "service",
				Domains: []string{"*"},
				TypedPerFilterConfig: map[string]*any.Any{
					"envoy.filters.http.ext_authz": utils.MessageToAny(&envoy_authz.ExtAuthzPerRoute{
						Override: &envoy_authz.ExtAuthzPerRoute_CheckSettings{
							CheckSettings: &envoy_authz.CheckSettings{
								ContextExtensions: map[string]string{
									"virtual_host": "service",
								},
							},
						},
					}),
				},
				Routes: []*routev3.Route{
					{
						Match: &routev3.RouteMatch{
							PathSpecifier: &routev3.RouteMatch_Prefix{
								Prefix: "/",
							},
						},
						Action: &routev3.Route_Route{
							Route: &routev3.RouteAction{
								ClusterSpecifier: &routev3.RouteAction_Cluster{
									Cluster: "original_dst",
								},
							},
						},
					},
				},
			},
		},
	}
	for _, path := range probesPath {
		routeConfig.VirtualHosts[0].Routes = append(
			[]*routev3.Route{{
				Match: &routev3.RouteMatch{
					PathSpecifier: &routev3.RouteMatch_Prefix{
						Prefix: path,
					},
				},
				Action: &routev3.Route_Route{
					Route: &routev3.RouteAction{
						ClusterSpecifier: &routev3.RouteAction_Cluster{
							Cluster: "original_dst",
						},
					},
				},
				TypedPerFilterConfig: map[string]*any.Any{
					"envoy.filters.http.ext_authz": utils.MessageToAny(&envoy_authz.ExtAuthzPerRoute{
						Override: &envoy_authz.ExtAuthzPerRoute_Disabled{
							Disabled: true,
						},
					}),
				},
			}}, routeConfig.VirtualHosts[0].Routes...)
	}

	httpM = &http_connection_manager.HttpConnectionManager{
		StatPrefix: "desktop_http",
		UpgradeConfigs: []*http_connection_manager.HttpConnectionManager_UpgradeConfig{
			{
				UpgradeType: "websocket",
			},
			{
				UpgradeType: "tailscale-control-protocol",
			},
		},
		SkipXffAppend: false,
		CodecType:     http_connection_manager.HttpConnectionManager_AUTO,
		RouteSpecifier: &http_connection_manager.HttpConnectionManager_RouteConfig{
			RouteConfig: routeConfig,
		},
		HttpFilters: httpFilters,
		HttpProtocolOptions: &corev3.Http1ProtocolOptions{
			AcceptHttp_10: true,
		},
	}

	return &envoyConfig{
		bs: &envoy_bootstrap.Bootstrap{
			Admin: &envoy_bootstrap.Admin{
				AccessLogPath: "/dev/stdout",
				Address:       createSocketAddress("0.0.0.0", 15000),
			},
			StaticResources: &envoy_bootstrap.Bootstrap_StaticResources{
				Listeners: []*envoy_listener.Listener{
					{
						Name:    "listener_0",
						Address: createSocketAddress("0.0.0.0", 15003),
						ListenerFilters: []*envoy_listener.ListenerFilter{
							{
								Name: "envoy.filters.listener.original_dst",
								ConfigType: &envoy_listener.ListenerFilter_TypedConfig{
									TypedConfig: utils.MessageToAny(&originaldst.OriginalDst{}),
								},
							},
						},
						FilterChains: []*envoy_listener.FilterChain{
							{
								Filters: []*envoy_listener.Filter{
									{
										Name: "envoy.filters.network.http_connection_manager",
										ConfigType: &envoy_listener.Filter_TypedConfig{
											TypedConfig: utils.MessageToAny(httpM),
										},
									},
								},
							},
						},
					},
					{
						Name:    "listener_image",
						Address: createSocketAddress("127.0.0.1", 15080),
						FilterChains: []*envoy_listener.FilterChain{
							{
								Filters: []*envoy_listener.Filter{
									{
										Name: "envoy.filters.network.http_connection_manager",
										ConfigType: &envoy_listener.Filter_TypedConfig{
											TypedConfig: utils.MessageToAny(&http_connection_manager.HttpConnectionManager{
												StatPrefix: "tapr_http",
												UpgradeConfigs: []*http_connection_manager.HttpConnectionManager_UpgradeConfig{
													{
														UpgradeType: "websocket",
													},
												},
												SkipXffAppend: false,
												CodecType:     http_connection_manager.HttpConnectionManager_AUTO,
												RouteSpecifier: &http_connection_manager.HttpConnectionManager_RouteConfig{
													RouteConfig: &routev3.RouteConfiguration{
														Name: "local_route",
														VirtualHosts: []*routev3.VirtualHost{
															{
																Name:    "service",
																Domains: []string{"*"},
																Routes: []*routev3.Route{
																	{
																		Match: &routev3.RouteMatch{
																			PathSpecifier: &routev3.RouteMatch_Prefix{
																				Prefix: "/images/upload",
																			},
																		},
																		Action: &routev3.Route_Route{
																			Route: &routev3.RouteAction{
																				ClusterSpecifier: &routev3.RouteAction_Cluster{
																					Cluster: "images",
																				},
																			},
																		},
																	},
																},
															},
														},
													},
												},
												HttpFilters: []*http_connection_manager.HttpFilter{
													{
														Name: "envoy.filters.http.router",
														ConfigType: &http_connection_manager.HttpFilter_TypedConfig{
															TypedConfig: utils.MessageToAny(&envoy_router.Router{}),
														},
													},
												},
												HttpProtocolOptions: &corev3.Http1ProtocolOptions{
													AcceptHttp_10: true,
												},
											}),
										},
									},
								},
							},
						},
					},
				},
				Clusters: []*clusterv3.Cluster{
					{
						Name: "original_dst",
						ConnectTimeout: &duration.Duration{
							Seconds: 5000,
						},
						ClusterDiscoveryType: &clusterv3.Cluster_Type{
							Type: clusterv3.Cluster_ORIGINAL_DST,
						},
						LbPolicy: clusterv3.Cluster_CLUSTER_PROVIDED,
					},

					{
						Name: "images",
						ConnectTimeout: &duration.Duration{
							Seconds: 5,
						},
						ClusterDiscoveryType: &clusterv3.Cluster_Type{
							Type: clusterv3.Cluster_LOGICAL_DNS,
						},
						DnsLookupFamily: clusterv3.Cluster_V4_ONLY,
						DnsRefreshRate: &duration.Duration{
							Seconds: 600,
						},
						LbPolicy: clusterv3.Cluster_ROUND_ROBIN,
						LoadAssignment: &endpointv3.ClusterLoadAssignment{
							ClusterName: "images",
							Endpoints: []*endpointv3.LocalityLbEndpoints{
								{
									LbEndpoints: []*endpointv3.LbEndpoint{
										{
											HostIdentifier: &endpointv3.LbEndpoint_Endpoint{
												Endpoint: &endpointv3.Endpoint{
													Address: createSocketAddress("tapr-images-svc.user-system-"+username, 8080),
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		username: username,
	}
}

func (ec *envoyConfig) WithPolicy() *envoyConfig {
	filter := &http_connection_manager.HttpFilter{
		Name: "envoy.filters.http.ext_authz",
		ConfigType: &http_connection_manager.HttpFilter_TypedConfig{
			TypedConfig: utils.MessageToAny(&envoy_authz.ExtAuthz{
				Services: &envoy_authz.ExtAuthz_HttpService{
					HttpService: &envoy_authz.HttpService{
						PathPrefix: "/api/verify/",
						ServerUri: &corev3.HttpUri{
							Uri: "authelia-backend.user-system-" + ec.username + ":9091",
							HttpUpstreamType: &corev3.HttpUri_Cluster{
								Cluster: "authelia",
							},
							Timeout: &duration.Duration{
								Seconds: 0,
								Nanos:   250000000,
							},
						},
						AuthorizationRequest: &envoy_authz.AuthorizationRequest{
							AllowedHeaders: &matcherv3.ListStringMatcher{
								Patterns: []*matcherv3.StringMatcher{
									{
										MatchPattern: &matcherv3.StringMatcher_Exact{
											Exact: "accept",
										},
									},
									{
										MatchPattern: &matcherv3.StringMatcher_Exact{
											Exact: "cookie",
										},
									},
									{
										MatchPattern: &matcherv3.StringMatcher_Exact{
											Exact: "proxy-authorization",
										},
									},
									{
										MatchPattern: &matcherv3.StringMatcher_Prefix{
											Prefix: "x-unauth-",
										},
									},
									{
										MatchPattern: &matcherv3.StringMatcher_Exact{
											Exact: "x-authorization",
										},
									},
									{
										MatchPattern: &matcherv3.StringMatcher_Exact{
											Exact: "x-bfl-user",
										},
									},
									{
										MatchPattern: &matcherv3.StringMatcher_Exact{
											Exact: "terminus-nonce",
										},
									},
								},
							},
							HeadersToAdd: []*corev3.HeaderValue{
								{
									Key:   "X-Forwarded-Method",
									Value: "%REQ(:METHOD)%",
								},
								{
									Key:   "X-Forwarded-Proto",
									Value: "%REQ(:SCHEME)%",
								},
								{
									Key:   "X-Forwarded-Host",
									Value: "%REQ(:AUTHORITY)%",
								},
								{
									Key:   "X-Forwarded-Uri",
									Value: "%REQ(:PATH)%",
								},
								{
									Key:   "X-Forwarded-For",
									Value: "%DOWNSTREAM_REMOTE_ADDRESS_WITHOUT_PORT%",
								},
							},
						},
						AuthorizationResponse: &envoy_authz.AuthorizationResponse{
							AllowedUpstreamHeaders: &matcherv3.ListStringMatcher{
								Patterns: []*matcherv3.StringMatcher{
									{
										MatchPattern: &matcherv3.StringMatcher_Exact{
											Exact: "authorization",
										},
									},
									{
										MatchPattern: &matcherv3.StringMatcher_Exact{
											Exact: "proxy-authorization",
										},
									},
									{
										MatchPattern: &matcherv3.StringMatcher_Prefix{
											Prefix: "remote-",
										},
									},
									{
										MatchPattern: &matcherv3.StringMatcher_Prefix{
											Prefix: "authelia-",
										},
									},
								},
							},
							AllowedClientHeaders: &matcherv3.ListStringMatcher{
								Patterns: []*matcherv3.StringMatcher{
									{
										MatchPattern: &matcherv3.StringMatcher_Exact{
											Exact: "set-cookie",
										},
									},
								},
							},
						},
					},
				},
				FailureModeAllow: false,
			}),
		},
	}
	httpFilters = append([]*http_connection_manager.HttpFilter{filter}, httpFilters...)
	httpM.HttpFilters = httpFilters
	ec.bs.StaticResources.Listeners[0].FilterChains[0].Filters[0] = &envoy_listener.Filter{
		Name: "envoy.filters.network.http_connection_manager",
		ConfigType: &envoy_listener.Filter_TypedConfig{
			TypedConfig: utils.MessageToAny(httpM),
		},
	}
	ec.bs.StaticResources.Clusters = append(ec.bs.StaticResources.Clusters, &clusterv3.Cluster{
		Name: "authelia",
		ConnectTimeout: &duration.Duration{
			Nanos: 250000000,
		},
		ClusterDiscoveryType: &clusterv3.Cluster_Type{
			Type: clusterv3.Cluster_LOGICAL_DNS,
		},
		DnsLookupFamily: clusterv3.Cluster_V4_ONLY,
		DnsRefreshRate: &duration.Duration{
			Seconds: 600,
		},
		LbPolicy: clusterv3.Cluster_ROUND_ROBIN,
		LoadAssignment: &endpointv3.ClusterLoadAssignment{
			ClusterName: "authelia",
			Endpoints: []*endpointv3.LocalityLbEndpoints{
				{
					LbEndpoints: []*endpointv3.LbEndpoint{
						{
							HostIdentifier: &endpointv3.LbEndpoint_Endpoint{
								Endpoint: &endpointv3.Endpoint{
									Address: createSocketAddress("authelia-backend.user-system-"+ec.username, 9091),
								},
							},
						},
					},
				},
			},
		},
	})
	return ec
}

func (ec *envoyConfig) WithWebSocket() *envoyConfig {
	routeConfig.VirtualHosts[0].Routes = append(routeConfig.VirtualHosts[0].Routes, &routev3.Route{
		Match: &routev3.RouteMatch{
			PathSpecifier: &routev3.RouteMatch_Prefix{
				Prefix: "/ws",
			},
		},
		Action: &routev3.Route_Route{
			Route: &routev3.RouteAction{
				ClusterSpecifier: &routev3.RouteAction_Cluster{
					Cluster: "ws_original_dst",
				},
			},
		},
	})
	filter := &envoy_listener.Filter{
		Name: "envoy.filters.network.http_connection_manager",
		ConfigType: &envoy_listener.Filter_TypedConfig{
			TypedConfig: utils.MessageToAny(&http_connection_manager.HttpConnectionManager{
				StatPrefix: "desktop_http",
				UpgradeConfigs: []*http_connection_manager.HttpConnectionManager_UpgradeConfig{
					{
						UpgradeType: "websocket",
					},
					{
						UpgradeType: "tailscale-control-protocol",
					},
				},
				SkipXffAppend: false,
				CodecType:     http_connection_manager.HttpConnectionManager_AUTO,
				RouteSpecifier: &http_connection_manager.HttpConnectionManager_RouteConfig{
					RouteConfig: routeConfig,
				},
				HttpFilters: httpFilters,
				HttpProtocolOptions: &corev3.Http1ProtocolOptions{
					AcceptHttp_10: true,
				},
			}),
		},
	}
	ec.bs.StaticResources.Listeners[0].FilterChains[0].Filters[0] = filter
	ec.bs.StaticResources.Clusters = append(ec.bs.StaticResources.Clusters, &clusterv3.Cluster{
		Name: "ws_original_dst",
		ConnectTimeout: &duration.Duration{
			Seconds: 5000,
		},
		ClusterDiscoveryType: &clusterv3.Cluster_Type{
			Type: clusterv3.Cluster_LOGICAL_DNS,
		},
		DnsLookupFamily: clusterv3.Cluster_V4_ONLY,
		DnsRefreshRate: &duration.Duration{
			Seconds: 600,
		},
		LbPolicy: clusterv3.Cluster_ROUND_ROBIN,
		LoadAssignment: &endpointv3.ClusterLoadAssignment{
			ClusterName: "ws_original_dst",
			Endpoints: []*endpointv3.LocalityLbEndpoints{
				{
					LbEndpoints: []*endpointv3.LbEndpoint{
						{
							HostIdentifier: &endpointv3.LbEndpoint_Endpoint{
								Endpoint: &endpointv3.Endpoint{
									Address: createSocketAddress("localhost", 40010),
								},
							},
						},
					},
				},
			},
		},
	})
	return ec
}

func (ec *envoyConfig) WithUpload() *envoyConfig {
	routeConfig.VirtualHosts[0].Routes = append(routeConfig.VirtualHosts[0].Routes, &routev3.Route{
		Match: &routev3.RouteMatch{
			PathSpecifier: &routev3.RouteMatch_Prefix{
				Prefix: "/upload",
			},
		},
		Action: &routev3.Route_Route{
			Route: &routev3.RouteAction{
				ClusterSpecifier: &routev3.RouteAction_Cluster{
					Cluster: "upload_original_dst",
				},
			},
		},
	})

	filter := &envoy_listener.Filter{
		Name: "envoy.filters.network.http_connection_manager",
		ConfigType: &envoy_listener.Filter_TypedConfig{
			TypedConfig: utils.MessageToAny(&http_connection_manager.HttpConnectionManager{
				StatPrefix: "desktop_http",
				UpgradeConfigs: []*http_connection_manager.HttpConnectionManager_UpgradeConfig{
					{
						UpgradeType: "websocket",
					},
					{
						UpgradeType: "tailscale-control-protocol",
					},
				},
				SkipXffAppend: false,
				CodecType:     http_connection_manager.HttpConnectionManager_AUTO,
				RouteSpecifier: &http_connection_manager.HttpConnectionManager_RouteConfig{
					RouteConfig: routeConfig,
				},
				HttpFilters: httpFilters,
				HttpProtocolOptions: &corev3.Http1ProtocolOptions{
					AcceptHttp_10: true,
				},
			}),
		},
	}
	ec.bs.StaticResources.Listeners[0].FilterChains[0].Filters[0] = filter
	ec.bs.StaticResources.Clusters = append(ec.bs.StaticResources.Clusters, &clusterv3.Cluster{
		Name: "upload_original_dst",
		ConnectTimeout: &duration.Duration{
			Seconds: 5000,
		},
		ClusterDiscoveryType: &clusterv3.Cluster_Type{
			Type: clusterv3.Cluster_LOGICAL_DNS,
		},
		DnsLookupFamily: clusterv3.Cluster_V4_ONLY,
		DnsRefreshRate: &duration.Duration{
			Seconds: 600,
		},
		LbPolicy: clusterv3.Cluster_ROUND_ROBIN,
		LoadAssignment: &endpointv3.ClusterLoadAssignment{
			ClusterName: "upload_original_dst",
			Endpoints: []*endpointv3.LocalityLbEndpoints{
				{
					LbEndpoints: []*endpointv3.LbEndpoint{
						{
							HostIdentifier: &endpointv3.LbEndpoint_Endpoint{
								Endpoint: &endpointv3.Endpoint{
									Address: createSocketAddress("localhost", 40030),
								},
							},
						},
					},
				},
			},
		},
	})
	return ec
}

func createSocketAddress(addr string, port uint32) *envoy_core.Address {
	return &envoy_core.Address{
		Address: &envoy_core.Address_SocketAddress{
			SocketAddress: &envoy_core.SocketAddress{
				Address: addr,
				PortSpecifier: &envoy_core.SocketAddress_PortValue{
					PortValue: port,
				},
			},
		},
	}
}
