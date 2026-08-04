package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// GitSize represents an integer value with optional k/M/G suffixes.
// Suffixes multiply by 1024 (k), 1048576 (M), or 1073741824 (G).
type GitSize struct {
	raw string
	n   int64
}

// NewGitSize creates a GitSize from a raw string value.
// The string can be a plain integer or include k/M/G suffixes.
func NewGitSize(s string) (GitSize, error) {
	n, err := parseGitSize(s)
	if err != nil {
		return GitSize{}, err
	}
	return GitSize{raw: s, n: n}, nil
}

// NewGitSizeInt creates a GitSize from an int64 value.
func NewGitSizeInt(n int64) GitSize {
	return GitSize{n: n}
}

// UnmarshalGitConfig implements Unmarshaler.
func (s *GitSize) UnmarshalGitConfig(data []byte) error {
	s.raw = string(data)
	n, err := parseGitSize(s.raw)
	if err != nil {
		return err
	}
	s.n = n
	return nil
}

// MarshalGitConfig implements Marshaler.
func (s GitSize) MarshalGitConfig() (string, error) {
	if s.raw != "" {
		return s.raw, nil
	}
	return strconv.FormatInt(s.n, 10), nil
}

func (s GitSize) Int64() int64 { return s.n }

func parseGitSize(s string) (int64, error) {
	if s == "" {
		return 0, nil
	}
	s = strings.TrimSpace(s)
	idx := strings.IndexFunc(s, func(r rune) bool {
		return (r < '0' || r > '9') && r != '-' && r != '+'
	})
	if idx == -1 {
		return strconv.ParseInt(s, 10, 64)
	}
	numPart := s[:idx]
	suffix := strings.ToLower(strings.TrimSpace(s[idx:]))
	n, err := strconv.ParseInt(numPart, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("cannot parse size %q: %w", s, err)
	}
	switch suffix {
	case "k":
		n *= 1024
	case "m":
		n *= 1024 * 1024
	case "g":
		n *= 1024 * 1024 * 1024
	default:
		return 0, fmt.Errorf("unknown size suffix %q in %q", suffix, s)
	}
	return n, nil
}

// BoolOrInt represents a value that can be either a boolean or an integer.
type BoolOrInt struct {
	Bool *bool
	Int  *int64
}

// NewBoolOrIntBool creates a BoolOrInt from a bool value.
func NewBoolOrIntBool(b bool) BoolOrInt {
	return BoolOrInt{Bool: &b}
}

// NewBoolOrIntInt creates a BoolOrInt from an int64 value.
func NewBoolOrIntInt(n int64) BoolOrInt {
	return BoolOrInt{Int: &n}
}

// UnmarshalGitConfig implements Unmarshaler.
func (b *BoolOrInt) UnmarshalGitConfig(data []byte) error {
	s := string(data)
	if v, err := parseBool(s); err == nil {
		b.Bool = &v
		b.Int = nil
		return nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fmt.Errorf("cannot parse %q as bool-or-int", s)
	}
	b.Int = &n
	b.Bool = nil
	return nil
}

func (b BoolOrInt) MarshalGitConfig() (string, error) {
	if b.Bool != nil {
		return formatBool(*b.Bool), nil
	}
	if b.Int != nil {
		return strconv.FormatInt(*b.Int, 10), nil
	}
	return "", nil
}

// AutoBool represents a tri-state boolean: always, true, auto, false, never.
type AutoBool string

const (
	// AutoBoolAlways always enables the feature.
	AutoBoolAlways AutoBool = "always"
	// AutoBoolTrue enables the feature.
	AutoBoolTrue AutoBool = "true"
	// AutoBoolAuto enables the feature only when output is to a terminal.
	AutoBoolAuto AutoBool = "auto"
	// AutoBoolFalse disables the feature.
	AutoBoolFalse AutoBool = "false"
	// AutoBoolNever never enables the feature.
	AutoBoolNever AutoBool = "never"
)

// UnmarshalGitConfig implements Unmarshaler.
func (a *AutoBool) UnmarshalGitConfig(data []byte) error {
	*a = AutoBool(strings.ToLower(string(data)))
	return nil
}

// MarshalGitConfig implements Marshaler.
func (a AutoBool) MarshalGitConfig() (string, error) {
	return string(a), nil
}

// Color represents a Git color specification with optional foreground,
// background, and attributes.
type Color struct {
	Foreground string
	Background string
	Attributes []string
}

// NewColor creates a Color with the given foreground, background, and attributes.
func NewColor(fg, bg string, attrs ...string) Color {
	return Color{Foreground: fg, Background: bg, Attributes: attrs}
}

// UnmarshalGitConfig implements Unmarshaler.
func (c *Color) UnmarshalGitConfig(data []byte) error {
	s := strings.TrimSpace(string(data))
	if s == "" || s == "normal" || s == "reset" {
		if s == "reset" {
			c.Attributes = []string{"reset"}
		}
		return nil
	}
	tokens := strings.Fields(s)
	for _, tok := range tokens {
		lower := strings.ToLower(tok)
		if isColorName(lower) || isColorNumber(tok) || isColorHex(tok) {
			if c.Foreground == "" {
				c.Foreground = tok
			} else if c.Background == "" {
				c.Background = tok
			}
		} else {
			c.Attributes = append(c.Attributes, tok)
		}
	}
	return nil
}

// MarshalGitConfig implements Marshaler.
func (c Color) MarshalGitConfig() (string, error) {
	var parts []string
	if c.Foreground != "" {
		parts = append(parts, c.Foreground)
	}
	if c.Background != "" {
		parts = append(parts, c.Background)
	}
	parts = append(parts, c.Attributes...)
	return strings.Join(parts, " "), nil
}

func isColorName(s string) bool {
	switch s {
	case "normal", "default", "black", "red", "green", "yellow",
		"blue", "magenta", "cyan", "white",
		"brightblack", "brightred", "brightgreen", "brightyellow",
		"brightblue", "brightmagenta", "brightcyan", "brightwhite":
		return true
	}
	return false
}

func isColorNumber(s string) bool {
	n, err := strconv.Atoi(s)
	return err == nil && n >= 0 && n <= 255
}

func isColorHex(s string) bool {
	if len(s) == 0 || s[0] != '#' {
		return false
	}
	hex := s[1:]
	return len(hex) == 3 || len(hex) == 6
}

// ExpiryDate represents a Git expiry-date value. It preserves the raw
// string for round-trip fidelity and supports parsing relative dates
// (e.g. "2.weeks.ago", "90.days") against a caller-supplied reference time.
type ExpiryDate struct {
	raw   string
	Never bool
	Now   bool
}

// NewExpiryDate creates an ExpiryDate from a raw git-config string.
func NewExpiryDate(s string) ExpiryDate {
	e := ExpiryDate{raw: s}
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "never":
		e.Never = true
	case "now":
		e.Now = true
	}
	return e
}

// NewExpiryDateNever creates an ExpiryDate representing "never".
func NewExpiryDateNever() ExpiryDate {
	return ExpiryDate{Never: true}
}

// NewExpiryDateNow creates an ExpiryDate representing "now".
func NewExpiryDateNow() ExpiryDate {
	return ExpiryDate{Now: true}
}

// UnmarshalGitConfig implements Unmarshaler.
func (e *ExpiryDate) UnmarshalGitConfig(data []byte) error {
	e.raw = string(data)
	switch strings.ToLower(strings.TrimSpace(e.raw)) {
	case "never":
		e.Never = true
	case "now":
		e.Now = true
	}
	return nil
}

// MarshalGitConfig implements Marshaler.
func (e ExpiryDate) MarshalGitConfig() (string, error) {
	if e.raw != "" {
		return e.raw, nil
	}
	if e.Never {
		return "never", nil
	}
	if e.Now {
		return "now", nil
	}
	return "", nil
}

// Raw returns the original string value.
func (e ExpiryDate) Raw() string { return e.raw }

// Parse resolves the expiry date into an absolute time relative to now.
// It handles "never" (zero time), "now" (the reference time), relative
// durations like "2.weeks.ago" or "90.days", and RFC3339 timestamps.
// Bare integers are treated as seconds in the past.
func (e ExpiryDate) Parse(now time.Time) (time.Time, error) {
	if e.Never {
		return time.Time{}, nil
	}
	if e.Now {
		return now, nil
	}
	if e.raw == "" {
		return time.Time{}, nil
	}

	s := strings.TrimSpace(e.raw)

	// Try relative date: <count>.<unit>[.ago], e.g. "2.weeks.ago", "90.days", "1.day"
	if d, ok := parseRelativeDuration(s); ok {
		return now.Add(-d), nil
	}

	// Try bare integer (seconds in the past).
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return now.Add(-time.Duration(n) * time.Second), nil
	}

	// Try RFC3339.
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}

	return time.Time{}, fmt.Errorf("invalid expiry date: %q", e.raw)
}

