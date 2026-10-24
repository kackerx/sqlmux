//go:build !windows

package db

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func dialSSH(t *testing.T) net.Conn {
	t.Helper()
	c, err := SSHDialer("me@jump").Dial(context.Background(), "tcp", "db.internal:5432")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// A dial starts `ssh -W addr dest` with no prompt and no forward that fails
// quietly, and what goes in comes out of its stdout: the connection (F15.2).
func TestSSHDialerEcho(t *testing.T) {
	args := FakeSSH(t, "exec cat")
	c := dialSSH(t)
	if a := c.RemoteAddr(); a.Network() != "tcp" || a.String() != "db.internal:5432" { // pgx dials a cancel request to it again
		t.Errorf("remote address %s %s", a.Network(), a)
	}
	big := bytes.Repeat([]byte("0123456789abcdef"), 1<<16) // 1 MB: more than a pipe holds
	go func() {
		if _, err := c.Write(big); err != nil {
			t.Error(err)
		}
	}()
	got := make([]byte, len(big))
	if _, err := io.ReadFull(c, got); err != nil || !bytes.Equal(got, big) {
		t.Fatalf("echo: %v", err)
	}
	if b, _ := os.ReadFile(args); string(b) != "-W db.internal:5432 -o BatchMode=yes -o ExitOnForwardFailure=yes me@jump\n" {
		t.Errorf("ssh was run as %q", b)
	}
}

// What ssh said when it failed is the error, and a clean end of it is EOF:
// both on the read that finds the connection gone, and on the write after.
func TestSSHDialerFailure(t *testing.T) {
	FakeSSH(t, "echo 'me@jump: Permission denied (publickey).' >&2; exit 255")
	c := dialSSH(t)
	_, err := c.Read(make([]byte, 1))
	if err == nil || !strings.Contains(err.Error(), "ssh me@jump: me@jump: Permission denied (publickey).") {
		t.Fatalf("read: %v", err)
	} else if !NetworkError(err) { // the tunnel is gone: a request on it is to be dialed again (Broken)
		t.Errorf("not a network error: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if _, err := c.Write([]byte("x")); err == nil || !strings.Contains(err.Error(), "Permission denied") {
		t.Fatalf("write: %v", err)
	}
	FakeSSH(t, "head -n1 >/dev/null")
	c = dialSSH(t)
	_, _ = c.Write([]byte("bye\n"))
	if _, err := c.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("a clean end: %v", err)
	}
	FakeSSH(t, "exit 3") // silent: the status is all there is
	c = dialSSH(t)
	if _, err := c.Read(make([]byte, 1)); err == nil || !strings.Contains(err.Error(), "exit status 3") {
		t.Fatalf("no stderr: %v", err)
	} else if !NetworkError(err) {
		t.Errorf("not a network error: %v", err)
	}
}

// A child ssh left holding its stderr (a ProxyCommand) does not keep the
// error waiting: it is read when the process is gone, the child not waited for.
func TestSSHDialerStderrHeldOpen(t *testing.T) {
	FakeSSH(t, "echo 'proxy failed' >&2; sleep 20 >&2 & exit 255")
	c := dialSSH(t)
	start := time.Now()
	_, err := c.Read(make([]byte, 1))
	if err == nil || !strings.Contains(err.Error(), "proxy failed") || time.Since(start) > 2*time.Second {
		t.Fatalf("%v after %v", err, time.Since(start))
	}
}

// Failed is why the last connection ended, when it ended with an error.
func TestSSHDialerFailed(t *testing.T) {
	d := SSHDialer("me@jump")
	if d.Failed() != nil {
		t.Fatal("nothing dialed yet")
	}
	FakeSSH(t, "echo 'no route' >&2; exit 255")
	c, err := d.Dial(context.Background(), "tcp", "h:1")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := d.Failed(); err == nil || !strings.Contains(err.Error(), "ssh me@jump: no route") {
		t.Fatalf("failed: %v", err)
	}
	FakeSSH(t, "exec cat") // a connection that is up has not failed; neither has one that ended well
	c2, _ := d.Dial(context.Background(), "tcp", "h:1")
	defer c2.Close()
	_, _ = c2.Write([]byte("x"))
	if err := d.Failed(); err != nil {
		t.Errorf("up: %v", err)
	}
	FakeSSH(t, "exit 0")
	c3, _ := d.Dial(context.Background(), "tcp", "h:1")
	defer c3.Close()
	if err := d.Failed(); err != nil {
		t.Errorf("ended well: %v", err)
	}
}

// With no ssh to run the dial says so, naming the destination.
func TestSSHDialerMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := SSHDialer("me@jump").Dial(context.Background(), "tcp", "h:1")
	if err == nil || !strings.Contains(err.Error(), "ssh me@jump") {
		t.Fatalf("%v", err)
	}
}

