package pawnd

import (
	"bytes"
	"testing"
)

func Test_PrefixedWriter(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		inbuf  string
		output string
	}{
		{"Empty data", "", "", ""},
		{"Newline only", "\n", "", "AZ\n"},
		{"One line", "abc\n", "", "AabcZ\n"},
		{"One line no-newline", "abc", "", "Aabc"},
		{"One line in-buffer", "abc\n", "Apre", "ApreabcZ\n"},
		{"One line in-buffer no-newline", "abc", "Apre", "Apreabc"},
		{"Two lines", "abc\nf\n", "", "AabcZ\nAfZ\n"},
		{"Two lines in-buffer", "abc\nf\n", "Apre", "ApreabcZ\nAfZ\n"},
		{"Two lines, last without newline", "abc\nf", "", "AabcZ\nAf"},
		{"Three lines", "abc\nf\ng\n", "", "AabcZ\nAfZ\nAgZ\n"},
		{"Three lines in-buffer", "abc\nf\ng\n", "Apre", "ApreabcZ\nAfZ\nAgZ\n"},
		{"Three lines, last without newline", "abc\nf", "", "AabcZ\nAf"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := &bytes.Buffer{}
			p := &PrefixedWriter{
				Prefix: []byte("A"),
				Eol:    []byte("Z\n"),
				Out:    out,
				buf:    bytes.NewBufferString(tt.inbuf),
			}
			_, err := p.Write([]byte(tt.input))
			if err != nil {
				t.Errorf("prefixedwriter.Write() error = %v", err)
				return
			}
			p.buf.WriteTo(out)
			if out.String() != tt.output {
				t.Errorf("Unexpected output:\ndata:\ngot: [%v]\nexpected: [%v]",
					out.String(), tt.output,
				)
				return
			}
		})
	}
}