// parseRelativeDuration parses git's relative date format:
// <count>.<unit>[.ago] — e.g. "2.weeks.ago", "90.days", "1.day", "3.months.ago".
func parseRelativeDuration(s string) (time.Duration, bool) {
	parts := strings.Split(strings.ToLower(s), ".")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, false
	}
	if len(parts) == 3 && parts[2] != "ago" {
		return 0, false
	}

	n, err := strconv.ParseFloat(parts[0], 64)
	if err != nil || n <= 0 {
		return 0, false
	}

	var d time.Duration
	switch units := parts[1]; units {
	case "second", "seconds":
		d = time.Duration(n * float64(time.Second))
	case "minute", "minutes":
		d = time.Duration(n * float64(time.Minute))
	case "hour", "hours":
		d = time.Duration(n * float64(time.Hour))
	case "day", "days":
		d = time.Duration(n * float64(24*time.Hour))
	case "week", "weeks":
		d = time.Duration(n * float64(7*24*time.Hour))
	case "month", "months":
		// Approximate a month as 30 days, matching git's behaviour.
		d = time.Duration(n * float64(30*24*time.Hour))
	case "year", "years":
		// Approximate a year as 365 days, matching git's behaviour.
		d = time.Duration(n * float64(365*24*time.Hour))
	default:
		return 0, false
	}

	return d, true
}

// FollowRedirects controls whether Git follows HTTP redirects.
// Valid values are "true" (follow all redirects), "false" (treat redirects
// as errors), and "initial" (follow only the initial request, not
// subsequent ones). The default is "initial".
type FollowRedirects string

const (
	FollowRedirectsAll     FollowRedirects = "true"
	FollowRedirectsNone    FollowRedirects = "false"
	FollowRedirectsInitial FollowRedirects = "initial"
)

// UnmarshalGitConfig implements Unmarshaler.
func (f *FollowRedirects) UnmarshalGitConfig(data []byte) error {
	*f = FollowRedirects(strings.ToLower(string(data)))
	return nil
}

// MarshalGitConfig implements Marshaler.
func (f FollowRedirects) MarshalGitConfig() (string, error) {
	return string(f), nil
}

// Pathname is a string that stores a Git config path value verbatim.
// Tilde expansion and prefix substitution are the caller's responsibility.
type Pathname string

// UnmarshalGitConfig implements Unmarshaler.
func (p *Pathname) UnmarshalGitConfig(data []byte) error {
	*p = Pathname(data)
	return nil
}

// MarshalGitConfig implements Marshaler.
func (p Pathname) MarshalGitConfig() (string, error) {
	return string(p), nil
}

// AdviceConfig maps the advice.* configuration variables.
type AdviceConfig struct {
	AddEmbeddedRepo                    *bool `gitconfig:"addembeddedrepo"`
	AddEmptyPathspec                   *bool `gitconfig:"addemptypathspec"`
	AddIgnoredFile                     *bool `gitconfig:"addignoredfile"`
	AmWorkDir                          *bool `gitconfig:"amworkdir"`
	AmbiguousFetchRefspec              *bool `gitconfig:"ambiguousfetchrefspec"`
	CheckoutAmbiguousRemoteBranchName  *bool `gitconfig:"checkoutambiguousremotebranchname"`
	CommitBeforeMerge                  *bool `gitconfig:"commitbeforemerge"`
	DetachedHead                       *bool `gitconfig:"detachedhead"`
	Diverging                          *bool `gitconfig:"diverging"`
	FetchShowForcedUpdates             *bool `gitconfig:"fetchshowforcedupdates"`
	ForceDeleteBranch                  *bool `gitconfig:"forcedeletebranch"`
	IgnoredHook                        *bool `gitconfig:"ignoredhook"`
	ImplicitIdentity                   *bool `gitconfig:"implicitidentity"`
	MergeConflict                      *bool `gitconfig:"mergeconflict"`
	NestedTag                          *bool `gitconfig:"nestedtag"`
	PushAlreadyExists                  *bool `gitconfig:"pushalreadyexists"`
	PushFetchFirst                     *bool `gitconfig:"pushfetchfirst"`
	PushNeedsForce                     *bool `gitconfig:"pushneedsforce"`
	PushNonFFCurrent                   *bool `gitconfig:"pushnonffcurrent"`
	PushNonFFMatching                  *bool `gitconfig:"pushnonffmatching"`
	PushRefNeedsUpdate                 *bool `gitconfig:"pushrefneedsupdate"`
	PushUnqualifiedRefname             *bool `gitconfig:"pushunqualifiedrefname"`
	PushUpdateRejected                 *bool `gitconfig:"pushupdaterejected"`
	RebaseTodoError                    *bool `gitconfig:"rebasetodoerror"`
	RefSyntax                          *bool `gitconfig:"refsyntax"`
	ResetNoRefresh                     *bool `gitconfig:"resetnorefresh"`
	ResolveConflict                    *bool `gitconfig:"resolveconflict"`
	RmHints                            *bool `gitconfig:"rmhints"`
	SequencerInUse                     *bool `gitconfig:"sequencerinuse"`
	SkippedCherryPicks                 *bool `gitconfig:"skippedcherrypicks"`
	SparseIndexExpanded                *bool `gitconfig:"sparseindexexpanded"`
	StatusAheadBehind                  *bool `gitconfig:"statusaheadbehind"`
	StatusHints                        *bool `gitconfig:"statushints"`
	StatusUoption                      *bool `gitconfig:"statusuoption"`
	SubmoduleAlternateErrorStrategyDie *bool `gitconfig:"submodulealternateerrorstrategydie"`
	SubmoduleMergeConflict             *bool `gitconfig:"submodulemergeconflict"`
	SubmodulesNotUpdated               *bool `gitconfig:"submodulesnotupdated"`
	SuggestDetachingHead               *bool `gitconfig:"suggestdetachinghead"`
	UpdateSparsePath                   *bool `gitconfig:"updatesparsepath"`
	WaitingForEditor                   *bool `gitconfig:"waitingforeditor"`
	WorktreeAddOrphan                  *bool `gitconfig:"worktreeaddorphan"`
}

// AliasConfig maps an alias.<name> subsection.
type AliasConfig struct {
	Command string `gitconfig:"command"`
}

// BranchConfig maps a branch.<name> subsection.
type BranchConfig struct {
	Remote       string `gitconfig:"remote"`
	PushRemote   string `gitconfig:"pushremote"`
	Merge        string `gitconfig:"merge"`
	MergeOptions string `gitconfig:"mergeoptions"`
	Rebase       string `gitconfig:"rebase"`
	Description  string `gitconfig:"description"`
}

// BrowserConfig maps a browser.<tool> subsection.
type BrowserConfig struct {
	Cmd  string `gitconfig:"cmd"`
	Path string `gitconfig:"path"`
}

// CheckoutConfig maps the checkout.* section.
type CheckoutConfig struct {
	DefaultRemote           string `gitconfig:"defaultremote"`
	Guess                   *bool  `gitconfig:"guess"`
	Workers                 int    `gitconfig:"workers"`
	ThresholdForParallelism int    `gitconfig:"thresholdforparallelism"`
}

// CleanConfig maps the clean.* section.
type CleanConfig struct {
	RequireForce *bool `gitconfig:"requireforce" gitconfigDefault:"true"`
}

// CloneConfig maps the clone.* section.
type CloneConfig struct {
	DefaultRemoteName string `gitconfig:"defaultremotename" gitconfigDefault:"origin"`
	RejectShallow     *bool  `gitconfig:"rejectshallow"`
	FilterSubmodules  *bool  `gitconfig:"filtersubmodules"`
}

// ColorConfig maps the color.* section variables.
type ColorConfig struct {
	UI          *AutoBool `gitconfig:"ui"`
	Advice      *AutoBool `gitconfig:"advice"`
	Branch      *AutoBool `gitconfig:"branch"`
	Diff        *AutoBool `gitconfig:"diff"`
	Grep        *AutoBool `gitconfig:"grep"`
	Interactive *AutoBool `gitconfig:"interactive"`
	Pager       *bool     `gitconfig:"pager"`
	Push        *AutoBool `gitconfig:"push"`
	Remote      *AutoBool `gitconfig:"remote"`
	ShowBranch  *AutoBool `gitconfig:"showbranch"`
	Status      *AutoBool `gitconfig:"status"`
	Transport   *AutoBool `gitconfig:"transport"`
}

// ColorBranchSlots maps the [color "branch"] subsection.
type ColorBranchSlots struct {
	Current  *Color `gitconfig:"current"`
	Local    *Color `gitconfig:"local"`
	Remote   *Color `gitconfig:"remote"`
	Upstream *Color `gitconfig:"upstream"`
	Plain    *Color `gitconfig:"plain"`
}

