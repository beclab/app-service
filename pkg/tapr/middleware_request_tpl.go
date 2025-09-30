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

const mariadbRequest = `apiVersion: apr.bytetrade.io/v1alpha1
kind: MiddlewareRequest
metadata:
  name: {{ .AppName }}-mariadb
  namespace: {{ .Namespace }}
spec:
  app: {{ .AppName }}
  appNamespace: {{ .AppNamespace }}
  middleware: mariadb
  mariadb:
    databases:
    {{- range $k, $v := .Middleware.Databases }}
    - name: {{ $v.Name }}
    {{- end }}
    password:
     {{- if not (eq .Middleware.Password "") }}
      value: {{ .Middleware.Password }}
     {{- else }}
      valueFrom:
        secretKeyRef:
          name: {{ .AppName }}-{{ .Namespace }}-mariadb-password
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
        {{- if gt (len $v.Export) 0 }}
        export:
          {{- range $ek, $ev := $v.Export }}
        - appName: {{ $ev.AppName }}
          pub: {{ $ev.Pub }}
          sub: {{ $ev.Sub }}
          {{- end }}
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

const minioRequest = `apiVersion: apr.bytetrade.io/v1alpha1
kind: MiddlewareRequest
metadata:
  name: {{ .AppName }}-minio
  namespace: {{ .Namespace }}
spec:
  app: {{ .AppName }}
  appNamespace: {{ .AppNamespace }}
  middleware: minio
  minio:
    buckets:
    {{- range $k, $v := .Middleware.Buckets }}
    - name: {{ $v.Name }}
    {{- end }}
    password:
     {{- if not (eq .Middleware.Password "") }}
      value: {{ .Middleware.Password }}
     {{- else }}
      valueFrom:
        secretKeyRef:
          name: {{ .AppName }}-{{ .Namespace }}-minio-password
          key: "password"
	 {{- end }}
    user: {{ .Middleware.Username }}
`

const rabbitmqRequest = `apiVersion: apr.bytetrade.io/v1alpha1
kind: MiddlewareRequest
metadata:
  name: {{ .AppName }}-rabbitmq
  namespace: {{ .Namespace }}
spec:
  app: {{ .AppName }}
  appNamespace: {{ .AppNamespace }}
  middleware: rabbitmq
  rabbitmq:
    vhosts:
    {{- range $k, $v := .Middleware.VHosts }}
    - name: {{ $v.Name }}
    {{- end }}
    password:
     {{- if not (eq .Middleware.Password "") }}
      value: {{ .Middleware.Password }}
     {{- else }}
      valueFrom:
        secretKeyRef:
          name: {{ .AppName }}-{{ .Namespace }}-rabbitmq-password
          key: "password"
     {{- end }}
    user: {{ .Middleware.Username }}
`

const elasticsearchRequest = `apiVersion: apr.bytetrade.io/v1alpha1
kind: MiddlewareRequest
metadata:
  name: {{ .AppName }}-elasticsearch
  namespace: {{ .Namespace }}
spec:
  app: {{ .AppName }}
  appNamespace: {{ .AppNamespace }}
  middleware: elasticsearch
  elasticsearch:
    indexes:
    {{- range $k, $v := .Middleware.Indexes }}
    - name: {{ $v.Name }}
    {{- end }}
    password:
     {{- if not (eq .Middleware.Password "") }}
      value: {{ .Middleware.Password }}
     {{- else }}
      valueFrom:
        secretKeyRef:
          name: {{ .AppName }}-{{ .Namespace }}-elasticsearch-password
          key: "password"
     {{- end }}
    user: {{ .Middleware.Username }}
