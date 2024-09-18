package python3_test

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/mjarkk/gofast-fasthttp/example/python3"
)

func examplePath() string {
	basePath, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	return filepath.Join(basePath, "webapp.py")
}

func testEnv() error {
	// a short script to test python and flup installation
	script := `
import sys
try:
  from flup.server.fcgi import WSGIServer
except ImportError as err:
  print(err)
  sys.exit(1)
`
	cmd := exec.Command("python", "-c", script)
	res, err := cmd.CombinedOutput()
	if err == nil {
		return err
	}
	return fmt.Errorf("%s", res)
}

func waitConn(socket string) <-chan net.Conn {
	chanConn := make(chan net.Conn)
	go func() {
		log.Printf("wait for socket: %s", socket)
		for {
			if conn, err := net.Dial("unix", socket); err != nil {
				time.Sleep(time.Millisecond * 2)
			} else {
				chanConn <- conn
				break
			}
		}
	}()
	return chanConn
}

func TestHandler(t *testing.T) {

	if err := testEnv(); err != nil {
		if os.Getenv("CI") != "" {
			t.Errorf("environment setup error: %s", err)
			return
		}
		t.Skipf("skip test: %s", err)
		return
	}

	webapp := examplePath()
	socket := filepath.Join(filepath.Dir(webapp), "test.sock")

	// define webapp.py command
	cmd := exec.Command(webapp)
	cmd.Env = append(os.Environ(), "TEST_PY3FCGI_SOCK="+socket)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// start the command and wait for its exit
	done := make(chan error, 1)
	go func() {
		if err := cmd.Start(); err != nil {
			done <- err
			return
		}
		// wait if the command started successfully
		log.Printf("started successfully")
		log.Printf("process=%#v", cmd.Process)
		done <- cmd.Wait()
		log.Printf("wait ended")
	}()

	// wait until socket ready
	conn := <-waitConn(socket)
	conn.Close()
	log.Printf("socket ready")

	// start the proxy handler
	h := python3.NewHandler(webapp, "unix", socket)

	//cmd.Wait()
	get := func(path string) (w *httptest.ResponseRecorder, err error) {
		r, err := http.NewRequest("GET", path, nil)
		if err != nil {
			return
		}
		w = httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return
	}

	testDone := make(chan bool)
	go func() {
		w, err := get("/")
		if err != nil {
			t.Errorf("unexpected error %v", err)
			testDone <- false
			return
		}
		if want, have := "hello index", w.Body.String(); want != have {
			t.Errorf("expected %#v, got %#v", want, have)
			testDone <- false
			return
		}
		testDone <- true
	}()

	select {
	case testSuccess := <-testDone:
		if !testSuccess {
			log.Printf("test failed")
		}
	case <-time.After(3 * time.Second):
		log.Printf("test timeout")
	case err := <-done:
		if err != nil {
			log.Printf("process done with error = %v", err)
		} else {
			log.Print("process done gracefully without error")
		}
	}

	log.Printf("send SIGTERM")
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		log.Fatal("failed to kill: ", err)
	}
	log.Println("process killed")
}
