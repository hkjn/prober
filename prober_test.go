package prober

import (
	"errors"
	"log"
	"sort"
	"testing"
	"time"
)

type (
	// fakeTime implements timeT for tests by pretending it's always the specified Time.
	fakeTime struct{ time.Time }
	// testProber is a Probe implementation that retrurns specified Result when Probe() is called.
	testProber struct{ result Result }
)

func (ft fakeTime) Now() time.Time     { return ft.Time }
func (fakeTime) Sleep(d time.Duration) {}

func (p testProber) Probe() Result                                               { return p.result }
func (p testProber) Alert(name, desc string, badness int, records Records) error { return nil }

func TestProbe_runProbe(t *testing.T) {
	type (
		want struct {
			wait     time.Duration
			state    *Probe
			silenced bool
		}
	)
	parseTime := func(s string) time.Time {
		ft, err := time.Parse(time.RFC822, s)
		if err != nil {
			log.Fatalf("FATAL: Couldn't parse time: %v\n", err)
		}
		return ft
	}
	cases := []struct {
		in   *Probe
		want want
	}{
		{
			in: &Probe{
				Prober:     testProber{Passed()},
				Name:       "TestProber1",
				Desc:       "A test prober.",
				Records:    Records{},
				Badness:    0,
				badnessInc: 10,
				Interval:   time.Minute,
				t:          fakeTime{parseTime("19 Nov 98 15:14 UTC")},
			},
			want: want{
				wait: *DefaultInterval,
				state: &Probe{
					Prober: testProber{Passed()},
					Name:   "TestProber1",
					Desc:   "A test prober.",
					Records: Records{
						// TODO(hkjn): Clean up Timestamp vs TimeMillis.
						Record{
							Timestamp:  parseTime("19 Nov 98 15:14 UTC"),
							TimeMillis: "Nov 19 15:14:00.000",
							Result:     Passed(),
						},
					},
					Badness:    0,
					badnessInc: 10,
					Interval:   time.Minute,
					t:          fakeTime{parseTime("19 Nov 98 15:14 UTC")},
				},
			},
		},
		{
			in: &Probe{
				Prober:     testProber{FailedWith(errors.New("TestProber2 failing on purpose"))},
				Name:       "TestProber2",
				Desc:       "A test prober that fails.",
				Records:    Records{},
				Badness:    0,
				badnessInc: 10,
				Interval:   time.Minute,
				t:          fakeTime{parseTime("19 Nov 98 15:14 UTC")},
			},
			want: want{
				wait: *DefaultInterval,
				state: &Probe{
					Name: "TestProber2",
					Desc: "A test prober that fails.",
					Records: Records{
						Record{
							Timestamp:  parseTime("19 Nov 98 15:14 UTC"),
							TimeMillis: "Nov 19 15:14:00.000",
							Result:     FailedWith(errors.New("TestProber2 failing on purpose")),
						},
					},
					Badness:    defaultBadnessInc,
					badnessInc: defaultBadnessInc,
					Interval:   *DefaultInterval,
				},
			},
		},
		{
			in: &Probe{
				Prober:     testProber{FailedWith(errors.New("TestProber3 failing on purpose"))},
				Name:       "TestProber3",
				Desc:       "A test prober that alerts.",
				Records:    Records{},
				Badness:    90,
				badnessInc: 10,
				Interval:   time.Minute,
				t:          fakeTime{parseTime("19 Nov 98 15:14 UTC")},
			},
			want: want{
				wait: *DefaultInterval,
				state: &Probe{
					Name: "TestProber3",
					Desc: "A test prober that alerts.",
					Records: Records{
						Record{
							Timestamp:  parseTime("19 Nov 98 15:14 UTC"),
							TimeMillis: "Nov 19 15:14:00.000",
							Result:     FailedWith(errors.New("TestProber3 failing on purpose")),
						},
					},
					badnessInc: 10,
					Badness:    100,
					Interval:   time.Minute,
					alerting:   true,
				},
			},
		},
		{
			in: &Probe{
				Prober:        testProber{FailedWith(errors.New("TestProber4 failing on purpose"))},
				Name:          "TestProber4",
				Desc:          "A test prober that is silenced.",
				Records:       Records{},
				SilencedUntil: SilenceTime{parseTime("19 Nov 98 15:30 UTC")},
				Badness:       90,
				badnessInc:    10,
				Interval:      time.Minute,
				t:             fakeTime{parseTime("19 Nov 98 15:14 UTC")},
			},
			want: want{
				wait: *DefaultInterval,
				state: &Probe{
					Name: "TestProber4",
					Desc: "A test prober that is silenced.",
					Records: Records{
						Record{
							Timestamp:  parseTime("19 Nov 98 15:14 UTC"),
							TimeMillis: "Nov 19 15:14:00.000",
							Result:     FailedWith(errors.New("TestProber4 failing on purpose")),
						},
					},
					badnessInc:    10,
					Badness:       0,
					SilencedUntil: SilenceTime{parseTime("19 Nov 98 15:30 UTC")},
					Interval:      time.Minute,
				},
				silenced: true,
			},
		},
		{
			in: &Probe{
				Prober:        testProber{FailedWith(errors.New("TestProber5 failing on purpose"))},
				Name:          "TestProber5",
				Desc:          "A test prober that was recently silenced.",
				Records:       Records{},
				SilencedUntil: SilenceTime{parseTime("19 Nov 98 15:13 UTC")},
				Badness:       90,
				badnessInc:    10,
				Interval:      time.Minute,
				t:             fakeTime{parseTime("19 Nov 98 15:14 UTC")},
			},
			want: want{
				wait: *DefaultInterval,
				state: &Probe{
					Name: "TestProber5",
					Desc: "A test prober that was recently silenced.",
					Records: Records{
						Record{
							Timestamp:  parseTime("19 Nov 98 15:14 UTC"),
							TimeMillis: "Nov 19 15:14:00.000",
							Result:     FailedWith(errors.New("TestProber5 failing on purpose")),
						},
					},
					badnessInc:    10,
					Badness:       100,
					SilencedUntil: SilenceTime{parseTime("19 Nov 98 15:13 UTC")},
					Interval:      time.Minute,
					alerting:      true,
				},
				silenced: false,
			},
		},
		{
			in: &Probe{
				Prober:        testProber{FailedWith(errors.New("TestProber6 failing on purpose"))},
				Name:          "TestProber6",
				Desc:          "A test prober that is silenced and not alerting.",
				Records:       Records{},
				SilencedUntil: SilenceTime{parseTime("19 Nov 98 15:30 UTC")},
				Badness:       50,
				badnessInc:    10,
				Interval:      time.Minute,
				t:             fakeTime{parseTime("19 Nov 98 15:14 UTC")},
			},
			want: want{
				wait: *DefaultInterval,
				state: &Probe{
					Name: "TestProber6",
					Desc: "A test prober that is silenced and not alerting.",
					Records: Records{
						Record{
							Timestamp:  parseTime("19 Nov 98 15:14 UTC"),
							TimeMillis: "Nov 19 15:14:00.000",
							Result:     FailedWith(errors.New("TestProber6 failing on purpose")),
						},
					},
					badnessInc:    10,
					Badness:       0,
					SilencedUntil: SilenceTime{parseTime("19 Nov 98 15:30 UTC")},
					Interval:      time.Minute,
				},
				silenced: true,
			},
		},
	}

	for i, tt := range cases {
		got := tt.in.runProbe()
		if got != tt.want.wait {
			t.Errorf("[%d] %+v.runProbe() => %v; want %v\n",
				i, tt.in, got, tt.want.wait)
		} else if !tt.in.Equal(tt.want.state) {
			t.Errorf("[%d] Got probe in state:\n%+v\nWant:\n%+v\n",
				i, tt.in, tt.want.state)
		} else if tt.in.Silenced() != tt.want.silenced {
			t.Errorf("[%d] %v.Silenced()=%v, want %v\n",
				i, tt.in, tt.in.Silenced(), tt.want.silenced)
		}
	}
}

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
				&Probe{Name: "worse", Badness: 50, alerting: true},
				&Probe{Name: "bad", Badness: 50, alerting: false},
			},
			want: true,
		},
		{
			in: Probes{
				&Probe{
					Name:     "good",
					Badness:  0,
					alerting: false,
				},
				&Probe{
					Name:          "bad",
					Badness:       50,
					SilencedUntil: parseTime("15 Jun 16 15:04 UTC"),
					alerting:      true,
				},
			},
			want: true,
		},
		{
			in: Probes{
				&Probe{
					Name:          "bad but silenced for a shorter time",
					Badness:       150,
					alerting:      true,
					SilencedUntil: parseTime("15 Jun 16 15:04 UTC"),
				},
				&Probe{
					Name:          "bad and silenced for a long time",
					Badness:       150,
					alerting:      true,
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
					alerting:      true,
					SilencedUntil: parseTime("15 Jun 17 15:04 UTC"),
				},
				&Probe{
					Name:          "bad and silenced for a long time but not alerting",
					Badness:       80,
					alerting:      false,
					SilencedUntil: parseTime("15 Jun 17 15:04 UTC"),
				},
			},
			want: true,
		},
		{
			in: Probes{
				&Probe{
					Name:          "bad but silenced for a long time",
					Badness:       50,
					alerting:      true,
					Disabled:      false,
					SilencedUntil: parseTime("15 Jun 17 15:04 UTC"),
				},
				&Probe{
					Name:     "strange and bad",
					Badness:  2500,
					alerting: true,
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
				&Probe{Name: "bad", Badness: 50, alerting: false},
				&Probe{Name: "worse", Badness: 50, alerting: true},
				&Probe{Name: "still bad", Badness: 49},
				&Probe{Name: "less bad", Badness: 20, alerting: true},
			},
			want: Probes{
				&Probe{Name: "worse", Badness: 50, alerting: true},
				&Probe{Name: "bad", Badness: 50, alerting: false},
				&Probe{Name: "still bad", Badness: 49},
				&Probe{Name: "less bad", Badness: 20, alerting: true},
			},
		},
		{
			in: Probes{
				&Probe{Name: "bad", Badness: 50, alerting: false},
				&Probe{Name: "worse", Badness: 50, alerting: true},
				&Probe{Name: "disabled", Disabled: true},
				&Probe{Name: "less bad", Badness: 20, alerting: true},
			},
			want: Probes{
				&Probe{Name: "worse", Badness: 50, alerting: true},
				&Probe{Name: "bad", Badness: 50, alerting: false},
				&Probe{Name: "less bad", Badness: 20, alerting: true},
				&Probe{Name: "disabled", Disabled: true},
			},
		},
		{
			in: Probes{
				// A probe shouldn't normally both be disabled and have high
				// Badness or be Alerting, but this is a unit test, and we
				// still should put the Disabled probe last..
				&Probe{Name: "strange and bad", Badness: 2500, alerting: true, Disabled: true},
				&Probe{Name: "normal bad", Badness: 50, alerting: true, Disabled: false},
				&Probe{Name: "not bad", Badness: 0, alerting: false, Disabled: false},
				&Probe{Name: "just disabled", Badness: 0, alerting: false, Disabled: true},
			},
			want: Probes{
				&Probe{Name: "normal bad", Badness: 50, alerting: true, Disabled: false},
				&Probe{Name: "not bad", Badness: 0, alerting: false, Disabled: false},
				&Probe{Name: "strange and bad", Badness: 2500, alerting: true, Disabled: true},
				&Probe{Name: "just disabled", Badness: 0, alerting: false, Disabled: true},
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
