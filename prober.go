// Package prober provides black-box monitoring mechanisms.
//
// To use, define Probe() and Alert() on a type, then pass it to NewProbe:
//   struct FooProber{ someState int }
//
//   // Probe "Foo". E.g. do a network call and compare it to what
//   // was expected.
//   func (p FooProber) Probe() error {
//     // Returning non-nil indicates that the probe failed.
//   }
//   // Send an alert. Called if the probe fails too often.
//   func (p FooProber) Alert() error {
//   }
//   ...
//
//   // Create the probe.
//   p := prober.NewProbe(FooProber{1}, "FooProber", "Probes the Foo")
//
//   // Run the probe. This call blocks forever, so you may
//   // want to do this in a goroutine — you could e.g. register a web
//   // handler to show the contents of p.Records here.
//   go p.Run()
package prober

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/golang/glog"
	"gopkg.in/yaml.v2"
	"hkjn.me/timeutils"
)

var (
	MaxAlertFrequency = time.Minute * 15 // never send alerts more often than this
	DefaultInterval   = flag.Duration("probe_interval", time.Second*61, "duration to pause between prober runs")
	logDir            = os.TempDir()          // logging directory
	logName           = "prober.outcomes.log" // name of logging file
	alertThreshold    = flag.Int("alert_threshold", 100, "level of 'badness' before alerting")
	alertsDisabled    = flag.Bool("no_alerts", false, "disables alerts when probes fail too often")
	disabledProbes    = make(selectedProbes)
	onlyProbes        = make(selectedProbes)
	defaultMinBadness = 0  // default minimum allowed `badness`
	defaultBadnessInc = 10 // default increment on failed probe
	defaultBadnessDec = 1  // default decrement on successful probe
	onceOpen          sync.Once
	logFile           *os.File
	bufferSize        = 200 // maximum number of results per prober to keep
	parseFlags        = sync.Once{}
	results           = [2]string{"Pass", "Fail"}
)

const (
	Pass ResultCode = iota
	Fail
)

type (
	// Result describes the outcome of a single probe.
	Result struct {
		Code    ResultCode
		Error   error
		Info    string // Optional extra information
		InfoUrl string // Optional URL to further information
	}

	// ResultCode describes pass/fail outcomes for probes.
	ResultCode int

	// Record is the result of a single probe run.
	Record struct {
		Timestamp  time.Time `yaml: "-"`
		TimeMillis string    // same as Timestamp but makes it into the YAML logs
		Result     Result    // the result of the probe run
	}

	// Records is a grouping of probe records that implements sort.Interface.
	Records []Record

	// Prober is a mechanism that can probe some target(s).
	Prober interface {
		Probe() Result                                               // probe target(s) once
		Alert(name, desc string, badness int, records Records) error // send alert
	}

	// Option is a setting for an individual prober.
	Option func(*Probe)

	// selectedProbes is a set of probes to be enabled/disabled.
	selectedProbes map[string]bool

	// Probe is a stateful representation of repeated probe runs.
	Probe struct {
		Prober            // underlying prober mechanism
		Name, Desc string // name, description of the probe
		// If badness reaches alert threshold, an alert email is sent and
		// alertThreshold resets.
		Badness    int
		Interval   time.Duration // how often to probe
		Timeout    time.Duration // timeout for probe call, defaults to same as probing inteval
		Alerting   bool          // whether this probe is currently alerting
		LastAlert  time.Time     // time of last alert sent, if any
		Disabled   bool          // whether this probe is disabled
		Records    Records       // historical records of probe runs
		minBadness int           // minimum allowed `badness` value
		badnessInc int           // how much to increment `badness` on failure
		badnessDec int           // how much to decrement `badness` on success
		reportFn   func(Result)  // function to call to report probe results
	}
	Probes []*Probe
)

// String returns the English name of the result.
func (r ResultCode) String() string { return results[r] }

// Passed returns whether the probe result indicates a pass.
func (r Result) Passed() bool { return r.Code == Pass }

// FailedWith returns a Result representing failure with given error.
func FailedWith(err error) Result {
	return Result{
		Code:  Fail,
		Error: err,
		Info:  fmt.Sprintf("The probe failed with %q", err.Error()),
	}
}

