package tui

import (
	"math"
	"testing"
	"time"
)

func TestUpdateDownloadRateUsesRollingTransferBytes(t *testing.T) {
	now := time.Unix(100, 0)
	m := Model{
		transferSamples: []transferSample{
			{at: now.Add(-4 * time.Second), bytes: 0},
			{at: now.Add(-3 * time.Second), bytes: 1_000_000},
			{at: now, bytes: 4_000_000},
		},
	}

	m.updateDownloadRate(now, 3*time.Second)

	if diff := math.Abs(m.downloadRate - 1_000_000); diff > 0.01 {
		t.Fatalf("downloadRate = %.2f, want 1000000 bytes/sec", m.downloadRate)
	}
	if got, want := len(m.transferSamples), 2; got != want {
		t.Fatalf("retained samples = %d, want %d", got, want)
	}
}

func TestUpdateDownloadRateFallsToZeroWithoutRecentTraffic(t *testing.T) {
	now := time.Unix(100, 0)
	m := Model{
		transferSamples: []transferSample{
			{at: now.Add(-4 * time.Second), bytes: 500},
			{at: now, bytes: 500},
		},
	}

	m.updateDownloadRate(now, 3*time.Second)

	if m.downloadRate != 0 {
		t.Fatalf("downloadRate = %.2f, want 0", m.downloadRate)
	}
}
