package tapr

import (
	"bytes"
	"bytetrade.io/web3os/app-service/pkg/appcfg"
	"testing"
)

func TestGenMiddleRequest(t *testing.T) {
	var (
		middleware   MiddlewareType = "postgres"
		appName                     = "myapp"
		appNamespace                = "mynamespace"
		namespace                   = "user-system-hysyeah"
		username                    = "myuser"
		password                    = "mypassword"
	)
	// test genPostgresRequest function
	databases := []appcfg.Database{{Name: "mydb", Distributed: true,
		Extensions: []string{"vectors", "postgis"}, Scripts: []string{"BEGIN", "COMMIT"}}}

	result, err := GenMiddleRequest(middleware, appName, appNamespace, namespace, username, password, databases, nil, "")
	if err != nil {
		t.Errorf("GenMiddleRequest returned an error: %v", err)
	}
	expected := []byte(`apiVersion: apr.bytetrade.io/v1alpha1
kind: MiddlewareRequest
metadata:
  name: myapp-postgres
  namespace: user-system-hysyeah
spec:
  app: myapp
  appNamespace: mynamespace
  middleware: postgres
  postgreSQL:
    databases:
    - distributed: true
      name: mydb
      extensions:
      - vectors
      - postgis
      scripts:
      - BEGIN
      - COMMIT
    password:
      value: mypassword
    user: myuser
`)
	if !bytes.Equal(result, expected) {
		t.Errorf("GenMiddleRequest<postgres> returned incorrect result.\nExpected:\n%s\nActual:\n%s", expected, result)
	}

	// test genRedisRequest function
	middleware = "redis"
	result, err = GenMiddleRequest(middleware, appName, appNamespace, namespace, username, password, databases, nil, "")
	if err != nil {
		t.Errorf("GenMiddleRequest returned an error: %v", err)
	}
	expected = []byte(`apiVersion: apr.bytetrade.io/v1alpha1
kind: MiddlewareRequest
metadata:
  name: myapp-redis
  namespace: user-system-hysyeah
spec:
  app: myapp
  appNamespace: mynamespace
  middleware: redis
  redis:
    namespace: mydb
    password:
      value: mypassword
`)
	if !bytes.Equal(result, expected) {
		t.Errorf("GenMiddleRequest<redis> returned incorrect result.\nExpected:\n%s\nActual:\n%s", expected, result)
	}

	// test genMongodbRequest function
	middleware = "mongodb"
	result, err = GenMiddleRequest(middleware, appName, appNamespace, namespace, username, password, databases, nil, "")
	if err != nil {
		t.Errorf("GenMiddleRequest returned an error: %v", err)
	}
	expected = []byte(`apiVersion: apr.bytetrade.io/v1alpha1
kind: MiddlewareRequest
metadata:
  name: myapp-mongodb
  namespace: user-system-hysyeah
spec:
  app: myapp
  appNamespace: mynamespace
  middleware: mongodb
  mongodb:
    databases:
    - name: mydb
      scripts:
      - BEGIN
      - COMMIT
    password:
      value: mypassword
    user: myuser
`)
	if !bytes.Equal(result, expected) {
		t.Errorf("GenMiddleRequest<mongodb> returned incorrect result.\nExpected:\n%s\nActual:\n%s", expected, result)
	}

	// test genNatsRequest function
	middleware = "nats"
	result, err = GenMiddleRequest(middleware, appName, appNamespace, namespace, username, password, databases, &appcfg.NatsConfig{
		Username: "user1",
		Subjects: []appcfg.Subject{{
			Name: "subject1",
			Permission: appcfg.PermissionCfg{
				Pub: "allow",
				Sub: "allow",
			},
			Export: []appcfg.PermissionCfg{
				{
					AppName: "gitlab",
					Pub:     "allow",
					Sub:     "allow",
				},
				{
					AppName: "gitlab2",
					Pub:     "allow",
					Sub:     "allow",
				},
			},
		}},
		Refs: []appcfg.Ref{
			{
				AppName: "myapp",
				Subjects: []appcfg.RefSubject{
					{
						Name: "refsubject2",
						Perm: []string{"pub", "sub"},
					},
				},
			},
		},
	}, "busty")
	if err != nil {
		t.Errorf("GenMiddleRequest returned an error: %v", err)
	}
	expected = []byte(`apiVersion: apr.bytetrade.io/v1alpha1
kind: MiddlewareRequest
metadata:
  name: myapp-nats
  namespace: user-system-hysyeah
spec:
  app: myapp
  appNamespace: mynamespace
  middleware: nats
  nats:
    user: myuser
    subjects:
      - name: terminus.mynamespace.myapp.subject1
        permission:
          pub: allow
          sub: allow
        export:
          appName: gitlab
          pub: allow
          sub: allow
    refs:
      - appName: myapp
        subjects:
          - name: terminus.myapp-busty.myapp.refsubject2
            perm:
            - pub
            - sub
`)
	if !bytes.Equal(result, expected) {
		t.Errorf("GenMiddleRequest<nats> returned incorrect result.\nExpected:\n%s\nActual:\n%s", expected, result)
	}

}
