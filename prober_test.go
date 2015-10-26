package prober

import (
	"sort"
	"testing"
	"time"
)

// TODO(hkjn): Add tests that start a bunch of probes in goroutines,
// expects them to have Probe() called, Badness changed, Alert()
// should be called, etc.

func TestProbes_Less(t *testing.T) {
	parseTime := func(v string) SilenceTime {
		ts, err := time.Parse(time.RFC822, v)
		if err != nil {
			t.Fatalf("buggy test, can't parse time: %v", err)
		}
		return SilenceTime{ts}
	}
	cases := []struct {
		in   Probes
		want bool
	}{
		{
			in: Probes{
				&Probe{Badness: 51},
				&Probe{Badness: 50},
			},
			want: true,
		},
		{
			in: Probes{
				&Probe{Name: "Abc"},
				&Probe{Name: "Def"},
			},
			want: true,
		},
		{
			in: Probes{
				&Probe{Name: "worse", Badness: 50, Alerting: true},
				&Probe{Name: "bad", Badness: 50, Alerting: false},
			},
			want: true,
		},
		{
			in: Probes{
				&Probe{
					Name:     "good",
					Badness:  0,
					Alerting: false,
				},
				&Probe{
					Name:          "bad",
					Badness:       50,
					SilencedUntil: parseTime("15 Jun 16 15:04 UTC"),
					Alerting:      true,
				},
			},
			want: true,
		},
		{
			in: Probes{
				&Probe{
					Name:          "bad but silenced for a shorter time",
					Badness:       150,
					Alerting:      true,
					SilencedUntil: parseTime("15 Jun 16 15:04 UTC"),
				},
				&Probe{
					Name:          "bad and silenced for a long time",
					Badness:       150,
					Alerting:      true,
					SilencedUntil: parseTime("15 Jun 17 15:04 UTC"),
				},
			},
			want: true,
		},
		{
			in: Probes{
				&Probe{
					Name:          "bad but silenced for a long time",
					Badness:       80,
					Alerting:      true,
					SilencedUntil: parseTime("15 Jun 17 15:04 UTC"),
				},
				&Probe{
					Name:          "bad and silenced for a long time but not alerting",
					Badness:       80,
					Alerting:      false,
					SilencedUntil: parseTime("15 Jun 17 15:04 UTC"),
				},
			},
			want: true,
		},
		{
			in: Probes{
				&Probe{
					Name:          "bad but silenced for a long time",
					Badness:       150,
					Alerting:      true,
					Disabled:      false,
					SilencedUntil: parseTime("15 Jun 17 15:04 UTC"),
				},
				&Probe{
					Name:     "strange and bad",
					Badness:  2500,
					Alerting: true,
					Disabled: true,
				},
			},
			want: true,
		},
	}

	for i, tt := range cases {
		// Note that we in these tests always compare element 0 to element
		// 1, and always expect Less() to be true. The pair-wise
		// comparison is "less" if the two probes are in the "natural
		// order", which here is that "worse" probes are sorted before
		// "less worse" probes.
		got := tt.in.Less(0, 1)
		if got != tt.want {
			t.Errorf("[%d] %v.Less(0, 1) => %v; want %v\n",
				i, tt.in, got, tt.want)
		}
	}
}

func TestProbes_Sort(t *testing.T) {
	parseTime := func(v string) SilenceTime {
		ts, err := time.Parse(time.RFC822, v)
		if err != nil {
			t.Fatalf("buggy test, can't parse time: %v", err)
		}
		return SilenceTime{ts}
	}
	cases := []struct {
		in   Probes
		want Probes
	}{
		{
			in:   Probes{},
			want: Probes{},
		},
		{
			in: Probes{
				&Probe{Badness: 50},
				&Probe{Badness: 51},
				&Probe{Badness: 49},
			},
			want: Probes{
				&Probe{Badness: 51},
				&Probe{Badness: 50},
				&Probe{Badness: 49},
			},
		},
		{
			in: Probes{
				&Probe{Name: "bad", Badness: 50, Alerting: false},
				&Probe{Name: "worse", Badness: 50, Alerting: true},
				&Probe{Name: "still bad", Badness: 49},
				&Probe{Name: "less bad", Badness: 20, Alerting: true},
			},
			want: Probes{
				&Probe{Name: "worse", Badness: 50, Alerting: true},
				&Probe{Name: "bad", Badness: 50, Alerting: false},
				&Probe{Name: "still bad", Badness: 49},
				&Probe{Name: "less bad", Badness: 20, Alerting: true},
			},
		},
		{
			in: Probes{
				&Probe{Name: "bad", Badness: 50, Alerting: false},
				&Probe{Name: "worse", Badness: 50, Alerting: true},
				&Probe{Name: "disabled", Disabled: true},
				&Probe{Name: "less bad", Badness: 20, Alerting: true},
			},
			want: Probes{
				&Probe{Name: "worse", Badness: 50, Alerting: true},
				&Probe{Name: "bad", Badness: 50, Alerting: false},
				&Probe{Name: "less bad", Badness: 20, Alerting: true},
				&Probe{Name: "disabled", Disabled: true},
			},
		},
		{
			in: Probes{
				// A probe shouldn't normally both be disabled and have high
				// Badness or be Alerting, but this is a unit test, and we
				// still should put the Disabled probe last..
				&Probe{Name: "strange and bad", Badness: 2500, Alerting: true, Disabled: true},
				&Probe{Name: "normal bad", Badness: 50, Alerting: true, Disabled: false},
				&Probe{Name: "not bad", Badness: 0, Alerting: false, Disabled: false},
				&Probe{Name: "just disabled", Badness: 0, Alerting: false, Disabled: true},
			},
			want: Probes{
				&Probe{Name: "normal bad", Badness: 50, Alerting: true, Disabled: false},
				&Probe{Name: "not bad", Badness: 0, Alerting: false, Disabled: false},
				&Probe{Name: "strange and bad", Badness: 2500, Alerting: true, Disabled: true},
				&Probe{Name: "just disabled", Badness: 0, Alerting: false, Disabled: true},
			},
		},
	}
	for i, tt := range cases {
		got := make(Probes, len(tt.in))
		copy(got, tt.in)
		sort.Sort(got)
		if !got.Equal(tt.want) {
			t.Errorf("[%d] sort.Sort(%v) => %+v; want %+v\n",
				i, tt.in, got, tt.want)
		}
	}
}
