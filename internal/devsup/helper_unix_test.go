//go:build linux || darwin

package devsup

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// The test binary doubles as the dev server the tests supervise: with
// SUPERVISOR_TEST_HELPER set, TestMain runs the helper instead of the tests.
//
//	listen       accept on $PORT and fork a grandchild (`sleep`) into the same group
//	ignore-term  the same, but ignore SIGTERM so only SIGKILL ends it
//	echo         accept on $PORT and echo every connection's bytes back
//	exit         print "line 1".."line 30" and exit with $CODE
//	env          print "env NAME=value" (or "env NAME unset") for each NAME in
//	             $PRINT_ENV, then listen as `listen` does
func TestMain(m *testing.M) {
	switch os.Getenv("SUPERVISOR_TEST_HELPER") {
	case "":
		os.Exit(m.Run())
	case "listen":
		os.Exit(helperListen(false))
	case "ignore-term":
		os.Exit(helperListen(true))
	case "echo":
		os.Exit(helperEcho())
	case "env":
		for name := range strings.SplitSeq(os.Getenv("PRINT_ENV"), ",") {
			if v, ok := os.LookupEnv(name); ok {
				fmt.Printf("env %s=%s\n", name, v)
			} else {
				fmt.Printf("env %s unset\n", name)
			}
		}
		os.Exit(helperListen(false))
	case "exit":
		for i := 1; i <= 30; i++ {
			fmt.Printf("line %d\n", i)
		}
		code, _ := strconv.Atoi(os.Getenv("CODE"))
		os.Exit(code)
	}
	os.Exit(2)
}

func helperListen(ignoreTerm bool) int {
	if ignoreTerm {
		signal.Ignore(syscall.SIGTERM)
	}
	var lc net.ListenConfig
	l, err := lc.Listen(context.Background(), "tcp", ":"+os.Getenv("PORT"))
	if err != nil {
		fmt.Println("listen:", err)
		return 3
	}
	defer l.Close()
	grandchild := exec.CommandContext(context.Background(), "sleep", "300")
	if err := grandchild.Start(); err != nil {
		return 4
	}
	fmt.Printf("listening pid=%d grandchild=%d\n", os.Getpid(), grandchild.Process.Pid)
	for {
		c, err := l.Accept()
		if err != nil {
			return 0
		}
		_ = c.Close()
	}
}

func helperEcho() int {
	var lc net.ListenConfig
	l, err := lc.Listen(context.Background(), "tcp", ":"+os.Getenv("PORT"))
	if err != nil {
		fmt.Println("listen:", err)
		return 3
	}
	defer l.Close()
	for {
		c, err := l.Accept()
		if err != nil {
			return 0
		}
		go func() {
			defer c.Close()
			_, _ = io.Copy(c, c)
		}()
	}
}

// helperCmd is the shell command that runs this test binary as a helper.
func helperCmd(t *testing.T, mode string, env ...string) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return "SUPERVISOR_TEST_HELPER=" + mode + " " + strings.Join(env, " ") + " exec " + shellQuote(exe)
}

// testPorts hands out ports below every kernel's ephemeral range, so a port
// another test's dial or listener just released is never handed out again
// while a parallel test still expects it to be its own.
var testPorts = struct {
	sync.Mutex
	next int
}{next: 20000 + os.Getpid()%6000}

// freePort returns a port nothing listens on.
func freePort(t *testing.T) int {
	t.Helper()
	testPorts.Lock()
	defer testPorts.Unlock()
	for range 2000 {
		port := testPorts.next
		testPorts.next++
		if testPorts.next >= 32000 {
			testPorts.next = 20000
		}
		if portFree(port) {
			return port
		}
	}
	t.Fatal("no free test port in 20000-31999")
	return 0
}

// released waits for a stopped group's port to be free. Under parallel fork
// load darwin was seen holding it for a moment after the whole group was
// reaped; a start waits for its port for the same reason (waitPortFree).
func released(t *testing.T, port int) {
	t.Helper()
	eventually(t, "port "+strconv.Itoa(port)+" to be released", func() bool { return portFree(port) })
}

// eventually polls cond until it holds or 10 s pass.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func listening(port int) bool {
	return portAccepts(port)
}

// processAlive reports whether pid exists and is not a zombie we already reaped.
func processAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}
