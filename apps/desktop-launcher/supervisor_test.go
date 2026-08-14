package main

import (
	"testing"
	"time"
)

func TestReadyRegex_Match(t *testing.T) {
	line := "dsh web: http://127.0.0.1:34567"
	match := readyPattern.FindStringSubmatch(line)
	if match == nil {
		t.Fatal("expected match")
	}
	if match[1] != "http://127.0.0.1:34567" {
		t.Errorf("expected http://127.0.0.1:34567, got %s", match[1])
	}
}

func TestReadyRegex_WithLAN(t *testing.T) {
	line := "dsh web: http://127.0.0.1:34567 (LAN: http://192.168.1.100:34567)"
	match := readyPattern.FindStringSubmatch(line)
	if match == nil {
		t.Fatal("expected match")
	}
	if match[1] != "http://127.0.0.1:34567" {
		t.Errorf("expected http://127.0.0.1:34567, got %s", match[1])
	}
}

func TestReadyRegex_NoMatch(t *testing.T) {
	lines := []string{
		"Hello world",
		"dsh: starting...",
		"[INFO] Server listening on port 3000",
	}
	for _, line := range lines {
		if readyPattern.MatchString(line) {
			t.Errorf("unexpected match for: %s", line)
		}
	}
}

func TestBackoffTiming(t *testing.T) {
	opts := DefaultSupervisorOptions()
	for attempt := 1; attempt <= 5; attempt++ {
		delay := opts.RestartDelayMs * (1 << (attempt - 1))
		if delay > opts.MaxRestartDelayMs {
			delay = opts.MaxRestartDelayMs
		}
		t.Logf("attempt %d: delay %dms", attempt, delay)
	}
	// attempt 1: 500ms, 2: 1000ms, 3: 2000ms, 4: 4000ms, 5: 800ms
}

func TestSupervisor_MockChild(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	env := DesktopEnv{
		Command: "sh",
		Args:    []string{"testdata/mock-dsh-web.sh"},
		LogDir:  t.TempDir(),
		Port:    "0",
	}
	sup := NewSupervisor(env, DefaultSupervisorOptions())
	sup.Start()

	select {
	case url := <-sup.Ready():
		if url != "http://127.0.0.1:18080" {
			t.Errorf("expected http://127.0.0.1:18080, got %s", url)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for ready")
	}

	sup.Stop()
}
