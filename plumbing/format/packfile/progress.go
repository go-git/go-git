package packfile

import (
	"fmt"
	"io"
	"time"
)

const (
	kib = 1024
	mib = 1024 * 1024
	gib = 1024 * 1024 * 1024
)

// objectProgress reports git-style clone progress for one phase, matching
// the client-side lines index-pack prints (builtin/index-pack.c):
//
//	Receiving objects: 100% (31/31), 1.49 KiB | 1.49 KiB/s, done.
//	Resolving deltas: 100% (14/14), done.
//
// In-progress updates use CR so a terminal overwrites the line; the done
// line ends with a newline. Percent is based on object count, as git's
// display() does. Throughput is optional and uses the same 1024-based
// KiB/MiB/GiB units as git's strbuf_humanise_bytes.
type objectProgress struct {
	w       io.Writer
	title   string
	total   uint32
	current uint32
	lastPct int
	bytes   func() int64
	start   time.Time
	showTP  bool
	started bool
}

func newObjectProgress(w io.Writer, title string, total uint32, showThroughput bool, bytes func() int64) *objectProgress {
	return &objectProgress{
		w:       w,
		title:   title,
		total:   total,
		lastPct: -1,
		bytes:   bytes,
		start:   time.Now(),
		showTP:  showThroughput,
	}
}

func (p *objectProgress) step() {
	if p == nil {
		return
	}
	p.current++
	p.write(false)
}

func (p *objectProgress) done() {
	if p == nil {
		return
	}
	if p.total > 0 {
		p.current = p.total
	}
	p.write(true)
}

func (p *objectProgress) write(done bool) {
	if p.w == nil {
		return
	}
	if p.total == 0 {
		if done {
			_, _ = io.WriteString(p.w, fmt.Sprintf("\r%s: 100%% (0/0), done.\n", p.title))
		}
		return
	}

	pct := int(uint64(p.current) * 100 / uint64(p.total))
	if pct > 100 {
		pct = 100
	}
	if !done && p.started && pct == p.lastPct {
		return
	}
	p.lastPct = pct
	p.started = true

	buf := fmt.Sprintf("\r%s: %3d%% (%d/%d)", p.title, pct, p.current, p.total)
	if p.showTP && p.bytes != nil {
		n := p.bytes()
		elapsed := time.Since(p.start).Seconds()
		if n > 0 && elapsed > 0 {
			rate := uint64(float64(n) / elapsed)
			buf += fmt.Sprintf(", %s | %s/s", humanBytes(uint64(n)), humanBytes(rate))
		}
	}
	if done {
		buf += ", done.\n"
	}
	_, _ = io.WriteString(p.w, buf)
}

func humanBytes(n uint64) string {
	switch {
	case n >= gib:
		return fmt.Sprintf("%.2f GiB", float64(n)/float64(gib))
	case n >= mib:
		return fmt.Sprintf("%.2f MiB", float64(n)/float64(mib))
	case n >= kib:
		return fmt.Sprintf("%.2f KiB", float64(n)/float64(kib))
	default:
		return fmt.Sprintf("%d bytes", n)
	}
}
