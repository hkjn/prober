package prober

import (
	"sort"
	"testing"
)

func TestProbes_Sort(t *testing.T) {
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
				&Probe{Name: "Foo"},
				&Probe{Name: "Bar"},
			},
			want: Probes{
				&Probe{Name: "Bar"},
				&Probe{Name: "Foo"},
			},
		},
		{
			in: Probes{
				&Probe{Name: "bad", Badness: 50, Alerting: false},
				&Probe{Name: "worse", Badness: 50, Alerting: true},
				&Probe{Name: "okayish", Badness: 49},
				&Probe{Name: "also bad", Badness: 20, Alerting: true},
			},
			want: Probes{
				&Probe{Name: "worse", Badness: 50, Alerting: true},
				&Probe{Name: "also bad", Badness: 20, Alerting: true},
				&Probe{Name: "bad", Badness: 50, Alerting: false},
				&Probe{Name: "okayish", Badness: 49},
			},
		},
		{
			in: Probes{
				// A probe shouldn't normally both be disabled and have high
				// Badness/be Alerting, but we still should put the Disabled probe last..
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