// FailedWith returns a Result representing failure with given error and extra information.
func FailedWithInfo(err error, info, infoUrl string) Result {
	return Result{
		Code:    Fail,
		Error:   err,
		Info:    info,
		InfoUrl: infoUrl,
	}
}

// Passed returns a Result representing pass.
func Passed() Result { return Result{Code: Pass} }

// PasseWith returns a Result representing pass, with extra info.
func PassedWith(info, url string) Result {
	return Result{
		Code:    Pass,
		Info:    info,
		InfoUrl: url,
	}
}

// String returns the flag's value.
func (d *selectedProbes) String() string {
	s := ""
	i := 0
	for p, _ := range *d {
		if i > 0 {
			s += ","
		}
		s += p
	}
	return s
}

// Get is part of the flag.Getter interface. It always returns nil for
// this flag type since the struct is not exported.
func (d *selectedProbes) Get() interface{} { return nil }

// Syntax: -disable_probes=FooProbe,BarProbe
func (d *selectedProbes) Set(value string) error {
	vals := strings.Split(value, ",")
	m := *d
	for _, p := range vals {
		m[p] = true
	}
	return nil
}

// NewProbe returns a new probe from given prober implementation.
func NewProbe(p Prober, name, desc string, options ...Option) *Probe {
	parseFlags.Do(func() {
		if !flag.Parsed() {
			flag.Parse()
		}
	})
	probe := &Probe{
		Prober:     p,
		Name:       name,
		Desc:       desc,
		Badness:    defaultMinBadness,
		Interval:   *DefaultInterval,
		Timeout:    *DefaultInterval,
		Records:    Records{},
		minBadness: defaultMinBadness,
		badnessInc: defaultBadnessInc,
		badnessDec: defaultBadnessDec,
	}
	for _, opt := range options {
		opt(probe)
	}
	return probe
}

// Interval sets the interval for the prober.
func Interval(interval time.Duration) func(*Probe) {
	return func(p *Probe) {
		p.Interval = interval
	}
}

// Timeout sets the timeout for the prober.
func Timeout(timeout time.Duration) func(*Probe) {
	return func(p *Probe) {
		p.Timeout = timeout
	}
}

// Report sets the function to call to report probe results.
func Report(fn func(Result)) func(*Probe) {
	return func(p *Probe) {
		p.reportFn = fn
	}
}

// FailurePenalty sets the amount `badness` is incremented on failure for the prober.
func FailurePenalty(badnessInc int) func(*Probe) {
	return func(p *Probe) {
		p.badnessInc = badnessInc
	}
}

// SuccessReward sets the amount `badness` is decremented on success for the prober.
func SuccessReward(badnessDec int) func(*Probe) {
	return func(p *Probe) {
		p.badnessDec = badnessDec
	}
}

// Run repeatedly runs the probe, blocking forever.
func (p *Probe) Run() {
	glog.Infof("[%s] Starting..\n", p.Name)

	for {
		if p.enabled() {
			p.runProbe()
		} else {
			p.Disabled = true
			glog.Infof("[%s] is disabled, will now exit", p.Name)
			return
		}
	}
}

// String returns a human-readable representation of the Probe.
func (p *Probe) String() string {
	return fmt.Sprintf("&Probe{Name: %q, Desc: %q}", p.Name, p.Desc)
}

// enabled returns true if this probe is enabled.
func (p *Probe) enabled() bool {
	if len(onlyProbes) > 0 {
		if _, ok := onlyProbes[p.Name]; ok {
			// We only want specific probes, but this probe is one of them.
			return true
		}
		return false
	}

	if _, ok := disabledProbes[p.Name]; ok {
		// This probe is explicitly disabled.
		return false
	}
	return true
}

// runProbe runs the probe once.
func (p *Probe) runProbe() {
	c := make(chan Result, 1)
	start := time.Now().UTC()
	go func() {
		glog.Infof("[%s] Probing..\n", p.Name)
		c <- p.Probe()
	}()
	select {
	case r := <-c:
		// We got a result of some sort from the prober.
		p.handleResult(r)
		wait := p.Timeout - time.Since(start)
		glog.V(2).Infof("[%s] needs to sleep %v more here\n", p.Name, wait)
		time.Sleep(wait)
	case <-time.After(p.Interval):
		// Probe didn't finish in time for us to run the next one, report as failure.
		glog.Errorf("[%s] Timed out\n", p.Name)
		timeoutFail := FailedWith(
			fmt.Errorf("%s timed out (with probe interval %1.1f sec)",
				p.Name,
				p.Interval.Seconds()))
		p.handleResult(timeoutFail)
	}
}

