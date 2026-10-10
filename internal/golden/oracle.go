package golden

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// OracleImage is the container holding Fuzzball 7 built from the C
// sources.
const OracleImage = "localhost/fbmuck-oracle"

// oraclePort is the port the oracle listens on inside its container.
const oraclePort = 4201

// oracleLabel marks a container this harness started, so a sweep can
// recognise one of its own and nothing else.
const oracleLabel = "fbemerald-golden-oracle"

// Script is what to send a server once connected, one command per
// entry.
type Script []string

// The sentinel a transcript is bounded by. Each command is followed
// by a pose carrying a marker, so the harness knows when the
// command's output has finished rather than guessing with a timeout.
const (
	connectMarker = "EMERALDCONNECTED"
	doneMarker    = "EMERALDDONE"
)

// RunOracle drives the C server through a script and returns one
// transcript.
func RunOracle(ctx context.Context, fx *Fixture, script Script) (string, error) {
	steps, err := RunOracleSteps(ctx, fx, script, nil)
	return strings.Join(steps, ""), err
}

// RunOracleSteps is the same, returning each command's output
// separately so a failure names the case it belongs to.
func RunOracleSteps(ctx context.Context, fx *Fixture, script Script,
	pauses map[int]time.Duration) ([]string, error) {
	return withOracle(ctx, fx, func(conn net.Conn) ([]string, error) {
		return drive(conn, script, pauses)
	})
}

// RunOracleQuiet drives the C server without the marker poses,
// bounding each command's output by a period of silence instead.
//
// A marker cannot be used for a session that holds the input line:
// the MUF editor reads "!pose EMERALDDONE" as the editor command "x"
// — its last word begins with the cancel letter — and a program
// waiting on a READ eats the marker outright. Waiting for quiet is
// slower and is only worth it for those cases, so the marker path
// stays the default.
func RunOracleQuiet(ctx context.Context, fx *Fixture, script Script,
	quiet time.Duration) ([]string, error) {
	return withOracle(ctx, fx, func(conn net.Conn) ([]string, error) {
		return driveQuiet(conn, script, quiet)
	})
}

// withOracle starts a C server holding the fixture, runs fn against
// it, and tears it down.
func withOracle(ctx context.Context, fx *Fixture,
	fn func(net.Conn) ([]string, error)) ([]string, error) {

	var out []string
	one := func(conns []net.Conn) error {
		var err error
		out, err = fn(conns[0])
		return err
	}
	err := withOracleConns(ctx, fx, 1, one)
	return out, err
}

// withOracleConns is the same with n connections, which is what a
// two-seat case needs: the container is started once and dialled
// twice, since two servers would be two worlds.
func withOracleConns(ctx context.Context, fx *Fixture, n int,
	fn func([]net.Conn) error) error {
	// The C server writes back into its game directory — a
	// dump, and the macro table — so it runs against a copy.
	// Without this a case that defines a macro leaves it behind
	// for this server to import, and the two transcripts stop
	// being of the same world.
	dir, err := copyFixture(fx.Dir)
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	if err := os.MkdirAll(dir+"/logs", 0o755); err != nil {
		return err
	}

	port, err2 := freePort()
	if err2 != nil {
		return err2
	}
	name := fmt.Sprintf("fbgold-%d", port)

	// --rm so a crashed run leaves nothing behind; the container
	// is torn down explicitly as well, in case the server does
	// not exit on its own. Neither covers the process being
	// killed, which is what the label is for -- see
	// SweepStaleOracles.
	run := exec.CommandContext(ctx, "podman", "run", "--rm", "-d",
		"--name", name,
		"--label", oracleLabel+"=1",
		"-p", fmt.Sprintf("127.0.0.1:%d:%d", port, oraclePort),
		"-v", dir+":/game:z",
		OracleImage,
		"-dbin", "/game/data/test.db",
		"-dbout", "/game/data/out.db",
		"-gamedir", "/game",
		"-port", fmt.Sprint(oraclePort),
		"-nodetach",
	)
	if out, err := run.CombinedOutput(); err != nil {
		return fmt.Errorf("starting the oracle: %v: %s",
			err, out)
	}
	defer func() {
		// -t 0 because this is reached either after the
		// script's own @shutdown, when the container is
		// already gone and this is a no-op, or on an error
		// path where its dump is written into a temp
		// directory that no longer exists. Without it a
		// failing case waits out podman's ten-second stop
		// timeout, which a container's PID 1 reaches in full
		// unless it installs a SIGTERM handler.
		_ = exec.Command("podman", "rm", "-f", "-t", "0",
			name).Run()
	}()

	conns := make([]net.Conn, 0, n)
	defer func() {
		for _, c := range conns {
			_ = c.Close()
		}
	}()
	for len(conns) < n {
		conn, err := dialWithRetry(ctx,
			fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			logs, _ := exec.Command("podman", "logs",
				name).CombinedOutput()
			return fmt.Errorf("connecting to the "+
				"oracle: %w (logs: %s)", err, logs)
		}
		conns = append(conns, conn)
	}

	return fn(conns)
}

