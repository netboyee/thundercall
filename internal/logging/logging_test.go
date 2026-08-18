package logging

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

func TestLoggerSuppressesDebugAtInfoLevel(t *testing.T) {
	var buffer bytes.Buffer
	restoreOutput := log.Writer()
	restoreFlags := log.Flags()
	defer log.SetOutput(restoreOutput)
	defer log.SetFlags(restoreFlags)

	log.SetOutput(&buffer)
	log.SetFlags(0)
	Configure("info")

	logger := New("test")
	logger.Debugf("event=debug")
	logger.Infof("event=info")

	output := buffer.String()
	if strings.Contains(output, "event=debug") {
		t.Fatalf("output = %q, want debug log suppressed", output)
	}
	if !strings.Contains(output, "level=INFO component=test event=info") {
		t.Fatalf("output = %q, want info log emitted", output)
	}
}

func TestLoggerEmitsDebugAtDebugLevel(t *testing.T) {
	var buffer bytes.Buffer
	restoreOutput := log.Writer()
	restoreFlags := log.Flags()
	defer log.SetOutput(restoreOutput)
	defer log.SetFlags(restoreFlags)

	log.SetOutput(&buffer)
	log.SetFlags(0)
	Configure("debug")

	logger := New("test")
	logger.Debugf("event=debug")

	output := buffer.String()
	if !strings.Contains(output, "level=DEBUG component=test event=debug") {
		t.Fatalf("output = %q, want debug log emitted", output)
	}
}
