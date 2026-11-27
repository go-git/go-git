package sideband

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSanitize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		message string
		keepCR  bool
		want    string
	}{
		{"empty", "", true, ""},
		{"plain text", "Counting objects: 3, done.\n", true, "Counting objects: 3, done.\n"},
		{"tab and line feed", "a\tb\n", false, "a\tb\n"},
		{"carriage return kept", "50%\r100%\n", true, "50%\r100%\n"},
		{"carriage return masked", "50%\r100%\n", false, "50%^M100%\n"},
		{"utf-8", "Récupération 完了\n", false, "Récupération 完了\n"},
		{"bytes from 0x80 up", "\x80\x9b\x9f\xff", false, "\x80\x9b\x9f\xff"},
		{"nul", "a\x00b", true, "a^@b"},
		{"bell and backspace", "\a\b", true, "^G^H"},
		{"vertical tab and form feed", "\v\f", true, "^K^L"},
		{"highest c0 controls", "\x1c\x1d\x1e\x1f", true, "^\\^]^^^_"},
		{"delete", "a\x7fb", true, "a^?b"},
		{"caret in text", "a^[b", true, "a^[b"},
		{"color", "\x1b[31mred\x1b[0m", false, "\x1b[31mred\x1b[0m"},
		{"color without parameters", "\x1b[m", false, "\x1b[m"},
		{"color with many parameters", "\x1b[1;4;38;5;196m", false, "\x1b[1;4;38;5;196m"},
		{"color with colons", "\x1b[38:2::255:0:0m", false, "\x1b[38:2::255:0:0m"},
		{"color with only separators", "\x1b[;;m", false, "\x1b[;;m"},
		{"two colors in a row", "\x1b[1m\x1b[31m", false, "\x1b[1m\x1b[31m"},
		{"escape alone", "\x1b", true, "^["},
		{"escape at end", "done\x1b", true, "done^["},
		{"escape and bracket at end", "done\x1b[", true, "done^[["},
		{"color cut off", "\x1b[31", true, "^[[31"},
		{"color cut off before a later m", "\x1b[31\nm", true, "^[[31\nm"},
		{"escape before a color", "\x1b\x1b[31m", true, "^[\x1b[31m"},
		{"cursor up", "\x1b[3A", true, "^[[3A"},
		{"erase display", "\x1b[2J", true, "^[[2J"},
		{"cursor position", "\x1b[10;20H", true, "^[[10;20H"},
		{"private mode", "\x1b[?25l", true, "^[[?25l"},
		{"space before final byte", "\x1b[31 m", true, "^[[31 m"},
		{"upper case m", "\x1b[31M", true, "^[[31M"},
		{"foreign parameter", "\x1b[31;xm", true, "^[[31;xm"},
		{"window title", "\x1b]0;title\a", true, "^[]0;title^G"},
		{"window title ended by string terminator", "\x1b]2;title\x1b\\", true, "^[]2;title^[\\"},
		{"device control string", "\x1bP1$r\x1b\\", true, "^[P1$r^[\\"},
		{"reset", "\x1bc", true, "^[c"},
		{"masking between kept text", "a\x01b\x1b[1mc\x02d", true, "a^Ab\x1b[1mc^Bd"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := sanitize([]byte(tc.message), tc.keepCR)
			assert.Equal(t, tc.want, string(got))
		})
	}
}

func TestSanitizeReturnsCleanMessageUncopied(t *testing.T) {
	t.Parallel()

	message := []byte("remote: \x1b[32mok\x1b[m\tdone\r\n")
	got := sanitize(message, true)
	assert.Same(t, &message[0], &got[0])
	assert.Len(t, got, len(message))

	masked := sanitize(message, false)
	assert.Equal(t, "remote: \x1b[32mok\x1b[m\tdone^M\n", string(masked))
	assert.Equal(t, "remote: \x1b[32mok\x1b[m\tdone\r\n", string(message), "the input is left as it was")
}

func TestColorSequenceLen(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  int
	}{
		{"", 0},
		{"\x1b", 0},
		{"\x1b[", 0},
		{"\x1b[m", 3},
		{"\x1b[0m", 4},
		{"\x1b[1;31mtext", 7},
		{"\x1b[38:2::255:0:0m", 16},
		{"\x1b[;:;m", 6},
		{"\x1b[31", 0},
		{"\x1b[31;", 0},
		{"\x1b[31x", 0},
		{"\x1b[31;xm", 0},
		{"\x1b[31 m", 0},
		{"\x1b[2J", 0},
		{"\x1b]0;m", 0},
		{"\x1bm", 0},
		{"[31m", 0},
		{"x\x1b[31m", 0},
	}

	for _, tc := range tests {
		assert.Equal(t, tc.want, colorSequenceLen([]byte(tc.input)), "%q", tc.input)
	}
}
