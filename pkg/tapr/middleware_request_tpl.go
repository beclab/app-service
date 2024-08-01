package tapr

import (
	"bytes"
	"text/template"
)

const postgresRequest = `apiVersion: apr.bytetrade.io/v1alpha1
kind: MiddlewareRequest
metadata:
  name: {{ .AppName }}-postgres
  namespace: {{ .Namespace }}
spec:
  app: {{ .AppName }}
  appNamespace: {{ .AppNamespace }}
  middleware: postgres
  postgreSQL:
    databases:
    {{- range $k, $v := .Middleware.Databases }}
    - distributed: {{ $v.Distributed }}
      name: {{ $v.Name }}
      {{- if gt (len $v.Extensions) 0 }}
      extensions:
      {{- range $i, $ext := $v.Extensions }}
      - {{ $ext }}
      {{- end }}
      {{- end }}
      {{- if gt (len $v.Scripts) 0 }}
      scripts:
      {{- range $i, $s := $v.Scripts }}
      - '{{ $s }}'
      {{- end }}
      {{- end }}
      
    {{- end }}
    password:
     {{- if not (eq .Middleware.Password "") }}
      value: {{ .Middleware.Password }}
     {{- else }}
      valueFrom:
        secretKeyRef:
          name: {{ .AppName }}-{{ .Namespace }}-postgres-password
          key: "password"
	 {{- end }}
    user: {{ .Middleware.Username }}
`

const redisRequest = `apiVersion: apr.bytetrade.io/v1alpha1
kind: MiddlewareRequest
metadata:
  name: {{ .AppName }}-redis
  namespace: {{ .Namespace }}
spec:
  app: {{ .AppName }}
  appNamespace: {{ .AppNamespace }}
  middleware: redis
  redis:
    namespace: {{ .Middleware.Namespace }}
    password:
     {{- if not (eq .Middleware.Password "") }}
      value: {{ .Middleware.Password }}
     {{- else }}
      valueFrom:
        secretKeyRef:
          name: {{ .AppName }}-{{ .Namespace }}-redis-password
          key: "password"
	 {{- end }}
`

const mongodbRequest = `apiVersion: apr.bytetrade.io/v1alpha1
kind: MiddlewareRequest
metadata:
  name: {{ .AppName }}-mongodb
  namespace: {{ .Namespace }}
spec:
  app: {{ .AppName }}
  appNamespace: {{ .AppNamespace }}
  middleware: mongodb
  mongodb:
    databases:
    {{- range $k, $v := .Middleware.Databases }}
    - name: {{ $v.Name }}
	{{- if gt (len $v.Scripts) 0 }}
      scripts:
      {{- range $i, $s := $v.Scripts }}
      - '{{ $s }}'
      {{- end }}
    {{- end }}
    {{- end }}
    password:
     {{- if not (eq .Middleware.Password "") }}
      value: {{ .Middleware.Password }}
     {{- else }}
      valueFrom:
        secretKeyRef:
          name: {{ .AppName }}-{{ .Namespace }}-mongodb-password
          key: "password"
	 {{- end }}
    user: {{ .Middleware.Username }}
`
const natsRequest = `apiVersion: apr.bytetrade.io/v1alpha1
kind: MiddlewareRequest
metadata:
  name: {{ .AppName }}-nats
  namespace: {{ .Namespace }}
spec:
  app: {{ .AppName }}
  appNamespace: {{ .AppNamespace }}
  middleware: nats
  nats:
    user: {{ .Middleware.Username }}
    password:
     {{- if not (eq .Middleware.Password "") }}
      value: {{ .Middleware.Password }}
     {{- else }}
      valueFrom:
        secretKeyRef:
          name: {{ .AppName }}-{{ .Namespace }}-nats-password
          key: "password"
     {{- end }}
    {{- if gt (len .Middleware.Subjects) 0 }}
    subjects: 
      {{- range $k, $v := .Middleware.Subjects }}
      - name: {{ $v.Name }}
        permission:
          pub: {{ $v.Permission.Pub }}
          sub: {{ $v.Permission.Sub }}
        {{- if $v.Export }}
        export:
          appName: {{ $v.Export.AppName }}
          pub: {{ $v.Export.Pub }}
          sub: {{ $v.Export.Sub }}
        {{- end }}
      {{- end }}
    {{- end }}
    {{- if gt (len .Middleware.Refs) 0 }}
    refs: 
      {{- range $k, $v := .Middleware.Refs }}
      - appName: {{ $v.AppName }}
        subjects:
          {{- range $sk, $sv := $v.Subjects }}
          - name: {{ $sv.Name }}
            perm:
            {{- range $pk, $pv := $sv.Perm }}
            - {{ $pv }}
            {{- end }}
          {{- end }}
      {{- end }}
    {{- else }}
    refs: []
    {{- end }}
`