// ColorDiffSlots maps the [color "diff"] subsection.
type ColorDiffSlots struct {
	Context                   *Color `gitconfig:"context"`
	Meta                      *Color `gitconfig:"meta"`
	Frag                      *Color `gitconfig:"frag"`
	Func                      *Color `gitconfig:"func"`
	Old                       *Color `gitconfig:"old"`
	New                       *Color `gitconfig:"new"`
	Commit                    *Color `gitconfig:"commit"`
	Whitespace                *Color `gitconfig:"whitespace"`
	OldMoved                  *Color `gitconfig:"oldmoved"`
	NewMoved                  *Color `gitconfig:"newmoved"`
	OldMovedDimmed            *Color `gitconfig:"oldmoveddimmed"`
	OldMovedAlternative       *Color `gitconfig:"oldmovedalternative"`
	OldMovedAlternativeDimmed *Color `gitconfig:"oldmovedalternativedimmed"`
	NewMovedDimmed            *Color `gitconfig:"newmoveddimmed"`
	NewMovedAlternative       *Color `gitconfig:"newmovedalternative"`
	NewMovedAlternativeDimmed *Color `gitconfig:"newmovedalternativedimmed"`
	ContextDimmed             *Color `gitconfig:"contextdimmed"`
	OldDimmed                 *Color `gitconfig:"olddimmed"`
	NewDimmed                 *Color `gitconfig:"newdimmed"`
	ContextBold               *Color `gitconfig:"contextbold"`
	OldBold                   *Color `gitconfig:"oldbold"`
	NewBold                   *Color `gitconfig:"newbold"`
}

// ColorDecorateSlots maps the [color "decorate"] subsection.
type ColorDecorateSlots struct {
	Branch       *Color `gitconfig:"branch"`
	RemoteBranch *Color `gitconfig:"remotebranch"`
	Tag          *Color `gitconfig:"tag"`
	Stash        *Color `gitconfig:"stash"`
	HEAD         *Color `gitconfig:"head"`
	Grafted      *Color `gitconfig:"grafted"`
}

// ColorGrepSlots maps the [color "grep"] subsection.
type ColorGrepSlots struct {
	Context       *Color `gitconfig:"context"`
	Filename      *Color `gitconfig:"filename"`
	Function      *Color `gitconfig:"function"`
	LineNumber    *Color `gitconfig:"linenumber"`
	Column        *Color `gitconfig:"column"`
	Match         *Color `gitconfig:"match"`
	MatchContext  *Color `gitconfig:"matchcontext"`
	MatchSelected *Color `gitconfig:"matchselected"`
	Selected      *Color `gitconfig:"selected"`
	Separator     *Color `gitconfig:"separator"`
}

// ColorInteractiveSlots maps the [color "interactive"] subsection.
type ColorInteractiveSlots struct {
	Prompt *Color `gitconfig:"prompt"`
	Header *Color `gitconfig:"header"`
	Help   *Color `gitconfig:"help"`
	Error  *Color `gitconfig:"error"`
}

// ColorPushSlots maps the [color "push"] subsection.
type ColorPushSlots struct {
	Error *Color `gitconfig:"error"`
}

// ColorRemoteSlots maps the [color "remote"] subsection.
type ColorRemoteSlots struct {
	Hint    *Color `gitconfig:"hint"`
	Warning *Color `gitconfig:"warning"`
	Success *Color `gitconfig:"success"`
	Error   *Color `gitconfig:"error"`
}

// ColorStatusSlots maps the [color "status"] subsection.
type ColorStatusSlots struct {
	Header       *Color `gitconfig:"header"`
	Added        *Color `gitconfig:"added"`
	Updated      *Color `gitconfig:"updated"`
	Changed      *Color `gitconfig:"changed"`
	Untracked    *Color `gitconfig:"untracked"`
	Branch       *Color `gitconfig:"branch"`
	NoBranch     *Color `gitconfig:"nobranch"`
	LocalBranch  *Color `gitconfig:"localbranch"`
	RemoteBranch *Color `gitconfig:"remotebranch"`
	Unmerged     *Color `gitconfig:"unmerged"`
}

// ColorTransportSlots maps the [color "transport"] subsection.
type ColorTransportSlots struct {
	Rejected *Color `gitconfig:"rejected"`
}

// ColumnConfig maps the column.* section.
type ColumnConfig struct {
	UI     string `gitconfig:"ui"`
	Branch string `gitconfig:"branch"`
	Clean  string `gitconfig:"clean"`
	Status string `gitconfig:"status"`
	Tag    string `gitconfig:"tag"`
}

// CommitConfig maps the commit.* section.
type CommitConfig struct {
	Cleanup  string    `gitconfig:"cleanup"`
	GPGSign  *bool     `gitconfig:"gpgsign"`
	Status   *bool     `gitconfig:"status" gitconfigDefault:"true"`
	Template Pathname  `gitconfig:"template"`
	Verbose  BoolOrInt `gitconfig:"verbose"`
}

// CommitGraphConfig maps the commitGraph.* section.
type CommitGraphConfig struct {
	GenerationVersion int   `gitconfig:"generationversion" gitconfigDefault:"2"`
	MaxNewFilters     int   `gitconfig:"maxnewfilters"`
	ChangedPaths      *bool `gitconfig:"changedpaths"`
	ReadChangedPaths  int   `gitconfig:"changedpathsversion" gitconfigDefault:"-1"`
}

// CoreConfig maps the core.* section.
type CoreConfig struct {
	RepositoryFormatVersion int      `gitconfig:"repositoryformatversion"`
	Bare                    bool     `gitconfig:"bare"`
	Worktree                Pathname `gitconfig:"worktree"`
	LogAllRefUpdates        *bool    `gitconfig:"logallrefupdates"`
	CommentChar             string   `gitconfig:"commentchar"`
	CommentString           string   `gitconfig:"commentstring"`
	FileMode                *bool    `gitconfig:"filemode"`
	IgnoreCase              *bool    `gitconfig:"ignorecase"`
	PrecomposeUnicode       *bool    `gitconfig:"precomposeunicode"`
	ProtectHFS              *bool    `gitconfig:"protecthfs"`
	ProtectNTFS             *bool    `gitconfig:"protectntfs"`
	HideDotFiles            string   `gitconfig:"hidedotfiles"`
	FSMonitor               string   `gitconfig:"fsmonitor"`
	FSMonitorHookVersion    string   `gitconfig:"fsmonitorhookversion"`
	TrustCTime              *bool    `gitconfig:"trustctime" gitconfigDefault:"true"`
	SplitIndex              *bool    `gitconfig:"splitindex"`
	UntrackedCache          string   `gitconfig:"untrackedcache"`
	CheckStat               string   `gitconfig:"checkstat"`
	QuotePath               *bool    `gitconfig:"quotepath" gitconfigDefault:"true"`
	EOL                     string   `gitconfig:"eol"`
	SafeCRLF                string   `gitconfig:"safecrlf"`
	AutoCRLF                string   `gitconfig:"autocrlf"`
	CheckRoundtripEncoding  string   `gitconfig:"checkroundtripencoding"`
	Symlinks                *bool    `gitconfig:"symlinks"`
	GitProxy                []string `gitconfig:"gitproxy,multivalue"`
	SSHCommand              string   `gitconfig:"sshcommand"`
	IgnoreStat              *bool    `gitconfig:"ignorestat"`
	PreferSymlinkRefs       *bool    `gitconfig:"prefersymlinkrefs"`
	AlternateRefsCommand    string   `gitconfig:"alternaterefscommand"`
	AlternateRefsPrefixes   string   `gitconfig:"alternaterefsprefixes"`
	SharedRepository        string   `gitconfig:"sharedrepository"`
	WarnAmbiguousRefs       *bool    `gitconfig:"warnambiguousrefs" gitconfigDefault:"true"`
	Compression             int      `gitconfig:"compression"`
	LooseCompression        int      `gitconfig:"loosecompression"`
	PackedGitWindowSize     GitSize  `gitconfig:"packedgitwindowsize"`
	PackedGitLimit          GitSize  `gitconfig:"packedgitlimit"`
	DeltaBaseCacheLimit     GitSize  `gitconfig:"deltabasecachelimit"`
	BigFileThreshold        GitSize  `gitconfig:"bigfilethreshold" gitconfigDefault:"512m"`
	ExcludesFile            Pathname `gitconfig:"excludesfile"`
	AskPass                 string   `gitconfig:"askpass"`
	AttributesFile          Pathname `gitconfig:"attributesfile"`
	HooksPath               Pathname `gitconfig:"hookspath"`
	Editor                  string   `gitconfig:"editor"`
	FilesRefLockTimeout     int      `gitconfig:"filesreflocktimeout" gitconfigDefault:"100"`
	PackedRefsTimeout       int      `gitconfig:"packedrefstimeout" gitconfigDefault:"1000"`
	Pager                   string   `gitconfig:"pager"`
	Whitespace              string   `gitconfig:"whitespace"`
	Fsync                   string   `gitconfig:"fsync"`
	FsyncMethod             string   `gitconfig:"fsyncmethod"`
	FsyncObjectFiles        *bool    `gitconfig:"fsyncobjectfiles"`
	PreloadIndex            *bool    `gitconfig:"preloadindex" gitconfigDefault:"true"`
	UnsetEnvVars            string   `gitconfig:"unsetenvvars"`
	CreateObject            string   `gitconfig:"createobject"`
	NotesRef                string   `gitconfig:"notesref" gitconfigDefault:"refs/notes/commits"`
	CommitGraph             *bool    `gitconfig:"commitgraph" gitconfigDefault:"true"`
	UseReplaceRefs          *bool    `gitconfig:"usereplacerefs" gitconfigDefault:"true"`
	MultiPackIndex          *bool    `gitconfig:"multipackindex" gitconfigDefault:"true"`
	SparseCheckout          *bool    `gitconfig:"sparsecheckout"`
	SparseCheckoutCone      *bool    `gitconfig:"sparsecheckoutcone"`
	Abbrev                  string   `gitconfig:"abbrev"`
	MaxTreeDepth            int      `gitconfig:"maxtreedepth"`
}