// Close ends the process; a read's deadline is the connection's (pgx sets one
// for its timeouts and its cancels).
func TestSSHConnCloseAndDeadline(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	FakeSSH(t, "echo $$ >"+pidFile+"; exec sleep 30")
	c := dialSSH(t)
	ne := net.Error(nil)
	for _, set := range []func(time.Time) error{c.SetDeadline, c.SetReadDeadline} {
		_ = c.SetDeadline(time.Time{}) // the one before has run out
		_ = set(time.Now().Add(50 * time.Millisecond))
		start := time.Now()
		if _, err := c.Read(make([]byte, 1)); !errors.As(err, &ne) || !ne.Timeout() || time.Since(start) > 2*time.Second {
			t.Fatalf("deadline: %v after %v", err, time.Since(start))
		}
	}
	_ = c.SetDeadline(time.Time{})
	var pid int
	for i := 0; i < 100 && pid == 0; i++ { // the script writes it as it starts
		b, _ := os.ReadFile(pidFile)
		if pid, _ = strconv.Atoi(strings.TrimSpace(string(b))); pid == 0 {
			time.Sleep(20 * time.Millisecond)
		}
	}
	if pid == 0 {
		t.Fatal("no pid")
	}
	start := time.Now()
	c.Close()
	if time.Since(start) > 3*time.Second {
		t.Errorf("Close took %v", time.Since(start))
	}
	if err := syscall.Kill(pid, 0); err == nil {
		t.Errorf("process %d is still there", pid)
	}
}

// SetDeadline is the write's too: a server that reads nothing does not hold a
// write for ever.
func TestSSHConnWriteDeadline(t *testing.T) {
	FakeSSH(t, "exec sleep 30")
	c := dialSSH(t)
	ne := net.Error(nil)
	for _, set := range []func(time.Time) error{c.SetDeadline, c.SetWriteDeadline} {
		_ = c.SetDeadline(time.Time{})
		_ = set(time.Now().Add(100 * time.Millisecond))
		start := time.Now()
		if _, err := c.Write(bytes.Repeat([]byte("x"), 1<<20)); !errors.As(err, &ne) || !ne.Timeout() || time.Since(start) > 2*time.Second {
			t.Fatalf("%v after %v", err, time.Since(start))
		}
	}
}

// The dialer is what a context carries.
func TestDialerInContext(t *testing.T) {
	if _, ok := DialerOf(context.Background()); ok {
		t.Fatal("none")
	}
	d := Dialer{Name: "x"}
	if got, ok := DialerOf(WithDialer(context.Background(), d)); !ok || got.Name != "x" {
		t.Fatalf("%+v %v", got, ok)
	}
}

// What ssh said is kept to its last 4 KB: a long complaint is the error with its
// end, which is the part that says why.
func TestSSHDialerFailureTail(t *testing.T) {
	FakeSSH(t, `i=0; while [ $i -lt 300 ]; do echo "line $i of what ssh has to say about it"; i=$((i+1)); done >&2; echo "the last line" >&2; exit 255`)
	c := dialSSH(t)
	_, err := c.Read(make([]byte, 1))
	if err == nil || !strings.HasSuffix(err.Error(), "the last line") || strings.Contains(err.Error(), "line 0 ") || len(err.Error()) > 4200 {
		t.Fatalf("%d bytes, ending %q", len(fmt.Sprint(err)), fmt.Sprint(err)[max(len(fmt.Sprint(err))-40, 0):])
	}
}
