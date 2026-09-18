package golden

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"
)

// OracleImage is the container holding Fuzzball 7 built from the C sources.
const OracleImage = "localhost/fbmuck-oracle"

// oraclePort is the port the oracle listens on inside its container.
const oraclePort = 4201

// Script is what to send a server once connected, one command per entry.
type Script []string

// The sentinel a transcript is bounded by. Each command is followed by a pose
// carrying a marker, so the harness knows when the command's output has
// finished rather than guessing with a timeout.
const (
	connectMarker = "EMERALDCONNECTED"
	doneMarker    = "EMERALDDONE"
)

// RunOracle drives the C server through a script and returns one transcript.
func RunOracle(ctx context.Context, fx *Fixture, script Script) (string, error) {
	steps, err := RunOracleSteps(ctx, fx, script, nil)
	return strings.Join(steps, ""), err
}

// RunOracleSteps is the same, returning each command's output separately so a
// failure names the case it belongs to.
func RunOracleSteps(ctx context.Context, fx *Fixture, script Script,
	pauses map[int]time.Duration) ([]string, error) {
	if err := os.MkdirAll(fx.Dir+"/logs", 0o755); err != nil {
		return nil, err
	}

	port, err := freePort()
	if err != nil {
		return nil, err
	}
	name := fmt.Sprintf("fbgold-%d", port)

	// --rm so a crashed run leaves nothing behind; the container is torn
	// down explicitly as well, in case the server does not exit on its own.
	run := exec.CommandContext(ctx, "podman", "run", "--rm", "-d",
		"--name", name,
		"-p", fmt.Sprintf("127.0.0.1:%d:%d", port, oraclePort),
		"-v", fx.Dir+":/game:z",
		OracleImage,
		"-dbin", "/game/data/test.db",
		"-dbout", "/game/data/out.db",
		"-gamedir", "/game",
		"-port", fmt.Sprint(oraclePort),
		"-nodetach",
	)
	if out, err := run.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("starting the oracle: %v: %s", err, out)
	}
	defer func() {
		_ = exec.Command("podman", "rm", "-f", name).Run()
	}()

	conn, err := dialWithRetry(ctx, fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		logs, _ := exec.Command("podman", "logs", name).CombinedOutput()
		return nil, fmt.Errorf("connecting to the oracle: %w (logs: %s)", err, logs)
	}
	defer conn.Close()

	return drive(conn, script, pauses)
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

// drive runs a script against a connected server, returning each command's
// output separately.
//
// Each command is followed by a marker pose, so the harness reads until the
// marker rather than waiting a fixed time. That keeps the transcript
// deterministic even when a command produces output slowly.
func drive(conn net.Conn, script Script, pauses map[int]time.Duration) ([]string, error) {
	br := bufio.NewReader(conn)
	out := make([]string, 0, len(script))

	send := func(line string) error {
		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		_, err := conn.Write([]byte(line + "\n"))
		return err
	}

	// readTo collects output until the marker appears, discarding the
	// marker line itself.
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
	// The welcome banner and login output are not part of what is compared.
	if _, err := readTo(connectMarker); err != nil {
		return nil, fmt.Errorf("logging in: %w", err)
	}

	for i, cmd := range script {
		if err := send(cmd); err != nil {
			return out, err
		}
		// A program that suspends itself needs time to resume before
		// the marker is sent, or its later output lands in the next
		// step's transcript.
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