// add appends the record to the buffer for the probe, keeping it within bufferSize.
func (p *Probe) addRecord(r Record) {
	p.Records = append(p.Records, r)
	if len(p.Records) >= bufferSize {
		over := len(p.Records) - bufferSize
		glog.V(2).Infof("[%s] buffer is over %d, reslicing it\n", p.Name, bufferSize)
		p.Records = p.Records[over:]
	}
	glog.V(2).Infof("[%s] buffer is now %d elements\n", p.Name, len(p.Records))
}

// Equal returns true if the probes are equal.
func (p1 *Probe) Equal(p2 *Probe) bool {
	if p2 == nil {
		return false
	}
	if p1.Name != p2.Name {
		return false
	}
	if p1.Badness != p2.Badness {
		return false
	}
	if p1.Interval != p2.Interval {
		return false
	}
	if p1.Timeout != p2.Timeout {
		return false
	}
	if p1.Alerting != p2.Alerting {
		return false
	}
	if !p1.LastAlert.Equal(p2.LastAlert) {
		return false
	}
	if p1.Disabled != p2.Disabled {
		return false
	}
	if !p1.Records.Equal(p2.Records) {
		return false
	}
	if p1.minBadness != p2.minBadness {
		return false
	}
	if p1.badnessInc != p2.badnessInc {
		return false
	}
	return true
}

// Equal returns true if the Records are equal.
func (rs1 Records) Equal(rs2 Records) bool {
	if len(rs1) != len(rs2) {
		return false
	}
	for i, r1 := range rs1 {
		r2 := rs2[i]
		if !r1.Equal(r2) {
			return false
		}
	}
	return true
}

// Implement sort.Interface for Records. The sort order is chronological.
func (rs Records) Len() int           { return len(rs) }
func (rs Records) Swap(i, j int)      { rs[i], rs[j] = rs[j], rs[i] }
func (rs Records) Less(i, j int) bool { return rs[i].Timestamp.Before(rs[j].Timestamp) }

// RecentFailures returns only recent probe failures among the records.
func (pr Records) RecentFailures() Records {
	failures := make(Records, 0)
	for _, r := range pr {
		if !r.Result.Passed() && !r.Timestamp.Before(time.Now().Add(-time.Hour)) {
			failures = append(failures, r)
		}
	}
	sort.Sort(sort.Reverse(failures))
	return failures
}

// Ago describes the duration since the record occured.
func (r Record) Ago() string {
	return timeutils.DescDuration(time.Since(r.Timestamp))
}

// Marshal returns the record in YAML form.
func (r Record) marshal() []byte {
	b, err := yaml.Marshal(r)
	if err != nil {
		glog.Fatalf("failed to marshal record %+v: %v", r, err)
	}
	return b
}

// Equal returns true if the Record objects are equal.
func (r1 Record) Equal(r2 Record) bool {
	if !r1.Timestamp.Equal(r2.Timestamp) {
		return false
	}
	if r1.TimeMillis != r2.TimeMillis {
		return false
	}
	if r1.Result != r2.Result {
		return false
	}
	return true
}

// openLog opens the log file.
func openLog() {
	logPath := filepath.Join(logDir, logName)
	glog.V(1).Infof("Using YAML log file %q\n", logPath)
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, os.ModePerm)
	if err != nil {
		glog.Fatalf("failed to open %q: %v\n", logPath, err)
	}
	logFile = f
}