// CredentialConfig maps the credential.* section.
type CredentialConfig struct {
	Helper          []string `gitconfig:"helper,multivalue"`
	Interactive     *bool    `gitconfig:"interactive"`
	UseHTTPPath     *bool    `gitconfig:"usehttppath"`
	SanitizePrompt  *bool    `gitconfig:"sanitizeprompt" gitconfigDefault:"true"`
	ProtectProtocol *bool    `gitconfig:"protectprotocol" gitconfigDefault:"true"`
	Username        string   `gitconfig:"username"`
}

// CredentialURLConfig maps a credential.<url>.* subsection.
type CredentialURLConfig struct {
	Helper      []string `gitconfig:"helper,multivalue"`
	Interactive *bool    `gitconfig:"interactive"`
	UseHTTPPath *bool    `gitconfig:"usehttppath"`
	Username    string   `gitconfig:"username"`
}

// DiffConfig maps the diff.* section.
type DiffConfig struct {
	AutoRefreshIndex   *bool    `gitconfig:"autorefreshindex" gitconfigDefault:"true"`
	DirStat            string   `gitconfig:"dirstat"`
	StatNameWidth      int      `gitconfig:"statnamewidth"`
	StatGraphWidth     int      `gitconfig:"statgraphwidth"`
	Context            int      `gitconfig:"context" gitconfigDefault:"3"`
	InterHunkContext   int      `gitconfig:"interhunkcontext"`
	External           string   `gitconfig:"external"`
	TrustExitCode      *bool    `gitconfig:"trustexitcode"`
	IgnoreSubmodules   string   `gitconfig:"ignoresubmodules"`
	MnemonicPrefix     *bool    `gitconfig:"mnemonicprefix"`
	NoPrefix           *bool    `gitconfig:"noprefix"`
	SrcPrefix          string   `gitconfig:"srcprefix"`
	DstPrefix          string   `gitconfig:"dstprefix"`
	Relative           *bool    `gitconfig:"relative"`
	OrderFile          Pathname `gitconfig:"orderfile"`
	RenameLimit        int      `gitconfig:"renamelimit"`
	Renames            string   `gitconfig:"renames" gitconfigDefault:"true"`
	SuppressBlankEmpty *bool    `gitconfig:"suppressblankempty"`
	Submodule          string   `gitconfig:"submodule" gitconfigDefault:"short"`
	WordRegex          string   `gitconfig:"wordregex"`
	IndentHeuristic    *bool    `gitconfig:"indentheuristic" gitconfigDefault:"true"`
	Algorithm          string   `gitconfig:"algorithm"`
	WSErrorHighlight   string   `gitconfig:"wserrorhighlight"`
	ColorMoved         string   `gitconfig:"colormoved"`
	ColorMovedWS       string   `gitconfig:"colormovedws"`
	Tool               string   `gitconfig:"tool"`
	GUITool            string   `gitconfig:"guitool"`
}

// DiffDriverConfig maps a diff.<driver>.* subsection.
type DiffDriverConfig struct {
	Command       string `gitconfig:"command"`
	TrustExitCode *bool  `gitconfig:"trustexitcode"`
	XFuncName     string `gitconfig:"xfuncname"`
	Binary        *bool  `gitconfig:"binary"`
	TextConv      string `gitconfig:"textconv"`
	WordRegex     string `gitconfig:"wordregex"`
	CacheTextConv *bool  `gitconfig:"cachetextconv"`
}

// DifftoolConfig maps a difftool.<tool>.* subsection.
type DifftoolConfig struct {
	Cmd           string `gitconfig:"cmd"`
	Path          string `gitconfig:"path"`
	TrustExitCode *bool  `gitconfig:"trustexitcode"`
	Prompt        *bool  `gitconfig:"prompt"`
	GUIDefault    string `gitconfig:"guidefault"`
}

// ExtensionsConfig maps the extensions.* section.
type ExtensionsConfig struct {
	ObjectFormat       string `gitconfig:"objectformat"`
	CompatObjectFormat string `gitconfig:"compatobjectformat"`
	Noop               *bool  `gitconfig:"noop"`
	NoopV1             *bool  `gitconfig:"noop-v1"`
	PartialClone       string `gitconfig:"partialclone"`
	PreciousObjects    *bool  `gitconfig:"preciousobjects"`
	RefStorage         string `gitconfig:"refstorage"`
	RelativeWorktrees  *bool  `gitconfig:"relativeworktrees"`
	WorktreeConfig     *bool  `gitconfig:"worktreeconfig"`
}

// FeatureConfig maps the feature.* section.
type FeatureConfig struct {
	Experimental *bool `gitconfig:"experimental"`
	ManyFiles    *bool `gitconfig:"manyfiles"`
}

// FetchConfig maps the fetch.* section.
type FetchConfig struct {
	RecurseSubmodules    string `gitconfig:"recursesubmodules" gitconfigDefault:"on-demand"`
	FsckObjects          *bool  `gitconfig:"fsckobjects"`
	UnpackLimit          int    `gitconfig:"unpacklimit"`
	Prune                *bool  `gitconfig:"prune"`
	PruneTags            *bool  `gitconfig:"prunetags"`
	All                  *bool  `gitconfig:"all"`
	Output               string `gitconfig:"output"`
	NegotiationAlgorithm string `gitconfig:"negotiationalgorithm"`
	ShowForcedUpdates    *bool  `gitconfig:"showforcedupdates" gitconfigDefault:"true"`
	Parallel             int    `gitconfig:"parallel"`
	WriteCommitGraph     *bool  `gitconfig:"writecommitgraph"`
	BundleURI            string `gitconfig:"bundleuri"`
	BundleCreationToken  string `gitconfig:"bundlecreationtoken"`
}

// FilterDriverConfig maps a filter.<driver>.* subsection.
type FilterDriverConfig struct {
	Clean  string `gitconfig:"clean"`
	Smudge string `gitconfig:"smudge"`
}

// FormatConfig maps the format.* section.
type FormatConfig struct {
	Attach               string   `gitconfig:"attach"`
	From                 string   `gitconfig:"from"`
	ForceInBodyFrom      *bool    `gitconfig:"forceinbodyfrom"`
	Numbered             string   `gitconfig:"numbered"`
	Headers              []string `gitconfig:"headers,multivalue"`
	To                   []string `gitconfig:"to,multivalue"`
	CC                   []string `gitconfig:"cc,multivalue"`
	SubjectPrefix        string   `gitconfig:"subjectprefix"`
	CoverFromDescription string   `gitconfig:"coverfromdescription"`
	Signature            string   `gitconfig:"signature"`
	SignatureFile        Pathname `gitconfig:"signaturefile"`
	Suffix               string   `gitconfig:"suffix" gitconfigDefault:".patch"`
	EncodeEmailHeaders   *bool    `gitconfig:"encodeemailheaders" gitconfigDefault:"true"`
	Pretty               string   `gitconfig:"pretty"`
	Thread               string   `gitconfig:"thread"`
	SignOff              *bool    `gitconfig:"signoff"`
	CoverLetter          string   `gitconfig:"coverletter"`
	OutputDirectory      Pathname `gitconfig:"outputdirectory"`
	FilenameMaxLength    int      `gitconfig:"filenamemaxlength" gitconfigDefault:"64"`
	UseAutoBase          string   `gitconfig:"useautobase"`
	Notes                string   `gitconfig:"notes"`
	MboxRD               *bool    `gitconfig:"mboxrd"`
	NoPrefix             *bool    `gitconfig:"noprefix"`
}

// FsckConfig maps the fsck.* section.
type FsckConfig struct {
	SkipList Pathname `gitconfig:"skiplist"`
}

// FsmonitorConfig maps the fsmonitor.* section.
type FsmonitorConfig struct {
	AllowRemote *bool  `gitconfig:"allowremote"`
	SocketDir   string `gitconfig:"socketdir"`
}