func GenMiddleRequest(middleware MiddlewareType, appName, appNamespace, namespace, username, password string,
	databases []Database, natsConfig *NatsConfig, ownerName string) ([]byte, error) {
	switch middleware {
	case TypePostgreSQL:
		return genPostgresRequest(appName, appNamespace, namespace, username, password, databases)
	case TypeRedis:
		return genRedisRequest(appName, appNamespace, namespace, password, databases)
	case TypeMongoDB:
		return genMongodbRequest(appName, appNamespace, namespace, username, password, databases)
	case TypeNats:
		return genNatsRequest(appName, appNamespace, namespace, username, password, natsConfig, ownerName)
	default:
		return []byte{}, nil
	}
}

func genPostgresRequest(appName, appNamespace, namespace, username, password string, databases []Database) ([]byte, error) {
	tpl, err := template.New("postgresRequest").Parse(postgresRequest)
	if err != nil {
		return []byte{}, err
	}
	var middlewareRequest bytes.Buffer
	data := struct {
		AppName      string
		AppNamespace string
		Namespace    string
		Middleware   *PostgresConfig
	}{
		AppName:      appName,
		AppNamespace: appNamespace,
		Namespace:    namespace,
		Middleware: &PostgresConfig{
			Username:  username,
			Password:  password,
			Databases: databases,
		},
	}
	err = tpl.Execute(&middlewareRequest, data)
	if err != nil {
		return []byte{}, err
	}
	return middlewareRequest.Bytes(), nil
}

func genRedisRequest(appName, appNamespace, namespace, password string, databases []Database) ([]byte, error) {
	tpl, err := template.New("redisRequest").Parse(redisRequest)
	if err != nil {
		return []byte{}, err
	}
	var middlewareRequest bytes.Buffer
	data := struct {
		AppName      string
		AppNamespace string
		Namespace    string
		Middleware   *RedisConfig
	}{
		AppName:      appName,
		AppNamespace: appNamespace,
		Namespace:    namespace,
		Middleware: &RedisConfig{
			Password:  password,
			Namespace: databases[0].Name,
		},
	}
	err = tpl.Execute(&middlewareRequest, data)
	if err != nil {
		return []byte{}, err
	}
	return middlewareRequest.Bytes(), nil
}

func genMongodbRequest(appName, appNamespace, namespace, username, password string, databases []Database) ([]byte, error) {
	tpl, err := template.New("mongodbRequest").Parse(mongodbRequest)
	if err != nil {
		return []byte{}, err
	}
	var middlewareRequest bytes.Buffer
	data := struct {
		AppName      string
		AppNamespace string
		Namespace    string
		Middleware   *MongodbConfig
	}{
		AppName:      appName,
		AppNamespace: appNamespace,
		Namespace:    namespace,
		Middleware: &MongodbConfig{
			Username:  username,
			Password:  password,
			Databases: databases,
		},
	}
	err = tpl.Execute(&middlewareRequest, data)
	if err != nil {
		return []byte{}, err
	}
	return middlewareRequest.Bytes(), nil
}

func genNatsRequest(appName, appNamespace, namespace, username, password string, natsConfig *NatsConfig, ownerName string) ([]byte, error) {
	tpl, err := template.New("natsRequest").Parse(natsRequest)
	if err != nil {
		return []byte{}, err
	}
	var middlewareRequest bytes.Buffer

	natsConfig.Username = username
	natsConfig.Password = password

	data := struct {
		AppName      string
		AppNamespace string
		Namespace    string
		Middleware   *NatsConfig
	}{
		AppName:      appName,
		AppNamespace: appNamespace,
		Namespace:    namespace,
		Middleware:   natsConfig,
	}

	err = tpl.Execute(&middlewareRequest, data)
	if err != nil {
		return []byte{}, err
	}
	return middlewareRequest.Bytes(), nil
}
