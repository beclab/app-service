package tapr

import (
	"bytes"
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
	databases := []Database{{Name: "mydb", Distributed: true,
		Extensions: []string{"vectors", "postgis"}, Scripts: []string{"BEGIN", "COMMIT"}}}

	result, err := GenMiddleRequest(middleware, appName, appNamespace, namespace, username, password, databases, []Index{})
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
	result, err = GenMiddleRequest(middleware, appName, appNamespace, namespace, username, password, databases, []Index{})
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
	result, err = GenMiddleRequest(middleware, appName, appNamespace, namespace, username, password, databases, []Index{})
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

	// test genZincRequest function
	middleware = "zinc"
	_, err = GenMiddleRequest(middleware, appName, appNamespace, namespace, username, password, databases, []Index{{Name: "myindex"}})
	if err != nil {
		t.Errorf("GenMiddleRequest returned an error: %v", err)
	}
}
