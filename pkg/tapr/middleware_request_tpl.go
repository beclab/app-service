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

const zincRequest = `apiVersion: apr.bytetrade.io/v1alpha1
kind: MiddlewareRequest
metadata:
  name: {{ .AppName }}-zinc
  namespace: {{ .Namespace }}
spec:
  app: {{ .AppName }}
  appNamespace: {{ .AppNamespace }}
  middleware: zinc
  zinc:
    user: {{ .Middleware.Username }}
    password:
     {{- if not (eq .Middleware.Password "") }}
      value: {{ .Middleware.Password }}
     {{- else }}
      valueFrom:
        secretKeyRef:
          name: {{ .AppName }}-{{ .Namespace }}-zinc-password
          key: "password"
	 {{- end }}
    indexes:
    {{- range $k, $v := .Middleware.Indexes }}
    - name: {{ $v.Name }}
      namespace: {{ $.Namespace }}
      key: mappings
    {{-  end }}
`

func GenMiddleRequest(middleware MiddlewareType, appName, appNamespace, namespace, username, password string,
	databases []Database, indexes []Index) ([]byte, error) {
	switch middleware {
	case TypePostgreSQL:
		return genPostgresRequest(appName, appNamespace, namespace, username, password, databases)
	case TypeRedis:
		return genRedisRequest(appName, appNamespace, namespace, password, databases)
	case TypeMongoDB:
		return genMongodbRequest(appName, appNamespace, namespace, username, password, databases)
	case TypeZincSearch:
		return genZincRequest(appName, appNamespace, namespace, username, password, indexes)
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

func genZincRequest(appName, appNamespace, namespace, username, password string, indexes []Index) ([]byte, error) {
	tpl, err := template.New("ZincRequest").Parse(zincRequest)
	if err != nil {
		return []byte{}, err
	}
	var middlewareRequest bytes.Buffer

	data := struct {
		AppName      string
		AppNamespace string
		Namespace    string
		Middleware   *ZincSearchConfig
	}{
		AppName:      appName,
		AppNamespace: appNamespace,
		Namespace:    namespace,
		Middleware: &ZincSearchConfig{
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