// dialWithRetry waits for the oracle to start listening.
func dialWithRetry(ctx context.Context, addr string) (net.Conn, error) {
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
		if err == nil {
			return conn, nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return nil, fmt.Errorf("the oracle never started listening on %s", addr)
}

// drive runs a script against a connected server, returning each
// command's output separately.
//
// Each command is followed by a marker pose, so the harness reads
// until the marker rather than waiting a fixed time. That keeps the
// transcript deterministic even when a command produces output
// slowly.
func drive(conn net.Conn, script Script, pauses map[int]time.Duration) ([]string, error) {
	br := bufio.NewReader(conn)
	out := make([]string, 0, len(script))

	send := func(line string) error {
		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		_, err := conn.Write([]byte(line + "\n"))
		return err
	}

	// readTo collects output until the marker appears, discarding
	// the marker line itself.
	readTo := func(marker string) (string, error) {
		var got strings.Builder
		deadline := time.Now().Add(30 * time.Second)
		for {
			_ = conn.SetReadDeadline(deadline)
			line, err := br.ReadString('\n')
			if strings.Contains(line, marker) {
				return got.String(), nil
			}
			got.WriteString(line)
			if err != nil {
				return got.String(), err
			}
			if time.Now().After(deadline) {
				return got.String(), fmt.Errorf("timed out waiting for %s", marker)
			}
		}
	}

	if err := send("connect One " + godPassword); err != nil {
		return nil, err
	}
	if err := send("!pose " + connectMarker); err != nil {
		return nil, err
	}
	// The welcome banner and login output are not part of what is
	// compared.
	if _, err := readTo(connectMarker); err != nil {
		return nil, fmt.Errorf("logging in: %w", err)
	}

	for i, cmd := range script {
		if err := send(cmd); err != nil {
			return out, err
		}
		// A program that suspends itself needs time to resume
		// before the marker is sent, or its later output
		// lands in the next step's transcript.
		if d, ok := pauses[i]; ok {
			time.Sleep(d)
		}
		if err := send("!pose " + doneMarker); err != nil {
			return out, err
		}
		got, err := readTo(doneMarker)
		out = append(out, got)
		if err != nil {
			return out, err
		}
	}

	_ = send("@shutdown")
	return out, nil
}

// freePort asks the kernel for a port nothing is using.
func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// OracleAvailable reports whether the oracle image has been built.
func OracleAvailable() bool {
	return exec.Command("podman", "image", "exists", OracleImage).Run() == nil
}

// SweepStaleOracles removes oracle containers an earlier run left
// behind, and reports how many there were.
//
// withOracle's teardown is a defer and "--rm" fires only when the
// container exits, so neither covers the test binary being *killed*
// rather than finishing: the fbmuck inside keeps running, holding its
// published port and its memory for as long as the machine is up. One
// sat here for 39 hours after the editor driving it was killed, and
// nothing in the suite would ever have mentioned it.
//
// The filter is the label rather than the "fbgold-" name, so this can
// only remove a container this harness started. It runs once from
// TestMain, before any case asks for a container -- which is correct
// while the cases share one process, and would need rethinking only
// for two concurrent "go test" runs of this package.
func SweepStaleOracles() int {
	out, err := exec.Command("podman", "ps", "-aq",
		"--filter", "label="+oracleLabel+"=1").Output()
	if err != nil {
		return 0
	}
	ids := strings.Fields(string(out))
	if len(ids) == 0 {
		return 0
	}
	// -t 0: measured at 10.1s against 0.07s, because podman sends
	// SIGTERM and a container's PID 1 ignores it, so the stop
	// timeout runs out before SIGKILL. An abandoned oracle has
	// nothing worth flushing and its game directory is long gone.
	args := append([]string{"rm", "-f", "-t", "0"}, ids...)
	_ = exec.Command("podman", args...).Run()
	return len(ids)
}

// driveQuiet runs a script with no markers, taking a pause in the
// output as the end of a command's response.
func driveQuiet(conn net.Conn, script Script, quiet time.Duration) ([]string, error) {
	br := bufio.NewReader(conn)
	out := make([]string, 0, len(script))

	read := func(d time.Duration) string {
		var b strings.Builder
		for {
			_ = conn.SetReadDeadline(time.Now().Add(d))
			line, err := br.ReadString('\n')
			b.WriteString(line)
			if err != nil {
				return b.String()
			}
		}
	}
	send := func(line string) error {
		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		_, err := conn.Write([]byte(line + "\n"))
		return err
	}

	read(3 * time.Second) // the welcome banner
	if err := send("connect One " + godPassword); err != nil {
		return nil, err
	}
	read(quiet) // login output is not part of what is compared

	for _, cmd := range script {
		if err := send(cmd); err != nil {
			return out, err
		}
		out = append(out, read(quiet))
	}
	_ = send("@shutdown")
	return out, nil
}

// copyFixture duplicates a fixture directory so the C server can
// write into it without changing the original.
func copyFixture(src string) (string, error) {
	dst, err := os.MkdirTemp("", "fbgold-fixture-")
	if err != nil {
		return "", err
	}
	err = filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
	if err != nil {
		os.RemoveAll(dst)
		return "", err
	}
	return dst, nil
}