// GcConfig maps the gc.* section.
type GcConfig struct {
	AggressiveDepth         int        `gitconfig:"aggressivedepth" gitconfigDefault:"50"`
	AggressiveWindow        int        `gitconfig:"aggressivewindow" gitconfigDefault:"250"`
	Auto                    int        `gitconfig:"auto" gitconfigDefault:"6700"`
	AutoPackLimit           int        `gitconfig:"autopacklimit" gitconfigDefault:"50"`
	AutoDetach              *bool      `gitconfig:"autodetach" gitconfigDefault:"true"`
	BigPackThreshold        GitSize    `gitconfig:"bigpackthreshold"`
	WriteCommitGraph        *bool      `gitconfig:"writecommitgraph" gitconfigDefault:"true"`
	LogExpiry               ExpiryDate `gitconfig:"logexpiry" gitconfigDefault:"1.day"`
	PackRefs                string     `gitconfig:"packrefs" gitconfigDefault:"true"`
	CruftPacks              *bool      `gitconfig:"cruftpacks" gitconfigDefault:"true"`
	MaxCruftSize            GitSize    `gitconfig:"maxcruftsize"`
	PruneExpire             ExpiryDate `gitconfig:"pruneexpire" gitconfigDefault:"2.weeks.ago"`
	WorktreePruneExpire     ExpiryDate `gitconfig:"worktreepruneexpire" gitconfigDefault:"3.months.ago"`
	ReflogExpire            ExpiryDate `gitconfig:"reflogexpire" gitconfigDefault:"90.days"`
	ReflogExpireUnreachable ExpiryDate `gitconfig:"reflogexpireunreachable" gitconfigDefault:"30.days"`
	RecentObjectsHook       string     `gitconfig:"recentobjectshook"`
	RepackFilter            string     `gitconfig:"repackfilter"`
	RepackFilterTo          string     `gitconfig:"repackfilterto"`
	RerereResolved          ExpiryDate `gitconfig:"rerereresolved" gitconfigDefault:"60.days"`
	RerereUnresolved        ExpiryDate `gitconfig:"rerereunresolved" gitconfigDefault:"15.days"`
}

// GPGConfig maps the gpg.* section.
type GPGConfig struct {
	Program       string `gitconfig:"program"`
	Format        string `gitconfig:"format"`
	MinTrustLevel string `gitconfig:"mintrustlevel"`
}

// GPGSSHConfig maps the [gpg "ssh"] subsection.
type GPGSSHConfig struct {
	Program            string   `gitconfig:"program"`
	DefaultKeyCommand  string   `gitconfig:"defaultkeycommand"`
	AllowedSignersFile Pathname `gitconfig:"allowedsignersfile"`
	RevocationFile     Pathname `gitconfig:"revocationfile"`
}

// GPGFormatConfig maps a gpg.<format>.* subsection.
type GPGFormatConfig struct {
	Program string `gitconfig:"program"`
}

// GrepConfig maps the grep.* section.
type GrepConfig struct {
	LineNumber        *bool  `gitconfig:"linenumber"`
	Column            *bool  `gitconfig:"column"`
	PatternType       string `gitconfig:"patterntype"`
	ExtendedRegexp    *bool  `gitconfig:"extendedregexp"`
	Threads           int    `gitconfig:"threads"`
	FullName          *bool  `gitconfig:"fullname"`
	FallbackToNoIndex *bool  `gitconfig:"fallbacktonoindex"`
}

// HelpConfig maps the help.* section.
type HelpConfig struct {
	Browser     string `gitconfig:"browser"`
	Format      string `gitconfig:"format"`
	AutoCorrect string `gitconfig:"autocorrect"`
	HTMLPath    string `gitconfig:"htmlpath"`
}

// HTTPConfig maps the http.* section.
type HTTPConfig struct {
	Proxy                         string          `gitconfig:"proxy"`
	ProxyAuthMethod               string          `gitconfig:"proxyauthmethod"`
	ProxySSLCert                  Pathname        `gitconfig:"proxysslcert"`
	ProxySSLKey                   Pathname        `gitconfig:"proxysslkey"`
	ProxySSLCertPasswordProtected *bool           `gitconfig:"proxysslcertpasswordprotected"`
	ProxySSLCAInfo                Pathname        `gitconfig:"proxysslcainfo"`
	EmptyAuth                     *bool           `gitconfig:"emptyauth"`
	ProactiveAuth                 string          `gitconfig:"proactiveauth"`
	Delegation                    string          `gitconfig:"delegation"`
	ExtraHeader                   []string        `gitconfig:"extraheader,multivalue"`
	CookieFile                    Pathname        `gitconfig:"cookiefile"`
	SaveCookies                   *bool           `gitconfig:"savecookies"`
	Version                       string          `gitconfig:"version"`
	CurloptResolve                []string        `gitconfig:"curloptresolve,multivalue"`
	SSLVersion                    string          `gitconfig:"sslversion"`
	SSLCipherList                 string          `gitconfig:"sslcipherlist"`
	SSLVerify                     *bool           `gitconfig:"sslverify" gitconfigDefault:"true"`
	SSLCert                       Pathname        `gitconfig:"sslcert"`
	SSLKey                        Pathname        `gitconfig:"sslkey"`
	SSLCertPasswordProtected      *bool           `gitconfig:"sslcertpasswordprotected"`
	SSLCAInfo                     Pathname        `gitconfig:"sslcainfo"`
	SSLCAPath                     Pathname        `gitconfig:"sslcapath"`
	SSLBackend                    string          `gitconfig:"sslbackend"`
	SSLCertType                   string          `gitconfig:"sslcerttype"`
	SSLKeyType                    string          `gitconfig:"sslkeytype"`
	SchannelCheckRevoke           *bool           `gitconfig:"schannelcheckrevoke" gitconfigDefault:"true"`
	SchannelUseSSLCAInfo          *bool           `gitconfig:"schannelusesslcainfo"`
	PinnedPubkey                  string          `gitconfig:"pinnedpubkey"`
	SSLTry                        *bool           `gitconfig:"ssltry"`
	MaxRequests                   int             `gitconfig:"maxrequests" gitconfigDefault:"5"`
	MinSessions                   int             `gitconfig:"minsessions" gitconfigDefault:"1"`
	PostBuffer                    GitSize         `gitconfig:"postbuffer" gitconfigDefault:"1m"`
	LowSpeedLimit                 int             `gitconfig:"lowspeedlimit"`
	LowSpeedTime                  int             `gitconfig:"lowspeedtime"`
	KeepAliveIdle                 int             `gitconfig:"keepaliveidle"`
	KeepAliveInterval             int             `gitconfig:"keepaliveinterval"`
	KeepAliveCount                int             `gitconfig:"keepalivecount"`
	NoEPSV                        *bool           `gitconfig:"noepsv"`
	UserAgent                     string          `gitconfig:"useragent"`
	FollowRedirects               FollowRedirects `gitconfig:"followredirects" gitconfigDefault:"initial"`
}

// HTTPURLConfig maps an http.<url>.* subsection.
type HTTPURLConfig struct {
	SSLVerify   *bool    `gitconfig:"sslverify"`
	SSLCert     Pathname `gitconfig:"sslcert"`
	SSLKey      Pathname `gitconfig:"sslkey"`
	SSLCAInfo   Pathname `gitconfig:"sslcainfo"`
	SSLCAPath   Pathname `gitconfig:"sslcapath"`
	Proxy       string   `gitconfig:"proxy"`
	CookieFile  Pathname `gitconfig:"cookiefile"`
	PostBuffer  GitSize  `gitconfig:"postbuffer"`
	ExtraHeader []string `gitconfig:"extraheader,multivalue"`
}

// I18nConfig maps the i18n.* section.
type I18nConfig struct {
	CommitEncoding    string `gitconfig:"commitencoding" gitconfigDefault:"utf-8"`
	LogOutputEncoding string `gitconfig:"logoutputencoding"`
}

// IndexConfig maps the index.* section.
type IndexConfig struct {
	RecordEndOfIndexEntries *bool  `gitconfig:"recordendofindexentries"`
	RecordOffsetTable       *bool  `gitconfig:"recordoffsettable"`
	Sparse                  *bool  `gitconfig:"sparse"`
	Threads                 string `gitconfig:"threads" gitconfigDefault:"true"`
	Version                 int    `gitconfig:"version"`
	SkipHash                *bool  `gitconfig:"skiphash"`
}

// InitConfig maps the init.* section.
type InitConfig struct {
	TemplateDir         Pathname `gitconfig:"templatedir"`
	DefaultBranch       string   `gitconfig:"defaultbranch"`
	DefaultObjectFormat string   `gitconfig:"defaultobjectformat"`
	DefaultRefFormat    string   `gitconfig:"defaultrefformat"`
}

// InteractiveConfig maps the interactive.* section.
type InteractiveConfig struct {
	SingleKey  *bool  `gitconfig:"singlekey"`
	DiffFilter string `gitconfig:"difffilter"`
}

// LogConfig maps the log.* section.
type LogConfig struct {
	AbbrevCommit         *bool    `gitconfig:"abbrevcommit"`
	Date                 string   `gitconfig:"date"`
	Decorate             string   `gitconfig:"decorate"`
	InitialDecorationSet string   `gitconfig:"initialdecorationset"`
	ExcludeDecoration    []string `gitconfig:"excludedecoration,multivalue"`
	DiffMerges           string   `gitconfig:"diffmerges" gitconfigDefault:"separate"`
	Follow               *bool    `gitconfig:"follow"`
	GraphColors          string   `gitconfig:"graphcolors"`
	ShowRoot             *bool    `gitconfig:"showroot" gitconfigDefault:"true"`
	ShowSignature        *bool    `gitconfig:"showsignature"`
	Mailmap              *bool    `gitconfig:"mailmap" gitconfigDefault:"true"`
}

