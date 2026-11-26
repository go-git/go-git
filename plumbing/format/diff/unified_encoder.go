package diff

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/go-git/go-git/v6/plumbing"
	textdiff "github.com/go-git/go-git/v6/plumbing/diff"
)

// DefaultContextLines is the default number of context lines.
const DefaultContextLines = 3

// UnifiedEncoder encodes a unified diff into the provided Writer. It does not
// support similarity index for renames or sorting hash representations.
type UnifiedEncoder struct {
	io.Writer

	hunkOptions textdiff.HunkOptions

	// srcPrefix and dstPrefix are prepended to file paths.
	srcPrefix string
	dstPrefix string

	// color is the color configuration. The default is no color.
	color ColorConfig
}

// NewUnifiedEncoder returns a new UnifiedEncoder that writes to w.
func NewUnifiedEncoder(w io.Writer, contextLines int) *UnifiedEncoder {
	return &UnifiedEncoder{
		Writer:      w,
		srcPrefix:   "a/",
		dstPrefix:   "b/",
		hunkOptions: textdiff.HunkOptions{Context: contextLines},
	}
}

// SetHunkOptions replaces the context controls and returns e.
// The encoder copies options and does not modify them.
func (e *UnifiedEncoder) SetHunkOptions(options textdiff.HunkOptions) *UnifiedEncoder {
	e.hunkOptions = options
	return e
}

// SetColor sets e's color configuration and returns e.
func (e *UnifiedEncoder) SetColor(colorConfig ColorConfig) *UnifiedEncoder {
	e.color = colorConfig
	return e
}

// SetSrcPrefix sets e's srcPrefix and returns e.
func (e *UnifiedEncoder) SetSrcPrefix(prefix string) *UnifiedEncoder {
	e.srcPrefix = prefix
	return e
}

// SetDstPrefix sets e's dstPrefix and returns e.
func (e *UnifiedEncoder) SetDstPrefix(prefix string) *UnifiedEncoder {
	e.dstPrefix = prefix
	return e
}

// Encode encodes patch.
func (e *UnifiedEncoder) Encode(patch Patch) error {
	return e.EncodeContext(context.Background(), patch)
}

