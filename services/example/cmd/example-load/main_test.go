package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/tienduc120/go-base/platform/loadgen"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func TestParseFlagsUsesBoundedDefaults(t *testing.T) {
	t.Parallel()

	opts, err := parseFlags(nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	if opts.baseURL != defaultURL || opts.load.Duration != defaultDuration || opts.load.Workers != defaultWorkers {
		t.Errorf("options = %+v, want the defaults", opts)
	}
}

func TestParseFlagsReadsTheOverrides(t *testing.T) {
	t.Parallel()

	args := []string{"-url", "http://localhost:9000", "-duration", "30s", "-workers", "8", "-max-requests", "500"}
	opts, err := parseFlags(args, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	if opts.baseURL != "http://localhost:9000" || opts.load.Duration != 30*time.Second ||
		opts.load.Workers != 8 || opts.load.MaxRequests != 500 {
		t.Errorf("options = %+v", opts)
	}
}

func TestParseFlagsRefusesATargetOffThisMachineUnlessAllowed(t *testing.T) {
	t.Parallel()

	for _, target := range []string{"http://example.com", "https://api.internal:8443", "http://10.1.2.3:8080"} {
		if _, err := parseFlags([]string{"-url", target}, io.Discard); err == nil {
			t.Errorf("%s: a load program must not be pointed at a shared address by accident", target)
		}
		if _, err := parseFlags([]string{"-url", target, "-allow-remote"}, io.Discard); err != nil {
			t.Errorf("%s with -allow-remote: %v", target, err)
		}
	}
	for _, target := range []string{"http://127.0.0.1:8080", "http://localhost:8080", "http://[::1]:8080"} {
		if _, err := parseFlags([]string{"-url", target}, io.Discard); err != nil {
			t.Errorf("%s is on this machine: %v", target, err)
		}
	}
}

func TestParseFlagsRefusesWhatIsNotAURL(t *testing.T) {
	t.Parallel()

	for _, bad := range []string{"", "not a url", "127.0.0.1:8080"} {
		if _, err := parseFlags([]string{"-url", bad}, io.Discard); err == nil {
			t.Errorf("-url %q should be refused", bad)
		}
	}
	if _, err := parseFlags([]string{"-nope"}, io.Discard); err == nil {
		t.Error("an unknown flag should be refused")
	}
}

func TestRunPrintsTheSummaryAndPassesWhenNothingFailed(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	opts := options{load: loadgen.Config{Workers: 2, Duration: time.Minute, MaxRequests: 10}}

	err := run(t.Context(), opts, func(context.Context, loadgen.Worker) error { return nil }, &out)

	if err != nil || !strings.Contains(out.String(), "10 requests") {
		t.Errorf("err = %v, output:\n%s", err, out.String())
	}
}

func TestRunFailsAndSaysWhyWhenRequestsFail(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	opts := options{load: loadgen.Config{Workers: 1, Duration: time.Minute, MaxRequests: 10}}

	err := run(t.Context(), opts, func(context.Context, loadgen.Worker) error { return errors.New("status 500") }, &out)

	if err == nil || !strings.Contains(out.String(), "status 500") {
		t.Errorf("err = %v, output:\n%s, want a failure that shows the first error", err, out.String())
	}
}

func TestRunRefusesAnUnboundedConfig(t *testing.T) {
	t.Parallel()

	opts := options{load: loadgen.Config{Workers: 0, Duration: time.Minute}}
	err := run(t.Context(), opts, func(context.Context, loadgen.Worker) error { return nil }, io.Discard)

	if err == nil {
		t.Error("a load run with no workers is a mistake, not an empty success")
	}
}