// MaintenanceConfig maps the maintenance.* section.
type MaintenanceConfig struct {
	Auto       *bool  `gitconfig:"auto" gitconfigDefault:"true"`
	AutoDetach *bool  `gitconfig:"autodetach" gitconfigDefault:"true"`
	Strategy   string `gitconfig:"strategy"`
}

// MaintenanceTaskConfig maps a maintenance.<task>.* subsection.
type MaintenanceTaskConfig struct {
	Enabled  *bool  `gitconfig:"enabled"`
	Schedule string `gitconfig:"schedule"`
}

// MergeConfig maps the merge.* section.
type MergeConfig struct {
	ConflictStyle     string    `gitconfig:"conflictstyle"`
	DefaultToUpstream *bool     `gitconfig:"defaulttoupstream" gitconfigDefault:"true"`
	FF                string    `gitconfig:"ff"`
	VerifySignatures  *bool     `gitconfig:"verifysignatures"`
	BranchDesc        *bool     `gitconfig:"branchdesc"`
	Log               BoolOrInt `gitconfig:"log"`
	SuppressDest      []string  `gitconfig:"suppressdest,multivalue"`
	RenameLimit       int       `gitconfig:"renamelimit"`
	Renames           string    `gitconfig:"renames"`
	DirectoryRenames  string    `gitconfig:"directoryrenames" gitconfigDefault:"conflict"`
	Renormalize       *bool     `gitconfig:"renormalize"`
	Stat              string    `gitconfig:"stat" gitconfigDefault:"true"`
	AutoStash         *bool     `gitconfig:"autostash"`
	Tool              string    `gitconfig:"tool"`
	GUITool           string    `gitconfig:"guitool"`
	Verbosity         int       `gitconfig:"verbosity" gitconfigDefault:"2"`
}

// MergeDriverConfig maps a merge.<driver>.* subsection.
type MergeDriverConfig struct {
	Name      string `gitconfig:"name"`
	Driver    string `gitconfig:"driver"`
	Recursive string `gitconfig:"recursive"`
}

// MergetoolConfig maps a mergetool.<tool>.* subsection.
type MergetoolConfig struct {
	Path          string `gitconfig:"path"`
	Cmd           string `gitconfig:"cmd"`
	HideResolved  *bool  `gitconfig:"hideresolved"`
	TrustExitCode *bool  `gitconfig:"trustexitcode"`
}

// MergetoolGlobals maps the mergetool.* section-level variables.
type MergetoolGlobals struct {
	KeepBackup      *bool  `gitconfig:"keepbackup" gitconfigDefault:"true"`
	KeepTemporaries *bool  `gitconfig:"keeptemporaries"`
	WriteToTemp     *bool  `gitconfig:"writetotemp"`
	Prompt          *bool  `gitconfig:"prompt" gitconfigDefault:"true"`
	GUIDefault      string `gitconfig:"guidefault"`
}

// NotesConfig maps the notes.* section.
type NotesConfig struct {
	MergeStrategy string   `gitconfig:"mergestrategy"`
	DisplayRef    []string `gitconfig:"displayref,multivalue"`
	RewriteMode   string   `gitconfig:"rewritemode"`
	RewriteRef    []string `gitconfig:"rewriteref,multivalue"`
}

// NotesNameConfig maps a notes.<name>.* subsection.
type NotesNameConfig struct {
	MergeStrategy string `gitconfig:"mergestrategy"`
}

// PackConfig maps the pack.* section.
type PackConfig struct {
	Window                     int     `gitconfig:"window" gitconfigDefault:"10"`
	Depth                      int     `gitconfig:"depth" gitconfigDefault:"50"`
	WindowMemory               GitSize `gitconfig:"windowmemory"`
	Compression                int     `gitconfig:"compression"`
	AllowPackReuse             string  `gitconfig:"allowpackreuse" gitconfigDefault:"true"`
	Island                     string  `gitconfig:"island"`
	IslandCore                 string  `gitconfig:"islandcore"`
	DeltaCacheSize             GitSize `gitconfig:"deltacachesize" gitconfigDefault:"256m"`
	DeltaCacheLimit            int     `gitconfig:"deltacachelimit" gitconfigDefault:"1000"`
	Threads                    int     `gitconfig:"threads"`
	IndexVersion               int     `gitconfig:"indexversion" gitconfigDefault:"2"`
	PackSizeLimit              GitSize `gitconfig:"packsizelimit"`
	UseBitmaps                 *bool   `gitconfig:"usebitmaps" gitconfigDefault:"true"`
	UseBitmapBoundaryTraversal *bool   `gitconfig:"usebitmapboundarytraversal"`
	UseSparse                  *bool   `gitconfig:"usesparse" gitconfigDefault:"true"`
	UsePathWalk                *bool   `gitconfig:"usepathwalk"`
	PreferBitmapTips           string  `gitconfig:"preferbitmaptips"`
	WriteBitmapHashCache       *bool   `gitconfig:"writebitmaphashcache" gitconfigDefault:"true"`
	WriteBitmapLookupTable     *bool   `gitconfig:"writebitmaplookuptable"`
	ReadReverseIndex           *bool   `gitconfig:"readreverseindex" gitconfigDefault:"true"`
	WriteReverseIndex          *bool   `gitconfig:"writereverseindex" gitconfigDefault:"true"`
}

// ProtocolConfig maps the protocol.* section.
type ProtocolConfig struct {
	Allow   string `gitconfig:"allow"`
	Version int    `gitconfig:"version" gitconfigDefault:"2"`
}

// ProtocolNameConfig maps a protocol.<name>.* subsection.
type ProtocolNameConfig struct {
	Allow string `gitconfig:"allow"`
}

// PullConfig maps the pull.* section.
type PullConfig struct {
	FF        string `gitconfig:"ff"`
	Rebase    string `gitconfig:"rebase"`
	Octopus   string `gitconfig:"octopus"`
	AutoStash *bool  `gitconfig:"autostash"`
	TwoHead   string `gitconfig:"twohead"`
}

// PushConfig maps the push.* section.
type PushConfig struct {
	AutoSetupRemote    *bool    `gitconfig:"autosetupremote"`
	Default            string   `gitconfig:"default" gitconfigDefault:"simple"`
	FollowTags         *bool    `gitconfig:"followtags"`
	GPGSign            string   `gitconfig:"gpgsign"`
	PushOption         []string `gitconfig:"pushoption,multivalue"`
	RecurseSubmodules  string   `gitconfig:"recursesubmodules"`
	UseForceIfIncludes *bool    `gitconfig:"useforceifincludes"`
	Negotiate          *bool    `gitconfig:"negotiate"`
	UseBitmaps         *bool    `gitconfig:"usebitmaps" gitconfigDefault:"true"`
}

// RebaseConfig maps the rebase.* section.
type RebaseConfig struct {
	Backend              string `gitconfig:"backend"`
	Stat                 *bool  `gitconfig:"stat"`
	AutoSquash           *bool  `gitconfig:"autosquash"`
	AutoStash            *bool  `gitconfig:"autostash"`
	UpdateRefs           *bool  `gitconfig:"updaterefs"`
	MissingCommitsCheck  string `gitconfig:"missingcommitscheck" gitconfigDefault:"ignore"`
	InstructionFormat    string `gitconfig:"instructionformat"`
	AbbreviateCommands   *bool  `gitconfig:"abbreviatecommands"`
	RescheduleFailedExec *bool  `gitconfig:"reschedulefailedexec"`
	ForkPoint            *bool  `gitconfig:"forkpoint" gitconfigDefault:"true"`
	RebaseMerges         string `gitconfig:"rebasemerges"`
	MaxLabelLength       int    `gitconfig:"maxlabellength"`
}

// ReceiveConfig maps the receive.* section.
type ReceiveConfig struct {
	AdvertiseAtomic      *bool    `gitconfig:"advertiseatomic" gitconfigDefault:"true"`
	AdvertisePushOptions *bool    `gitconfig:"advertisepushoptions"`
	AutoGC               *bool    `gitconfig:"autogc" gitconfigDefault:"true"`
	CertNonceSeed        string   `gitconfig:"certnonceseed"`
	CertNonceSlop        int      `gitconfig:"certnonceslop"`
	FsckObjects          *bool    `gitconfig:"fsckobjects"`
	KeepAlive            int      `gitconfig:"keepalive" gitconfigDefault:"5"`
	UnpackLimit          int      `gitconfig:"unpacklimit"`
	MaxInputSize         GitSize  `gitconfig:"maxinputsize"`
	DenyDeletes          *bool    `gitconfig:"denydeletes"`
	DenyDeleteCurrent    *bool    `gitconfig:"denydeletecurrent"`
	DenyCurrentBranch    string   `gitconfig:"denycurrentbranch" gitconfigDefault:"refuse"`
	DenyNonFastForwards  *bool    `gitconfig:"denynonfastforwards"`
	HideRefs             []string `gitconfig:"hiderefs,multivalue"`
	ProcReceiveRefs      []string `gitconfig:"procreceiverefs,multivalue"`
	UpdateServerInfo     *bool    `gitconfig:"updateserverinfo"`
	ShallowUpdate        *bool    `gitconfig:"shallowupdate"`
}

