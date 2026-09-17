package dict

import (
	"reflect"
	"testing"
)

func TestIndex(t *testing.T) {
	words := []string{"Zebra", "apple", "Apple pie", "e-mail", "apple", "  banana  split "}
	ix := BuildIndex(words)
	if got := ix.Exact("APPLE"); !reflect.DeepEqual(got, []int32{1, 4}) {
		t.Errorf("exact apple = %v", got)
	}
	if got := ix.Exact("email"); !reflect.DeepEqual(got, []int32{3}) {
		t.Errorf("loose email = %v", got)
	}
	if got := ix.Exact("banana split"); !reflect.DeepEqual(got, []int32{5}) {
		t.Errorf("normalised = %v", got)
	}
	if got := ix.Exact("nothing"); got != nil {
		t.Errorf("missing = %v", got)
	}
	p := ix.Prefix("app", 10)
	if len(p) != 2 { // "apple" (deduplicated) and "apple pie"
		t.Errorf("prefix = %v", p)
	}
	if got := ix.Prefix("", 10); got != nil {
		t.Errorf("empty prefix = %v", got)
	}
}

func TestNormalize(t *testing.T) {
	if NormalizeKey("  Hello   World ") != "hello world" {
		t.Error("normalize")
	}
	if LooseKey("Don't-Stop 1") != "dontstop1" {
		t.Error("loose")
	}
}
