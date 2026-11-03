package data

import "testing"

func TestPlainText(t *testing.T) {
	for _, test := range []struct {
		name, source, want string
	}{
		{"paragraphs", "<p>First paragraph.</p><p>Second paragraph.</p>", "First paragraph.\n\nSecond paragraph."},
		{"list", "<p>Steps:</p><ul><li>First <strong>step</strong></li><li>Second step</li></ul><p>Done.</p>", "Steps:\n\nFirst step\nSecond step\n\nDone."},
		{"list trailing text", "<ul><li>First</li><li>Second</li></ul>After", "First\nSecond\n\nAfter"},
		{"line breaks", "<p>First<br>Second<br>Third</p>", "First\nSecond\nThird"},
		{"inline punctuation", "<p>Hello <em>world</em>, read <a href='/'>this</a>!</p>", "Hello world, read this!"},
		{"inline word", "<p>un<strong>break</strong>able</p>", "unbreakable"},
		{"source formatting", "<p>One\n  <em>formatted</em>\n paragraph.</p>", "One formatted paragraph."},
		{"scripts and styles", "<p>Before<script>alert('bad')</script><style>p {color:red}</style> after</p>", "Before after"},
		{"entities", "<p>A &amp; B &lt; C &gt; D&nbsp;!</p>", "A & B < C > D !"},
		{"plain paragraphs", "  First line\nSecond line\n\n\nLast line  ", "First line\nSecond line\n\nLast line"},
		{"plain entities", "A &amp; B", "A & B"},
		{"literal comparison", "x < 3 and y > 2", "x < 3 and y > 2"},
		{"preformatted lines", "<pre>line one\nline two</pre>", "line one\nline two"},
		{"empty", " \n ", ""},
		{"hidden only", "<script>bad</script><style>bad</style>", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := plainText(test.source); got != test.want {
				t.Errorf("plainText() = %q, want %q", got, test.want)
			}
		})
	}
}
