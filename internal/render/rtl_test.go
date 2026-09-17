package render

import "testing"

func TestIsRTL(t *testing.T) {
	cases := map[string]bool{
		"":                           false,
		"hello world":                false,
		"سلام دنیا":                  true,
		"مرحبا بالعالم":              true,
		"שלום עולם":                  true,
		"good morning = صبح بخیر":    false, // tie goes to LTR only when fewer RTL letters
		"صبح بخیر (good)":            true,
		"1234 ...":                   false,
		"<b>سلام</b> <i>world</i> x": false,
	}
	for in, want := range cases {
		if got := IsRTL(in); got != want {
			t.Errorf("IsRTL(%q) = %v, want %v", in, got, want)
		}
	}
	if !IsRTLHTML(`<div class="x">این یک <b>آزمایش</b> است</div>`) {
		t.Error("IsRTLHTML should detect Persian inside markup")
	}
	if IsRTLHTML(`<div dir="rtl">plain english</div>`) {
		t.Error("attributes must not count as text")
	}
}