`

func GenMiddleRequest(middleware MiddlewareType, appName, appNamespace, namespace, username, password string,
	databases []Database, natsConfig *NatsConfig, ownerName string, buckets []Bucket, vhosts []VHost, indexes []Index) ([]byte, error) {
	switch middleware {
	case TypePostgreSQL:
		return genPostgresRequest(appName, appNamespace, namespace, username, password, databases)
	case TypeRedis:
		return genRedisRequest(appName, appNamespace, namespace, password, databases)
	case TypeMongoDB:
		return genMongodbRequest(appName, appNamespace, namespace, username, password, databases)
	case TypeNats:
		return genNatsRequest(appName, appNamespace, namespace, username, password, natsConfig, ownerName)
	case TypeMinio:
		return genMinioRequest(appName, appNamespace, namespace, username, password, buckets)
	case TypeRabbitMQ:
		return genRabbitMQRequest(appName, appNamespace, namespace, username, password, vhosts)
	case TypeElasticsearch:
		return genElasticsearchRequest(appName, appNamespace, namespace, username, password, indexes)
	case TypeMariaDB:
		return genMariadbRequest(appName, appNamespace, namespace, username, password, databases)
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

func genMariadbRequest(appName, appNamespace, namespace, username, password string, databases []Database) ([]byte, error) {
	tpl, err := template.New("mariadbRequest").Parse(mariadbRequest)
	if err != nil {
		return []byte{}, err
	}
	var middlewareRequest bytes.Buffer
	data := struct {
		AppName      string
		AppNamespace string
		Namespace    string
		Middleware   *MariaDBConfig
	}{
		AppName:      appName,
		AppNamespace: appNamespace,
		Namespace:    namespace,
		Middleware: &MariaDBConfig{
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

func genMinioRequest(appName, appNamespace, namespace, username, password string, buckets []Bucket) ([]byte, error) {
	tpl, err := template.New("minioRequest").Parse(minioRequest)
	if err != nil {
		return []byte{}, err
	}
	var middlewareRequest bytes.Buffer
	data := struct {
		AppName      string
		AppNamespace string
		Namespace    string
		Middleware   *MinioConfig
	}{
		AppName:      appName,
		AppNamespace: appNamespace,
		Namespace:    namespace,
		Middleware: &MinioConfig{
			Username: username,
			Password: password,
			Buckets:  buckets,
		},
	}
	err = tpl.Execute(&middlewareRequest, data)
	if err != nil {
		return []byte{}, err
	}
	return middlewareRequest.Bytes(), nil
}

func genRabbitMQRequest(appName, appNamespace, namespace, username, password string, vhosts []VHost) ([]byte, error) {
	tpl, err := template.New("rabbitmqRequest").Parse(rabbitmqRequest)
	if err != nil {
		return []byte{}, err
	}
	var middlewareRequest bytes.Buffer
	data := struct {
		AppName      string
		AppNamespace string
		Namespace    string
		Middleware   *RabbitMQConfig
	}{
		AppName:      appName,
		AppNamespace: appNamespace,
		Namespace:    namespace,
		Middleware: &RabbitMQConfig{
			Username: username,
			Password: password,
			VHosts:   vhosts,
		},
	}
	err = tpl.Execute(&middlewareRequest, data)
	if err != nil {
		return []byte{}, err
	}
	return middlewareRequest.Bytes(), nil
}

func genElasticsearchRequest(appName, appNamespace, namespace, username, password string, indexes []Index) ([]byte, error) {
	tpl, err := template.New("elasticsearchRequest").Parse(elasticsearchRequest)
	if err != nil {
		return []byte{}, err
	}
	var middlewareRequest bytes.Buffer
	data := struct {
		AppName      string
		AppNamespace string
		Namespace    string
		Middleware   *ElasticsearchConfig
	}{
		AppName:      appName,
		AppNamespace: appNamespace,
		Namespace:    namespace,
		Middleware: &ElasticsearchConfig{
			Username: username,
			Password: password,
			Indexes:  indexes,
		},
	}
	err = tpl.Execute(&middlewareRequest, data)
	if err != nil {
		return []byte{}, err
	}
	return middlewareRequest.Bytes(), nil
}
