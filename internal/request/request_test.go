package request

import "testing"

func TestParseTimestamp(t *testing.T) {
	tests := []struct {
		in      string
		seconds float64
		fromEnd bool
		ok      bool
	}{
		{"90", 90, false, true},
		{"300", 300, false, true}, // a leading component is unbounded
		{"1:30", 90, false, true},
		{"12:04:00", 43440, false, true},
		{"1:30.5", 90.5, false, true},
		{"-10", 10, true, true},
		{"-1:00", 60, true, true},
		{" 1:30 ", 90, false, true},
		{"", 0, false, false},
		{"1:2", 0, false, false},
		{"1:30:00:00", 0, false, false},
		{"abc", 0, false, false},
		{"1:3a", 0, false, false},
		{"1:60", 0, false, false},
		{"1:95", 0, false, false},
		{"1:60:00", 0, false, false},
		{"1:59:59", 7199, false, true},
	}
	for _, tt := range tests {
		seconds, fromEnd, ok := parseTimestamp(tt.in)
		if ok != tt.ok || (ok && (seconds != tt.seconds || fromEnd != tt.fromEnd)) {
			t.Errorf("parseTimestamp(%q) = %v, %v, %v; want %v, %v, %v",
				tt.in, seconds, fromEnd, ok, tt.seconds, tt.fromEnd, tt.ok)
		}
	}
}

func valid() Request {
	return Request{
		URL:       "https://www.youtube.com/watch?v=abc",
		Options:   DefaultOptions(),
		OutputDir: "/tmp",
	}
}

func fields(ps []Problem) []Field {
	var out []Field
	for _, p := range ps {
		out = append(out, p.Field)
	}
	return out
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Request)
		want   []Field
	}{
		{"ready", func(*Request) {}, nil},
		{"url with spaces around it", func(r *Request) { r.URL = "  http://example.com/v  " }, nil},
		{"empty url", func(r *Request) { r.URL = " " }, []Field{FieldURL}},
		{"not http", func(r *Request) { r.URL = "ftp://example.com/v" }, []Field{FieldURL}},
		{"no output", func(r *Request) { r.OutputDir = "" }, []Field{FieldOutput}},
		{"range with both ends", func(r *Request) { r.Range = &Range{"1:00", "2:00"} }, nil},
		{"range to the end", func(r *Request) { r.Range = &Range{Start: "1:00"} }, nil},
		{"range from the start", func(r *Request) { r.Range = &Range{End: "2:00"} }, nil},
		{"range relative to the end", func(r *Request) { r.Range = &Range{"5:00", "-10"} }, nil},
		{"empty range", func(r *Request) { r.Range = &Range{} }, []Field{FieldRange}},
		{"bad timestamp", func(r *Request) { r.Range = &Range{"1:2", ""} }, []Field{FieldRange}},
		{"inverted range", func(r *Request) { r.Range = &Range{"2:00", "1:00"} }, []Field{FieldRange}},
		{"empty span", func(r *Request) { r.Range = &Range{"1:00", "60"} }, []Field{FieldRange}},
		{"no pasted cookies", func(r *Request) {
			r.Auth = Auth{Mode: PastedCookies, Cookies: "# just a comment"}
		}, []Field{FieldCookies}},
		{"pasted cookies", func(r *Request) {
			r.Auth = Auth{Mode: PastedCookies, Cookies: ".youtube.com\tTRUE\t/\tTRUE\t0\tSID\tvalue"}
		}, nil},
		{"browser", func(r *Request) { r.Auth = Auth{Mode: FromBrowser, Browser: "safari"} }, nil},
		{"several problems", func(r *Request) { r.URL = ""; r.OutputDir = "" }, []Field{FieldURL, FieldOutput}},
	}
	for _, tt := range tests {
		r := valid()
		tt.change(&r)
		got := fields(r.Validate())
		if len(got) != len(tt.want) {
			t.Errorf("%s: problems %v, want %v", tt.name, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("%s: problems %v, want %v", tt.name, got, tt.want)
				break
			}
		}
	}
}

func TestProblemsHaveMessages(t *testing.T) {
	r := Request{Range: &Range{}, Auth: Auth{Mode: PastedCookies}}
	for _, p := range r.Validate() {
		if p.Message == "" {
			t.Errorf("problem on %s has no message", p.Field)
		}
	}
}

func TestAudioFormat(t *testing.T) {
	if !FLAC.Lossless() || !WAV.Lossless() || MP3.Lossless() {
		t.Error("only FLAC and WAV are lossless")
	}
	if WAV.HoldsCoverArt() || !M4A.HoldsCoverArt() {
		t.Error("every format but WAV holds cover art")
	}
}