// EncodeContext encodes a patch with cancellation and backend error propagation.
// The context must be non-nil.
func (e *UnifiedEncoder) EncodeContext(ctx context.Context, patch Patch) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	sb := &strings.Builder{}
	if message := patch.Message(); message != "" {
		sb.WriteString(message)
		if !strings.HasSuffix(message, "\n") {
			sb.WriteByte('\n')
		}
	}

	for _, filePatch := range patch.FilePatches() {
		if err := ctx.Err(); err != nil {
			return err
		}
		result := filePatch.Diff()
		isBinary := filePatch.IsBinary()
		if !isBinary && result == nil {
			return errors.New("diff: text patch has no diff result")
		}
		if isBinary {
			e.writeFilePatchHeader(sb, filePatch, true)
			continue
		}
		headerWritten := false
		for hunk, err := range result.Hunks(ctx, e.hunkOptions) {
			if err != nil {
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if !headerWritten {
				e.writeFilePatchHeader(sb, filePatch, true)
				headerWritten = true
			}
			e.writeHunkHeader(sb, hunk)
			for _, line := range hunk.Lines {
				if err := ctx.Err(); err != nil {
					return err
				}
				if err := e.writeLine(sb, line); err != nil {
					return err
				}
			}
		}
		if !headerWritten {
			e.writeFilePatchHeader(sb, filePatch, false)
		}
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	n, err := io.WriteString(e.Writer, sb.String())
	if err == nil && n != sb.Len() {
		return io.ErrShortWrite
	}
	return err
}

func (e *UnifiedEncoder) writeFilePatchHeader(sb *strings.Builder, filePatch FilePatch, hasChanges bool) {
	from, to := filePatch.Files()
	if from == nil && to == nil {
		return
	}
	isBinary := filePatch.IsBinary()

	var lines []string
	switch {
	case from != nil && to != nil:
		if !isBinary && !hasChanges && from.Mode() == to.Mode() && from.Path() == to.Path() {
			return
		}
		hashEquals := from.Hash() == to.Hash()
		lines = append(lines,
			fmt.Sprintf("diff --git %s%s %s%s",
				e.srcPrefix, from.Path(), e.dstPrefix, to.Path()),
		)
		if from.Mode() != to.Mode() {
			lines = append(lines,
				fmt.Sprintf("old mode %o", from.Mode()),
				fmt.Sprintf("new mode %o", to.Mode()),
			)
		}
		if from.Path() != to.Path() {
			lines = append(lines,
				fmt.Sprintf("rename from %s", from.Path()),
				fmt.Sprintf("rename to %s", to.Path()),
			)
		}
		if from.Mode() != to.Mode() && !hashEquals {
			lines = append(lines,
				fmt.Sprintf("index %s..%s", from.Hash(), to.Hash()),
			)
		} else if !hashEquals {
			lines = append(lines,
				fmt.Sprintf("index %s..%s %o", from.Hash(), to.Hash(), from.Mode()),
			)
		}
		if !hashEquals && (isBinary || hasChanges) {
			lines = e.appendPathLines(lines, e.srcPrefix+from.Path(), e.dstPrefix+to.Path(), isBinary)
		}
	case from == nil:
		lines = append(lines,
			fmt.Sprintf("diff --git %s %s", e.srcPrefix+to.Path(), e.dstPrefix+to.Path()),
			fmt.Sprintf("new file mode %o", to.Mode()),
			fmt.Sprintf("index %s..%s", plumbing.ZeroHash, to.Hash()),
		)
		if isBinary || hasChanges {
			lines = e.appendPathLines(lines, "/dev/null", e.dstPrefix+to.Path(), isBinary)
		}
	case to == nil:
		lines = append(lines,
			fmt.Sprintf("diff --git %s %s", e.srcPrefix+from.Path(), e.dstPrefix+from.Path()),
			fmt.Sprintf("deleted file mode %o", from.Mode()),
			fmt.Sprintf("index %s..%s", from.Hash(), plumbing.ZeroHash),
		)
		if isBinary || hasChanges {
			lines = e.appendPathLines(lines, e.srcPrefix+from.Path(), "/dev/null", isBinary)
		}
	}

	sb.WriteString(e.color[Meta])
	sb.WriteString(lines[0])
	for _, line := range lines[1:] {
		sb.WriteByte('\n')
		sb.WriteString(line)
	}
	sb.WriteString(e.color.Reset(Meta))
	sb.WriteByte('\n')
}

func (e *UnifiedEncoder) appendPathLines(lines []string, fromPath, toPath string, isBinary bool) []string {
	if isBinary {
		return append(lines,
			fmt.Sprintf("Binary files %s and %s differ", fromPath, toPath),
		)
	}
	return append(lines,
		fmt.Sprintf("--- %s", fromPath),
		fmt.Sprintf("+++ %s", toPath),
	)
}

func (e *UnifiedEncoder) writeHunkHeader(sb *strings.Builder, h textdiff.Hunk) {
	sb.WriteString(e.color[Frag])
	sb.WriteString("@@ -")
	writeRange(sb, h.Before.Start, h.Before.End-h.Before.Start)
	sb.WriteString(" +")
	writeRange(sb, h.After.Start, h.After.End-h.After.Start)
	sb.WriteString(" @@")
	sb.WriteString(e.color.Reset(Frag))
	if h.Section != "" {
		sb.WriteByte(' ')
		sb.WriteString(e.color[Func])
		sb.WriteString(h.Section)
		sb.WriteString(e.color.Reset(Func))
	}
	sb.WriteByte('\n')
}

func writeRange(sb *strings.Builder, start, count int) {
	if count != 0 {
		start++
	}
	sb.WriteString(strconv.Itoa(start))
	if count != 1 {
		sb.WriteByte(',')
		sb.WriteString(strconv.Itoa(count))
	}
}

func (e *UnifiedEncoder) writeLine(sb *strings.Builder, line textdiff.Line) error {
	key := Context
	var prefix byte
	switch line.Operation {
	case textdiff.Equal:
		prefix = ' '
	case textdiff.Delete:
		prefix = '-'
		key = Old
	case textdiff.Insert:
		prefix = '+'
		key = New
	default:
		return fmt.Errorf("diff: invalid line operation %d", line.Operation)
	}
	content := line.Content
	end := len(content)
	if end > 0 && content[end-1] == '\n' {
		end--
	}
	if end > 0 && content[end-1] == '\r' {
		end--
	}
	sb.WriteString(e.color[key])
	sb.WriteByte(prefix)
	sb.Write(content[:end])
	sb.WriteString(e.color.Reset(key))
	sb.Write(content[end:])
	if len(content) == 0 || content[len(content)-1] != '\n' {
		sb.WriteByte('\n')
		sb.WriteString(e.color[Context])
		sb.WriteString("\\ No newline at end of file")
		sb.WriteString(e.color.Reset(Context))
		sb.WriteByte('\n')
	}
	return nil
}