// RemoteConfig maps a remote.<name>.* subsection.
type RemoteConfig struct {
	URL                []string `gitconfig:"url,multivalue"`
	PushURL            []string `gitconfig:"pushurl,multivalue"`
	Proxy              string   `gitconfig:"proxy"`
	ProxyAuthMethod    string   `gitconfig:"proxyauthmethod"`
	Fetch              []string `gitconfig:"fetch,multivalue"`
	Push               []string `gitconfig:"push,multivalue"`
	Mirror             *bool    `gitconfig:"mirror"`
	SkipFetchAll       *bool    `gitconfig:"skipfetchall"`
	SkipDefaultUpdate  *bool    `gitconfig:"skipdefaultupdate"`
	ReceivePack        string   `gitconfig:"receivepack"`
	UploadPack         string   `gitconfig:"uploadpack"`
	TagOpt             string   `gitconfig:"tagopt"`
	VCS                string   `gitconfig:"vcs"`
	Prune              *bool    `gitconfig:"prune"`
	PruneTags          *bool    `gitconfig:"prunetags"`
	Promisor           *bool    `gitconfig:"promisor"`
	PartialCloneFilter string   `gitconfig:"partialclonefilter"`
	ServerOption       []string `gitconfig:"serveroption,multivalue"`
	FollowRemoteHEAD   string   `gitconfig:"followremotehead" gitconfigDefault:"create"`
}

// RepackConfig maps the repack.* section.
type RepackConfig struct {
	UseDeltaBaseOffset   *bool   `gitconfig:"usedeltabaseoffset" gitconfigDefault:"true"`
	PackKeptObjects      *bool   `gitconfig:"packkeptobjects"`
	UseDeltaIslands      *bool   `gitconfig:"usedeltaislands"`
	WriteBitmaps         *bool   `gitconfig:"writebitmaps"`
	UpdateServerInfo     *bool   `gitconfig:"updateserverinfo" gitconfigDefault:"true"`
	CruftWindow          int     `gitconfig:"cruftwindow"`
	CruftWindowMemory    GitSize `gitconfig:"cruftwindowmemory"`
	CruftDepth           int     `gitconfig:"cruftdepth"`
	CruftThreads         int     `gitconfig:"cruftthreads"`
	MIDXMustContainCruft *bool   `gitconfig:"midxmustcontaincruft" gitconfigDefault:"true"`
}

// RerereConfig maps the rerere.* section.
type RerereConfig struct {
	AutoUpdate *bool `gitconfig:"autoupdate"`
	Enabled    *bool `gitconfig:"enabled"`
}

// SafeConfig maps the safe.* section.
type SafeConfig struct {
	BareRepository string   `gitconfig:"barerepository" gitconfigDefault:"all"`
	Directory      []string `gitconfig:"directory,multivalue"`
}

// SequenceConfig maps the sequence.* section.
type SequenceConfig struct {
	Editor string `gitconfig:"editor"`
}

// SplitIndexConfig maps the splitIndex.* section.
type SplitIndexConfig struct {
	MaxPercentChange  int        `gitconfig:"maxpercentchange" gitconfigDefault:"20"`
	SharedIndexExpire ExpiryDate `gitconfig:"sharedindexexpire" gitconfigDefault:"2.weeks.ago"`
}

// SSHConfig maps the ssh.* section.
type SSHConfig struct {
	Variant string `gitconfig:"variant"`
}

// StashConfig maps the stash.* section.
type StashConfig struct {
	Index                *bool `gitconfig:"index"`
	ShowIncludeUntracked *bool `gitconfig:"showincludeuntracked"`
	ShowPatch            *bool `gitconfig:"showpatch"`
	ShowStat             *bool `gitconfig:"showstat" gitconfigDefault:"true"`
}

// StatusConfig maps the status.* section.
type StatusConfig struct {
	RelativePaths        *bool     `gitconfig:"relativepaths" gitconfigDefault:"true"`
	Short                *bool     `gitconfig:"short"`
	Branch               *bool     `gitconfig:"branch"`
	AheadBehind          *bool     `gitconfig:"aheadbehind" gitconfigDefault:"true"`
	DisplayCommentPrefix *bool     `gitconfig:"displaycommentprefix"`
	RenameLimit          int       `gitconfig:"renamelimit"`
	Renames              string    `gitconfig:"renames"`
	ShowStash            *bool     `gitconfig:"showstash"`
	ShowUntrackedFiles   string    `gitconfig:"showuntrackedfiles" gitconfigDefault:"normal"`
	SubmoduleSummary     BoolOrInt `gitconfig:"submodulesummary"`
}

// SubmoduleConfig maps a submodule.<name>.* subsection.
type SubmoduleConfig struct {
	URL                    string `gitconfig:"url"`
	Update                 string `gitconfig:"update"`
	Branch                 string `gitconfig:"branch"`
	FetchRecurseSubmodules string `gitconfig:"fetchrecursesubmodules"`
	Ignore                 string `gitconfig:"ignore"`
	Active                 *bool  `gitconfig:"active"`
}

// SubmoduleGlobals maps the submodule.* section-level variables.
type SubmoduleGlobals struct {
	Active                 []string `gitconfig:"active,multivalue"`
	Recurse                *bool    `gitconfig:"recurse"`
	PropagateBranches      *bool    `gitconfig:"propagatebranches"`
	FetchJobs              int      `gitconfig:"fetchjobs" gitconfigDefault:"1"`
	AlternateLocation      string   `gitconfig:"alternatelocation"`
	AlternateErrorStrategy string   `gitconfig:"alternateerrorstrategy" gitconfigDefault:"die"`
}

// TagConfig maps the tag.* section.
type TagConfig struct {
	ForceSignAnnotated *bool  `gitconfig:"forcesignannotated"`
	Sort               string `gitconfig:"sort"`
	GPGSign            *bool  `gitconfig:"gpgsign"`
}

// TrailerConfig maps the trailer.* section.
type TrailerConfig struct {
	Separators string `gitconfig:"separators"`
	Where      string `gitconfig:"where" gitconfigDefault:"end"`
	IfExists   string `gitconfig:"ifexists" gitconfigDefault:"addIfDifferentNeighbor"`
	IfMissing  string `gitconfig:"ifmissing" gitconfigDefault:"add"`
}

// TrailerKeyConfig maps a trailer.<keyAlias>.* subsection.
type TrailerKeyConfig struct {
	Key       string `gitconfig:"key"`
	Where     string `gitconfig:"where"`
	IfExists  string `gitconfig:"ifexists"`
	IfMissing string `gitconfig:"ifmissing"`
	Command   string `gitconfig:"command"`
	Cmd       string `gitconfig:"cmd"`
}

// TransferConfig maps the transfer.* section.
type TransferConfig struct {
	CredentialsInURL    string   `gitconfig:"credentialsinurl" gitconfigDefault:"allow"`
	FsckObjects         *bool    `gitconfig:"fsckobjects"`
	HideRefs            []string `gitconfig:"hiderefs,multivalue"`
	UnpackLimit         int      `gitconfig:"unpacklimit" gitconfigDefault:"100"`
	AdvertiseSID        *bool    `gitconfig:"advertisesid"`
	BundleURI           *bool    `gitconfig:"bundleuri"`
	AdvertiseObjectInfo *bool    `gitconfig:"advertiseobjectinfo"`
}

// UploadPackConfig maps the uploadpack.* section.
type UploadPackConfig struct {
	HideRefs                 []string `gitconfig:"hiderefs,multivalue"`
	AllowTipSHA1InWant       *bool    `gitconfig:"allowtipsha1inwant"`
	AllowReachableSHA1InWant *bool    `gitconfig:"allowreachablesha1inwant"`
	AllowAnySHA1InWant       *bool    `gitconfig:"allowanysha1inwant"`
	KeepAlive                int      `gitconfig:"keepalive" gitconfigDefault:"5"`
	PackObjectsHook          string   `gitconfig:"packobjectshook"`
	AllowFilter              *bool    `gitconfig:"allowfilter"`
}

// URLConfig maps a url.<base>.* subsection.
type URLConfig struct {
	InsteadOf     []string `gitconfig:"insteadof,multivalue"`
	PushInsteadOf []string `gitconfig:"pushinsteadof,multivalue"`
}

// UserConfig maps the user.* section.
type UserConfig struct {
	Name          string `gitconfig:"name"`
	Email         string `gitconfig:"email"`
	SigningKey    string `gitconfig:"signingkey"`
	UseConfigOnly *bool  `gitconfig:"useconfigonly"`
}