// handleResult handles a return value from a Probe() run.
func (p *Probe) handleResult(r Result) {
	if p.reportFn != nil {
		// Call custom report function, if specified.
		p.reportFn(r)
	}
	if r.Passed() {
		if p.Badness > p.minBadness {
			p.Badness -= p.badnessDec
		}
		glog.V(1).Infof("[%s] Pass, badness is now %d.\n", p.Name, p.Badness)
	} else {
		p.Badness += p.badnessInc
		glog.Errorf("[%s] Failed while probing, badness is now %d: %v\n", p.Name, p.Badness, r.Error)
	}
	p.logResult(r)

	if p.Badness < *alertThreshold {
		p.Alerting = false
		return
	}

	p.Alerting = true
	if *alertsDisabled {
		glog.Infof("[%s] would now be alerting, but alerts are supressed\n", p.Name)
		return
	}

	glog.Infof("[%s] is alerting\n", p.Name)
	if time.Since(p.LastAlert) < MaxAlertFrequency {
		glog.V(1).Infof("[%s] will not alert, since last alert was sent %v back\n", p.Name, time.Since(p.LastAlert))
		return
	}
	// Send alert notification in goroutine to not block further
	// probing.
	// TODO: There is a race condition here, if email sending takes long
	// enough for further Probe() runs to finish, which would queue up
	// several duplicate alert emails. This shouldn't often happen, but
	// technically should be bounded by a timeout to prevent the
	// possibility.
	go p.sendAlert()
}

// sendAlert calls the Alert() implementation and handles the outcome.
func (p *Probe) sendAlert() {
	err := p.Alert(p.Name, p.Desc, p.Badness, p.Records)
	if err != nil {
		glog.Errorf("[%s] failed to alert: %v", p.Name, err)
		// Note: We don't reset badness here; next cycle we'll keep
		// trying to send the alert.
	} else {
		glog.Infof("[%s] sent alert email, resetting badness to 0\n", p.Name)
		p.LastAlert = time.Now().UTC()
		p.Badness = p.minBadness
	}
}

// logResult logs the result of a probe run.
func (p *Probe) logResult(res Result) {
	onceOpen.Do(openLog)
	now := time.Now().UTC()
	rec := Record{
		Timestamp:  now,
		TimeMillis: now.Format(time.StampMilli),
		Result:     res,
	}

	p.addRecord(rec)
	_, err := logFile.Write(rec.marshal())
	if err != nil {
		glog.Fatalf("failed to write record to log: %v", err)
	}
}

// Equal returns true if both Probes are equal.
func (ps1 Probes) Equal(ps2 Probes) bool {
	if len(ps1) != len(ps2) {
		return false
	}
	for i, p1 := range ps1 {
		if !ps2[i].Equal(p1) {
			return false
		}
	}
	return true
}

// Implement sort.Interface for Probes.
func (ps Probes) Len() int { return len(ps) }

// Less returns true if probe i should sort before probe j.
//
// Less is implemented to give the natural order that's likely to be
// most useful when ordering Probes, i.e. with the ones requiring
// attention first. Since the default sort order is ascending, this
// means that "lower values" will correspond to probes in worse state.
func (ps Probes) Less(i, j int) bool {
	if ps[i].Disabled != ps[j].Disabled {
		// Disabled probes sort after (higher value than) non-disabled ones.
		return ps[j].Disabled
	}
	if ps[i].Alerting != ps[j].Alerting {
		// Alerting probes sort before (lower value than) non-alerting ones.
		return ps[i].Alerting
	}
	if ps[i].Badness != ps[j].Badness {
		// Probes with higher badness sort before ones with lower badness.
		return ps[i].Badness > ps[j].Badness
	}
	if ps[i].LastAlert != ps[j].LastAlert {
		// Probes that alerted longer ago sort after ones that alerted
		// more recently.
		return ps[i].LastAlert.After(ps[j].LastAlert)
	}
	if len(ps[i].Records) != len(ps[j].Records) {
		// Probes with shorter history sort after those with longer
		// history.
		return len(ps[i].Records) > len(ps[j].Records)
	}
	// Tie-breaker: Sort by name.
	if ps[i].Name != ps[j].Name {
		return ps[i].Name < ps[j].Name
	}
	// Tie-breaker #2: Sort by desc.
	if ps[i].Desc != ps[j].Desc {
		return ps[i].Desc < ps[j].Desc
	}
	// We have no way of comparing.
	return true
}
func (ps Probes) Swap(i, j int) { ps[i], ps[j] = ps[j], ps[i] }

func init() {
	flag.Var(&disabledProbes, "disabled_probes", "comma-separated list of probes to disable")
	flag.Var(&onlyProbes, "only_probes", "comma-separated list of the only probes to enable")
}