// AuthorConfig maps the author.* section.
type AuthorConfig struct {
	Name  string `gitconfig:"name"`
	Email string `gitconfig:"email"`
}

// CommitterConfig maps the committer.* section.
type CommitterConfig struct {
	Name  string `gitconfig:"name"`
	Email string `gitconfig:"email"`
}

// WorktreeConfig maps the worktree.* section.
type WorktreeConfig struct {
	GuessRemote      *bool `gitconfig:"guessremote"`
	UseRelativePaths *bool `gitconfig:"userelativepaths"`
}

// IncludeConfig maps the include.* section.
type IncludeConfig struct {
	Path []Pathname `gitconfig:"path,multivalue"`
}

// IncludeIfConfig maps an includeIf.<condition>.* subsection.
type IncludeIfConfig struct {
	Path []Pathname `gitconfig:"path,multivalue"`
}

// PagerConfig maps a pager.<cmd> subsection.
type PagerConfig struct {
	Cmd string `gitconfig:"cmd"`
}

// PrettyConfig maps a pretty.<name> subsection.
type PrettyConfig struct {
	Format string `gitconfig:"format"`
}

// RefTableConfig maps the reftable.* section.
type RefTableConfig struct {
	BlockSize       int   `gitconfig:"blocksize" gitconfigDefault:"4096"`
	RestartInterval int   `gitconfig:"restartinterval"`
	IndexObjects    *bool `gitconfig:"indexobjects" gitconfigDefault:"true"`
	GeometricFactor int   `gitconfig:"geometricfactor" gitconfigDefault:"2"`
	LockTimeout     int   `gitconfig:"locktimeout" gitconfigDefault:"100"`
}

// RemotesConfig maps a remotes.<group> subsection.
type RemotesConfig struct {
	Remotes []string `gitconfig:"remotes,multivalue"`
}

// Config is the top-level struct encompassing all Git configuration variables
// as documented in the git-config specification. It uses struct tags from
// the x/config package to drive marshaling and unmarshaling.
type Config struct {
	Core             CoreConfig                        `gitconfig:"core"`
	User             UserConfig                        `gitconfig:"user"`
	Author           AuthorConfig                      `gitconfig:"author"`
	Committer        CommitterConfig                   `gitconfig:"committer"`
	Advice           AdviceConfig                      `gitconfig:"advice"`
	Aliases          map[string]*AliasConfig           `gitconfig:"alias,subsection"`
	Branches         map[string]*BranchConfig          `gitconfig:"branch,subsection"`
	Browsers         map[string]*BrowserConfig         `gitconfig:"browser,subsection"`
	Checkout         CheckoutConfig                    `gitconfig:"checkout"`
	Clean            CleanConfig                       `gitconfig:"clean"`
	Clone            CloneConfig                       `gitconfig:"clone"`
	Color            ColorConfig                       `gitconfig:"color"`
	ColorBranch      *ColorBranchSlots                 `gitconfig:"color,subsection" gitconfigSub:"branch"`
	ColorDiff        *ColorDiffSlots                   `gitconfig:"color,subsection" gitconfigSub:"diff"`
	ColorDecorate    *ColorDecorateSlots               `gitconfig:"color,subsection" gitconfigSub:"decorate"`
	ColorGrep        *ColorGrepSlots                   `gitconfig:"color,subsection" gitconfigSub:"grep"`
	ColorInteractive *ColorInteractiveSlots            `gitconfig:"color,subsection" gitconfigSub:"interactive"`
	ColorPush        *ColorPushSlots                   `gitconfig:"color,subsection" gitconfigSub:"push"`
	ColorRemote      *ColorRemoteSlots                 `gitconfig:"color,subsection" gitconfigSub:"remote"`
	ColorStatus      *ColorStatusSlots                 `gitconfig:"color,subsection" gitconfigSub:"status"`
	ColorTransport   *ColorTransportSlots              `gitconfig:"color,subsection" gitconfigSub:"transport"`
	Column           ColumnConfig                      `gitconfig:"column"`
	Commit           CommitConfig                      `gitconfig:"commit"`
	CommitGraph      CommitGraphConfig                 `gitconfig:"commitgraph"`
	Credential       CredentialConfig                  `gitconfig:"credential"`
	Credentials      map[string]*CredentialURLConfig   `gitconfig:"credential,subsection"`
	Diff             DiffConfig                        `gitconfig:"diff"`
	DiffDrivers      map[string]*DiffDriverConfig      `gitconfig:"diff,subsection"`
	Difftools        map[string]*DifftoolConfig        `gitconfig:"difftool,subsection"`
	Extensions       ExtensionsConfig                  `gitconfig:"extensions"`
	Feature          FeatureConfig                     `gitconfig:"feature"`
	Fetch            FetchConfig                       `gitconfig:"fetch"`
	FilterDrivers    map[string]*FilterDriverConfig    `gitconfig:"filter,subsection"`
	Format           FormatConfig                      `gitconfig:"format"`
	Fsck             FsckConfig                        `gitconfig:"fsck"`
	Fsmonitor        FsmonitorConfig                   `gitconfig:"fsmonitor"`
	Gc               GcConfig                          `gitconfig:"gc"`
	GPG              GPGConfig                         `gitconfig:"gpg"`
	GPGSSH           *GPGSSHConfig                     `gitconfig:"gpg,subsection" gitconfigSub:"ssh"`
	GPGFormats       map[string]*GPGFormatConfig       `gitconfig:"gpg,subsection"`
	Grep             GrepConfig                        `gitconfig:"grep"`
	Help             HelpConfig                        `gitconfig:"help"`
	HTTP             HTTPConfig                        `gitconfig:"http"`
	HTTPURLs         map[string]*HTTPURLConfig         `gitconfig:"http,subsection"`
	I18n             I18nConfig                        `gitconfig:"i18n"`
	Index            IndexConfig                       `gitconfig:"index"`
	Init             InitConfig                        `gitconfig:"init"`
	Interactive      InteractiveConfig                 `gitconfig:"interactive"`
	Log              LogConfig                         `gitconfig:"log"`
	Maintenance      MaintenanceConfig                 `gitconfig:"maintenance"`
	MaintenanceTasks map[string]*MaintenanceTaskConfig `gitconfig:"maintenance,subsection"`
	Merge            MergeConfig                       `gitconfig:"merge"`
	MergeDrivers     map[string]*MergeDriverConfig     `gitconfig:"merge,subsection"`
	Mergetool        MergetoolGlobals                  `gitconfig:"mergetool"`
	MergetoolTools   map[string]*MergetoolConfig       `gitconfig:"mergetool,subsection"`
	Notes            NotesConfig                       `gitconfig:"notes"`
	NotesNames       map[string]*NotesNameConfig       `gitconfig:"notes,subsection"`
	Pack             PackConfig                        `gitconfig:"pack"`
	PagerCmds        map[string]*PagerConfig           `gitconfig:"pager,subsection"`
	PrettyNames      map[string]*PrettyConfig          `gitconfig:"pretty,subsection"`
	Protocol         ProtocolConfig                    `gitconfig:"protocol"`
	ProtocolNames    map[string]*ProtocolNameConfig    `gitconfig:"protocol,subsection"`
	Pull             PullConfig                        `gitconfig:"pull"`
	Push             PushConfig                        `gitconfig:"push"`
	Rebase           RebaseConfig                      `gitconfig:"rebase"`
	Receive          ReceiveConfig                     `gitconfig:"receive"`
	RefTable         RefTableConfig                    `gitconfig:"reftable"`
	RemoteNames      map[string]*RemoteConfig          `gitconfig:"remote,subsection"`
	Remotes          map[string]*RemotesConfig         `gitconfig:"remotes,subsection"`
	Repack           RepackConfig                      `gitconfig:"repack"`
	Rerere           RerereConfig                      `gitconfig:"rerere"`
	Safe             SafeConfig                        `gitconfig:"safe"`
	Sequence         SequenceConfig                    `gitconfig:"sequence"`
	SplitIndex       SplitIndexConfig                  `gitconfig:"splitindex"`
	SSH              SSHConfig                         `gitconfig:"ssh"`
	Stash            StashConfig                       `gitconfig:"stash"`
	Status           StatusConfig                      `gitconfig:"status"`
	SubmoduleGlobals SubmoduleGlobals                  `gitconfig:"submodule"`
	Submodules       map[string]*SubmoduleConfig       `gitconfig:"submodule,subsection"`
	Tag              TagConfig                         `gitconfig:"tag"`
	Trailer          TrailerConfig                     `gitconfig:"trailer"`
	Trailers         map[string]*TrailerKeyConfig      `gitconfig:"trailer,subsection"`
	Transfer         TransferConfig                    `gitconfig:"transfer"`
	UploadPack       UploadPackConfig                  `gitconfig:"uploadpack"`
	URLs             map[string]*URLConfig             `gitconfig:"url,subsection"`
	Worktree         WorktreeConfig                    `gitconfig:"worktree"`
	Include          IncludeConfig                     `gitconfig:"include"`
	Includes         map[string]*IncludeIfConfig       `gitconfig:"includeif,subsection"`
}
